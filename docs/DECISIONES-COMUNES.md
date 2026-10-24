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
| D-75 | **El alta de una party desde una empresa exige elegir la relación de negocio con ella.** Transmitida por Karpo C# (2026-10-15) | «sí» a «¿Exigir la relación en el alta?» |
| D-76 | **El permiso para modificar terceros sólo relacionados va por rol** (en Go, `Parties.Party.UpdateRelated`). Transmitida por Karpo C# | «sí» a «¿El permiso para modificar terceros sólo relacionados va por rol?» |
| D-77 | **Los terceros que ya existían antes de la regla de D-74 quedan de sólo lectura para el usuario normal**; los modifica el administrador con el permiso. Principio: los datos importantes de un cliente o proveedor viven en la relación, que es de la empresa; el tercero sólo tiene datos básicos. Transmitida por Karpo C# | «vamos a aplicar la opción a […] si talleres gomez es cliente de acme el volumen de datos de talleres gomez es muy básico teléfono dirección etc. y los datos mas importantes ni siquiera están en talleres gomez sino en la relación de cliente con acme que si pertenece a acme no a talleres gomez […]» |
| D-78 | **Que un tercero pase a ser empresa interna por el alta de uno de sus usuarios exige la aprobación del administrador global**, con pruebas documentales de que actúa en su nombre; el primer paso es que el nuevo usuario confirme que los datos que ya existen son correctos. Diseño abierto. Transmitida por Karpo C# | «esto solo significa que ese alta tiene que estar aprobada por el superglobal que tiene que recibir pruebas documentales de que el usuario que dice ser talleres gomez actua realmente como representante de talleres gomez […] el primer paso es que el nuevo usuario de talleres gomez tiene que confirmar que los datos que ya existen son correctos y aportar pruebas de ello» |


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
| D-73 | **La vista por pareja se puede filtrar y ordenar por los datos de la pareja** (número de cliente, bloqueo) desde la versión 1 | «si» a «¿es obligatorio en la primera versión ordenar la lista por número de cliente o por bloqueo?»; confirmada también en la sesión de Trench el 2026-10-16 |
| D-74 | **Para saber qué empresa dio de alta una party basta la auditoría** (quién la creó y en la misma operación que su relación de negocio con esa empresa); **no hay tipo de relación nuevo ni dato nuevo en el modelo común**, y la relación del alta es la de negocio que elige quien da de alta. Aclarada en la sesión de Karpo C# | «no se si hace falta decir quien le dio de alta mas alla de la información histórica del registro. y la relacion se crea como en karpo c#»; y, a la aclaración en la sesión de Karpo C#: «sí, basta la auditoría y la relación de negocio» |
| D-79 | **Al duplicado se le pueden emitir facturas rectificativas (abonos) de facturas ya emitidas**: corregir lo facturado es una obligación. Todo lo nuevo (factura ordinaria, presupuesto, pedido) sigue bloqueado. Precisa D-70 | «la de go» (esta sesión, 2026-10-15), a la pregunta de si un duplicado rechaza también los abonos, como hacía Trench, o permite la rectificativa de una factura ya emitida, como Karpo Go |
| D-80 | **A una ficha fusionada se le permiten también los abonos sueltos**, sin factura de origen. Amplía D-79. Transmitida por la sesión de Trench (2026-10-16) | Contestada allí; Javier pide comprobar que ningún proyecto tenga regla, caso o dato que lo contradiga. Karpo Go no tiene abonos sueltos: no lo contradice |
| D-81 | **Una factura emitida siempre tiene número y no se puede modificar.** El abono que D-79 permite a una ficha fusionada rectifica una factura emitida, es decir, numerada. Transmitida por la sesión de Trench | Contestada allí. En Trench, «emitida» es tener el documento finalizado con número; en Karpo Go ya es así |
| D-82 | **Un borrador de factura se puede modificar aunque su cliente sea una ficha fusionada; lo que no se modifica es la factura emitida.** Precisa la lectura común de D-79 a D-81: «ni modificar lo existente» se refiere a lo emitido | «los borradores de factura si se pueden modificar lo que no se puede modificar son las facturas emitidas» (esta sesión, 2026-10-16) |
| D-83 | **Una regla de negocio rota responde 422** en el contrato HTTP común (Karpo C# usaba 409). Cierra la decisión 12 del contrato | «422» (esta sesión, 2026-10-17), a «una regla de negocio rota, ¿409 o 422?» |
| D-84 | **Los comentarios del código de Karpo Go van en español**, como dice su `CLAUDE.md`; los identificadores, en inglés. Lo existente, en inglés, se traduce aparte | «español» (esta sesión, 2026-10-17) |

**D-70 y D-79 en Trench** (su sesión, 2026-10-16; en su `main`, PR 47): con una ficha fusionada se
rechaza crear, modificar o confirmar pedidos, presupuestos y facturas, y también reservas de divisa
y operaciones financieras. Se permite el abono que rectifica una factura dirigida a esa ficha; otro
abono, 409. Se permite cerrar lo pendiente: consultar, cancelar o anular lo existente, y quitar o
sustituir la ficha en un pedido. Javier lo cerró el mismo día: D-80 y D-81. **En Trench, D-80 a D-82 están en su `main`** (PR 48, 2026-10-16): una
factura emitida no admite líneas, borrado ni volver a borrador, sólo pasar a pagada, cancelada o
anulada; sólo la emisión la deja emitida y con número; el borrador de una ficha fusionada se
modifica, pero no se emite; los datos antiguos en estado emitida o pagada sin documento cuentan como
emitidos.

**Lectura común de D-79 a D-81** (propuesta por Karpo C#, compartida por Trench; no es palabra de
Javier): con una ficha fusionada, en facturación se admiten exactamente (a) el abono que rectifica
una factura emitida, numerada, dirigida a esa ficha y al mismo receptor, y (b) el abono suelto, sin
factura de origen. Nada más: ni facturas nuevas, ni pedidos, ni modificar lo existente. Theros: no
le aplica. Karpo C#: nada lo contradice (aún no tiene fusión; su abono ya admite ir sin factura de
origen).

**D-73 en Trench** (su sesión, 2026-10-17; en su `main`, commit 84a553fd, PR 51): «Relaciones de
clientes / proveedores» se filtran y ordenan en el servidor por los datos de la pareja, con
`POST /api/{customer,supplier}-relationships/search/enriched` y un `PartyFilterDto` ampliado. Sólo
lectura, como en Karpo Go. Clientes: `customerNumber` (exacto), `customerNumberContains` e
`isBlocked` (pedidos o albaranes bloqueados; sin perfil, no bloqueada). Proveedores:
`supplierCodeInTenant` (exacto), `supplierCodeInTenantContains` (no hay número de proveedor) e
`isBlocked` (pedidos o recepciones). Los dos: `maxRiskFrom` y `maxRiskTo`, y los órdenes `isBlocked`
y `maxRisk`. **Los nombres no son los de Karpo Go** (`taxId`, `creditLimit`, `blocked`…): Trench
mantiene los suyos hasta adoptar el contrato común. En el mismo `main` (PR 50), un arreglo: el alta
de factura guarda ya el emisor y el receptor, y un abono con una factura de origen que no existe
responde 404. **D-83 en Trench:** aún no lo ha confirmado Javier en su sesión; allí sigue el 409.

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

### 1.1 sexies bis Fusión de fichas (sesión de Karpo C#)

Tomadas en la sesión de Karpo C# el 2026-10-20, a preguntas de esa sesión, y **transmitidas por
ella**; no se han confirmado aquí. La lectura es de esa sesión.

| # | Decisión (lectura) | Lo que dijo Javier |
|---|---|---|
| D-95 | **Fusionar fichas exige un permiso propio** (`party.merge` en Karpo C#), distinto del de modificar una party | «sí» |
| D-96 | **Quien fusiona tiene que poder escribir en las dos fichas**, la que cede y la superviviente; si no, 403 | «sí» |
| D-97 | **No se encadenan fusiones**: una ficha ya fusionada no puede ser superviviente ni volver a fusionarse (422). Para A→B→C se deshace la primera o se fusiona A directamente en C | «no», a «¿se encadenan las fusiones?» |

**Pregunta para Trench** (de la sesión de Karpo C#): su resolución de la superviviente por puntero
admite cadenas; conviene comprobar que no tiene ninguna.

**Más decisiones sobre la fusión y la visibilidad** (sesión de Karpo C#, 2026-10-21, a preguntas de
esa sesión; **transmitidas**):

| # | Decisión (lectura) | Lo que dijo Javier |
|---|---|---|
| D-98 | **El permiso de fusionar se concede al administrador de organización una sola vez**: si el administrador global se lo retira, no vuelve en el siguiente arranque. Igual que `Parties.Party.UpdateRelated` | «sí» |
| D-99 | **Una empresa interna no puede ceder en una fusión** (no puede ser el duplicado) | «sí» |
| D-100 | **Cualquier listado de registros excluye por defecto los de fichas fusionadas** (pedidos, facturas, envíos, cobros…), y hay que pedirlos con el filtro (`includeMergedParties`). Amplía D-64, que lo decía de las parties | «los duplicados se filtran y es necesario solicitarlos en los filtros cualquier registro no solo pedidos» |
| D-101 | **Los nombres de las empresas internas del grupo son públicos dentro de la instalación.** Idea sin diseñar: un catálogo de parties público entre empresas, en el que cada empresa decide si participa | «sí, aunque creo que hay un catálogo de parties público entre empresas y la empresa puede decidir si quiere estar o no» |
| D-102 | **La autorización de la instancia financiera se aplaza** (en Karpo C#, FinancialKernel) | «ok» |
| D-103 | **Los contextos que preguntan a Parties si una ficha está fusionada (Pedidos, Facturación, Contabilidad…) lo hacen con una identidad de servicio propia, limitada a esa consulta**, no con los permisos del usuario: quien vende o factura no necesita el permiso de leer terceros | «A» |
| D-104 | **Al trasladar una relación de cliente o proveedor en una fusión (D-93), las direcciones por defecto de su perfil comercial** (envío, facturación, domiciliación; en proveedor, envío y facturación de origen) **se vacían en la superviviente** y la vista previa avisa de que hay que elegirlas; deshacer la fusión las devuelve | «A» |

### 1.1 sexies El autónomo y los identificadores (sesión de Trench)

Tomadas en la sesión de Trench el 2026-10-17 y **transmitidas por ella**; no se han confirmado aquí.
La lectura es de esa sesión.

| # | Decisión (lectura) | Lo que dijo Javier |
|---|---|---|
| D-85 | **El autónomo es una persona con el rol «Self-Employed»**, no una organización legal: una ficha es persona u organización, nunca las dos. El rol es sólo de personas y tiene vigencia; sus datos de actividad (nombre comercial, epígrafe, alta) van en un perfil del rol; su NIF es su documento de identidad. La importación crea personas cuando el NIF es un DNI o un NIE. Precisa D-53 y contesta su pregunta abierta | «sí» a esa propuesta. Había pensado en «persona y organización legal», pero rompía la invariante persona u organización. En Trench, 55 de 3.610 organizaciones legales tienen NIF de persona y 13 duplican a una persona con el mismo documento: se corrigen antes de converger |
| D-86 | **Una persona empresa del grupo (D-53) se identifica con su documento de identidad.** Sus datos registrales se ocultan por ahora y se decide más adelante; se empieza sólo por el catálogo y los listados, hasta que haya un caso real | Transmitida |
| D-87 | **Qué campo es identificador se marca explícitamente en el código**; la herramienta sólo lo propone | Transmitida (su D1) |
| D-88 | **El valor normalizado de un identificador se guarda en una tabla genérica de valores**, que sirve también para correo, teléfono, IBAN y campos personalizados | Transmitida (su D2) |
| D-89 | **El nombre solo nunca basta para decir que dos fichas son la misma**: sólo propone candidatas junto con otro dato | Transmitida (su D3) |
| D-90 | **Un campo personalizado no puede ser identificador en la primera versión del contrato común**; ese nombre de propiedad queda reservado | Transmitida (su D5) |
| D-91 | **Los 38 CIF repetidos de Trench se fusionan** antes de converger. Precisa D-50 | Transmitida |
| D-92 | **Un dato con formato erróneo avisa y se guarda como incidencia, sin impedir guardar, en el correo, el teléfono, el IBAN y los campos personalizados; el documento de identidad inválido se sigue rechazando** (D-5). En las importaciones sigue D-9: la party con el documento rechazado entra marcada con el error, con un filtro para encontrarla. Resuelve el choque entre el D4 de Trench y D-5 | «b» (en la sesión de Trench, 2026-10-18), a las lecturas que se le plantearon desde aquí |
| D-93 | **Al fusionar, cada relación de negocio vigente de la ficha fusionada (cliente o proveedor con una empresa) pasa a la superviviente, con su perfil comercial, si la superviviente no tiene ya esa relación con esa misma empresa**; si ya la tiene, se revisa caso por caso. Precisa D-64 («las relaciones se quedan donde están») para ese caso | «sí» (en la sesión de Trench, 2026-10-18). Motivo de esa sesión: en Trench, 41 de las 44 relaciones de las fichas a fusionar no las tiene la superviviente, y con el bloqueo de facturación y pedidos quedarían inservibles |

**D-85 y D-93 en Trench** (su sesión, 2026-10-18; en su `main`, commit c6b3c233). D-85, hecho en
parte (PR 53): las importaciones crean una persona con el rol «Self Employed» cuando el NIF es un
DNI o un NIE, y la reutilizan si ya existe con ese documento; faltan el perfil del rol y corregir
las 55 organizaciones con NIF de persona ya importadas. D-93, hecho (PR 54): al fusionar, las
relaciones vigentes de cliente o de proveedor pasan a la superviviente con su perfil comercial si
no las tiene ya con esa empresa, y deshacer la fusión las devuelve; faltan que la pantalla de
fusión las enseñe y decidir las direcciones por defecto del perfil trasladado.

**D-91 y D-93 aplicados en los datos de Trench** (su sesión, 2026-10-20; su PR 58). Tras una copia
de seguridad y un ensayo en un contenedor aparte, se fusionaron en su PostgreSQL local las 38 fichas
con el CIF repetido: 35 relaciones de negocio pasaron a la superviviente y 3 se quedaron porque ya
las tenía; no se perdió ninguna. Quedan 6 CIF repetidos a propósito: parecen empresas distintas con
el CIF mal dado de alta en Sage y se corrigen allí. En Trench, «un documento, una party» (D-50) se
cumple salvo en esos 6 casos, identificados.

### 1.1 septies Decimales (sesión de Theros)

Propuesta por la sesión de Theros y decidida en esta sesión el 2026-10-20.

| # | Decisión (lectura) | Lo que dijo Javier |
|---|---|---|
| D-94 | **El framework tiene un tipo de columna decimal `{dec:p,s}` (NUMERIC/DECIMAL/NUMBER; texto en SQLite), se escribe y se lee sin float, y se puede filtrar y ordenar por valor. Las columnas decimales que hoy son texto en los contextos de Karpo Go se pasan a numéricas (tarea aparte), para parecerse a Trench y Karpo C#** | «1 sí, 2 a» (esta sesión, 2026-10-20), a la propuesta de Theros |

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
| D-7, D-10, D-28 | **Talleres Pérez deja de ser cliente de Acme en marzo y vuelve en septiembre. ¿Vuelve con su número y su bloqueo por impago, o empieza de cero?** En Trench el perfil comercial cuelga de cada relación: empezaría de cero. En Go el límite y el bloqueo viven en `receivables` por pareja y se conservan; el número queda guardado en la relación terminada, nadie más puede usarlo, y hay que volver a dárselo a mano a la relación nueva. (Pregunta de la sesión de Theros) |
| D-31 | Si hay relaciones de cliente de quien no tiene el rol de cliente, ¿qué significa entonces el rol? (va con D-17) |
| D-39 | ¿Qué variantes cuentan en Karpo Go? Hoy es un solo proceso (D-2) |
| D-40 | Go prueba además en MySQL y SQLite. ¿Siguen como motores de prueba, o sobran? Dato de Theros: prueba lo generado en SQLite, PostgreSQL, SQL Server y Oracle; su plantilla de Go dice admitir MySQL sin ninguna prueba. Si sobran, en Theros SQLite quedaría sólo como motor embebido de las pruebas sin contenedores |
| D-44 | ¿Se termina entonces el broker NATS de Go, que está aparcado en una rama? |
| D-47 | Cuándo y cómo lo hereda Go (Security de Go tiene su propio diseño de contraseñas) |
| — | Supresión de datos personales (RGPD): sin decidir, pendiente de consulta legal |
| D-62 | **Dos tipos de relación con los lados distintos.** «Agent Relationship» (`…0008`): Agent → Internal Organization en Trench, Agent → Customer en Go. «Organization Rollup» (`…0005`): Department → Internal Organization en Trench; en Go, cualquier unidad organizativa → cualquier organización |
| D-62 | «Organization Unit» (`…0105`) cuelga de «Legal» en las semillas SQL de Trench y de «Organization» en su catálogo de código y en Karpo Go. «Entidad financiera» (`…00F1`) está en las semillas de Trench y otro script la borra |
| D-62 | «Salesperson» (`…0001-…00C3`) es en Trench un rol de pedido, no de party; en Go es un rol de party. La relación `prospect` sólo existe en Go |
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
| D-15 | La afiliación de una party a una empresa termina con su relación, y la empresa deja de verla. **Excepción mientras dura una fusión** (D-93): la ficha fusionada sigue afiliada a la empresa por la relación que pasó a la superviviente, para que la empresa la siga encontrando (D-64) y pueda deshacer la fusión (D-96); al deshacerla, esa afiliación se cierra, vuelva la relación o no | Esperar las tres respuestas |
| D-16, D-18 a D-23 | Hecho sólo el alta de tipos de rol y de relación sin campos | El mecanismo entero, contra el contrato de Trench. Antes hay que resolver que Go guarda un agregado borrando y reinsertando sus filas hijas |
| D-17 | «Cliente» es un rol **y además** un tipo de relación; la relación exige que el rol exista antes; el límite y el bloqueo están en `receivables` y el número de cliente en la relación (PR 17) | Esperar las cuatro respuestas. Los datos de la pareja ya están donde D-17 dice; lo que cambia es el papel del rol |
| D-28 | No hay un perfil comercial único: el número está en Parties, el límite y el bloqueo en `receivables`, el perfil de proveedor en `purchases` | Decidir si en Go «fuente única» es un contexto o basta con que cada dato tenga un solo dueño |
| D-29 | No hay identificadores externos en la pareja. La importación guarda la correspondencia con Sage y Apiscore en sus referencias (`imports`), por registro | Decidir si eso cumple |
| D-30 a D-35 | No hay búsqueda de relaciones, ni vista por pareja, ni expresión de filtro. Go no tiene front | La búsqueda de relaciones y la expresión, cuando se apruebe el contrato HTTP |
| D-24 | Go sirve sus rutas, no el contrato | El contrato está sin aprobar |
| D-39, D-44 | Un solo proceso; el broker NATS está aparcado en una rama sin fusionar | Decidir qué variantes cuentan en Go |
| D-48 | **Hecho** (rama `feat/parties-roles-terminados`): `GET /api/parties?role=…&includeEndedRoles=true` | — |
| D-49 | **Hecho** (PR 21, más los arreglos de la revisión en `feat/parties-desactivar-con-relaciones`): el caso de uso asigna el rol que falte antes de establecer la relación. No se asigna «Internal Organization» ni un rol que cuelgue de él (D-62), ni ningún rol a una party que quien llama no puede escribir (decidido en esta sesión tras la revisión independiente) | Que Javier confirme la segunda restricción |
| D-55, D-56 | `EXTENSIBILIDAD.md` corregido; el mecanismo no está implementado | El mecanismo |
| D-59 | La empresa no tiene acrónimo | Dato nuevo, requisito previo de la extensibilidad |
| D-50 | Cumple: índice único sobre tipo, país y número | — |
| D-54, D-62 | **Hecho** (PR 20 y rama `feat/parties-catalogo-comun`, migraciones 19 y 21): «Team» bajo «Informal»; sembrados Club, Group y los cuatro tipos de relación | «Duplicate Of», cuando se resuelva el choque de identificador (§2) |
| D-53 | **Hecho** (rama `feat/parties-persona-proveedor-o-empresa`, migración 20): los dos tipos cuelgan de la raíz. Los demás contextos guardan a la empresa por identificador y no cambian. Una persona empresa no puede tener unidades organizativas debajo | Que una persona empresa use su documento de identidad como principal (D-86): la regla de documentos de D-85 ya lo deja, no hay más. Go no tiene importación |
| D-85 | **Hecho** (rama `feat/parties-autonomo`, migración 25): el autónomo es una persona con el rol «Self Employed». Al añadir un documento, una organización sólo lleva TXID u OTHR y una persona todos salvo el TXID español (el CIF); 422 `parties.document_not_for_kind`; lo guardado antes se lee tal cual. El perfil del rol (nombre comercial, epígrafe, fecha de alta) en `PUT /api/parties/{id}/roles/{roleId}/self-employed`, sólo en «Self Employed» o un rol que cuelgue de él, con `Parties.Party.Update` y el ámbito de cualquier cambio de la party; el rol lo enseña en `selfEmployed`. Detalle en [PARTIES.md](PARTIES.md) §19 | La importación (Go no tiene). Las parties ya guardadas que no cumplen la regla de documentos no se corrigen solas |
| D-95, D-96 | **Hecho** (rama `feat/parties-permiso-fusion`): fusionar es establecer una relación «Duplicate Of», y marcarla o deshacerla pide el permiso propio `Parties.Party.Merge` (lo tiene el administrador de organización, no el usuario estándar) y poder cambiar las dos parties, la que cede y la superviviente; si no, 403 | — |
| D-97 | Cumple: una party marcada como duplicado no es operable, así que no puede ser superviviente ni marcarse otra vez (422) | — |
| D-98 | No: en Go el permiso llega al administrador de organización por la regla estándar, que se recalcula para los roles del sistema en cada sincronización del catálogo; una retirada no se conserva | Conceder una sola vez (y lo mismo para `UpdateRelated`, que tiene el mismo problema) |
| D-99 | **Hecho** (rama `feat/parties-empresa-interna-no-cede`): marcar como duplicado una empresa interna es 422 `parties.merge_internal_organization`, también para el administrador global | — |
| D-100 | Sólo en la búsqueda de parties y en la vista por pareja (`includeMergedParties`) | Los demás listados (pedidos, facturas, envíos, cobros…) |
| D-101 | Por comprobar qué ve hoy de una empresa interna un usuario de otra | El catálogo público entre empresas, sin diseñar |
| D-102 | No aplica: Go no tiene una autorización propia de la instancia financiera | — |
| D-103 | Cumple de otra forma: el puerto `TaxIdentities` de Parties, que lleva la marca de duplicado (`blocked`), lee el repositorio dentro del proceso, sin los permisos del usuario. No hay identidad de servicio porque no hay llamada remota | Una identidad de servicio, cuando los contextos vayan en procesos separados |
| D-104 | No aplica hoy: el perfil comercial de Go (crédito, condiciones de venta, perfil de proveedor) no tiene direcciones por defecto, y Go no tiene vista previa de la fusión | Cuando el perfil tenga direcciones |
| D-64 | **Hecho** (rama `feat/parties-duplicado-bloqueado`, migración 24): marcar con «Duplicate Of» bloquea la party; se lee, la búsqueda la deja fuera salvo `includeMergedParties=true` (nombre de Trench, su PR 45), no admite cambios ni relaciones nuevas; terminar la relación lo deshace. Lo marca quien puede modificar el duplicado; una empresa interna, sólo el global. Las relaciones de negocio, véase D-93 | — |
| D-93 | **Hecho** (rama `feat/parties-fusion-relaciones`; migración 27 de Parties y 4 de Compras): al marcar el duplicado, cada relación vigente de cliente o proveedor de la que cede con una empresa pasa a la superviviente si ésta no la tiene (se termina y se establece una igual, con el número de cliente, que recuerda de cuál viene); si la tiene, se queda en la que cede y la respuesta la lista en `merge.kept`. Cobros, Pedidos y Compras copian el perfil de la pareja al oír `parties.relationship-transferred.v1`, si la superviviente no tiene uno. Fusionar y deshacer, sólo ahora (con un minuto de margen). Deshacer la fusión devuelve las relaciones (403 si alguna es de una empresa en la que no escribe quien deshace); los perfiles copiados se quedan en la superviviente y en la vuelta no se copia nada (decidido en esta sesión). Sólo pasan las de empresas en las que escribe quien fusiona (decisión conservadora de esta sesión, pendiente de Javier). Detalle en [PARTIES.md](PARTIES.md) §17 | Que Javier confirme el límite por ámbito y diga qué pasa con un perfil antiguo de la superviviente (§17). Que la pantalla de fusión enseñe lo trasladado (Go no tiene front) |
| D-92 | El documento de identidad inválido se rechaza (cumple D-5). Go valida hoy el IBAN (`vocab`) y lo rechaza si está mal formado | Comprobar qué rechaza hoy Go de correo, teléfono e IBAN y pasarlo a aviso con incidencia |
| D-70 | **Hecho** (PR 30, en borrador): Pedidos rechaza presupuestos, pedidos y confirmaciones para un duplicado; Facturación, preparar y emitir facturas ordinarias (las rectificativas, no: decidido en esta sesión). Lo saben por el contrato de identidades fiscales de Parties (`blocked`) | Compras, Cobros y Tesorería no lo miran |
| D-70, D-79, D-82 | Cumple: rechaza crear y emitir (o confirmar) para un duplicado y deja modificar los borradores | — |
| D-77, D-78 | **Hecho** (rama `feat/parties-empresa-interna-solo-global`): las altas anteriores al 2026-10-15 no cuentan como dadas de alta por su empresa; sólo el administrador global da «Internal Organization» | El circuito de D-78 (petición con pruebas, confirmación de datos, aprobación del global), diseño |
| D-51, D-73 | **Hecha la lectura** (PR 2 de karpo-fw-go, fusionado el 2026-10-17): `POST /api/customer-relationships/search` y `GET …/{id}`, y lo mismo para proveedores; una fila por pareja con número de cliente, límite y bloqueos de Cobros, Pedidos y Compras, filtrada y ordenada por ellos. Talleres Pérez sale una vez por empresa | El alta de la pareja; los campos que Go no tiene (código externo, delegación, motivo del bloqueo) |
| D-74 | **Hecho** (rama `feat/parties-alta-por-auditoria`): Go lo deduce de la auditoría, como pidió Javier el 2026-10-14 («prefiero que lo deduzca de los registros de auditoria tambien en go»): la party y la relación que la afilia nacieron juntas si su primer registro comparte el identificador de operación. Sin columna propia | — |
| D-75, D-76 | Cumple: un usuario que no es administrador global sólo da de alta con afiliación, y `Parties.Party.UpdateRelated` es un permiso del rol | — |
| D-65, D-66 | **Hecho** (rama `feat/parties-quien-modifica`; desde D-74, sin la migración 25): se sabe si la party nació con el alta por la auditoría; si no, modifica quien tenga `Parties.Party.UpdateRelated` (de entrada, el administrador de organización); una empresa interna de otros sólo la modifican sus usuarios. Las afiliaciones anteriores a la migración quedan como relacionadas | El permiso vale para todas las empresas del usuario, no por empresa |
| D-67 | **Hecho en parte:** una relación de familia no afilia a nadie | La marca por campo de los datos públicos de una persona, diseño |
| D-40 | Cumple los tres, y prueba además en MySQL y SQLite | — |
| D-45 | Cumple: cada caso de uso comprueba su permiso | — |
| D-46 | Una sesión de orquestación; revisión independiente de lo delegado y de los cambios grandes. **No** se hacen tres pasadas completas en verde antes de cada subida, sino una local y una contra los motores | Ajustar si Javier lo quiere así también para los PR en borrador |
| D-47 | Security de Go no tiene el rescate ni el restablecimiento por un administrador | Planificarlo con el diseño de Karpo |
| D-5, D-9, D-10, D-14, D-25, D-26 | **Cumple** (PR 13 a 18) | — |
| D-1, D-2, D-3, D-6 (caso sin atributos), D-7, D-11 | **Cumple** | — |
| D-94 | **Hecho: el tipo (PR 9) y las columnas de los contextos** (rama `feat/contextos-decimales`). El tipo: `{dec:p,s}` en `RenderDDL`, escritura y lectura sin float, comparación y orden por valor, `sqlrepo.AlterColumnToDecimal` (detalle en [ARQUITECTURA.md](ARQUITECTURA.md) §4). Las columnas: 126 columnas de 19 contextos pasan de texto a `{dec:p,s}` con una migración «decimal columns» por contexto (la siguiente a la última), se declaran en `Mapping.Decimals` y se escriben como `vocab.Decimal`. Cada dominio valida la escala de su columna (400). Un valor que aun así no cabe es `sqlrepo.ErrDecimalDoesNotFit`, un error interno; la precisión y la escala de cada una, y de dónde salen, en [ESQUEMA-MIGRACIONES.md](ESQUEMA-MIGRACIONES.md), «Columnas decimales de los contextos» | Divergencias en (p,s): productos y tarifas siguen a Karpo C# (18,4) y no a Trench (18,2), que tiene menos decimales que el dominio; en las líneas de nómina se sigue el DDL de los dos (18,4 y 18,2), no su EF (19,4). |
