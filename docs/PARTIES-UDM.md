# Parties y UDM 1 (cap. 2): conceptos básicos y datos por subtipo

> **Estado: decisiones aprobadas por Javier el 2026-10-04** (las diez de §6, con la opción A en
> PU-5) **e implementadas** (§7). Lo que se ha validado y lo que no está en §8.

Continúa [PARTIES.md](PARTIES.md). Cierra las decisiones **CV-5** y **CV-6** del estudio de
convergencia entre C# y Go (`Karpo_Estudio_Convergencia_Modelos.md` §7) y las filas F3, F7, F8,
F9 y F11 de su §4.1.

## 1. El caso que lo motiva

Theros ofrecerá una prueba gratuita. Javier decidió (D24 y D29 del estudio del generador, §7.3)
que **el tiempo de la prueba es un atributo de la relación de cliente potencial con la
organización interna**, y que se gestiona en Parties:

- alargar la prueba a un cliente potencial concreto es cambiar ese atributo;
- convertirlo en cliente es establecer la relación de cliente;
- `subscriptions` sólo pregunta a Parties si la prueba sigue vigente, por un puerto o escuchando
  sus eventos.

Antes de este trabajo Go no podía expresarlo:

| Qué hace falta | Qué hay | Dónde se ve |
|---|---|---|
| El rol de cliente potencial | Existe: `RoleProspect`, colgando de la raíz | `domain/wellknown.go:48` |
| Un tipo de relación «cliente potencial ↔ organización interna» | **No existe.** Hay 11 tipos; el más parecido es `RelCustomer` | `domain/wellknown.go:58-70` |
| Que la relación lleve un dato propio de su tipo | **No puede.** `Relationship` tiene tipo, las dos partes, sus roles, periodo y comentario | `domain/relationship.go:30-39` |
| Que el rol lleve un dato propio de su tipo | **No puede.** `PartyRole` tiene tipo y periodo | `domain/party.go:49-53` |

## 2. UDM 1, capítulo 2, frente a Parties de Go

✅ está · 🟡 está a medias · ❌ falta. Las rutas son relativas a `contexts/parties/`. La tabla
describe **el punto de partida**; lo que cambia con este trabajo está al pie.

Aviso: los scripts del libro **no están en el checkout** de `C:\Git\Paranoia\Karpo`. La carpeta
`020-Back/010-Documentation/Data Model Resource V1` a la que apuntan `020-Back/CLAUDE.md` y la
skill `udm-v1-reference.md` no existe (en `010-Documentation` sólo hay `XMLDoc`). La columna «UDM»
sale del libro tal como lo transcribe el C# y del estudio de convergencia; **no se ha podido
contrastar con los scripts originales**, así que no doy listas de columnas de UDM.

| # | Concepto de UDM | Estado | En Go | Qué falta |
|---|---|---|---|---|
| 1 | **Party** | ✅ | `Party` con `Kind` (`domain/party.go:19-77`) | — |
| 2 | **Persona** | 🟡 | `PersonDetails`: nombre con dos apellidos, género, nacimiento y estado civil (`domain/party.go:28-33`) | **No se puede editar por HTTP**: `UpdatePersonDetails` existe en el dominio (`domain/party.go:241`) y ningún caso de uso lo llama (F11) |
| 3 | **Organización** (legal e informal) | ✅ | `OrganizationDetails` con nombre legal, comercial y forma jurídica (`domain/party.go:43-46`, `domain/affiliation.go:89-100`) | — |
| 4 | **Rol de la party** | 🟡 | `PartyRole{ID, RoleType, Period}`, hijo de `Party` (`domain/party.go:49`) | **Datos propios del rol** |
| 5 | **Subtipos de rol** (cliente, cliente potencial, proveedor, empleado, accionista, unidad organizativa, organización interna, canal de distribución…) | 🟡 | Son filas del catálogo: 37 tipos, 7 de ellos categorías (`domain/wellknown.go:75-115`) | En UDM son entidades que pueden llevar atributos; en Go ninguno lleva. Falta el rol «hogar» (*household*), sin uso |
| 6 | **Tipo de rol** | ✅ | `RoleType` con un padre, categorías y aplicabilidad a persona u organización (`domain/roles.go:25-31`) | — |
| 7 | **Relación entre parties** | 🟡 | `Relationship`, agregado propio: tipo, partes, roles, periodo y comentario (`domain/relationship.go:30`) | **Datos propios de la relación, estado y prioridad** |
| 8 | **Subtipos de relación** (de cliente, empleo, jerarquía organizativa, sociedad, contacto, canal de distribución, proveedor…) | 🟡 | Son filas del catálogo: 11 tipos (`domain/wellknown.go:127-142`) | **No hay relación de cliente potencial.** Ninguno lleva datos. Faltan también la de canal de distribución y tres que el C# sólo tiene en la semilla SQL (filial, representación legal, contacto de colaborador), sin uso hoy en Go |
| 9 | **Tipo de relación** | ✅ | `RelationshipType` con el rol de cada lado y la marca de jerárquico (`domain/relationship.go:16-25`) | — |
| 10 | **Estado de la relación** | ❌ | Sólo el periodo: vigente o terminada | El C# tiene 9 estados (activa, inactiva, pendiente, suspendida, terminada, borrador, aprobada, rechazada y potencial), obligatorios en cada relación (F7) |
| 11 | **Prioridad de la relación** | ❌ | Nada | El C# tiene 5 prioridades con nivel (baja, normal, alta, crítica y urgente), obligatorias (F7) |
| 12 | **Clasificación** de la party | ✅ | 5 familias y 22 hojas, familias exclusivas y aplicabilidad (`domain/classification.go:42`) | La clasificación propia de personas del C# no está (F6) |
| 13 | **Identificación** | ✅ | `Identification`, hija de `Party`, con política por país y dígito de control (`domain/identification.go:216`) | — |
| 14 | **Mecanismo de contacto y propósito** | ✅ | `Contact` con correo, teléfono, fax, web y dirección postal; propósitos (`domain/contacts.go:24-106`) | El contacto no se comparte entre parties (CV-7, ya decidido). Sin «roles válidos por mecanismo» (F5) |
| 15 | **Delimitación geográfica de la dirección** | ✅ | Ids de Geografía en la dirección (`domain/contacts.go:68`); contexto `geography` | — |
| 16 | **Instalación y rol de la party en ella** | ✅ | `FacilityRole` (`domain/facility_roles.go:74`); contexto `facilities` | — |
| 17 | **Características** de la party | ❌ | Nada | Todo (F3). En C# hay 2 tablas y 8 rutas, y **ningún tipo sembrado** |
| 18 | **Comunicaciones** (evento, propósito, roles, estado) | ❌ | Nada | Todo (F1). Destino propuesto: contexto de interacciones (CV-14) |
| 19 | **Casos** | ❌ | Nada | Todo (F2). El mismo contexto que las comunicaciones |

Lectura: de los 19 conceptos, 9 están, 5 están a medias y 5 faltan. Los cinco a medias tienen la
misma causa salvo uno (la edición de persona): **un tipo de rol o de relación no puede llevar
datos**. Es lo que resuelve este documento.

Después de §7: la fila 2 pasa a ✅ (la persona se edita por HTTP). Las filas 7 y 8 ya llevan
datos por tipo y existe la relación de cliente potencial; siguen 🟡 sólo por el estado y la
prioridad, que se decidió no hacer (PU-8, filas 10 y 11). Las filas 4 y 5 no cambian: el rol
sigue sin datos hasta que haya uno que sea suyo (PU-2).

## 3. Qué dato lleva cada subtipo, y de quién es

Antes de elegir un mecanismo conviene mirar qué datos son. El C# transcribió los subtipos de UDM
como subclases con tabla propia (una tabla por subtipo, sin discriminador). Estas tablas dicen qué
guardaba cada una, de quién es ese dato según la regla de §5, y dónde está hoy en Go. Rutas del
C# relativas a `ErpKernel.Domain/Subdominios/Parties/`.

### Subtipos de relación

| Subtipo en C# | Datos propios | ¿De quién es? | En Go hoy |
|---|---|---|---|
| Base `PartyRelationship` (`Relationships/PartyRelationship/PartyRelationship.cs`) | Estado y prioridad, **obligatorios** | Ver PU-8 | Nada |
| | Título del cargo (`RoleTitleCode`), administrador único | De la relación (representación) | Nada |
| | Participación directa e indirecta, en porcentaje y en importe | De la relación (propiedad) | Nada |
| `CustomerRelationship` | Segmento (texto libre), límite de crédito, valor de vida del cliente | Segmento: clasificación de la party. Límite: Cobros. Valor de vida: se calcula de las ventas | Familia «segmento de cliente»; `CreditProfile` en `receivables` |
| `SupplierRelationship` | Condiciones comerciales (texto libre), valoración | Compras | `SupplierProfile` en `purchases` (sin valoración) |
| `Partnership` | Tipo de sociedad, porcentaje de participación | De la relación | Nada |
| `Stockholding` | Porcentaje | De la relación | Nada |
| `OrganizationRollup` | Fin de vigencia (repetido), nivel jerárquico, porcentaje de control, «cuenta para informes» | El nivel se deriva; el resto, de la relación | La marca de jerárquico; el nivel se calcula |

El porcentaje de participación está en C# en **tres sitios** (la base, `Stockholding` y
`Partnership`). `Employment` no es subclase: es una relación corriente de tipo `Employment`.

### Subtipos de rol

| Subtipo en C# | Datos propios | ¿De quién es? | En Go hoy |
|---|---|---|---|
| `Customer` (`RolType/SpecificRoles/Customer/Customer.cs`) | Número de cliente (único en toda la base), límite de crédito, motivo de bloqueo | Número: de la pareja. Límite y bloqueo: Cobros | Límite y bloqueo en `receivables`; **el número, en ningún sitio** |
| `Employee` | Número de empleado, alta, baja, categoría, grupo salarial | RRHH, por empleador | `Employment` en `hr` |
| `FinancialInstitution` | «Es entidad significativa» | Del rol | Nada; sin uso |
| `Supplier`, `InternalOrganization`, `Carrier`, `Shareholder` y los tres de cliente | **Ninguno** | — | — |

Y los perfiles de ErpDetail, que son los que de verdad tenían los datos:

| Perfil en C# | Colgaba de | ¿De quién es? | En Go hoy |
|---|---|---|---|
| `CustomerRelationshipCommercialProfile` (código de cliente en la empresa, riesgo, descuentos, tarifas, cuentas contables, zona…) | La **relación** de cliente, una a una | Cobros, Ventas, Contabilidad; el código, de la pareja | `receivables`; lo de ventas, pendiente |
| `SupplierRelationshipCommercialProfile` | La **relación** de proveedor | Compras | `purchases` |
| `InternalOrganizationProfile` (código interno, divisa, mes de inicio del ejercicio, prorrata, LEI, empresa de pruebas, admite autorregistro) | El **rol** de organización interna | Fiscal y Contabilidad casi todo | `fiscal` (`Taxpayer`), `accounting` (libro) |
| `EmployeeProfile` | El **rol** de empleado | RRHH y Nóminas | `hr`, `payroll` |

Tres cosas que se ven en estas tablas:

1. De las diez subclases de rol, **sólo tres llevan datos**, y de esos datos sólo uno («entidad
   significativa») es del rol en sentido estricto. Lo demás es de la pareja o de otra área.
2. El número de cliente está dos veces en C# (en el rol, único en toda la base, y en el perfil
   de la relación, por empresa). El segundo es el que responde al caso de varias empresas.
3. **El C# no tiene nada sobre la prueba**, y resolvió «potencial» de otra forma que la que pide
   D29: no hay tipo de relación de cliente potencial, sino una relación de cliente con **estado
   `Potential`** (decisión «A2+B2» del 2026-06-10, anotada en `Comun/WellKnownCatalog.cs:483-489`:
   «el rol Prospect queda como legado UDM»). Ver PU-5.

## 4. Opciones de diseño para los datos por subtipo

### Opción (a): detalles por tipo dentro de `Relationship` y de `PartyRole`

Es el mecanismo que `Party` ya usa para persona y organización: el tipo es un dato y los detalles
de cada tipo son objetos valor guardados en la misma tabla, en columnas que quedan vacías para
los demás tipos.

Ejemplo. La relación de cliente potencial de Ana con Paranoia:

```go
type ProspectDetails struct {
	TrialUntil *time.Time // nil: no trial granted
}

type Relationship struct {
	// ...type, parties, roles, period, remark (as today)
	prospect ProspectDetails // only meaningful when the type is the prospect relationship
}
```

```json
{ "type": "…prospect…", "fromParty": "ana", "toParty": "paranoia",
  "since": "2026-10-04T10:00:00Z", "prospect": { "trialUntil": "2026-11-03T10:00:00Z" } }
```

- **Esquema:** una columna anulable por dato en `party_relationships` (aquí, `trial_until`). Cada
  dato nuevo es un `ALTER TABLE … ADD` en los cinco motores, como los de la fase 3. No hay tablas
  nuevas.
- **Catálogos:** siguen igual. **Añadir un tipo sin datos sigue sin exigir código**: es una fila.
  Añadir un tipo *con* datos sí exige código (el objeto valor, la columna y su regla), que es lo
  correcto: un dato con reglas es modelo.
- **Eventos:** los `v1` actuales no cambian. Cada dato con significado de negocio publica su
  propio evento con sus campos (`parties.prospect-trial-changed.v1`), de modo que quien lo
  consume recibe un contrato con nombre y tipo.
- **Lo bueno:** las reglas viven en el agregado («la prueba no acaba antes de empezar la
  relación»), el dato se puede buscar en SQL con una especificación, y es exactamente lo que el
  lenguaje del generador llama `variants` (Fase A, §8.3 n.º 11).
- **Lo malo:** la tabla se ensancha con cada variante. Con las pocas variantes previstas no es
  un problema.

### Opción (b): perfil en el contexto que usa el dato

Es lo que ya hacen `receivables`, `purchases` y `hr`: el dato vive en un agregado del contexto que
lo usa, y Parties sólo aporta las dos parties y la afiliación.

Ejemplo. El límite de crédito de Talleres Pérez con Acme es un `CreditProfile` de `receivables`
con `Seller = Acme` y `Customer = Talleres Pérez` (`contexts/receivables/domain/credit.go:27`).

Una precisión sobre el código actual: esos perfiles **no se enlazan por el identificador de la
relación, sino por la pareja** (empresa y tercero). Es mejor así: si la relación se termina y se
vuelve a establecer, el perfil sigue siendo el mismo.

- **Esquema:** nada en Parties. Una tabla en el otro contexto.
- **Catálogos:** nada.
- **Eventos:** Parties no publica nada nuevo; el otro contexto publica los suyos.
- **Lo bueno:** cada dato está junto a las reglas y los permisos que lo usan. Un cliente puede
  tener un límite distinto con cada empresa del grupo.
- **Lo malo:** no sirve para un dato que es de Parties. El tiempo de prueba, por decisión D29, lo
  es: se gestiona desde el ERP como parte del trato con el cliente potencial, y `subscriptions`
  sólo lo consulta.

### Opción (c): atributos extendidos genéricos

Una tabla `relationship_attributes(relationship_id, name, value)` o una columna JSON, con la lista
de atributos permitidos declarada en la fila del tipo.

Ejemplo. El tipo «cliente potencial» declara un atributo `trialUntil` de tipo fecha; la relación
de Ana guarda la fila `("trialUntil", "2026-11-03T10:00:00Z")`.

- **Esquema:** un cambio único; después, ningún dato nuevo necesita migración.
- **Catálogos:** añadir un tipo *con* datos tampoco exige código: basta con declarar sus
  atributos en la fila.
- **Eventos:** un evento genérico con un mapa de nombre y valor. Quien lo consume tiene que
  saber de memoria qué nombres existen y de qué tipo es cada valor.
- **Lo bueno:** máxima flexibilidad; un cliente del ERP podría definir sus propios campos.
- **Lo malo:**
  - el valor es texto: las reglas («la prueba no acaba antes de empezar») no caben en el
    agregado y habría que escribirlas como metadatos;
  - buscar por un valor exige funciones JSON distintas en cada uno de los cinco motores, o un
    `JOIN` por atributo;
  - el generador no puede describirlo como modelo: sólo vería «un mapa».

  Es otro problema —los **campos personalizados por cliente**— y ya está apuntado como tal en el
  `BACKLOG.md` §2b («atributos extendidos declarativos») para el framework entero, no sólo para
  Parties.

### Comparación

| | (a) Detalles por tipo | (b) Perfil en otro contexto | (c) Atributos genéricos |
|---|---|---|---|
| Esquema de Parties | Una columna por dato | Nada | Una tabla o columna, una vez |
| Tipo nuevo **sin** datos | Una fila | Una fila | Una fila |
| Tipo nuevo **con** datos | Código y migración | Código en el otro contexto | Una fila |
| Reglas sobre el dato | En el agregado | En el agregado del otro contexto | Fuera del dominio |
| Evento publicado | Con nombre y campos | Del otro contexto | Mapa genérico |
| Búsqueda en SQL | Especificación normal | En el otro contexto | Distinta por motor |
| En el lenguaje del generador | `variants` | Otro agregado | No expresable |

## 5. Regla propuesta: dónde va cada dato

Tres preguntas, en este orden. La primera que se conteste «sí» decide.

1. **¿Es de otra área de negocio?** Si el dato lo usan las reglas de cobros, compras, nóminas,
   fiscal o suscripciones, y lo cambia alguien con un permiso de esa área, va en **su contexto**,
   en un perfil por pareja empresa–tercero (opción b).
   *Ejemplo: el límite de crédito, el porcentaje de retención del proveedor, el contrato laboral,
   el plan contratado.*
2. **¿Cambia según quién sea la otra parte?** Entonces es de la **relación** (opción a).
   *Ejemplo: el tiempo de prueba de Ana con Paranoia; el 30 % que Ana posee de Talleres Pérez.*
3. **¿Es de la party por jugar ese rol, sea quien sea la otra parte?** Entonces es del **rol**
   (opción a). *Ejemplo: el código de entidad de un banco, que es el mismo para todos sus
   clientes.*

Y si el dato es cierto aunque la party no juegue ningún rol, es de la **party** (la fecha de
nacimiento, la forma jurídica), que es lo que ya hay.

Una prueba práctica para la pregunta 1: *¿quién lo cambia y con qué permiso?* El tiempo de prueba
lo cambia quien lleva el trato con los clientes potenciales, desde el ERP; por eso es de Parties y
no de `subscriptions`, que sólo necesita saber si sigue vigente.

Consecuencia que conviene tener presente: con esta regla **casi ningún dato acaba en el rol**. Los
que el C# guardaba en los subtipos de rol resultan ser de la pareja o de otra área (§3).

## 6. Decisiones (aprobadas por Javier el 2026-10-04)

Numeradas `PU-n`. **Las diez quedaron aprobadas tal como se recomendaban, con la opción A en
PU-5.** Se conserva el texto de la propuesta, con sus alternativas, para que conste qué se
descartó y por qué.

**PU-1 · Los datos propios de un tipo de relación, ¿dónde van? (cierra CV-6)**
Recomendación: **opción (a)**. Se mantiene el catálogo de tipos y `Relationship` gana «detalles
por tipo» como objetos valor, con el mecanismo de persona y organización. Los tipos sin datos no
cambian. La opción (c) queda para los campos personalizados por cliente, en el framework.

**PU-2 · Los datos de un rol, ¿son del rol o de quien los usa? (cierra CV-5)**
Recomendación: **se decide dato a dato con la regla de §5**, y el resultado hoy es:

- lo que es de otra área ya está en su contexto (crédito en `receivables`, perfil de proveedor en
  `purchases`, empleo en `hr`) y ahí se queda;
- el **número de cliente** depende de la pareja (cada empresa del grupo numera a sus clientes):
  es un detalle de la *relación de cliente*, en Parties. Se implementa cuando llegue quien lo
  use (la importación de Sage o Ventas), no ahora;
- `PartyRole` admite detalles por tipo con el mismo mecanismo que la relación, pero **no se
  implementa hasta que aparezca el primer dato** que pase la pregunta 3. El único candidato del C#
  es «entidad significativa» de `FinancialInstitution`, que nadie usa. Un mecanismo sin un solo
  uso sería código muerto.

Alternativa: implementarlo ya con un dato concreto. Si tienes uno en mente, dímelo y entra como un
paso más. Aviso: los roles tienen jerarquía (un cliente de facturación *es* un cliente), así que
los detalles de un rol tendrían que heredarse hacia abajo; la relación no tiene ese problema.

**PU-3 · ¿Cómo sabe el código qué detalles lleva cada tipo?**
El discriminador de persona y organización es un enumerado. Aquí es una fila de catálogo.
Recomendación: **un código estable en la fila del tipo** (`relationship_types.code`, por ejemplo
`prospect`, `ownership`), opcional y único. Es lo que el lenguaje del generador ya prevé («las
claves de `of` son los códigos de sus entradas»). Un tipo sin código, o con un código que el
dominio no conoce, no lleva detalles.
Alternativa: usar el GUID conocido, sin columna nueva. Funciona en Go, pero deja al modelo sin
una clave legible.

**PU-4 · El tiempo de prueba: qué se guarda**
Recomendación: **la fecha y hora en que acaba la prueba** (`trialUntil`), opcional.

- Sin valor: a ese cliente potencial no se le ha dado prueba.
- La prueba está vigente en un instante si la relación está vigente y el instante es anterior a
  `trialUntil`.
- Alargar o acortar es cambiar el valor. Debe ser posterior al inicio de la relación; no se cambia
  en una relación terminada.
- Parties **no guarda la duración por defecto** («30 días»): es política comercial y la aporta
  quien da de alta al cliente potencial.

Alternativas: guardar un número de días (obliga a recalcular el final cada vez que se pregunta y
no expresa «hasta el día 3») o un periodo con inicio y fin (el inicio ya es el de la relación).

**PU-5 · «Cliente potencial»: ¿un tipo de relación propio, o un estado de la relación de cliente?**
Es la decisión con más fondo, porque **el C# y D29 dicen cosas distintas**.

- **Opción A (la de D29): un tipo propio.** `Prospect Relationship`: cliente potencial →
  organización interna. La prueba es un detalle de ese tipo. Convertir en cliente es establecer
  la relación de cliente y terminar la de cliente potencial; quedan las dos en el historial, cada
  una con sus fechas.
- **Opción B (la del C#, decisión «A2+B2» del 2026-06-10): un estado.** No hay tipo nuevo: es una
  relación de cliente con estado «potencial». Convertir es cambiar el estado. Sirve igual para
  «proveedor potencial».

Recomendación: **opción A.**

- En Go una relación exige que cada parte juegue el rol de su lado. Con B, el cliente potencial
  tendría que jugar el rol de cliente, y la búsqueda de clientes lo devolvería. El C# no lo nota
  porque no comprueba los roles (defecto R7 del estudio de convergencia).
- B obliga a guardar un estado (PU-8), y el dato «hasta cuándo fue cliente potencial» se pierde
  al cambiarlo.
- Es lo que dice UDM, donde el cliente potencial es un rol, y lo que dice D29.

El tipo nuevo llevaría el GUID `10000000-0000-0000-0002-000000000016`, libre en el catálogo y en
la semilla SQL del C# (los `…10` a `…15` están ocupados). Como toda relación con una organización
interna, afilia al cliente potencial y lo hace visible para ella; el alta dentro del ámbito
(`affiliation`) lo admite sin cambios y aceptará la prueba.

**Convertir en cliente no se automatiza** en esta tarea: son tres operaciones que ya existen
(asignar el rol de cliente, establecer la relación de cliente y terminar la de cliente
potencial). Si luego se quiere una sola, es un caso de uso que las compone.

Consecuencia para la convergencia: con A, los dos lados modelan «potencial» de forma distinta, y
hay que anotarlo en el estudio (§9).

**PU-6 · Cómo se entera `subscriptions`**
Recomendación: **las dos vías que pide D29**, las dos pequeñas:

- evento `parties.prospect-trial-changed.v1` (relación, cliente potencial, organización y nuevo
  final), publicado al establecer la relación con prueba y cada vez que cambie;
- puerto `contracts.Trials`: para una organización y una lista de parties, la prueba de cada una
  y si está vigente. Por lotes, como el directorio.

El fin de la relación ya se publica (`relationship-terminated.v1`) y no cambia.

**PU-7 · Participación en el capital (F8)**
Recomendación: **sí, como segundo detalle**: el porcentaje de participación directa en el tipo
`Ownership` («Ana posee el 30 % de Talleres Pérez»), entre 0 y 100. Demuestra que el mecanismo
sirve para más de un tipo. Quedan fuera los importes, la participación indirecta y la regla de
que las participaciones de una sociedad no sumen más de 100, hasta que alguien los use.
Alternativa: dejarlo fuera y que el único detalle sea la prueba.

**PU-8 · Estado y prioridad de la relación (F7)**
Recomendación: **no implementarlos ahora.**

- El **estado** que hoy importa se deriva: vigente o terminada sale del periodo; en prueba, de
  `trialUntil`. Este repositorio ya eligió derivar en vez de guardar en casos iguales (la vacante
  de un puesto en `hr`, «pagada» en `receivables`). Un estado guardado que repite lo que dicen las
  fechas acaba contradiciéndolas.
- De los 9 estados del C#, seis repiten las fechas (activa, inactiva, terminada) o son de un
  flujo de aprobación que Go no tiene (borrador, aprobada, rechazada). «Potencial» lo cubre PU-5.
  Quedan «pendiente» y «suspendida», sin consumidor.
- La **prioridad** es un dato de seguimiento comercial sin ningún consumidor en Go. Encaja mejor
  en el contexto de interacciones (CV-14), que es quien decide a quién atender primero.

Si los quieres por completitud de UDM, entran como último paso y de la forma más simple: dos
catálogos sembrados con los GUID del C# (`…0003-…` y `…0004-…`) y dos referencias **opcionales**
en `Relationship`, con un caso de uso para cambiarlas. Quedarían como datos descriptivos, sin
reglas. Si en PU-5 eliges la opción B, el estado deja de ser opcional y entra en el paso 1.

**PU-9 · Qué más de UDM 1 entra en esta tarea**

| Concepto | Propuesta |
|---|---|
| Edición de los datos de persona (F11) | **Entra**: caso de uso y ruta para `UpdatePersonDetails`. Es pequeño y hoy una persona no se puede corregir |
| Características (F3) | **Fuera.** Sin tipos sembrados no hay uso; es la opción (c) con otro nombre. Coincide con CV-8 |
| Roles válidos por mecanismo de contacto (F5), clasificación de personas (F6) | **Fuera**, pendientes de CV-8 |
| Comunicaciones y casos (F1, F2) | **Fuera**: contexto de interacciones (CV-14) |
| Título del cargo y administrador único (parte de F7) | **Fuera**: son de la relación de representación; se verán cuando Fiscal o Asesoría los pidan |
| Cuentas bancarias de terceros (F4) | **Fuera**: no es del capítulo 2; tiene su paso propio (P-21) |

**PU-10 · Permisos**
Recomendación: **un permiso nuevo, `Parties.Relationship.Update`**, para cambiar los detalles de
una relación (la prueba, la participación). Establecer con detalles sigue pidiendo
`Parties.Relationship.Create`.
Alternativa: un permiso aparte para la prueba (`Parties.Relationship.SetTrial`), si quieres
separar quién puede regalar tiempo de quién puede corregir una participación.

## 7. Lo que se ha hecho

Todo es aditivo: ninguna tabla, columna, ruta ni evento `v1` anterior ha cambiado.

| Paso | Contenido | Esquema | HTTP | Lenguaje publicado |
|---|---|---|---|---|
| 1 | Código estable en los tipos de relación (PU-3) y tipo `Prospect Relationship` (PU-5) | Migración 11: `relationship_types.code`, los códigos de los 11 tipos que había y la fila nueva `…0002-000000000016` | `GET /api/catalogs/party-relationship-types` devuelve `code` | — |
| 2 | Tiempo de prueba del cliente potencial (PU-4, PU-6) | Migración 12: `party_relationships.trial_until` | `prospect` en `POST /api/party-relationships` y en `affiliation` del alta; `PUT /api/party-relationships/{id}/trial`; el DTO devuelve `prospect: { trialUntil, inTrial }` | `parties.prospect-trial-changed.v1`; puerto `contracts.Trials` (`Module.Trials`) |
| 3 | Participación en el capital (PU-7) | Migración 13: `party_relationships.share_percent` (decimal exacto en texto, como el resto de contextos) | `ownership` en el alta de la relación; `PUT /api/party-relationships/{id}/ownership`; el DTO devuelve `ownership: { share }` | `parties.ownership-share-changed.v1` |
| 4 | Edición de persona (PU-9, F11) | — | `PUT /api/parties/{id}/person` (género, nacimiento y estado civil) | — (no cambia el nombre) |

Permiso nuevo (PU-10): `Parties.Relationship.Update`, para las dos rutas `PUT` de detalles.
Establecer una relación con sus detalles sigue pidiendo `Parties.Relationship.Create`; editar la
persona, `Parties.Party.Update`.

El permiso está declarado al catálogo de Security (`application.Permissions`). Por la regla
estándar de ese catálogo (leer, crear y actualizar), **lo recibe también el rol de usuario
estándar**: cualquier usuario estándar con acceso completo a la organización puede alargar una
prueba. Si eso no se quiere, la alternativa de PU-10 (una acción propia, como `SetTrial`) lo deja
sólo para el administrador de la organización y los roles a medida.

### Cómo queda en el código

- **El catálogo dice qué tipo es; el código, qué datos lleva.** `RelationshipType.Code`
  (`domain/relationship.go`) es opcional y único; `IndexRelationshipTypes` rechaza un catálogo
  con dos tipos del mismo código. Un tipo sin código, o con un código que el dominio no conoce,
  no lleva detalles: **añadir un tipo sin datos sigue siendo una fila**.
- **Los detalles son objetos valor dentro del agregado**, como en `Party`:
  `RelationshipDetails{Prospect, Ownership}`. Los de los demás tipos quedan vacíos.
- **Las reglas están en el agregado** y reciben el tipo, igual que `AssignRole` recibe el
  catálogo de roles:
  - `SetTrial`: sólo en tipos con código `prospect`; el final es posterior al inicio de la
    relación; `nil` retira la prueba; el mismo valor no hace nada.
  - `SetOwnershipShare`: sólo en tipos con código `ownership`; mayor que 0 y hasta 100, con dos
    decimales como mucho; `nil` la borra.
  - Los detalles de una relación terminada no se cambian (`parties.relationship_ended`).
  - Pedir un detalle a un tipo que no lo tiene es una violación de regla (422), no un dato que
    se ignora.
- **La prueba está vigente** si la relación lo está y el instante es anterior a `trialUntil`
  (`Relationship.InTrialAt`, y la especificación `InTrialAt` para buscarlo en SQL). Al terminar
  la relación la prueba deja de estar vigente y conserva su fecha.
- **Convertir en cliente** son tres operaciones que ya existían, y la party no sale del ámbito
  de la organización porque la relación de cliente la mantiene afiliada.
- **Para añadir un detalle a otro tipo** hacen falta cinco cosas: el código en la fila del tipo,
  un objeto valor en `RelationshipDetails` con su método, una columna anulable (migración), el
  campo en el mapeo y en el DTO, y su evento `v1`.

Ejemplo, el alta de un cliente potencial con 30 días de prueba, en una sola petición a
`POST /api/organizations`:

```json
{ "legalName": "Flotas Ana",
  "affiliation": { "organization": "<paranoia>",
                   "relationshipType": "10000000-0000-0000-0002-000000000016",
                   "prospect": { "trialUntil": "2026-11-03T10:00:00Z" } } }
```

La party recibe el rol de cliente potencial, la relación, la afiliación y la prueba en la misma
transacción, y se publican `party-registered`, `party-role-assigned`,
`relationship-established`, `prospect-trial-changed` y `party-affiliated`.

## 8. Validación

Ejecutado (2026-10-04, Windows, Go 1.27):

- **Dominio** (`domain/udm_test.go`): códigos únicos y tipos sin código; prueba concedida,
  alargada, igual, anterior al inicio, retirada, vigencia semiabierta, relación terminada y tipo
  equivocado; participación con sus límites (0, negativa, 100,01 y tres decimales rechazadas;
  100 aceptada), borrado y tipo equivocado.
- **Extremo a extremo HTTP** (`parties_test.go`), en memoria y en SQLite migrada y cambiada en
  caliente:
  - catálogo con códigos;
  - alta de un cliente potencial con prueba dentro del ámbito;
  - alargar la prueba: sin permiso 403, acceso de sólo lectura 403, ajeno 404, fecha anterior al
    inicio 400, correcto 200 con la versión incrementada;
  - puerto `Trials`;
  - prueba en una relación de cliente o de empleo: 422;
  - relación de cliente potencial sin prueba, concedida después y retirada;
  - conversión en cliente: la prueba deja de estar vigente, la relación terminada no se cambia
    (422) y la party sigue visible;
  - edición de persona: correcto, fecha futura 400, género desconocido 400, sólo lectura 403,
    ajeno 404, organización 422, y los campos omitidos quedan vacíos;
  - participación: 130 % rechazado (400), 30 %, corrección a 45,5 %, texto y tres decimales 400,
    borrado;
  - eventos `v1` consumidos por un CRM con bandeja de entrada: cuatro de prueba y tres de
    participación.
- **Toda la batería del repositorio** (`go test ./...`): pasa, con la salvedad de abajo.

- **Integración** (`integration/parties_context_test.go`, `TestPartiesContext`), ejecutada el
  2026-10-06 en PostgreSQL, SQL Server, Oracle (GUID RFC y .NET) y MySQL: pasa en los cinco.
  Cubre las migraciones 11 a 13, los códigos leídos de SQL, el alta con prueba, la ida y vuelta
  de `trial_until`, la especificación `InTrialAt` sobre la columna anulable, el puerto `Trials`,
  la retirada, la participación como decimal exacto y la edición de persona.

- **Batería de integración completa** (`integration/`, 18 pruebas: los doce contextos, la
  conformidad del framework y el cambio en caliente), ejecutada el 2026-10-06 en los cuatro
  motores: pasa entera. Los demás contextos migran también las tablas de Parties, así que
  comprueba que las migraciones 11 a 13 no les afectan.
- **Tras reubicar el cambio sobre Productos, Inventario, Pedidos y Security** (que entraron
  después): `go test ./...` pasa entero, y la integración de Parties y de Security pasa en los
  cuatro motores. La batería de integración completa no se ha repetido sobre esa base.

Salvedad: `TestParties_EndToEnd_MemoryThenSQLite` falla de forma intermitente en Windows
(1 o 2 de cada 40 ejecuciones) con «end e-mail: status 422». **Es anterior a este cambio**: se
reproduce igual en el código sin tocar. La prueba termina un contacto en el mismo tic de reloj
en que lo creó y el dominio exige que el fin sea posterior al inicio.

## 9. Qué hay que trasladar fuera de este repositorio

No se ha tocado nada en `C:\Git\Paranoia\Karpo`. Queda para Javier:

- **Estudio de convergencia:** CV-5 y CV-6 quedan contestadas por PU-2 y PU-1; F7 por PU-8; F8
  por PU-7; F11 por PU-9; el número de cliente tiene destino (detalle de la relación de cliente).
  El paso P-23 se parte en «participación» (hecho) y «estado y prioridad» (no se hace).
- **Una fila nueva en su inventario:** «cliente potencial» es en C# un estado de la relación de
  cliente y en Go es un tipo de relación propio (PU-5). Es una diferencia de estructura (b2) que
  el estudio no recoge. También son nuevos en Go, y no existen en C#, el código de los tipos de
  relación y los dos eventos `v1` de §7.
- **Catálogo del C#:** si algún día adopta el tipo de relación de cliente potencial, con el mismo
  GUID (`…0002-000000000016`).
- **Autorregistro:** en C# el alta por autorregistro crea la organización con el rol de cliente
  potencial (`CreateProspectOrganizationAsync`). Es el punto donde Theros dará la prueba, y toca
  al port de Security a Go, que va en otra sesión.
- **Lenguaje del generador** (Fase A, §8.3 n.º 11): se confirma la propuesta. El catálogo de
  tipos de relación gana un atributo `code`, y las variantes de `Relationship` se discriminan
  por él. El modelo de `Relationship` tiene ahora dos variantes (`prospect` y `ownership`).

## 10. Pendiente

- Detalles de rol en `PartyRole` (PU-2): cuando aparezca el primer dato que sea del rol.
- Número de cliente como detalle de la relación de cliente (PU-2): cuando llegue la importación
  de Sage o Ventas.
- Convertir un cliente potencial en cliente en una sola operación (PU-5).
- Participación: importes, participación indirecta y la regla de que las participaciones de una
  sociedad no sumen más de 100 (PU-7).
- Estado y prioridad de la relación (PU-8): no se hacen; la prioridad, si acaso, en el contexto
  de interacciones.
- Código estable también en los tipos de rol, cuando el generador lo pida para sus semillas.
