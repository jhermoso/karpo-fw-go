# Contexto Security: evaluación y port a Go

Evaluación del contexto limitado **Security** de Karpo C# antes de portarlo a `contexts/security`.
Sigue el método de [PARTIES.md](PARTIES.md) y continúa [AUTORIZACION.md](AUTORIZACION.md), que ya
evaluó el **contrato** de autorización (lo que un contexto necesita saber de quien llama). Aquí se
evalúa **el contexto que guarda esos datos**: usuarios, roles, permisos, acceso por organización,
sesiones e inicio de sesión.

> **Estado:** las siete decisiones de la sección 7 fueron **aprobadas por Javier el 2026-10-04**.
> La implementación está en `contexts/security`; lo ejecutado y lo no ejecutado, en la sección 8.

## 1. Punto de partida en C#

Security no tiene código propio de rebanada: los proyectos `ErpKernel.Security.*` solo enlazan los
ficheros del monolito.

| Medida | Valor |
|---|---|
| Ficheros en `ErpKernel.*/Subdominios/Security` | 103: dominio 27, contratos de dominio 8, aplicación 13, contratos de aplicación 27, infraestructura 28 |
| Ficheros de borde | 13 en `ErpDetail.Distribution.Publisher/Security`, 9 en la publicadora `Security.Distribution.Publisher` |
| Entidades y tablas | 7 y 7, en el esquema `security`; seis llevan una columna `Discriminator` con un único valor posible |
| Rutas HTTP | 49: 7 en `/api/auth`, 11 de usuarios, 7 de roles, 2 de permisos, 1 de organizaciones accesibles, 1 de contexto interno y 20 en `/api/admin` (18 son de administración de base de datos e importación, no de Security) |
| Catálogo sembrado | 5 roles, 273 permisos (68 recursos × 4 acciones + el comodín `*.*.*`) y 535 parejas rol-permiso |
| Pruebas | 159 de integración HTTP y 60 unitarias |
| Dependencias de Parties | 3 puertos: `IPartyDirectory`, `IInternalOrganizationCatalog`, `ISelfRegistrationPartiesGateway` |

Las siete tablas:

| Tabla | Contenido | Columnas que nadie lee |
|---|---|---|
| `security_user` | party, nombre de usuario, hash + sal + algoritmo, último acceso, intentos fallidos, bloqueo, «debe cambiar la contraseña» | — |
| `security_role` | nombre, descripción, nivel jerárquico, «rol de sistema» | `HierarchyLevel`, `IsSystemRole` (nunca vale `true`) |
| `security_permission` | código y sus tres segmentos, descripción | — |
| `security_role_permission` | rol + permiso (único) | `IsActive` |
| `security_user_role` | usuario + rol (**sin** índice único) | `AssignedBy`, `AssignmentContext`, `InitDate`, `ExpirationDate` |
| `security_organization_access` | usuario + organización (**sin** índice único), nivel como texto libre, «incluye filiales» | `IncludeSubsidiaries` solo se transporta |
| `security_refresh_token` | hash del token, emisión, caducidad, revocación | `DeviceInfo`, `IpAddress`, `RevokedBy` |

Lo que **no** existe en C#, aunque el dominio lo sugiere: desbloquear un usuario, restablecer una
contraseña, desactivar un usuario y cambiar el nivel de un acceso. Los usuarios, los roles y los
accesos solo se pueden crear y **borrar físicamente**.

## 2. Defectos encontrados

Todos comprobados leyendo el código; las rutas son relativas a `020-Back/020-Source`. Abreviaturas:
`UA` = `ErpKernel.Application/Subdominios/Security/Users/SecurityUserApplicationService.cs`,
`RA` = `…/Roles/SecurityRoleApplicationService.cs`,
`AUTH` = `ErpKernel.Infrastructure/Subdominios/Security/Providers/VanillaAuthenticationProvider.cs`,
`SEED` = `Security.Distribution.Publisher/Costuras/SembradorDeSeguridadAlArrancar.cs`.

**Escalada de privilegios y administración delegada**

- **Un administrador de organización puede borrar al administrador global.** Conceder acceso a
  una organización no comprueba a quién (`UA:336-364`), y un usuario «está en mi ámbito» en cuanto
  tiene un acceso a una de mis organizaciones. Basta concederle la mía para poder editarlo,
  borrarlo o quitarle roles (`UA:240-255`, `UA:298-322`).
- **No se protege al último administrador global** ni se impide actuar sobre uno mismo al editar
  o borrar (`UA:220-255`); el rol `GlobalSuperAdmin` se puede borrar (`RA:136-143`).
- **El bloqueo del rol de administrador global es por nombre** (`UA:267`): un rol a medida con el
  comodín `*.*.*` lo puede asignar cualquiera con `Security.User.Update`. `HierarchyLevel` no se
  compara nunca.
- **Para administrar usuarios basta un acceso de solo lectura**: se comprueba el ámbito de lectura,
  no el nivel (`UA:228`, `246`, `279`, `307`).
- **La protección de roles falla abierta sin contexto** (`RA:77-78`: `!scope.HasScope` deja pasar),
  y `OrganizationAdmin` se siembra con todos los `Security.Role.*`.
- **Retirar un acceso ignora el usuario de la ruta** (`UA:366-387`): se borra la fila por su Id,
  sea de quien sea.

**Inicio de sesión, sesiones y contraseñas**

- **PBKDF2-SHA256 con 10 000 iteraciones** y comparación de cadenas no constante
  (`Providers/Pbkdf2PasswordHasher.cs:15`, `:41`). La recomendación actual de OWASP para ese
  algoritmo es 600 000.
- **Sin política de contraseñas**: ni al crear el usuario ni al cambiarla (`UA:198-218`,
  `Auth/AuthenticationApplicationService.cs:111-126`).
- **Cambiar la contraseña no cierra las sesiones** ni apaga «debe cambiar la contraseña», que
  además no se exige en ningún sitio. Ningún caso de uso revoca tokens de renovación: solo el
  cierre de sesión y la propia renovación (`AUTH:173`, `:256`).
- **La renovación no detecta la reutilización**: presentar un token ya rotado solo devuelve 401; la
  sesión robada sigue viva. Tampoco mira si el usuario está bloqueado (`AUTH:169`).
- **El token de acceso lleva los roles, los permisos y el ámbito** (`AUTH:116-123`): hasta 272
  códigos para un `OrganizationAdmin`. El resolutor no los usa (contrato v1, D7), pero el
  resolutor heredado sí lee el rol del token (`UserOrganizationAccessResolver.cs:70`): un
  administrador degradado conserva la vista global hasta que caduca.
- **Un usuario desconocido responde sin calcular el hash** (`AUTH:54-55`): el tiempo de respuesta
  delata qué nombres existen.
- **Contraseñas por defecto en el código**: `"123"` en `AdminPasswordInitializer.cs:21` y en
  `SecuritySliceBootstrapperRegistration.cs:178`; contraseña de base de datos en
  `Security.Distribution.Publisher/appsettings.json:39`.

**Modelo y validación**

- **Ningún `Validate()` se ejecuta.** Los dos validadores (`SecurityUserValidator`,
  `SecurityRoleValidator`) no tienen llamadas, y cinco entidades devuelven éxito siempre.
- **El nivel de acceso es un texto libre** (`SecurityOrganizationAccess.cs:32-36`); un valor
  desconocido se convierte en `Restricted` sin avisar.
- **Roles y accesos duplicados**: sin índice único ni comprobación (`UA:285-295`, `355-363`);
  quitar un rol borra solo una de las filas.
- **La vigencia del rol no se aplica**: `ExpirationDate` se guarda (`UA:291`) y nadie la lee; un
  rol «caducado» sigue concediendo.
- **Nombre de usuario**: índice único sensible a mayúsculas y búsquedas con `UPPER()`: `Admin` y
  `admin` pueden coexistir. Renombrar no comprueba duplicados (`UA:231`).
- **Nada es atómico**: cada paso es su propio `SaveChanges`. Crear un usuario como administrador de
  organización lo guarda y después falla al releerlo, porque el usuario recién creado aún no está
  en su ámbito y la relectura devuelve nulo (`UA:217`, `SecurityUserEndpoints.cs:66`).
- **Eventos sin destinatario**: seis eventos de dominio, ningún manejador; dos no se emiten nunca.

**Siembra**

- **La siembra deshace lo que el administrador cambia**: vuelve a insertar las parejas rol-permiso
  que falten (`SEED:344-354`), reactiva permisos desactivados (`SEED:298-302`) y devuelve
  `StandardUser` a quien se quedó sin roles (`SEED:473-502`).
- **Si el nombre del usuario de arranque ya existe, se le da el rol de administrador global**
  conservando su contraseña (`SEED:408-425`).
- **Dependencia circular de arranque**: Security necesita que Parties cree una party técnica, y
  Parties necesita a Security para autorizar esa llamada; se resolvió con reintentos de hasta 155 s.

**Auto-registro** (`SelfRegistration/SelfRegistrationApplicationService.cs`): un anónimo recibe
`OrganizationAdmin` con los 272 permisos (`:179-180`); un nombre de usuario ya usado lanza un
conflicto con el nombre en el mensaje, que el endpoint no convierte en la respuesta genérica
(`:73-78`), y permite enumerarlos; y la empresa se crea antes que el usuario, así que un fallo
posterior la deja huérfana (`:171-180`).

## 3. C# frente a Go

| Tema | C# | Go (propuesta) |
|---|---|---|
| Agregados | 7 entidades sueltas, cada una con su «agregado» envoltorio y su repositorio | **`User`** (con sus roles, sus accesos a organizaciones y sus identidades como hijos), **`Role`** (con sus permisos), **`Session`** y **`PermissionEntry`** (una entrada del catálogo, cuya identidad de almacenamiento se deriva del código) |
| Usuario | Se borra físicamente, en cascada | **Nunca se borra: se desactiva.** Nombre único sin distinguir mayúsculas. Casos de uso que faltaban: desactivar, reactivar, desbloquear, restablecer contraseña |
| Formas de autenticarse | Solo usuario y contraseña; el resolutor exige `username` y `partyId` **en el token** | Un usuario tiene **credenciales**: contraseña (opcional) e **identidades externas** (emisor + sujeto). Ver decisión 1 |
| Token de acceso | HS256 con roles, permisos y ámbito dentro | Solo identidad (`sub`, `username`, `partyId`). Security emite a través de un puerto `TokenIssuer`; no importa `jwtauth` |
| Sesiones | Token de renovación sin detección de reutilización; nunca se revoca | `Session` con rotación y **familia**: reutilizar un token rotado cierra toda la familia. Se cierran todas al cambiar o restablecer la contraseña, al desactivar y al bloquear |
| Contraseña | PBKDF2 10 000, sin política | Puerto `PasswordHasher`, hash autodescriptivo. Ver decisión 3 |
| Bloqueo | 5 fallos, 15 minutos | Igual, con el reloj del dominio; el hash se calcula también para usuarios inexistentes |
| Rol | `HierarchyLevel` decorativo, `IsSystemRole` siempre falso | Sin nivel. Los cinco roles sembrados (**mismos GUID**) son de sistema de verdad: no se renombran ni se borran |
| Permiso | Fila con GUID derivado del orden de declaración | **Identificado por su código**. Cada contexto declara los suyos. Ver decisión 4 |
| Rol ↔ permiso, usuario ↔ rol | Tablas de unión con Id propio, duplicados posibles, vigencia sin aplicar | Hijos del agregado, únicos por construcción y por índice. Sin vigencia |
| Acceso por organización | Nivel como texto, duplicados posibles, solo alta y baja | Un acceso por usuario y organización, nivel `authz.AccessLevel` validado, **cambio de nivel**. `IncludeSubsidiaries` se conserva porque viaja en el contrato v1 (sigue sin ampliar nada, P2) |
| Administración delegada (P3) | Ámbito de lectura; ver defectos | Regla explícita. Ver decisión 5 |
| Directorio | `SecurityAuthorizationContextResolver` consulta cinco tablas | `application.Directory` implementa **`authz.Directory`** sobre los repositorios; el resolutor genérico del framework no cambia |
| Permisos de las rutas | Política en el endpoint | `pipeline.RequirePermission` en cada caso de uso |
| Atomicidad y eventos | Varios `SaveChanges`; eventos sin destinatario | Una unidad de trabajo por caso de uso; eventos en el outbox y lenguaje publicado v1 |
| Siembra | Tres servicios al arrancar que pisan cambios | Migraciones con semillas (roles y permisos propios) + sincronización del catálogo. Ver decisión 4 |
| Principales de servicio | Lista en configuración | Igual: ya existen en `authorization.Options`. Ver decisión 6 |

### Diseño

```text
contexts/security/
├── domain/          # User (+OrganizationAccess, ExternalIdentity, bloqueo), Role, Session,
│                    # PermissionEntry y Catalog, Administrator (las tres reglas), eventos, especificaciones
├── contracts/       # lenguaje publicado v1, Users (directorio), TokenVerifier
├── application/     # casos de uso con permisos, Directory (authz.Directory), Authenticator externo,
│                    # Catalog.Sync, Bootstrap, puertos PasswordHasher, TokenIssuer, PartyDirectory…
├── infrastructure/  # esquema sec_* de 5 motores con semillas, mapeos, PBKDF2, adaptador de Parties,
│                    # securityconformance (batería de conformidad de los mapeos)
├── distribution/    # /api/auth/…, /api/security/users|roles|permissions; HS256Issuer
└── module.go        # composición: Directory, Authenticator(verifier), SyncCatalog, BootstrapFromEnv
```

Lo que hace un host, y nada más:

```go
sec := security.Compose(sw, security.WithTokenIssuer(sdist.HS256Issuer{JWT: jwt}),
	security.WithParties(sinfra.PartiesDirectory{Directory: pm.Directory, Organizations: pm.Organizations}))
authn := authorization.Authenticators(jwt, sec.Authenticator(oidcVerifier)) // varias formas de autenticarse
resolver := authorization.NewResolver(sec.Directory, authorization.Options{}) // el directorio real
sec.HTTP.RegisterPublicRoutes(mux)                                              // login, refresh, logout
mux.Handle("/", distribution.Chain(protected, distribution.Authorize(authn, resolver)))
// al arrancar, tras migrar:
sec.SyncCatalog(ctx, slices.Concat(papp.Permissions(), bapp.Permissions() /* … */)...)
sec.BootstrapFromEnv(ctx)
```

Aportaciones al framework: `authorization.Authenticators` (combina autenticadores: el primero que
reconoce las credenciales gana, y una caída del origen nunca se disfraza de token inválido) y una
función `Permissions()` en la capa de aplicación de cada uno de los diecisiete contextos de negocio.

Tablas: `sec_users` (+ `sec_user_roles`, `sec_user_accesses`, `sec_user_identities`), `sec_roles`
(+ `sec_role_permissions`), `sec_permissions`, `sec_sessions`, más las bandejas de salida y la
auditoría.

```text
Authorization: Bearer …
   ├─ token propio (HS256)      ─► jwtauth.HS256            ─┐
   └─ token del proveedor (OIDC) ─► security.Authenticator   ─┤─► authz.Principal (usuario de Security)
        verifica (TokenVerifier) y busca (emisor, sujeto)    ─┘        │
                                                                       ▼
                      authorization.Resolver  ─►  authz.Directory = security.Module.Directory
                                                  (usuario activo, no bloqueado, roles → permisos, accesos)
```

**Permisos del propio contexto** (ocho, con la convención `Read`/`Update` de los demás contextos de
Go):

| Código | Qué permite |
|---|---|
| `Security.User.Read` | Ver usuarios, sus roles, accesos e identidades |
| `Security.User.Create` | Dar de alta un usuario |
| `Security.User.Update` | Renombrar, desactivar, reactivar, desbloquear, restablecer contraseña, enlazar identidades |
| `Security.UserRole.Assign` | Asignar y retirar roles |
| `Security.OrganizationAccess.Read` / `.Update` | Ver / conceder, cambiar y retirar accesos |
| `Security.Role.Read` / `.Update` | Ver / crear, editar y borrar roles y sus permisos (solo administrador global) |

El catálogo se lee con `Security.Role.Read`. No hay `Delete`: los usuarios no se borran.

**La regla de ámbito**, con nombre: un usuario **no pertenece a una organización**; tiene accesos.
Por eso Security no usa `authz.ScopeSpec` sobre un campo «propietario», sino dos especificaciones
propias que se traducen a SQL como las demás: `VisibleTo(organizaciones)` para leer (el usuario
tiene un acceso a alguna de las mías) y `AdministrableBy(organizaciones con Full)` para administrar
(todos sus accesos son a organizaciones donde tengo `Full`; decisión 5). Fuera de la vista, 404
uniforme; visible pero no administrable, 403.

**Rutas** (26 frente a 49):

- `POST /api/auth/login`, `/refresh` y `/logout` son públicas y se montan fuera de `Authorize`;
  `POST /api/auth/change-password` exige estar autenticado y ningún permiso;
- `GET /api/auth/context`: el contexto resuelto de quien llama (permisos, accesos, versión de
  política), con la forma de `authz.Resolution`. Sustituye a `/my-organizations`, a
  `/my-accessible-organizations` y al endpoint interno de C#, y es la base del futuro modo `Http`;
- `GET|POST /api/security/users`, `GET /api/security/users/{id}`, y sobre un usuario:
  `PUT …/username`, `POST …/deactivate`, `…/activate`, `…/unlock`, `…/reset-password`,
  `PUT|DELETE …/roles/{role}`, `PUT|DELETE …/organizations/{org}`, `POST|DELETE …/identities`;
- `GET|POST /api/security/roles`, `GET|PUT|DELETE /api/security/roles/{id}`,
  `PUT /api/security/roles/{id}/permissions`;
- `GET /api/security/permissions`.

## 4. Piezas que se retiran

| Pieza | Motivo |
|---|---|
| `HierarchyLevel` | Ninguna decisión lo lee. Lo sustituye la regla «no se concede lo que no se tiene» |
| `AssignedBy`, `AssignmentContext`, `InitDate`, `ExpirationDate` de la asignación de rol | No se leen; quién asignó y cuándo queda en la auditoría. La vigencia de roles se añadirá cuando un proceso la necesite |
| `DeviceInfo`, `IpAddress`, `RevokedBy` del token de renovación | Nunca se escriben |
| `Discriminator`, `IsActive` de rol-permiso, GUID de las tablas de unión | Artefactos de EF |
| `SecurityUserValidator`, `SecurityRoleValidator`, `SecurityUserSpecifications`, los cinco agregados envoltorio | Sin llamadas. Las invariantes van en los constructores |
| Roles, permisos y ámbito dentro del token | El contrato v1 ya prohíbe usarlos como fuente (D7) |
| `UserOrganizationAccessResolver`, `OrganizationScopeProvider`, `JwtCurrentActorResolver` | Ya sustituidos en [AUTORIZACION.md](AUTORIZACION.md) |
| Las 18 rutas de `/api/admin` de base de datos e importación | No son de Security; el esquema lo gestiona `sqlrepo.Migrator` |
| Auto-registro, usuarios por defecto por organización, `AdminPasswordInitializer` | Ver decisión 7 |

## 5. Lenguaje publicado (v1)

Origen `security`. Ningún evento lleva contraseñas, hashes ni tokens.

| Evento | Datos | Para quién |
|---|---|---|
| `security.user-registered.v1` | usuario, nombre, party | Directorios de nombres, auditoría |
| `security.user-deactivated.v1`, `security.user-reactivated.v1` | usuario | Quien mantenga sesiones o tareas de ese usuario |
| `security.user-locked.v1` | usuario, hasta cuándo | Alertas |
| `security.user-password-changed.v1` | usuario, si fue un restablecimiento | Avisos al titular |
| `security.user-role-assigned.v1`, `security.user-role-revoked.v1` | usuario, rol | Auditoría de cumplimiento |
| `security.organization-access-granted.v1`, `-changed.v1`, `-revoked.v1` | usuario, organización, nivel | Cachés por organización |
| `security.identity-linked.v1`, `security.identity-unlinked.v1` | usuario, emisor | Auditoría |
| `security.role-defined.v1`, `security.role-permissions-changed.v1`, `security.role-retired.v1` | rol, códigos | Auditoría |

Los inicios de sesión correctos y fallidos son eventos de dominio que quedan en la auditoría; no
se publican (mucho volumen y ningún contexto reacciona a ellos).

## 6. Puertos

**Ofrece**

| Puerto | Qué hace |
|---|---|
| `authz.Directory` (`Module.Directory`) | La foto de seguridad de un sujeto, leída en cada petición (D7). Sustituye a `authorization.MemoryDirectory` |
| `authz.Authenticator` (`Module.Authenticator(verifier)`) | Convierte un token de un proveedor externo en el usuario de Security enlazado |
| `contracts.Users` | `Resolve(ids)` y `ByParty(ids)`: nombre de usuario, party y estado, por lotes |
| `Module.SyncCatalog(ctx, declaraciones…)` | Recibe los permisos que declara cada contexto |
| `GET /api/auth/context` | El contexto resuelto, por HTTP |

**Necesita** (opcionales en la composición; sin ellos no se valida la referencia)

| Puerto | Lo cumple | Para qué |
|---|---|---|
| `PartyDirectory` | `contracts.Directory` de Parties | Que la party del usuario exista, y su nombre en las respuestas |
| `InternalOrganizations` | `contracts.InternalOrganizationCatalog` de Parties | Que solo se concedan accesos a organizaciones internas |
| `PasswordHasher`, `TokenIssuer`, `contracts.TokenVerifier` | infraestructura / punto de composición | Hash, emisión del token propio, verificación del token externo |

## 7. Decisiones (aprobadas por Javier el 2026-10-04)

1. **Identidad externa: un usuario tiene identidades (emisor + sujeto), y la contraseña es
   opcional.** El enlace es una fila hija del usuario, única en toda la instalación. Un token
   válido de un emisor conocido cuyo sujeto no está enlazado **no entra** (401): Security no crea
   usuarios por su cuenta; el alta la hace quien corresponda (una invitación, o el servicio de
   Theros) con los casos de uso `RegisterUser` y `LinkIdentity`. En este port se entrega el puerto
   `TokenVerifier`, el autenticador y sus pruebas con un verificador de prueba; el verificador
   real (OIDC con clave pública) se escribe cuando G-43 elija proveedor.
   *Ejemplo:* Ana entra en Theros con el proveedor de identidad. Su token dice
   `iss = https://id.ejemplo`, `sub = 8f2c…`. Security busca esa pareja, encuentra a `ana.garcia`
   y desde ahí todo es igual que si hubiera entrado con contraseña: mismos roles, mismos accesos.
   Si Ana solo usa el proveedor, su usuario no tiene contraseña y `/login` nunca la acepta.
   *Recomiendo: sí.*

2. **El token propio solo dice quién es; los permisos se piden.** El token de acceso lleva `sub`,
   `username` y `partyId`, y dura 15 minutos; la sesión se renueva con rotación y, si alguien
   presenta un token ya rotado, se cierran todas las sesiones de esa familia. Consecuencia para el
   front de Angular: deja de leer roles y permisos del token y llama a `GET /api/auth/context`.
   *Ejemplo:* a Luis le quitan el rol a las 10:00. Hoy su token sigue diciendo «OrganizationAdmin»
   hasta las 10:30 y el menú lo sigue mostrando; con la propuesta, la siguiente petición ya
   responde 403 y el contexto ya no trae el permiso.
   *Recomiendo: sí.*

3. **Contraseñas: PBKDF2-HMAC-SHA256 con 600 000 iteraciones, de la biblioteca estándar, con el
   hash autodescriptivo.** El hash guarda algoritmo y coste
   (`pbkdf2-sha256$600000$sal$hash`), así que subir el coste o cambiar de algoritmo no necesita
   migración: se recalcula en el siguiente inicio de sesión correcto. Eso incluye los hashes de C#
   (10 000 iteraciones), que se podrían importar y se actualizarían solos. Política: mínimo 12
   caracteres, máximo 128, distinta del nombre de usuario, sin reglas de composición.
   La alternativa es **Argon2id**, que resiste mejor el ataque con GPU y es la primera
   recomendación de OWASP, pero no está en la biblioteca estándar: añade la dependencia
   `golang.org/x/crypto` y unos 19 MB de memoria por cada inicio de sesión simultáneo.
   *Recomiendo PBKDF2 ahora (sin dependencias, como el resto del framework) y dejar Argon2id a un
   cambio de una línea en la composición si lo prefieres.*

4. **El catálogo de permisos lo declara cada contexto y se identifica por el código.** Hoy los
   contextos de Go comprueban **81 códigos**; el catálogo de C# tiene 272 y solo **5 coinciden**
   (`Parties.Party.Read/Create/Update`, `Payments.Payment.Read/Update`), porque C# nombra recursos
   que en Go no existen (`RRHH.*`, `Invoicing.*`, `Orders.*`…) y Go usa acciones que C# no tiene
   (`Issue`, `Approve`, `Submit`, `Settle`…). Propongo:
   - cada contexto exporta su lista (`Permissions()`), y el punto de composición se la pasa a
     `Security.SyncCatalog`, que da de alta los nuevos y desactiva los que ya nadie declara;
   - la clave del permiso es su código; **no se conservan los GUID de permiso de C#** (dependían
     del orden de declaración). **Sí se conservan los GUID de los cinco roles** y el del usuario
     de arranque;
   - los roles de sistema se calculan con la regla que ya aprobaste (P5), y no se editan:
     `GlobalSuperAdmin` el comodín; `OrganizationAdmin` todo salvo `Security.Role.Update`;
     `StandardUser` las acciones `Read`, `Create` y `Update` fuera de Security; `ReadOnlyUser`
     las `Read`; `Customer` ninguna. Las demás acciones (`Approve`, `Issue`, `Submit`…) solo las
     recibe `OrganizationAdmin` o un rol a medida.
   *Ejemplo:* al añadir el contexto Compras, su `Purchases.Invoice.Register` aparece en el
   catálogo en el siguiente arranque, `OrganizationAdmin` lo tiene y `StandardUser` no; si quieres
   que los administrativos registren facturas, creas el rol «Administrativo de compras» con ese
   permiso. Nada de lo que edites en un rol a medida lo pisa el arranque.
   *Recomiendo: sí.*

5. **Administración delegada: tres reglas con nombre** (concretan P3, que ya aprobaste).
   - **No se concede lo que no se tiene:** para asignar un rol hay que tener todos sus permisos;
     para conceder una organización hay que tener acceso `Full` a ella.
   - **Se administra a quien se abarca por completo:** un administrador puede editar, desactivar
     o dar roles a un usuario solo si tiene `Full` en **todas** las organizaciones de ese usuario.
     Un usuario sin accesos solo lo administra el administrador global; por eso el alta exige un
     primer acceso, igual que el alta de una party exige una afiliación.
   - **Nadie se administra a sí mismo, y siempre queda un administrador global activo.**
   *Ejemplo:* Marta administra Acme. Puede dar acceso a Acme a Pedro, que trabaja en Beta, y
   quitárselo después; pero no puede desactivar a Pedro ni cambiarle los roles, porque no abarca
   Beta. En C#, tras darle ese acceso, podría borrarlo.
   *Recomiendo: sí.*

6. **Los principales de servicio siguen en configuración.** Ya están en
   `authorization.Options.ServicePrincipals` (sin comodín, sin accesos, nunca administrador
   global) y `jwtauth` ya emite tokens con `actorKind=service`. No se crean tablas para ellos.
   *Ejemplo:* un proceso nocturno que lee facturas se declara en la configuración del host con
   `Billing.Invoice.Read`; darlo de alta o de baja es un despliegue, no una pantalla.
   *Recomiendo: sí, y pasarlos a base de datos el día que haya que administrarlos sin desplegar.*

7. **Qué no se porta, y cómo arranca una instalación.**
   - **Auto-registro:** no. El alta pública la hará el proveedor de identidad (D19).
   - **Usuarios por defecto por organización** y `AdminPasswordInitializer` (contraseña `123`): no.
   - **Usuario de arranque:** como en C#, se crea solo si no hay ningún administrador global
     activo y solo si llegan `KARPO_BOOTSTRAP_ADMIN_USER` y `KARPO_BOOTSTRAP_ADMIN_PASSWORD` por
     entorno; nace con «debe cambiar la contraseña», y **ahora se exige**: hasta que la cambie
     solo puede llamar a `/change-password`. Si el nombre ya existe, **no** se le da el rol: el
     arranque recibe un error y no se crea nada.
   - **Party del usuario de arranque:** el GUID reservado en C#
     (`20000000-0000-0000-0002-000000000004`), sin llamar a Parties. Security nunca escribe en
     Parties y desaparece la dependencia circular de arranque.
   - **Importar usuarios de C#:** fuera de esta fase; queda preparado por la decisión 3.
   *Recomiendo: sí a todo.*

## 8. Validación

Ejecutado el 2026-10-04:

- **Dominio** (`contexts/security/domain`): nombres de usuario, política de contraseñas, bloqueo
  y desbloqueo (un bloqueo caducado reinicia la cuenta), contraseña y activación, accesos e
  identidades sin duplicados, catálogo y regla estándar por rol, roles de sistema intocables, rol a
  medida sin comodín, las tres reglas de administración con el ejemplo de Marta y Pedro, sesiones
  y su rotación. Los GUID de los cinco roles y del usuario de arranque son los de C#.
- **Extremo a extremo HTTP** (`contexts/security/security_test.go`), con Security y Parties sobre
  el mismo backend, en memoria y después en SQLite migrada y cambiada en caliente:
  - arranque: catálogo sincronizado (idempotente), usuario de arranque solo con credenciales y
    solo si no hay administrador global; hasta cambiar la contraseña, 403 en todo;
  - el token de acceso no lleva roles, permisos ni ámbito;
  - alta de usuarios: nombre duplicado sin distinguir mayúsculas (422), party desconocida,
    organización no interna, nivel inventado y contraseña débil (400);
  - administración delegada: sin primer acceso 400, organización ajena 403, rol por encima de
    los propios 403, administrarse a uno mismo 403, roles reservados al administrador global;
    Pedro fuera de la vista (404), visible tras darle acceso a Acme pero no administrable (403);
  - revocación en la petición siguiente con el mismo token (rol retirado y usuario desactivado),
    con cambio de la versión de política;
  - bloqueo a los tres fallos, misma respuesta para un usuario inexistente, desbloqueo;
  - sesiones: rotación, un token rotado presentado otra vez cierra la familia, cierre de sesión,
    y restablecer la contraseña cierra las sesiones;
  - identidad externa: sin enlazar 401; enlazada, resuelve al mismo usuario con sus roles y
    accesos y usa Parties; una identidad no se enlaza a dos usuarios; un usuario sin contraseña
    nunca entra con contraseña;
  - roles: permiso desconocido y comodín 400, nombre duplicado 422, rol de sistema intocable,
    rol en uso no se retira; un contexto que deja de declarar sus permisos deja de conceder al
    momento y la sincronización no toca los roles a medida;
  - el último administrador global no se puede desactivar (422);
  - directorio `contracts.Users`, auditoría (nunca el hash) y lenguaje publicado consumido con
    bandeja de entrada.
- **Catálogo completo**: los diecisiete contextos de negocio declaran 109 permisos (eran doce y 81
  cuando se evaluó; después llegaron Productos, Inventario y Pedidos, `Parties.Relationship.Update`
  y `Parties.Relationship.SetTrial` con los detalles por tipo de relación, Activos y Trabajos), bien formados, sin
  repetir y cada uno en su espacio; con los ocho de Security y el comodín son 118. La regla estándar deja
  fuera del usuario estándar emitir facturas, aprobar nóminas, presentar modelos o liquidar remesas.
- **Hasher**: hash autodescriptivo, sal distinta cada vez, hash obsoleto detectado, formatos
  inválidos rechazados y un hash de C# (10 000 iteraciones) verificado y marcado para renovar.
- **Conformidad de los mapeos** (`infrastructure/securityconformance`): la semilla coincide con
  la regla; 37 especificaciones de usuarios, roles, permisos y sesiones devuelven en SQL lo mismo
  que en memoria; ida y vuelta; hijos reemplazados con el agregado; concurrencia optimista; e
  índices únicos (nombre de usuario y de rol sin distinguir mayúsculas, identidad externa, token).
  Pasa en **SQLite, PostgreSQL, SQL Server, Oracle (GUID RFC y .NET) y MySQL**.
- **Parties sobre el directorio real**: `TestParties_EndToEnd_MemoryThenSQLite` ejecuta su
  escenario completo dos veces, con el directorio en memoria del framework y con el contexto
  Security, sin cambiar ningún caso de uso de Parties.
- **Arquitectura**: dominio y contratos puros; la aplicación no alcanza la persistencia ni HTTP;
  `domain`, `contracts`, `application` e `infrastructure` no importan `jwtauth`.
- **Integración del contexto** (`integration/security_context_test.go`, Security y Parties
  migrados en la misma base): pasa en **PostgreSQL, SQL Server, Oracle y MySQL**.

La pasada de MySQL es del 2026-10-05, con el servidor para ella sola. Las del día anterior
coincidieron con otra ejecución completa de la suite contra las mismas bases y fallaron por eso
(interbloqueos al migrar, y el historial de migraciones borrado por el otro proceso): la suite
de integración no admite dos ejecuciones a la vez sobre los mismos servidores.

El 2026-10-06, tras rebasar sobre los contextos Productos, Inventario y Pedidos, se repitió todo
en un slot propio (`./integration/run.ps1 -Slot 2 -Run TestSecurity -Down`): la conformidad y la
integración del contexto pasan en los cinco motores.

## 9. Pendiente (fuera de este port)

- Verificador OIDC real y elección del proveedor (G-43).
- Resolutor en modo `Http` sobre `GET /api/auth/context`.
- Límite de intentos por origen al iniciar sesión (hoy solo hay bloqueo por usuario).
- Purga de las sesiones caducadas.
- Segundo factor, recuperación de contraseña por correo y alta por invitación.
- Reaccionar a `hr.employee-terminated.v1` desactivando al usuario de esa persona.
- Vigencia de las asignaciones de rol y `IncludeSubsidiaries` (P2).
- El front de Angular debe leer permisos y accesos de `GET /api/auth/context` (decisión 2).
- Importar los usuarios de C# (`infrastructure.FromCSharp` convierte sus hashes).
- Faceta `security` de Theros: contrastarla con este contexto (decisión A4 de la fase A).
