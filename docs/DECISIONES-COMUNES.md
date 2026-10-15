# Registro único de decisiones comunes

> **Qué es.** El sitio donde se anota cada decisión de Javier que vale para más de un proyecto
> (Trench, Karpo C#, Karpo Go, Theros). Lo mantiene la sesión de orquestación de Karpo Go.
> Javier lo encargó así el 2026-10-13, a través de la sesión de Karpo C# («acepto usar un único
> registro para decisiones comunes y que sea Karpo Go»), y **lo confirmó en esta sesión ese mismo
> día**, junto con todas las decisiones transmitidas hasta D-47 («confirmo el registro y las
> decisiones transmitidas»).
>
> **Dónde.** Repositorio `jhermoso/karpo-fw-go`, rama `docs/parties-modelo-comun`, fichero
> `docs/DECISIONES-COMUNES.md` (PR en borrador 11). Cuando se fusione, estará en la rama principal
> con la misma ruta.
>
> **Para qué sirve.** Antes de recomendar o decidir algo de modelo común, cada sesión lo lee; y
> cada sesión comprueba contra él en qué diverge su proyecto.

## Cómo se anota una decisión

1. **Quién.** Cualquier sesión, en cuanto Javier la toma. Si no es esta, la envía a «Orquestador
   Karpo Go» con: fecha, lo que dijo Javier (literal o casi), en qué sesión lo dijo y qué preguntas
   deja abiertas.
2. **Qué se escribe.** Una fila en §1: número, fecha, decisión en una frase, ejemplo, dónde está el
   detalle y en qué sesión se tomó.
3. **Confirmación.** Una decisión que llega por otra sesión se anota como «**transmitida**» hasta
   que Javier la confirma aquí o consta en un documento suyo. Las demás sesiones pueden apoyarse en
   ella, sabiendo que es transmitida. **Estado a 2026-10-13: de D-1 a D-47, todas confirmadas.**
   Donde abajo dice «transmitida por…» léase «tomada en la sesión de…, confirmada aquí el
   2026-10-13».
4. **Nunca se borra.** Si una decisión cambia, se anota la nueva y la antigua queda con «sustituida
   por D-n».
5. **Numeración.** `D-n`, una sola serie para todas las áreas. Las D-1 a D-16 son las del modelo
   común de Parties y conservan su número.

Lo que **no** va aquí: decisiones técnicas reversibles de un solo proyecto, y propuestas sin
decidir. Las propuestas están en sus documentos (§3).

## 1. Decisiones

«Sesión» dice dónde la tomó Javier. Las de Parties tienen su detalle, con ejemplos y lo que cambia
en cada lado, en [PARTIES-MODELO-COMUN.md](PARTIES-MODELO-COMUN.md).

### 1.1 Modelo de Parties

| # | Fecha | Decisión | Sesión |
|---|---|---|---|
| D-1 | 2026-10-11 | Los esquemas de Go y de C# siguen distintos, a propósito. Lo que se iguala es el modelo, las reglas y el contrato HTTP. Cómo se guarda una jerarquía es una opción de arquitectura de Theros | Karpo Go |
| D-2 | 2026-10-11 | Karpo Go sigue como monolito modular; los macroservicios, cuando el ERP esté más implementado | Karpo Go |
| D-3 | 2026-10-11 | El contacto es propio de cada party, no se comparte | Karpo Go |
| D-4 | 2026-10-11 | Se termina con fecha de fin; «borrar» es desactivar. No se desactiva algo de lo que dependen otras cosas vigentes | Karpo Go |
| D-5 | 2026-10-11 | Un documento de identidad inválido se rechaza siempre; las importaciones dejan un documento con los rechazados | Karpo Go |
| D-6 | 2026-10-11 | Catálogos híbridos: se siembra lo conocido; un tipo de rol sin atributos se añade como dato con su padre; uno con atributos va por la extensibilidad | Karpo Go |
| D-7 | 2026-10-11 | El límite de crédito es de la pareja empresa–cliente, no del cliente | Karpo Go |
| D-8 | 2026-10-11 | Un alta equivocada se quita con un comando «anular»: borra si nada depende del dato; si algo depende, lo marca como anulado | Karpo Go |
| D-9 | 2026-10-11 | Una party importada con el documento rechazado entra, marcada con el error, con un filtro para encontrarla | Karpo Go |
| D-10 | 2026-10-12 | El número de cliente es de la pareja empresa–cliente | Karpo Go |
| D-11 | 2026-10-12 | El estado y la prioridad de la relación quedan fuera del modelo | Karpo Go |
| D-12 | 2026-10-12 | La extensibilidad es un requisito del programa generado, no de Theros: primero la implementan Karpo y Trench | Karpo Go |
| D-13 | 2026-10-12 | Lo que bloquea o arrastra un borrado lo decide el ciclo de vida: los hijos que dependen del padre se borran con él; lo independiente ni se bloquea ni se arrastra. Se declara en el dominio de la relación | Karpo Go |
| D-14 | 2026-10-12 | La dirección postal lleva los campos españoles, y la geografía se apoya en NUTS para traducir los niveles entre países | Karpo Go |
| D-15 | 2026-10-12 | La visibilidad de una party no depende de la vigencia de su relación, sino de que esté activada. **Tres preguntas abiertas** (§2) | Karpo Go |
| D-16 | 2026-10-12 | La extensibilidad se hace en ejecución, por un administrador, para cualquier entidad, desde un espacio del front; el modelo son los configuradores de producto de Trench | Karpo Go |
| D-17 | 2026-10-13 | **Los clientes son relaciones, no roles.** Un cliente es la pareja parte–empresa con su perfil comercial; las condiciones comerciales (riesgo, bloqueo, número de cliente) son de la pareja. Es lo que Trench tiene como «Relaciones de clientes / proveedores». Confirmada aquí el 2026-10-13. **Preguntas abiertas** (§2) | Trench |

### 1.1 bis Clientes y proveedores como relaciones (sesión de Trench)

Todas **transmitidas** por la sesión de Trench el 2026-10-13, con la respuesta literal de Javier
cuando la dio. Fechas, según la hora de cada mensaje de Javier en esa sesión (hora peninsular):
D-28 y D-29, el 11 de octubre; D-30, D-31, D-32, D-33, D-34 y D-35, el 12 de octubre.

Sobre **D-17**: en la sesión de Trench no hay una frase de Javier con las palabras «los clientes
son relaciones»; lo que hay es su encargo del 12 de octubre de sustituir «Clientes» por «Relaciones
de clientes» (D-30). La frase literal es la que dijo en la sesión de Karpo C# el 13 de octubre:
«La primera es la que afecta a si los clientes son roles o relaciones. Son relaciones tal y como le
dije al orquestador de trench».

| # | Decisión | Lo que dijo Javier | Sesión |
|---|---|---|---|
| D-28 | El **bloqueo** del cliente también es de la pareja, y la **fuente única** de límite, bloqueo y número es el perfil comercial de la relación (completa D-7 y D-10) | «1 sí, confirmo. 2 el perfil comercial. 3 entran los tres» | Trench |
| D-29 | El perfil de la pareja lleva **dos identificadores externos**: el de Sage y el de Apiscore | «utiliza los dos… vamos a tener que integrarnos tarde o temprano con ambos sistemas» | Trench |
| D-30 | Las pantallas «Clientes», «Cliente↔Empresa» y «Proveedores» (y más adelante empleados) se sustituyen por **«Relaciones de clientes / proveedores / …»**: listado, ficha y filtros por pareja parte–empresa (es la forma concreta de D-17) | — | Trench |
| D-31 | El listado de relaciones de cliente enseña **toda** relación de cliente, no sólo las de quien tiene el rol de cliente | «1 toda relación de cliente» | Trench |
| D-32 | Esa búsqueda sólo enseña relaciones con las **empresas a las que el usuario tiene acceso** | «2 sí es correcto, confirmo» | Trench |
| D-33 | El alta de una relación **pide siempre la empresa**. Los símbolos de los filtros de rol son `&` y `\|` | «adelante con las dos recomendaciones» | Trench |
| D-34 | En la ficha se navega a siguiente, anterior, primero y último **de la paginación y de la selección, no del fichero** | literal | Trench |
| D-35 | Filtros rápidos por rol: los siete roles más usados, combinables con «y» / «o», con la expresión visible y **aplicada en la base de datos** mediante un DTO de especificación. De aquí sale `FilterExpressionDto` | — | Trench |

Confirmadas también allí, sin número nuevo: D-14 (los tres códigos NUTS erróneos se corrigen ya;
islas, niveles y piezas de dirección, después, en un tramo coordinado con Karpo C#) y D-16 (la
extensibilidad en ejecución; en Trench el orden es terminar el alta, luego el contrato, luego
la navegación).

### 1.1 ter Respuestas del 13 de octubre (sesión de Trench)

Tomadas en la sesión de Trench el 2026-10-13; Javier contestó allí a preguntas que este
registro tenía abiertas. **Confirmadas aquí el 2026-10-14** («sí a las 3»), con dos precisiones:
D-49 se lee como «sí, la cambiamos en Go», y en D-53 la persona que es organización interna se
comporta como cualquier empresa del grupo (emite facturas, tiene plan contable y empleados).

| # | Decisión | Lo que dijo Javier | Cierra |
|---|---|---|---|
| D-48 | **«Tiene el rol X» mira sólo los roles vigentes.** Para incluir los que ya no lo son, un filtro adicional en cada agregado o entidad, igual para todos | «roles sólo vigentes» · «Esto podemos solucionarlo para todos por igual añadiendo un filtro adicional en cada agregado o entidad que incluya o no los vigentes» | La pregunta de D-35 y la tercera de Theros |
| D-49 | **La relación asigna los roles si faltan**, y la regla contraria de Go se cambia. Leída como «sí, la cambiamos en Go»; confirmado aquí el 2026-10-14 | «si la cambiamos en go» | La pregunta 2 de D-17, si se confirma |
| D-50 | **Un documento no puede estar en dos parties**: la regla de unicidad se mantiene, y Trench limpia sus duplicados antes de converger | «limpiamos trench antes de converger» | «¿Un documento en dos parties?» |
| D-51 | **La vista por pareja («Relaciones de clientes / proveedores») es común a todos**: el contrato HTTP común lleva esas rutas | «preferiría que fuera común a todos» | La decisión 6 del contrato HTTP |
| D-52 | **En el filtro de roles no hay paréntesis por ahora**: se usa la precedencia de los operadores («y» liga más que «o»). El árbol de la expresión se mantiene; los paréntesis, más adelante, sobre la línea donde se ve la expresión | «de momento no podemos meter paréntesis vamos a utilizar la precedencia de los operadores…» | — |
| D-53 | **Una persona puede ser proveedor** (un autónomo), **e incluso organización interna** | «si, si puede, incluso puede ser organización interna» | — |

**El filtro de D-48, según Trench** (2026-10-13; hecho allí en Trench (PR 34), sin
fusionar, sin probar en Oracle ni SQL Server):

- Campo `includeEndedRoles`, booleano opcional, en el filtro de búsqueda de parties, hermano de
  `expression`. Ausente o `false`: sólo vigentes. No va dentro de `FilterExpressionDto`, que sigue
  en versión 1.
- «Vigente»: fecha de inicio ≤ ahora (UTC) y fecha de caducidad nula o posterior a ahora. Con
  `true` entran los terminados **y los que aún no han empezado**.
- Lo respetan los filtros planos de rol, la hoja `roleType`, las búsquedas de relaciones de cliente
  y de proveedor, y el recuento de roles más usados.
- D-52 en la interfaz: la lista de botones `&` / `|` se convierte siempre en una «o» de grupos «y»
  (`A | B & C` = `A o (B y C)`), tres niveles como mucho. El árbol no cambia.

Reafirmada también allí D-7: «el límite de crédito siempre pertenece a la pareja de empresa interna
y customer». No dice en qué contexto debe vivir.

### 1.1 quater Catálogo de tipos de rol (sesión de Karpo C#)

Tomada en las sesiones de Karpo C# y de Trench el 2026-10-14 («2 team» en las dos).
**Confirmada aquí el 2026-10-14.** Decide el nombre y el código; **no decide de qué cuelga** (§2).

| # | Decisión | Lo que dijo Javier | Contexto |
|---|---|---|---|
| D-54 | **El tipo de rol `10000000-0000-0000-0001-000000000037` se llama «Team»** y su código estable es `team` | «2 team», a la pregunta «"Team" u "Other Organization Unit" para el identificador …0037» | Las semillas SQL de Trench y de Karpo C# lo llaman «Team»; los catálogos en código de Karpo C# y de Karpo Go, «Other Organization Unit». Salió al preparar los códigos estables (Trench PR 36), que se rellenan por identificador |
| D-63 | **«Prospect Relationship» de Karpo Go pasa a `10000000-0000-0000-0002-000000000017`** y `…0016` queda para «Duplicate Of» de Trench (Party → Party, `duplicate-of`), que siembran todos | «mueve prospect a 0017» (esta sesión, 2026-10-14) | Cierra el choque de identificador de D-62. Hecho en Go (rama `feat/parties-prospect-0017`, migración 23). Karpo C# no tiene ninguno de los dos |
| D-64 | **Una party que es «duplicado de» otra está bloqueada**: se puede seguir encontrando y consultando (no es invisible), pero **por norma se excluye** al buscar o filtrar parties, también por rol, y **no puede intervenir en ninguna operación que no sea de consulta** | «uno es para decir que alguien es considerado como cliente potencial de la empresa y el otro es para indicar que esa party es un duplicado de otra y que por tanto no se puede operar con ella, es una forma de indicar que está bloqueado se puede seguir encontrando pero por norma general se debería excluir cuando se filtra o se busca en parties o parties con determinado rol. es diferente de ser invisible no es invisible, pero no puede intervenir en ninguna operación que no sea de consulta» (esta sesión, 2026-10-14) | Sin hacer en Go. Y, a la pregunta de qué pasa con sus relaciones vigentes: «pasarlas a otro puede ser complicado de momento se mantiene bloqueada ya veremos caso por caso si es posible fusionarlas eliminando una de ellas es probable que haya condiciones muy especificas para hacer eso» (2026-10-14): **las relaciones se quedan donde están; la party queda bloqueada, no desactivada**, así que no choca con D-4 |
| D-62 | **Los cuatro proyectos siembran los mismos tipos de rol y de relación**, con los mismos identificadores y códigos; y **«Team» cuelga de «Informal»**. Confirma también la excepción de D-49: una relación nunca asigna «Internal Organization» | «sí a las 2 primeras, team cuelga de informal siembra los mismos tipos en todos» (esta sesión, 2026-10-14) | Cierra de qué cuelga «Team» (en Trench, `D-ROLTYPE-0037-TEAM-VS-OTRA-UNIDAD`) y la pregunta de los tipos sin sembrar |

**El catálogo común, a 2026-10-14** (ids de la semilla de Trench):

- Tipos de rol: los 37 de Karpo Go, más «Club» (`…0001-…0038`, `club`) y «Group» (`…0039`, `group`), bajo «Informal» junto a «Team».
- Tipos de relación: los 12 de Karpo Go, más «Subsidiary» (`…0002-…0010`, `subsidiary`), «Legal Representation» (`…0011`, `legal-representation`), «Department Assignment» (`…0012`, `department-assignment`) y «Collaborator Contact» (`…0013`, `collaborator-contact`). Trench ha comprobado que lados y códigos coinciden.
- No son tipos de rol de party y no entran en este catálogo: Caller, Callee, Sender, Recipient, Worker, Project Manager, Reporter, Owner, Resolver y los de pedidos (en Trench, otra clase de tipo de rol). «App User» está comentado en la semilla de Trench.

**Estado en los otros proyectos, según sus sesiones (2026-10-14):** Trench tiene en PR sin
fusionar la excepción de D-49 (su PR 37; Javier la confirmó también allí: «confirmo, una relación
nunca asigna organización interna»), D-48 en listados y estadísticas (PR 38) y «Team», «Club» y
«Group» en su catálogo de código (PR 39); tiene en marcha la persona como proveedor y **sin empezar
la persona como organización interna**, que allí es grande. Karpo C# tiene todo ello sin aplicar,
detrás de su PR de permisos de Parties.

**Lo que hay que limpiar en Trench para D-50**, medido por su sesión en su PostgreSQL local el
2026-10-14 (sólo lectura, parties activas y sin fusionar): con la clave de Go (tipo, país, número)
no hay ningún documento repetido en las identificaciones. El problema está en que el CIF de una
organización legal vive además en otra columna (`legal_organization.TaxIdentity`), y ahí hay 38
CIF repetidos en 82 parties, probablemente la misma empresa importada dos veces. Además, 84
documentos con formato inválido y 197 sin validar, de 5.461. No se ha tocado ningún dato; en
Trench, `D-DUPLICADOS-LIMPIEZA-ANTES-DE-CONVERGER`.

### 1.1 quinquies Quién modifica a un tercero (sesión de Karpo C#)

Tomadas en la sesión de Karpo C# el 2026-10-14; **confirmadas aquí el mismo día, con la lectura de
esa sesión** («1 sí»). Las palabras son de Javier; la lectura en negrita es de esa sesión.

| # | Decisión (lectura) | Lo que dijo Javier |
|---|---|---|
| D-65 | **El alta de una party la vincula a la empresa interna de quien la crea**, y los usuarios de esa empresa pueden modificarla | «1 sí» a «¿El alta de una party la vincula a la empresa de quien la crea?» |
| D-66 | **Una relación con tu empresa da visibilidad sobre el tercero, no poder modificarlo**, salvo: si el tercero se dio de alta desde tu empresa (D-65), lo modificas; si existía y no es empresa interna de nadie, sólo lo modifica el administrador de tu empresa, con un permiso que el administrador global le puede retirar; si es empresa interna de otros, no lo modificas, salvo que seas usuario de ella o su administrador o el global te den permiso | «sí puede modificarlo si el tercero no es "empresa interna" y el usuario no lo es de dicha empresa […] 1- B no existe en el sistema, por tanto a puede modificarla sin problemas, 2- B existe en el sistema pero no es "internal organization" para nadie solo la puede modificar el administrador de A si el superglobal administrator no le retira el permiso para hacerlo. 3- La empresa B existe y además es internal organization para otros usuarios en los que no se incluye al usuario "a" en este caso el usuario "a" no la puede modificar y 4 la empresa existe, tiene el rol de "internal organization" y el usuario "a" sí es usuario de dicha empresa o el administrador de B o el superglobal le da permiso para modificarla. en este caso también puede modificarla» |
| D-67 | **De una persona que es empresa interna, desde esa empresa sólo cuentan sus relaciones de negocio**; sus datos personales sólo se ven como party pública campo a campo, si la propia persona lo autoriza (hace falta una marca por campo) | «3 sí efectivamente. siempre y cuando la propia persona autorice a marcar como públicos los campos que desean que sean públicos como party (habrá que añadir algún tipo de flag para marcar los campos)» |
| D-68 | **Las relaciones de negocio de una persona que es empresa interna incluyen también el representante legal, la jerarquía y los socios** («Legal Representation», «Organization Rollup», «Partnership»), además de empleo, cliente, proveedor, accionista y colaborador. Transmitida por Karpo C# | «sí» a «¿El representante legal, la jerarquía y los socios de una persona-empresa cuentan como de negocio?» |
| D-69 | **Un equipo («Team») no es una unidad organizativa**: no sale en la lista de unidades. Transmitida por Karpo C# | «no» a «¿Un equipo cuenta como unidad organizativa?» |


En Go, D-68 ya se cumple: sólo la relación de familia (y la de duplicado) no afilian. D-69 también:
Go no tiene lista de unidades organizativas y «Team» cuelga de «Informal».

Abierto según Karpo C#: si «administrador de A» es el administrador de organización con acceso a A;
el nombre del permiso que el global puede retirar; el diseño de la marca por campo.

**Respuestas de Javier del 2026-10-14 (esta sesión)** a lo que D-64 dejaba abierto:

| # | Decisión | Lo que dijo Javier |
|---|---|---|
| D-70 | **El bloqueo del duplicado alcanza a Facturación y Pedidos**: no se le factura ni se le hace un pedido | «el bloqueo sí alcanza a facturación y pedidos» |
| D-71 | **La fusión bloquea la ficha, no la desactiva** (también en Trench, que hoy la desactiva) | «bloquear si» |
| D-72 | **Karpo C# sigue con lo que tiene en marcha** antes de traer la fusión de Trench | «seguir con lo que tiene» |
| D-73 | **La vista por pareja se puede filtrar y ordenar por los datos de la pareja** (número de cliente, bloqueo) desde la versión 1 | «si» a «¿es obligatorio en la primera versión ordenar la lista por número de cliente o por bloqueo?» |
| D-74 | **Para saber qué empresa dio de alta una party basta la auditoría** (quién la creó y en la misma operación que su relación de negocio con esa empresa); **no hay tipo de relación nuevo ni dato nuevo en el modelo común**, y la relación del alta es la de negocio que elige quien da de alta. Aclarada en la sesión de Karpo C# | «no se si hace falta decir quien le dio de alta mas alla de la información histórica del registro. y la relacion se crea como en karpo c#»; y, a la aclaración en la sesión de Karpo C#: «sí, basta la auditoría y la relación de negocio» |

**D-64 en cada proyecto (2026-10-14):** en Trench, fusionado en `main` (su PR 45): lo que
decide «es duplicado» es su columna `MergedIntoPartyId`, y la relación «Duplicate Of» se crea
siempre con ella; filtro `includeMergedParties`. En Karpo Go, la relación «Duplicate Of» y su
proyección `parties.duplicate` (PR 28 y 29, en borrador), con el mismo filtro. **Karpo C# no tiene
fusión de fichas** (sólo el tipo sembrado): para tomar D-64 tiene que traerla antes; el orden lo
decide Javier. Divergencia de estructura entre los tres. Sigue abierto: la fusión de Trench
**desactiva** la ficha y D-64 dice «bloqueada, no desactivada».

**Lo que hace hoy la fusión de Trench** (su sesión, leyendo su código el 2026-10-14; intención
documentada, no comprobada en todas las lecturas): la ficha que cede no se borra ni cambia de
identificador; apunta a la superviviente, queda **inactiva** y se registra una relación «Duplicate
Of» hacia la superviviente, que se puede deshacer cerrándola. Sus documentos pasan a la
superviviente. Sus relaciones vigentes **no se terminan ni se trasladan**: siguen apuntando a ella y
«se resuelve por el puntero al leer». Según su código deja de ser elegible como cliente y toda
escritura nueva va a la superviviente. **Diferencia con D-64:** no se excluye por defecto de las
búsquedas ni de los listados por rol. **Choque con D-4:** si la fusión deja inactiva a una ficha con
relaciones vigentes, contradice que no se puede desactivar algo de lo que dependen relaciones
vigentes.

### 1.2 Extensibilidad

Detalle en [EXTENSIBILIDAD.md](EXTENSIBILIDAD.md) §1.

| # | Fecha | Decisión | Sesión |
|---|---|---|---|
| D-18 | 2026-10-12 | Un campo añadido es de toda la instalación o de una empresa: los dos ámbitos | Karpo Go |
| D-19 | 2026-10-12 | La pantalla es un apartado de administración, «Campos personalizados», con permiso propio | Karpo Go |
| D-20 | 2026-10-12 | Reglas, para empezar, las de Trench: tipo de valor, obligatorio y «obligatorio si». Los tipos que sean objetos valor o colecciones de entidades, más adelante y pensándolo antes | Karpo Go |
| D-21 | 2026-10-12 | La primera entidad fuera del sector financiero es el tipo de rol de Parties | Karpo Go |
| D-22 | 2026-10-12 | Lo generaliza primero Trench; Go lo implementa después contra el mismo contrato; Karpo C# lo recibe de Trench | Karpo Go |
| D-23 | 2026-10-12 | Los campos por empresa sólo caben en entidades que son de una empresa (relaciones, pedidos, reservas) | Karpo Go |

### 1.2 bis Contrato de extensibilidad (sesión de Trench)

Tomadas en la sesión de Trench (2026-10-13 tarde y 2026-10-14). **Confirmadas aquí el
2026-10-14** («confirmo D-55 a D-61»). Entre paréntesis, el número que llevan en el contrato de Trench (§4.1 y §4.1 bis).

| # | Decisión | Lo que dijo Javier |
|---|---|---|
| D-55 (1) | **La columna de un campo retirado se borra sólo si nunca tuvo valores**; si los tiene no se borra y se dice cuántos lo impiden. Sustituye a «retirar sin DDL» (decisión 8 de `EXTENSIBILIDAD.md`) | «1 b y se avisa cuantos valores existen que impiden el borrado» |
| D-56 (3) | **Los tipos hijos heredan los campos de los padres.** Sustituye a «sólo el tipo exacto» | «3 b los roles hijos heredan los campos de los padres» |
| D-57 (2) | **Un campo que pasa a obligatorio se exige en la siguiente modificación de cada registro**; el plan enseña cuántos quedan sin valor | «2 a y el aviso recomendado» |
| D-58 (5) | **Las referencias a catálogo (país, divisa) entran en la primera versión**, como un tipo de campo más | «5 dentro, confirmo» |
| D-59 (6) | **El acrónimo de empresa es obligatorio e inmutable**, y hay una función para que un administrador los cree en lote para una selección de registros. Sólo afecta a las organizaciones internas | «6 a, aunque el uso del acrónimo solo afecta en este caso a las organizaciones internas deberíamos facilitar una función para crearlos para una selección de registros por parte de un administrador» |
| D-60 (7) | **Trench da código estable a sus tipos de rol y de relación con la regla y los valores de Karpo Go** (nombre en minúsculas con guiones). Hecho en Trench (PR 36), sin fusionar | «7 sí» · «arranca ya los códigos estables y coordínalo con Karpo C#» |
| D-61 (4) | **Los campos de un producto financiero tienen una sola puerta**: se declaran en «Campos personalizados» y el producto los cita. Sólo afecta a quien tenga configuradores de producto | «4 b» |

### 1.3 Contrato HTTP y datos

| # | Fecha | Decisión | Sesión |
|---|---|---|---|
| D-24 | 2026-10-13 | El contrato HTTP común es **uno nuevo y limpio, escrito una vez**, que sirven los dos backs, empezando por lo que usan las pantallas principales | Karpo Go |
| D-25 | 2026-10-13 | En una importación, el documento español con el dígito de control mal se conserva como «otro documento», además de marcar a la party: por él se reconoce a quien ya se cargó | Karpo Go |
| D-26 | 2026-10-12 | Geografía: se separa el nivel estadístico (NUTS) de la «división principal» de cada país; provincia e isla son delimitaciones distintas | Karpo Go |

### 1.4 Forma de trabajo

| # | Fecha | Decisión | Sesión |
|---|---|---|---|
| D-27 | 2026-10-13 | Hay un único registro de decisiones comunes y lo mantiene Karpo Go (este documento). Confirmada aquí el 2026-10-13 | Karpo C# |

### 1.5 Generales (sesión de Karpo C#)

Todas **transmitidas** por la sesión de Karpo C# el 2026-10-13, con las palabras de Javier. Esa
sesión llevó también el arranque de Theros hasta el 12 de octubre.

| # | Fecha | Decisión | Lo que dijo Javier |
|---|---|---|---|
| D-36 | 2026-10-11 | **Compatibilidad de datos entre los tres ERP**: las bases de Karpo C# y Karpo Go no deberían divergir mucho de las de Trench; un error de Trench se corrige en Karpo y se traslada. **Precisada después por D-1** (los esquemas de Go y C# siguen distintos; se iguala modelo, reglas y contrato) | «…los dos sabores en lenguajes diferentes de karpo y cuyas bases de datos no debieran divergir excesivamente de las de trench si no ser las mismas, si bien si hay un error en las de trench podemos trasladarlo de karpo a trench. la idea sería mantener la misma estructura de datos si es posible» |
| D-37 | 2026-10-11 | **El modelo es agnóstico de la tecnología**, y lo que Go corrigió del modelo se lleva a Karpo C# | «En principio el modelo debería ser agnóstico de la tecnología subyacente. Si go ha encontrado errores de modelado tal vez debiéramos plantearnos corregirlos en karpo c# ya sea el unified o subdomain» |
| D-38 | 2026-10-12 | **Karpo C# adopta de Trench** las tablas y columnas de Parties que le faltan | «sí, deberíamos ser compatibles a no ser que karpo detecte un error que sea necesario corregir también en trench» |
| D-39 | 2026-10-12 | **Karpo es la construcción manual de la que Theros infiere su generador**: se implementan todas las opciones tecnológicas y de arquitectura posibles. En Karpo C#, el monolito y las rebanadas se mantienen los dos | «karpo es una construcción manual que tiene que permitir a theros inferir un generador por tanto necesitamos implementar todas las opciones tecnológicas y arquitectónicas que sea posible» |
| D-40 | 2026-10-10 y 11 | **Tres motores**: PostgreSQL, SQL Server y Oracle, también para las bases del propio Theros | «implantaremos 3 sabores postgress, sql server y oracle» |
| D-41 | 2026-10-12 y 13 | **Theros es una herramienta de desarrollo para modelos de lenguaje**, con resultado determinista. Karpo Go es un ejemplo de implementación del modelo en Go. No todo proyecto lleva Parties: un cuestionario previo, que conduce el modelo de lenguaje del cliente a través del plugin, decide qué contextos se aplican | «…orientado a ser usado como un tool de desarrollo para llms de forma que el resultado sea determinista y no probabilístico» · «No, no todo proyecto tiene que llevar parties» |
| D-42 | 2026-10-12 | **Un único front por ahora**, que puede ser Angular; otros (React, Unity…) más adelante. El editor de Theros, con React y xyflow | «vamos a empezar por ofrecer un único front de momento puede ser angular» |
| D-43 | 2026-10-12 | **Sin cobro por ahora**: basta con poder ofrecer una prueba | «antes de empezar a cobrar vamos a intentar tener algo que funcione» |
| D-44 | 2026-10-12 | **La infraestructura va detrás de contratos**: si una pieza depende de un contrato se puede terminar ahora y cambiar de tecnología después. Dicho a propósito del broker NATS de Go | «si la implementación depende de un contrato podemos terminarla sin problemas; más adelante podemos implementarla con otra tecnología siempre y cuando cumplamos el contrato» |
| D-45 | 2026-10-08 y 09 | **Permisos**: siempre que el modelo declare un permiso, se comprueba; un proyecto puede no declarar ninguno | «los permisos es necesario comprobarlos en todo momento» |
| D-46 | 2026-10-11 | **Forma de trabajo**: una sola sesión de orquestación por proyecto; antes de subir, integrado, compila, tres ejecuciones completas seguidas en verde, revisión de código aprobada y prueba de humo por interfaz con publicadoras de humo sólo en desarrollo | «mientras el código no se aprueba por el code review no pasa a la siguiente fase» |
| D-47 | 2026-10-12 y 13 | **Recuperación de contraseña**, que los demás proyectos heredan: el rescate es un comando del ejecutable; la contraseña temporal la genera el sistema; un administrador de organización sólo restablece a un usuario si todos los accesos de ese usuario están dentro de lo que él administra y todos sus permisos están contenidos en los suyos. Diseño en Karpo: `000-Docs/design/Karpo_Diseno_Recuperacion_Contrasena.md` | «los demás deberían heredar el sistema también» |

**No anotado, porque no es decisión de Javier** (lo dice la propia sesión de Karpo C#): que crear
una empresa interna exija ser administrador global (lo decidió esa sesión al cerrar un hueco de
seguridad; está en un PR que Javier ha pedido fusionar, sin confirmarlo con sus palabras); de qué
empresa es un envío o un trabajo; y cómo se añade un valor de catálogo en SQL Server y Oracle.

## 2. Preguntas que una decisión deja abiertas

| Decisión | Pregunta |
|---|---|
| D-8 | ¿Anular pide un permiso propio? ¿Qué cuenta como «depende»? |
| D-13 | Cómo se declara en el dominio que una relación es de dependencia (el diseño no está hecho) |
| D-15 | (1) Una party desactivada, ¿deja de verla también quien tendría que reactivarla? (2) Un antiguo cliente, ¿se puede editar desde la empresa o sólo ver? (3) ¿Cambia también lo que responde «a qué empresas pertenece hoy», que usan RRHH y Security? |
| D-17 | (1) ¿El rol «cliente» sigue existiendo, como consecuencia de tener la relación, o desaparece? (2) Si sigue, ¿lo asigna la relación al establecerse o tiene que existir antes? **Javier dijo en Trench, sin cerrarlo:** «creo que podemos decir que es la relación la que asigna los roles si estos no están previamente». Trench lo hace así; Go rechaza la relación si el rol falta. (3) ¿Vale lo mismo para proveedor, empleado y cliente potencial? (4) ¿La búsqueda de parties deja de poder filtrar «los que son cliente»? |
| D-17, D-30 | **En la pantalla «Clientes», Talleres Pérez, que compra a Acme y a Acme Levante, ¿sale una vez o dos**, una por empresa, cada una con su número de cliente? El contrato HTTP propuesto trata «cliente» como rol y daría una; D-17 y D-30 implican dos. (Pregunta de la sesión de Theros) |
| D-7, D-10, D-28 | **Talleres Pérez deja de ser cliente de Acme en marzo y vuelve en septiembre. ¿Vuelve con su número y su bloqueo por impago, o empieza de cero?** En Trench el perfil comercial cuelga de cada relación: empezaría de cero. En Go el límite y el bloqueo viven en `receivables` por pareja y se conservan; el número queda guardado en la relación terminada, nadie más puede usarlo, y hay que volver a dárselo a mano a la relación nueva. (Pregunta de la sesión de Theros) |
| D-31 | Si hay relaciones de cliente de quien no tiene el rol de cliente, ¿qué significa entonces el rol? (va con D-17) |
| D-35 | ¿«Tiene el rol X» mira sólo los roles vigentes hoy (Go) o cualquiera que haya tenido (Trench)? *Ejemplo:* Luis fue empleado hasta diciembre y hoy sólo es cliente; al filtrar por «Empleado», ¿sale? En Trench hoy sale; en Go, no. Si los dos comparten la expresión, tienen que contestar lo mismo. Trench recomendará «sólo vigentes» |
| D-39 | ¿Qué variantes cuentan en Karpo Go? Hoy es un solo proceso (D-2) |
| D-40 | Go prueba además en MySQL y SQLite. ¿Siguen como motores de prueba, o sobran? Dato de Theros: prueba lo generado en SQLite, PostgreSQL, SQL Server y Oracle; su plantilla de Go dice admitir MySQL sin ninguna prueba. Si sobran, en Theros SQLite quedaría sólo como motor embebido de las pruebas sin contenedores |
| D-44 | ¿Se termina entonces el broker NATS de Go, que está aparcado en una rama? |
| D-47 | Cuándo y cómo lo hereda Go (Security de Go tiene su propio diseño de contraseñas) |
| — | Supresión de datos personales (RGPD): sin decidir, pendiente de consulta legal |
| D-62 | **Dos tipos de relación con los lados distintos.** «Agent Relationship» (`…0008`): Agent → Internal Organization en Trench, Agent → Customer en Go. «Organization Rollup» (`…0005`): Department → Internal Organization en Trench; en Go, cualquier unidad organizativa → cualquier organización |
| D-62 | «Organization Unit» (`…0105`) cuelga de «Legal» en las semillas SQL de Trench y de «Organization» en su catálogo de código y en Karpo Go. «Entidad financiera» (`…00F1`) está en las semillas de Trench y otro script la borra |
| D-62 | «Salesperson» (`…0001-…00C3`) es en Trench un rol de pedido, no de party; en Go es un rol de party. La relación `prospect` sólo existe en Go |
| D-53 | ¿Un autónomo proveedor u organización interna se identifica con su NIF de persona? |
| D-53, D-67 | La primera mitad la contesta D-67 (sólo cuentan las relaciones de negocio). Falta qué tipos de relación son «de negocio» y el diseño de la marca por campo |
| D-49 | Diferencias de Karpo C# (su PR 31, sin fusionar), de su sesión: responde 409 donde Go da 422; exige además el permiso de asignar roles; una party recién creada en C# no es escribible por su empresa hasta tener una relación (en Go el alta ya la afilia); C# da por escribible una party del catálogo público y Go nunca; el alta de relación en C# no lleva fecha de inicio |
| D-56 | Que un tipo hijo no pueda declarar un código de campo que ya declara un ancestro, y qué pasa con los valores si un tipo cambia de padre. Por fijar entre Trench y Go |
| D-58 | Cómo se identifica entre instalaciones el valor de una referencia a catálogo (lo natural, el código ISO) |

## 3. Propuestas en curso (no son decisiones)

| Qué | Dónde | Estado |
|---|---|---|
| Contrato HTTP común de Parties, versión 1 | [CONTRATO-PARTIES-V1.md](CONTRATO-PARTIES-V1.md), rama `docs/contrato-http-parties` (PR 19) | Revisado por Karpo C# y por Trench; doce decisiones abiertas |
| Contrato de extensibilidad | Repositorio de Trench, `doc/Contrato_Extensibilidad_Campos_Personalizados_2026-10-13.md` | Revisado por Go; siete decisiones abiertas |
| Expresión de filtro (`FilterExpressionDto`) | Trench, en su rama principal | Adoptada tal cual en el contrato HTTP; falta acordar si mira sólo los roles vigentes |
| Lo que falta del modelo de Parties | [PARTIES-MODELO-COMUN.md](PARTIES-MODELO-COMUN.md) §3 | Unas 35 filas sin decidir |

## 3 bis. Lo que cada sesión dice que no cumple

Comunicado por cada sesión; no lo ha comprobado nadie más. El detalle de cada una vive en su
repositorio.

**Trench** (2026-10-13; «no es una auditoría fila a fila»):

| Decisión | Estado en Trench |
|---|---|
| D-3 | No: comparte mecanismos de contacto entre parties |
| D-4, D-8, D-13 | No: hay borrados de party, rol y relación |
| D-5, D-9 | A medias: guarda el estado de formato del documento; falta el filtro «con errores» |
| D-6 | No: catálogo de roles y jerarquía fijos en el código, sin código estable ni alta desde la aplicación |
| D-7, D-10, D-28 | Primera fase hecha (perfil por pareja, 1.888 números copiados, sólo en su PostgreSQL local); las columnas antiguas siguen hasta la segunda |
| D-11 | No: tiene estado y prioridad en la relación |
| D-14 | Sólo los tres códigos NUTS |
| D-15 | Sin comprobar |
| D-12, D-16 | Contrato escrito y en su rama principal; sin implementar |

**Karpo C#** (2026-10-13, sobre D-1 a D-35; sólo leyendo). De 35: cumple 3, a medias 4, diverge
20, no aplica 6, sin comprobar 2. De las 20, once tocan esquema. Su detalle:
`000-Docs/design/Karpo_Divergencias_Con_El_Registro_Comun_2026-10-13.md` en su repositorio.

| Decisión | Estado en Karpo C# |
|---|---|
| D-3 | No: el contacto es una entidad enlazada a varias parties (10 tablas) |
| D-4, D-8, D-13 | No: se borran rol, relación, identificación y contacto; no hay «terminar relación» ni «anular» |
| D-5, D-25 | No: el documento inválido se guarda marcado y sigue contando como identificación; sin índice único |
| D-9 | A medias: la marca está en la identificación, no en la party; sin filtro |
| D-6 | A medias: se crean tipos de rol por HTTP, sin código estable ni protección de los del programa |
| D-7, D-10, D-28 | No: el límite está en tres sitios; el número y el motivo de bloqueo, sólo en el rol |
| D-29 | A medias: el perfil tiene el código de Sage; falta el de Apiscore |
| D-11 | No: estado y prioridad son obligatorios, **y el estado decide la visibilidad** |
| D-15 | No: leer por identificador exige relación «activa» y sin caducar; ninguna ruta mira si la party está activa |
| D-12, D-16 | Sin empezar |
| D-14, D-26 | No: la dirección no tiene ninguna de las siete piezas; siguen los tres códigos insulares erróneos; sin islas ni niveles |
| D-17, D-30 | No: «cliente» es un rol con datos, pantalla propia y diez rutas. Proveedores ya van por la relación |
| D-33 | No: el alta de cliente crea party y rol sin empresa |
| D-35 | No: sólo «alguno» y «todos» |
| D-31, D-32, D-34 | Sin comprobar o sin encontrar |
| D-1, D-22, D-27 | Cumple |

**Dos bandos en una pregunta abierta de D-17.** Go y Karpo C# rechazan una relación si a un lado
le falta su rol (en C# desde el 7 de octubre, «como en Go»); Trench lo asigna.

**En «¿un documento en dos parties?»**, Karpo C# está con Trench: sin índice único y con filtro
de duplicados. Go es el único que lo impide.

**Reglas que Karpo C# propone como comunes** (las decidió su sesión al cerrar hallazgos de
revisión; **no son decisiones de Javier**, por eso no llevan número): un riesgo máximo vacío es
«sin límite» y un 0 es 0; toda ruta declara su permiso, lecturas incluidas; la cabecera de ámbito
nunca amplía; un pedido sin empresa toma la única en la que el usuario puede escribir, y si no hay
exactamente una, se rechaza. Las dos primeras ya son así en Go.

## 4. Divergencias de Karpo Go

Lo que Go no cumple todavía de lo decidido. Cada sesión lleva la suya en su repositorio.

| Decisión | Qué hace Go hoy | Qué falta |
|---|---|---|
| D-4 | **Hecho** (rama `feat/parties-desactivar-con-relaciones`): una party con relaciones sin terminar no se desactiva (422). Los roles no bloquean; lo de otros contextos, tampoco todavía | — |
| D-8 | **Hecho para los roles** (rama `feat/parties-anular-rol`, migración 22): `POST /api/parties/{id}/roles/{roleId}/annul` borra el rol si ninguna relación depende de él y, si alguna depende, lo termina y lo marca anulado. Decidido en esta sesión, a falta de respuesta a las dos preguntas de §2: permiso propio `Parties.PartyRole.Annul`, y «depende» son las relaciones | Anular relaciones, identificaciones, contactos y parties |
| D-13 | No hay declaración del ciclo de vida en el dominio | Necesita diseño |
| D-15 | La afiliación de una party a una empresa termina con su relación, y la empresa deja de verla | Esperar las tres respuestas |
| D-16, D-18 a D-23 | Hecho sólo el alta de tipos de rol y de relación sin campos | El mecanismo entero, contra el contrato de Trench. Antes hay que resolver que Go guarda un agregado borrando y reinsertando sus filas hijas |
| D-17 | «Cliente» es un rol **y además** un tipo de relación; la relación exige que el rol exista antes; el límite y el bloqueo están en `receivables` y el número de cliente en la relación (PR 17) | Esperar las cuatro respuestas. Los datos de la pareja ya están donde D-17 dice; lo que cambia es el papel del rol |
| D-28 | No hay un perfil comercial único: el número está en Parties, el límite y el bloqueo en `receivables`, el perfil de proveedor en `purchases` | Decidir si en Go «fuente única» es un contexto o basta con que cada dato tenga un solo dueño |
| D-29 | No hay identificadores externos en la pareja. La importación guarda la correspondencia con Sage y Apiscore en sus referencias (`imports`), por registro | Decidir si eso cumple |
| D-30 a D-35 | No hay búsqueda de relaciones, ni vista por pareja, ni expresión de filtro. Go no tiene front | La búsqueda de relaciones y la expresión, cuando se apruebe el contrato HTTP |
| D-24 | Go sirve sus rutas, no el contrato | El contrato está sin aprobar |
| D-39, D-44 | Un solo proceso; el broker NATS está aparcado en una rama sin fusionar | Decidir qué variantes cuentan en Go |
| D-48 | **Hecho** (rama `feat/parties-roles-terminados`): `GET /api/parties?role=…&includeEndedRoles=true` | — |
| D-49 | **Hecho** (PR 21, más los arreglos de la revisión en `feat/parties-desactivar-con-relaciones`): el caso de uso asigna el rol que falte antes de establecer la relación. No se asigna «Internal Organization» ni un rol que cuelgue de él (D-62), ni ningún rol a una party que quien llama no puede escribir (decidido en esta sesión tras la revisión independiente) | Que Javier confirme la segunda restricción |
| D-55, D-56 | `EXTENSIBILIDAD.md` dice lo contrario en los dos puntos (retirar sin DDL; sólo el tipo exacto). El mecanismo no está implementado | Corregir el diseño |
| D-59 | La empresa no tiene acrónimo | Dato nuevo, requisito previo de la extensibilidad |
| D-50 | Cumple: índice único sobre tipo, país y número | — |
| D-51 | No hay búsqueda de relaciones ni vista por pareja | Entra con el contrato HTTP |
| D-54, D-62 | **Hecho** (PR 20 y rama `feat/parties-catalogo-comun`, migraciones 19 y 21): «Team» bajo «Informal»; sembrados Club, Group y los cuatro tipos de relación | «Duplicate Of», cuando se resuelva el choque de identificador (§2) |
| D-53 | **Hecho** (rama `feat/parties-persona-proveedor-o-empresa`, migración 20): los dos tipos cuelgan de la raíz. Los demás contextos guardan a la empresa por identificador y no cambian. Una persona empresa no puede tener unidades organizativas debajo | El documento que identifica al autónomo; que la importación de proveedores cargue personas |
| D-64 | **Hecho** (rama `feat/parties-duplicado-bloqueado`, migración 24): marcar con «Duplicate Of» bloquea la party; se lee, la búsqueda la deja fuera salvo `includeMergedParties=true` (nombre de Trench, su PR 45), no admite cambios ni relaciones nuevas; terminar la relación lo deshace. Lo marca quien puede modificar el duplicado; una empresa interna, sólo el global | Que los otros contextos se nieguen a operar con un duplicado |
| D-70 | **Hecho** (PR 30, en borrador): Pedidos rechaza presupuestos, pedidos y confirmaciones para un duplicado; Facturación, preparar y emitir facturas ordinarias (las rectificativas, no: decidido en esta sesión). Lo saben por el contrato de identidades fiscales de Parties (`blocked`) | Compras, Cobros y Tesorería no lo miran |
| D-73 | Recogido en el contrato HTTP (`CONTRATO-PARTIES-V1.md` §7 quinquies) | La vista por pareja no está implementada en Go |
| D-74 | **Hecho** (rama `feat/parties-alta-por-auditoria`): Go lo deduce de la auditoría, como pidió Javier el 2026-10-14 («prefiero que lo deduzca de los registros de auditoria tambien en go»): la party y la relación que la afilia nacieron juntas si su primer registro comparte el identificador de operación. Sin columna propia | — |
| D-65, D-66 | **Hecho** (rama `feat/parties-quien-modifica`; desde D-74, sin la migración 25): se sabe si la party nació con el alta por la auditoría; si no, modifica quien tenga `Parties.Party.UpdateRelated` (de entrada, el administrador de organización); una empresa interna de otros sólo la modifican sus usuarios. Las afiliaciones anteriores a la migración quedan como relacionadas | El permiso vale para todas las empresas del usuario, no por empresa |
| D-67 | **Hecho en parte:** una relación de familia no afilia a nadie | La marca por campo de los datos públicos de una persona, diseño |
| D-40 | Cumple los tres, y prueba además en MySQL y SQLite | — |
| D-45 | Cumple: cada caso de uso comprueba su permiso | — |
| D-46 | Una sesión de orquestación; revisión independiente de lo delegado y de los cambios grandes. **No** se hacen tres pasadas completas en verde antes de cada subida, sino una local y una contra los motores | Ajustar si Javier lo quiere así también para los PR en borrador |
| D-47 | Security de Go no tiene el rescate ni el restablecimiento por un administrador | Planificarlo con el diseño de Karpo |
| D-5, D-9, D-10, D-14, D-25, D-26 | **Cumple** (PR 13 a 18) | — |
| D-1, D-2, D-3, D-6 (caso sin atributos), D-7, D-11 | **Cumple** | — |
