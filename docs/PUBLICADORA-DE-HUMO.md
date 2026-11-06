# Publicadora de humo

Una «publicadora de humo» es **un ejecutable aparte** que sirve lo mismo que `cmd/karpo` y
añade una **puerta por la que se entra sin contraseña**. Sirve para probar el conjunto desde un
navegador o con `curl` sin que nadie lea y teclee una credencial. **Sólo existe en desarrollo.**

| Qué | Dónde |
|---|---|
| La puerta | `host/smoke/` |
| El ejecutable | `cmd/karpo-smoke/` (SQLite) y `cmd/karpo-smoke-postgres/` (PostgreSQL, módulo aparte como `cmd/karpo-postgres`, para que el driver no entre en el módulo del framework) |
| La regla que la vigila | `pkg/testing/archtest/smoke_test.go` (reglas 10 y 11) |

## Arrancarla y entrar

```powershell
$env:KARPO_ENV = 'development'
go run -tags smoke ./cmd/karpo-smoke -port 8092 -seed
```

| Opción | Qué hace |
|---|---|
| `-port` | Puerto de `127.0.0.1` (por defecto 8092). No se puede elegir otra dirección |
| `-db` | Fichero SQLite, **que no exista**: si el fichero ya está, se niega antes de abrirlo y no lo toca (por defecto, uno en una carpeta temporal nueva; la ruta sale al arrancar) |
| `-seed` | Si no hay ninguna party, carga lo que pide el recorrido de [SMOKE-MANUAL.md](SMOKE-MANUAL.md): una empresa interna y su cliente, los tipos de IVA, un producto con existencias, una serie de facturación, condiciones de pago, un plan de cuentas con su libro y los usuarios de los perfiles `reader` y `clerk`. Imprime sus identificadores como líneas de PowerShell |

### Sobre PostgreSQL

`cmd/karpo-smoke-postgres` sirve lo mismo sobre PostgreSQL, el destino de producción. Tiene
`-port` y `-seed`; la base se da con `-postgres <cadena>` o, mejor, con la variable
`KARPO_SMOKE_POSTGRES_URL`, que no deja la contraseña en la línea de órdenes. Nunca se escribe en
un fichero.

- **Sólo arranca sobre una base de esta máquina**: la cadena tiene que apuntar a `127.0.0.1`,
  `::1`, `localhost` o un socket unix (también sus máquinas alternativas). Una cadena que apunte
  por error a otra base, aunque sea una de producción recién creada y vacía, se rechaza sin abrirla.
- **Sólo arranca sobre una base vacía.** Mira `pg_catalog` en todos los esquemas que no son de
  PostgreSQL (fuera de `pg_catalog`, `information_schema`, `pg_toast` y los demás `pg_*`):
  tablas, vistas, vistas materializadas, tablas foráneas, secuencias, funciones y tipos (sin los
  que crea PostgreSQL solo para cada tabla y los de array). Con un solo objeto se niega antes de
  migrar, dice cuántos hay y nombra algunos, y no toca nada. No mira otras bases del mismo
  servidor, roles ni extensiones sin objetos en esos esquemas. Entre comprobar y migrar pasa un
  instante sin cerrar: con una base local y desechable, nadie más escribe en ella.
- **Nunca borra nada.** Cada arranque, una base nueva: lo más sencillo es un contenedor
  desechable propio, como explica [SMOKE-MANUAL.md](SMOKE-MANUAL.md#arrancar-sobre-postgresql).
- El aviso del arranque nombra la base como `PostgreSQL usuario@máquina:puerto/base`, sin la
  contraseña. La carpeta de las exportaciones es temporal y se borra al parar.

```powershell
curl.exe -s "http://127.0.0.1:8092/api/smoke/token?as=admin"        # la sesión en JSON
# con el accessToken de esa respuesta:
curl.exe -s http://127.0.0.1:8092/api/parties -H "Authorization: Bearer <accessToken>"
```

El recorrido completo del ciclo de venta, paso a paso, está en [SMOKE-MANUAL.md](SMOKE-MANUAL.md).

Debería verse: un aviso en mayúsculas de que no hay autenticación, la ruta de la base, y en la
primera orden un JSON con `"success":true` y `"username":"smoke-admin"`.

## Rutas (el contrato común de las cuatro publicadoras de humo)

| Ruta | Qué hace |
|---|---|
| `GET /api/smoke/enter?as=<perfil>` | Página que guarda en `sessionStorage` lo que el front guarda tras un login (`auth-session`, `auth-token`, `auth-refresh-token`) y salta a `/`. Karpo Go no tiene front: está para que la puerta sea la misma en los cuatro proyectos |
| `GET /api/smoke/token?as=<perfil>` | La misma sesión en JSON, para `curl` |
| `POST /api/auth/login` | Para los perfiles fijos, cualquier contraseña vale. Para cualquier otro usuario es el login de siempre |

Todo lo demás es el anfitrión tal cual: sus rutas, sus permisos, `/api/auth/refresh`, `/healthz` y `/readyz`.

## Quién entra

Tres perfiles, fijos en el código (`host/smoke/smoke.go`, `Profiles`). Los dos de la empresa
sembrada usan los roles de Security y su acceso por organización, como cualquier usuario:

| Perfil | Usuario | Roles y acceso | Qué puede |
|---|---|---|---|
| `admin` | `smoke-admin` | `GlobalSuperAdmin` | Es el administrador global de la instalación: todo |
| `reader` | `smoke-reader` | `ReadOnlyUser`; acceso `ReadOnly` a la empresa sembrada | Leer lo de la empresa (parties, pedidos, facturas, cobros, asientos). Toda escritura, 403 |
| `clerk` | `smoke-clerk` | `StandardUser` y `SmokeSales`; acceso `Full` a la empresa sembrada | El trabajo comercial de la empresa: pedidos, albaranes, facturas (emitirlas también), cobros. No administra usuarios ni roles (403) ni ve otra empresa |

`SmokeSales` es un rol propio que crea la semilla con lo que `StandardUser` no da (éste sólo da
leer, crear y modificar): `Orders.Order.Confirm`, `Orders.Order.Deliver`, `Orders.Order.Cancel` y
`Billing.Invoice.Issue`.

`reader` y `clerk` **sólo existen con `-seed`**: los crea la semilla, justo después de la
empresa. Sin semilla, pedirlos responde 409 con `start the publisher with -seed`.

El nombre lleva «smoke» para que en la auditoría (`createdBy`) y en las trazas se vea que no es
una persona.

## Sin secretos

- La **clave que firma las sesiones** se genera al arrancar (32 bytes aleatorios) y vive en la
  memoria del proceso. No se lee `KARPO_JWT_SECRET`.
- La **contraseña de cada perfil** también se genera al arrancar, no se imprime ni se guarda.
  La publicadora la cambia una vez ella misma, para que el anfitrión no exija cambiarla antes
  de nada (el 403 del primer inicio de sesión).
- Se **niega a arrancar** si `KARPO_BOOTSTRAP_ADMIN_USER` o `KARPO_BOOTSTRAP_ADMIN_PASSWORD`
  están puestas: no acepta credenciales de nadie.
- Consecuencia: **sólo arranca sobre una base que aún no tiene administrador**. Al parar, nadie
  conoce la contraseña del que creó; un segundo arranque sobre el mismo fichero se niega. Cada
  arranque, base nueva.

## Las tres barreras

| Barrera | Cómo | Prueba |
|---|---|---|
| **Compilación** | Todos sus ficheros llevan `//go:build smoke`. Sin `-tags smoke` no está en ningún binario y `cmd/karpo-smoke` no compila | `go build ./cmd/karpo-smoke` falla con «build constraints exclude all Go files» |
| **Arranque** | Exige `KARPO_ENV` **exactamente** `development` (si falta, se niega) y sólo escucha en una dirección IP de bucle local (`127.0.0.0/8` o `::1`; el nombre `localhost` no vale). Además responde 403 a toda petición cuya cabecera `Host` no sea `127.0.0.1`, `[::1]` o `localhost` (con cualquier puerto): cierra el reenlace de DNS, en el que una página de fuera hace que su nombre resuelva a `127.0.0.1` desde el navegador del desarrollador. El proxy de desarrollo del front (`changeOrigin: true`) envía el `Host` de su destino, que es local | `TestNew_RefusesOutsideDevelopment`, `TestListen_RefusesWhatIsNotThisMachine`, `TestHost_OnlyThisMachine`, `TestRun_RefusesOutsideDevelopment` (con la etiqueta) |
| **Pipeline** | Una prueba **sin etiqueta**, que corre siempre, falla si un `.go` de fuera de la publicadora importa `host/smoke` (con etiqueta o sin ella, pruebas y todos los `cmd/*` incluidos), si uno sin la etiqueta nombra sus rutas, o si un fichero de la publicadora deja de estar tras la etiqueta. La etiqueta se lee como la lee el compilador (`go/build/constraint`), no como texto: `smoke \|\| linux` o una línea dentro de un comentario de bloque no cuentan | `TestSmoke_IsNotImportedByThePrograms`, `TestSmoke_StaysBehindItsTag`, y `TestSmoke_TheRuleBites`, que comprueba que la regla muerde |

## Comprobar

```powershell
go test -count=1 ./pkg/testing/archtest/...                               # la regla, sin etiqueta
go vet -tags smoke ./...
go test -tags smoke -count=1 ./host/smoke/... ./cmd/karpo-smoke/...       # la publicadora
go -C cmd/karpo-smoke-postgres test -tags smoke -count=1 ./...            # la de PostgreSQL, sin PostgreSQL
```

Debería verse `ok` en cada línea de paquete. El CI ejecuta lo mismo (`.github/workflows/ci.yml`).
