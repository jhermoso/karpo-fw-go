# Registro único de decisiones comunes

> **Qué es.** El sitio donde se anota cada decisión de Javier que vale para más de un proyecto
> (Trench, Karpo C#, Karpo Go, Theros). Lo mantiene la sesión de orquestación de Karpo Go.
> Javier lo encargó así el 2026-10-13, a través de la sesión de Karpo C# («acepto usar un único
> registro para decisiones comunes y que sea Karpo Go»); **falta que lo confirme en esta sesión**.
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
   ella, sabiendo que es transmitida.
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
| D-17 | 2026-10-13 | **Los clientes son relaciones, no roles.** Un cliente es la pareja parte–empresa con su perfil comercial; las condiciones comerciales (riesgo, bloqueo, número de cliente) son de la pareja. Es lo que Trench tiene como «Relaciones de clientes / proveedores». **Transmitida** por Karpo C#. **Preguntas abiertas** (§2) | Trench |

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

### 1.3 Contrato HTTP y datos

| # | Fecha | Decisión | Sesión |
|---|---|---|---|
| D-24 | 2026-10-13 | El contrato HTTP común es **uno nuevo y limpio, escrito una vez**, que sirven los dos backs, empezando por lo que usan las pantallas principales | Karpo Go |
| D-25 | 2026-10-13 | En una importación, el documento español con el dígito de control mal se conserva como «otro documento», además de marcar a la party: por él se reconoce a quien ya se cargó | Karpo Go |
| D-26 | 2026-10-12 | Geografía: se separa el nivel estadístico (NUTS) de la «división principal» de cada país; provincia e isla son delimitaciones distintas | Karpo Go |

### 1.4 Forma de trabajo

| # | Fecha | Decisión | Sesión |
|---|---|---|---|
| D-27 | 2026-10-13 | Hay un único registro de decisiones comunes y lo mantiene Karpo Go (este documento). **Transmitida** por Karpo C# | Karpo C# |

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
| — | ¿Un documento puede estar en dos parties? Go lo impide; Trench tiene duplicados reales y Karpo C# no tiene índice único |

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

**Karpo C#** (2026-10-13): está haciendo su comprobación; ha adelantado que hoy el **estado** de la
relación decide la visibilidad (leer por identificador exige relación «activa»), lo que toca a
D-11 y a D-15 a la vez.

## 4. Divergencias de Karpo Go

Lo que Go no cumple todavía de lo decidido. Cada sesión lleva la suya en su repositorio.

| Decisión | Qué hace Go hoy | Qué falta |
|---|---|---|
| D-4, D-8, D-13 | Desactivar una party no comprueba nada. No hay comando de anular ni declaración del ciclo de vida | Todo; D-13 necesita diseño |
| D-15 | La afiliación de una party a una empresa termina con su relación, y la empresa deja de verla | Esperar las tres respuestas |
| D-16, D-18 a D-23 | Hecho sólo el alta de tipos de rol y de relación sin campos | El mecanismo entero, contra el contrato de Trench. Antes hay que resolver que Go guarda un agregado borrando y reinsertando sus filas hijas |
| D-17 | «Cliente» es un rol **y además** un tipo de relación; la relación exige que el rol exista antes; el límite y el bloqueo están en `receivables` y el número de cliente en la relación (PR 17) | Esperar las cuatro respuestas. Los datos de la pareja ya están donde D-17 dice; lo que cambia es el papel del rol |
| D-28 | No hay un perfil comercial único: el número está en Parties, el límite y el bloqueo en `receivables`, el perfil de proveedor en `purchases` | Decidir si en Go «fuente única» es un contexto o basta con que cada dato tenga un solo dueño |
| D-29 | No hay identificadores externos en la pareja. La importación guarda la correspondencia con Sage y Apiscore en sus referencias (`imports`), por registro | Decidir si eso cumple |
| D-30 a D-35 | No hay búsqueda de relaciones, ni vista por pareja, ni expresión de filtro. Go no tiene front | La búsqueda de relaciones y la expresión, cuando se apruebe el contrato HTTP |
| D-24 | Go sirve sus rutas, no el contrato | El contrato está sin aprobar |
| D-39, D-44 | Un solo proceso; el broker NATS está aparcado en una rama sin fusionar | Decidir qué variantes cuentan en Go |
| D-40 | Cumple los tres, y prueba además en MySQL y SQLite | — |
| D-45 | Cumple: cada caso de uso comprueba su permiso | — |
| D-46 | Una sesión de orquestación; revisión independiente de lo delegado y de los cambios grandes. **No** se hacen tres pasadas completas en verde antes de cada subida, sino una local y una contra los motores | Ajustar si Javier lo quiere así también para los PR en borrador |
| D-47 | Security de Go no tiene el rescate ni el restablecimiento por un administrador | Planificarlo con el diseño de Karpo |
| D-5, D-9, D-10, D-14, D-25, D-26 | **Cumple** (PR 13 a 18) | — |
| D-1, D-2, D-3, D-6 (caso sin atributos), D-7, D-11 | **Cumple** | — |
