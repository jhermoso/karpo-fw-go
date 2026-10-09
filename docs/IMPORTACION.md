# Contexto Importación de datos

Port de la importación de C# (`OrganizationImportService`, sus lectores de Personio, Apiscore y
Sage, `import_run` y `legacy_reference`) al contexto `contexts/imports`.

Sirve para **cargar en Karpo lo que hoy vive en otros sistemas**: se le dan unos ficheros, dice qué
haría con ellos (vista previa), lo hace (ejecución) y deja escrito qué pasó línea a línea y en qué
se convirtió cada clave del sistema de origen.

> **Alcance.** Están el motor, el registro de ejecuciones, las referencias, la fuente Personio y,
> desde el 2026-10-08, **sus cinco cargadores en el anfitrión**: una importación de Personio crea
> empresas, departamentos, centros, personas y empleos. Ver «Cargadores de Personio». También
> están **las fuentes de Sage y de Apiscore con sus cargadores** (ver «Fuentes de Sage y de
> Apiscore») y **la del calendario de festivos** (ver «Fuente del calendario de festivos»).

## Método

| Criterio | Peso |
|---|---|
| **U** Utilidad (uso real en contextos, sin Fw ni tests) | 40 % |
| **C** Corrección de la implementación | 25 % |
| **D** Diseño de la firma | 20 % |
| **G** Encaje en Go | 15 % |

Puntuación de 1 a 5 por criterio; nota ponderada sobre 100. **Mantener** ≥ 70 · **Modificar** 45–69 ·
**Retirar** < 45 · **Sustituido**.

## Estado del C#

Unas 13.900 líneas en 50 ficheros (18.850 con pruebas). Carga solo datos maestros: empresas,
departamentos, centros, empleados, clientes, proveedores, IVA, plan de cuentas y cuentas bancarias.

- **Un único orquestador de 1.490 líneas** con doce pasos fijos. Cada fuente solo aporta un lector
  y un traductor; añadir una clase de dato nueva es tocar el orquestador, el contrato de 33
  contadores y las dos pasarelas.
- **Dos pasarelas que hacen lo mismo de forma distinta:** una por HTTP (2.384 líneas, unas 40
  rutas) y otra en proceso (1.143 líneas) en la que cuentas bancarias, puestos y todo lo de
  Apiscore son métodos vacíos que devuelven «omitido».
- **Nada es atómico.** Cada entidad son varias escrituras sueltas; una ejecución interrumpida deja
  agregados a medias (el propio código documenta una cuenta contable huérfana).
- **Una ejecución que se rompe queda «en curso» para siempre:** nadie escribe `failed`. Y el
  estado `completed_with_errors` que sí se escribe no está entre los del dominio.
- **Los mensajes no dicen dónde:** `import_run_message` tiene fichero, línea, entidad y código,
  pero el orquestador solo rellena la gravedad y un texto. El resumen guarda 11 de 33 contadores.
- **Clientes y proveedores se reconocen por el nombre**, aunque el contrato diga que por el NIF:
  dos homónimos se funden. Apiscore escribe referencias que luego nunca lee.
- **Un error 500 al buscar se toma por «no existe»** y provoca un alta duplicada.
- **«Personio» y «Apiscore» no son conexiones:** son CSV preparados a mano por guiones que no están
  en el repositorio. Sage se lee en vivo de su SQL Server con una contraseña de `sa` en el código.
- **Seguridad:**
  - se puede pedir al servidor que lea **cualquier ruta de su disco** (`OrgUnitsPath`);
  - sin límite de tamaño ni de filas;
  - las rutas `/v1/import/*` (abrir y cerrar ejecuciones, **escribir referencias**) solo piden
    estar autenticado: cualquiera puede desviar a qué entidad apunta una clave;
  - la cabecera `X-Import-Run` se acepta de cualquiera, así que cualquier petición puede marcar
    sus cambios como «importados».
- **Datos de un cliente dentro del código:** el nombre de la empresa importadora, 73 bancos, 24
  alias, 24 cuentas contables, 10 oficinas y 23 puestos con 33 alias, con sus identificadores.
- **La vista previa no es fiable:** ejecuta el mismo código con identificadores inventados, y lo
  que depende de ellos sale distinto que al ejecutar.
- **Sin ejecución en segundo plano, sin progreso y sin pantalla:** ninguna parte del frontal llama
  a estas rutas.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `legacy_reference` (clave de origen → entidad) | 5 | 3 | 4 | 4 | **83** | Mantener → agregado `Reference` (fuente, clase, ámbito, clave → entidad). Se lee **siempre** y manda sobre la clave natural |
| 2 | `PersonioCsvReader` + `PersonioMapper` | 5 | 3 | 3 | 3 | **76** | Mantener → fuente `Personio`: mismas columnas y alias, pluriempleo, nombre preferido; sin la empresa «por defecto» ni la tabla de puestos |
| 3 | Tres lectores de CSV escritos a mano | 4 | 3 | 2 | 4 | **67** | Modificar → un solo `ParseTable` sobre `encoding/csv`, con número de línea y columnas por alias |
| 4 | `import_run` + `import_run_message` | 4 | 2 | 3 | 4 | **66** | Modificar → agregado `Run` que **siempre se cierra**; mensajes con fichero, línea, clase, clave y código; un contador por clase de registro |
| 5 | Procedencia en la auditoría (`ImportContext`, cabecera `X-Import-Run`) | 4 | 2 | 3 | 4 | **66** | Modificar → la pone el motor en el contexto de cada registro (`WithImportProvenance`, ya en el framework). No hay cabecera |
| 6 | `ISourceReader` / `ISourceMapper` | 4 | 3 | 2 | 3 | **64** | Modificar → puerto `Source`: ficheros con un papel → registros neutros, sin tocar base de datos |
| 7 | Vista previa (`PreviewAsync`) | 4 | 3 | 2 | 3 | **64** | Modificar → `Preview` solo busca: «se crearía» o «ya existe». No escribe ni inventa identificadores |
| 8 | Sage (SQL en vivo, seudónimos, CSV por concepto) | 4 | 3 | 2 | 3 | **64** | Modificar → fase 2: una fuente sobre los CSV por concepto. El servidor no lee bases de datos ajenas |
| 9 | `OrganizationImportService` (doce pasos fijos) | 5 | 2 | 1 | 2 | **60** | Modificar → motor genérico: aplica los registros clase por clase, en el orden que pide la fuente |
| 10 | `IOrganizationImportGateway` (HTTP y en proceso) | 4 | 2 | 1 | 2 | **52** | Modificar → puerto `Loader`, uno por clase de registro, que el anfitrión escribe con los casos de uso del contexto dueño |
| 11 | Rutas de administración (CSV en línea, rutas del servidor, confirmación `main`) | 3 | 2 | 2 | 3 | **51** | Modificar → ficheros en la petición, con límite; permisos propios; sin rutas de disco |
| 12 | Apiscore (tres CSV del disco del servidor, tablas de Maccorp) | 3 | 2 | 2 | 3 | **51** | Modificar → fase 2: una fuente del anfitrión, con sus equivalencias como datos |
| 13 | Consola `Sage.Import` (`bulkload`, `export`, `verify`, `postimport`) | 2 | 3 | 2 | 3 | **48** | Modificar → fuera de este contexto: son herramientas de base de datos, no importación |
| 14 | Paginación de Apiscore (`Skip`/`Take`) y alta de usuarios al terminar | 2 | 2 | 2 | 3 | **43** | Retirar → el motor no pagina a mano; dar de alta usuarios es cosa de Security |
| 15 | Tablas fijas (oficinas, bancos, puestos, cuentas) | 2 | 2 | 1 | 3 | **39** | Retirar → son datos de cada instalación; los resuelve el cargador o la referencia |
| 16 | Rutas `/v1/import/*` (escribir referencias y ejecuciones) | 2 | 1 | 1 | 3 | **34** | Retirar → solo el motor escribe ejecuciones y referencias |

## Diseño en Go

Tres piezas, y solo la primera y la última saben de un sistema concreto:

- **Fuente** (`domain.Source`): convierte los ficheros de un sistema en **registros**. Un registro
  es una clase (`legal-entity`, `department`, `work-center`, `person`, `employment`), la clave por
  la que lo conoce el origen (dentro de un ámbito) y sus campos como texto. La fuente no consulta
  nada: lo que no puede usar lo devuelve como mensaje con su fichero y su línea.
- **Motor** (`application.Service`): aplica los registros clase por clase, en el orden que pide la
  fuente. Para cada uno:
  1. mira si la clave ya tiene **referencia**; si la tiene, esa es la entidad;
  2. si no, pregunta al cargador si existe por su clave natural (`Find`);
  3. al ejecutar, llama a `Apply` (crear o poner al día) y guarda la referencia.
  Un registro que falla se anota (con el código del error del contexto dueño) y los demás siguen.
- **Cargador** (`domain.Loader`), uno por clase de registro: lo escribe el anfitrión con los casos
  de uso del contexto dueño. Así una importación pasa por los mismos permisos y las mismas reglas
  que cualquier otra llamada, y cada registro es atómico en su contexto. Los cargadores ven las
  referencias de la ejecución (`Refs`), incluido lo que se acaba de crear.

Qué se guarda:

- **`Run`**: fuente, ficheros, quién, cuándo empezó y acabó, estado, un contador por clase (leídos,
  creados, actualizados, sin cambios, omitidos, fallidos) y hasta 5.000 mensajes (los demás se
  cuentan). Estados: `running`, `succeeded`, `completed-with-errors`, `failed`.
- **`Reference`**: fuente + clase + ámbito + clave → tipo e identidad de la entidad, la ejecución
  que la enlazó y la última que la creó o cambió. Única por fuente, clase, ámbito y clave.

Reglas:

- **Una ejecución siempre se cierra.** Si un cargador revienta o quien la lanzó se va, queda
  `failed` con el motivo y con lo que llevaba hecho. Si el proceso muere, `CloseStale` cierra las
  que llevan más de una hora (configurable, mínimo cinco minutos); si resulta que seguía viva,
  termina como `failed` pero conserva sus contadores.
- **Una ejecución por fuente a la vez** (422 `imports.run_in_progress`). Las vistas previas no
  cuentan.
- **Límites:** 5 MB entre todos los ficheros y 100.000 registros por ejecución.
- **Fichero ilegible** (comillas sin cerrar, sin cabecera): 422 y no se abre ejecución.
- **Clase sin cargador:** sus registros se omiten con un aviso; `GET /api/imports/sources` dice
  qué clases de cada fuente no tienen quien las cargue.

Fuente **Personio** (dos ficheros: `org-units`, obligatorio, y `people`):

- `unitType`: `InternalOrganization` → empresa; `Department` → departamento; cualquier otro →
  centro de trabajo. Dos departamentos con el mismo nombre en dos empresas son dos.
- Nombre de la persona: el preferido si lo hay; si no, nombre y apellidos.
- **Pluriempleo** (`A / B (PLURIEMPLEO)`): una persona y un empleo en cada empresa (nombre exacto
  o el único que empiece igual); solo el primero conserva departamento, centro y puesto.
- Fechas de alta y baja con los alias de C# (`hireDate`, `fechaAlta`, `Fecha de contratación`…), en
  ISO o día/mes/año.
- Se rechaza la línea, con su código: unidad sin nombre o sin tipo, empresa no declarada, persona
  sin número o sin nombre, número repetido, fecha ilegible o baja anterior al alta.

Además:

- **Permisos** (`application/permissions.go`): `Imports.Run.Read`, `Imports.Run.Execute` (también
  para la vista previa y para cerrar las colgadas) e `Imports.Reference.Read`.
- **Rutas:** `GET /api/imports/sources`, `POST /api/imports/preview`, `POST /api/imports/runs`,
  `GET /api/imports/runs`, `GET /api/imports/runs/{id}`, `POST /api/imports/runs/close-stale`,
  `GET /api/imports/references`.
- **Evento publicado:** `imports.run-finished.v1`.
- **Almacenamiento:** `imp_runs` (+ `imp_run_counts`, `imp_run_messages`), `imp_references`, las
  bandejas de salida y la auditoría.

## Decisiones (aprobadas por Javier el 2026-10-08)

1. **Un motor genérico con fuentes y cargadores**, en lugar del orquestador de doce pasos y sus
   dos pasarelas. Sugerencia: sí; añadir una fuente o una clase de dato deja de tocar el motor.
2. **Los cargadores usan los casos de uso del contexto dueño.** Importar pide `Imports.Run.Execute`
   **y** lo que el destino pida a cualquiera (lo acordado en la sesión de permisos: «crear» sobre
   el recurso). Ya no hace falta ser administrador global. Sugerencia: sí.
3. **Cada registro va por su cuenta:** el que falla se anota y el resto sigue; no hay una
   transacción de toda la ejecución (tampoco la había), pero cada registro sí es atómico en su
   contexto. Sugerencia: sí; deshacer 10.000 altas por una línea mala no es lo que se quiere.
4. **La referencia manda:** si la clave ya está enlazada se usa esa entidad aunque haya cambiado
   de nombre; si no, la clave natural que decida el cargador. Se leen para todas las fuentes.
   Sugerencia: sí; es lo que evita los duplicados por homónimos.
5. **Los ficheros llegan en la petición** (texto, 5 MB, 100.000 registros). El servidor no lee
   rutas de su disco ni bases de datos de otros sistemas. Sugerencia: sí. Para Sage, la extracción
   a CSV se hace fuera, como ya hace `Sage.Export`.
6. **La vista previa solo busca:** dice qué se crearía y qué existe, sin distinguir «se
   actualizaría» de «queda igual». Sugerencia: sí; es menos detalle que en C#, pero es verdad.
7. **Una ejecución por fuente a la vez**, y siempre se cierra (`failed` si se rompe, `CloseStale`
   si muere el proceso). Sugerencia: sí. Queda programar `CloseStale` al montar el servidor.
8. **Personio, empresa no declarada:** se rechaza la línea. En C# se asignaba a la primera empresa
   del fichero con un aviso. Con una sola empresa, las unidades sin padre son suyas. Sugerencia:
   sí; asignar empleados a la empresa equivocada es peor que no cargarlos.
9. **Los puestos van como texto** y las tablas de equivalencias (23 puestos, oficinas, bancos,
   cuentas) dejan de estar en el código: son datos de cada instalación que resuelve el cargador.
   Sugerencia: sí.
10. **Sin la confirmación escrita `main`** para ejecutar en producción: la protección es el
    permiso y la vista previa. Sugerencia: sí, pero es la más discutible; si prefieres conservar
    un segundo gesto, lo natural es pedirlo en la pantalla y no en el servidor.
11. **Quién ve qué:** cada uno ve las ejecuciones que lanzó y un administrador global todas; las
    referencias tienen permiso propio porque sus claves pueden ser datos personales (un DNI, un
    IBAN). Sugerencia: sí.
12. **Fuera de este contexto:** el volcado y la restauración de la base (`bulkload`, `export`,
    `verify`), el alta de usuarios al terminar y las rutas `/v1/import/*`. Sugerencia: sí.

## Validación

- **Dominio:**
  - Personio con un fichero como los de las pruebas de C# (dos empresas, departamentos homónimos,
    `"Auxiliar, Caja"` entre comillas, pluriempleo, nombre preferido, fechas con alias): los
    registros y cada línea rechazada con su fichero, su línea y su código; con una sola empresa,
    las unidades sin padre;
  - `ParseTable`: marca BOM, otro delimitador, valor con salto de línea, filas cortas y largas,
    líneas vacías, fichero ilegible; fechas, booleanos y nombres sin acentos;
  - `Run`: termina una vez, con o sin errores; mensajes cortados a 1.000 caracteres y contados a
    partir de 5.000; dada por muerta que luego termina;
  - `Reference`: ámbito global por defecto, claves válidas.
- **Extremo a extremo** (con un doble de los contextos dueños; en memoria y en SQLite migrada):
  - fuentes ofrecidas y clases sin cargador; registros omitidos con aviso;
  - lo que se rechaza antes de empezar: solo lectura 403, fuente desconocida 400, falta el fichero
    obligatorio 400, papel desconocido 400, fichero ilegible 422, demasiado grande 400;
  - vista previa: qué se crearía, cuatro errores con su línea, y el doble sigue vacío;
  - primera ejecución: 11 entidades, una persona rechazada por el contexto dueño (su código y su
    línea) y su empleo detrás, `completed-with-errors`, procedencia en cada llamada;
  - referencias: 11, dos empleos de una misma persona con distinto ámbito;
  - segunda ejecución un mes después: lo que no cambió queda igual, quien cambió de nombre y de
    correo se encuentra por su clave y se actualiza, quien faltaba entra; `succeeded`; primera y
    última ejecución de cada referencia; búsqueda por entidad;
  - ejecuciones: la última primero, filtros, estado inválido 400; otro usuario no ve las ajenas
    (404 y lista vacía) y el administrador global sí;
  - un cargador que revienta deja la ejecución `failed` con lo hecho;
  - con una ejecución en curso otra no empieza (422) y la vista previa sí; no se cierra por
    colgada antes de tiempo; dos horas después se da por muerta y, al terminar, sigue `failed`
    con sus contadores;
  - cinco eventos publicados.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: ejecución de ida y vuelta con sus
  contadores y mensajes (texto de 1.000 caracteres con eñes, líneas, orden), referencias con la
  misma clave en dos ámbitos, segunda ejecución reconocida solo por las referencias, vista previa,
  ejecución en curso y dada por muerta, y bandeja de salida.

## Cargadores de Personio (en el anfitrión)

Añadidos el 2026-10-08. Están en `host/loaders.go`, porque son quienes conocen a la vez
Importación y el contexto dueño de cada dato. Con ellos **una importación de Personio ya crea la
organización de verdad**.

| Clase | Dónde acaba | Cómo se reconoce si ya existe | Qué hace |
|---|---|---|---|
| `legal-entity` | Parties | organización interna del mismo nombre | la registra como organización interna |
| `department` | Parties | unidad del mismo nombre bajo su empresa | organización con rol de departamento, agregada a su empresa |
| `work-center` | Instalaciones | instalación del mismo nombre en su empresa | la registra como **edificio** |
| `person` | Parties | persona del mismo nombre entre las de su empresa | la registra afiliada como empleada a su primera empresa, con su correo |
| `employment` | RRHH | empleo con ese número en esa empresa | la contrata y, si el fichero trae fecha de baja, termina el empleo |
| `position` | RRHH | puesto de ese tipo en esa unidad que ya ocupa la persona | abre un puesto del tipo que dice el cargo, en su departamento, y se lo da desde su alta |
| `work-place` | Parties | papel de centro de trabajo que ya tiene en esa instalación | le da el papel de centro de trabajo en la instalación, desde su alta |

Reglas comunes:

- **Lo que existe no se toca.** Una importación rellena, no pisa lo que alguien mantiene a mano.
  La única actualización es terminar un empleo cuando el fichero pasa a traer la fecha de baja.
- **Cada registro, o entero o nada:** el departamento y su vínculo con la empresa, la persona con
  su afiliación y su correo, y la contratación con su baja van en una sola unidad de trabajo.
- **Con los permisos de quien importa:** hace falta `Imports.Run.Execute` y, además, lo que pida
  cada contexto para dar de alta eso mismo a mano.
- **La referencia manda en las siguientes ejecuciones:** dos personas con el mismo nombre se
  distinguen por su número de empleado una vez enlazadas. Si en la primera ejecución ya hay dos
  personas con ese nombre en la empresa, no se elige ninguna y se crea una nueva.

Lo que se rechaza, con su línea:

- **Centro de trabajo sin empresa** (`imports.work_center_without_company`): en Instalaciones
  toda instalación es de una organización.
- **Empleo sin fecha de alta** (`imports.hire_date_required`): RRHH la exige. La persona sí se
  carga; el empleo entra cuando el fichero traiga la fecha.
- **Lo que cuelga de algo que no se cargó** (`imports.unknown_company`,
  `imports.unknown_person`).

Decisiones (aprobadas por Javier el 2026-10-08):

1. **Un centro de trabajo se carga como edificio en Instalaciones**, no como oficina ni como
   centro de trabajo de RRHH. Una oficina exige dirección y teléfono, y el centro de RRHH su
   código de cuenta de cotización y su fecha de apertura; el fichero solo trae un nombre.
   Sugerencia: sí; completarlo es trabajo a mano o de una fuente que traiga esos datos.
2. **Sin fecha de alta no hay empleo.** Sugerencia: sí; inventar una fecha sería peor. En el
   fichero de ejemplo de C# las fechas eran opcionales, así que conviene revisar el fichero real
   antes de la primera carga.
3. ~~Departamento, centro y puesto del empleado no se cargan todavía.~~ **Hecho el mismo día**,
   ver «Dónde y de qué trabaja cada persona» más abajo.
4. **Pluriempleo:** la persona se registra con su primera empresa y se afilia a la segunda al
   contratarla allí, con fecha de hoy en Parties; las fechas reales del empleo las guarda RRHH.
   Sugerencia: sí.
5. **El apellido va entero al primer apellido** («García López» no se parte en dos).
   Sugerencia: sí; partirlo bien no es posible con apellidos compuestos.

### Dónde y de qué trabaja cada persona

La fuente Personio saca ahora dos registros más por persona, solo para su primera empresa:

- **`position`**, si el fichero trae cargo. En RRHH el departamento de alguien no es un dato
  suelto: es **la unidad del puesto que ocupa**. Así que el cargador abre un puesto del tipo que
  dice el cargo, en el departamento (o en la empresa, si no trae departamento), y se lo da a la
  persona desde su fecha de alta. Abrir y ocupar van juntos, o no se hace nada.
- **`work-place`**, si trae centro de trabajo: la persona pasa a tener en Parties el papel de
  centro de trabajo en la instalación que la importación cargó para ese centro.

Van como registros separados para que fallen por separado: un cargo que nadie conoce no impide
que a esa persona se le asigne su centro.

- **Los nombres se casan sin mirar mayúsculas, acentos ni espacios:** si el fichero de personas
  dice «OPERACIONES» y el de unidades declaró «Operaciones», es el mismo departamento.
- **El cargo tiene que existir** en el catálogo de tipos de puesto de RRHH, con ese título. Si no,
  la línea se rechaza (`imports.unknown_job_title`) diciendo cuál falta.
- **Departamento sin cargo:** no se puede registrar (un puesto necesita su tipo). Es un aviso
  (`personio.department_without_job`), no un error.
- **Persona sin empleo** (por ejemplo, sin fecha de alta): RRHH no le da un puesto; la línea se
  rechaza con el motivo de RRHH y entra en la ejecución en que ya tenga empleo.
- **Pluriempleo:** puesto y centro son los de la primera empresa, como en C#.

Decisiones (aprobadas por Javier el 2026-10-08):

6. **El departamento de una persona es la unidad de su puesto**, no una relación aparte. En C# era
   una relación `DepartmentAssignment` en Parties además del puesto. Sugerencia: sí; es como lo
   modela ya RRHH en Go, y evita guardar lo mismo en dos sitios.
7. **El cargo se casa por título exacto con el catálogo de RRHH.** En C# había una lista fija de
   23 puestos y 33 alias dentro del código. Sugerencia: sí; los cargos que falten se dan de alta
   en el catálogo. Si el fichero real usa muchas variantes del mismo cargo, lo razonable es
   añadir alias al catálogo, no al código.
8. **Cada importación abre un puesto nuevo para la persona**: no busca uno vacante que ocupar.
   Sugerencia: sí; un fichero de personas no dice qué plaza de la plantilla ocupa cada una.
9. **El centro de trabajo es un papel en Parties, no el del contrato de RRHH**, que exige tipo de
   contrato y convenio y el fichero no trae. Sugerencia: sí.

Validación:

- **Puestos y centros** (anfitrión, en memoria y en SQLite): cargo y departamento escritos en
  mayúsculas y centro con dos espacios, casados con lo declarado; el puesto de Ana en su
  departamento, del tipo de su cargo, ocupado; su papel de centro de trabajo en la instalación,
  desde su fecha de alta; cargo desconocido rechazado sin impedir el centro; persona sin empleo
  sin puesto, y con puesto un mes después cuando ya tiene empleo; departamento sin cargo como
  aviso; repetir no duplica puestos ni papeles.
- **Anfitrión** (en memoria y en SQLite): todas las clases de Personio tienen cargador; vista
  previa que no escribe; primera ejecución con dos empresas, un departamento homónimo en cada
  una, un centro cargado y otro rechazado, tres personas y tres empleos (uno rechazado por falta
  de fecha); comprobado en Parties (unidades bajo cada empresa, personas por empresa, la de
  pluriempleo en las dos), en Instalaciones y en RRHH (fechas de alta y de baja, la misma persona
  en dos empleos); segunda ejecución sin cambios ni duplicados; un mes después se contrata a
  quien ya trae fecha y se termina el empleo de quien se fue; doce referencias; y la auditoría de
  Parties marca la procedencia (fuente y ejecución).
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: la organización completa a través
  del anfitrión, repetida sin cambios, y una persona con dos empleos.

## Fuentes de Sage y de Apiscore

Añadidas el 2026-10-08. Las dos fuentes están en `contexts/imports/domain` (`sage.go`,
`apiscore.go` y lo que comparten en `trade.go`); sus cargadores, en `host/loaders_trade.go`.

### Qué lee cada una

**Sage** (`sage`): los CSV por concepto que escribe `Sage.Export`, separados por `;`. Solo el de
empresas es obligatorio; se sube lo que se tenga.

| Fichero (rol) | Qué da | Dónde acaba |
|---|---|---|
| `companies` (empresas.csv) | empresas | Parties, organización interna |
| `offices` (oficinas.csv) | centros de trabajo, con su dirección en las notas | Instalaciones |
| `persons` + `employees` (personas.csv, empleados.csv) | personas con su DNI, empleos con sus fechas y el centro de cada uno | Parties y RRHH |
| `customers`, `suppliers` (clientes.csv, proveedores.csv) | clientes y proveedores de cada empresa, con CIF y correo | Parties |
| `tax-rates` (catalogos.csv) | tipos de IVA | Fiscal |
| `chart` (plancontable.csv) | plan de cuentas de cada empresa | Contabilidad |
| `bank-accounts` (cuentasbancarias.csv) | cuentas de la empresa en bancos | Tesorería |

**Apiscore** (`apiscore`): los tres CSV del núcleo bancario, separados por `,`. Vale con uno.

| Fichero (rol) | Qué da | Dónde acaba |
|---|---|---|
| `payment-accounts` (cuentas_pago.csv) | cuentas de pago de clientes y sus titulares | Parties y sectorial financiero |
| `virtual-accounts` (cuentas_cv.csv) | cuentas virtuales multidivisa y sus titulares | Parties y sectorial financiero |
| `own-accounts` (cuentas_propias.csv) | cuentas de la entidad en bancos | Tesorería |

Los ficheros de Apiscore no dicen de qué entidad son. Se le dice al servidor con
`KARPO_APISCORE_ENTITY` (el nombre de la entidad, como en Parties). La importación la crea si no
existe, ya como entidad financiera; sin esa variable, una importación de Apiscore se rechaza.

### Reglas que se conservan del C#

- Persona de Sage: nombre y correo del fichero de personas, unido por DNI; si no está, el nombre
  completo del empleado. Número de empleado repetido: gana el primero.
- Clientes y proveedores: uno por empresa y código.
- Cuentas bancarias de Sage: la cuenta contable dice la divisa y si es segregada o de abandono
  (las 24 cuentas de `BancosContaUsePolicy`, como datos).
- Apiscore: titular organización si `TIPO_PERSONA` empieza por «JUR»; orden de columnas para el
  nombre; clave del cliente por documento y, si no hay, por nombre; bloqueada gana a abandonada;
  demo si lo dice `DEMO` o el estado es «Pruebas»; `MXP` es `MXN`; el uso sale de
  `OPERATIVA_DESCRIPCION`; `IBAN_ALL` manda sobre `IBAN` salvo que sea corto.
- País que expide el documento: un número español con sus dígitos de control correctos es español
  se llame como se llame; si no, por el tipo; si no, por la nacionalidad (las 73 formas de escribir
  un país de `countries.csv`, como datos).

### Qué cambia

- **Un cliente es un participante, no uno por empresa.** Se busca por su documento y, si no
  tiene, por nombre entre los de la empresa. Si ya existe (porque es cliente de otra empresa del
  grupo, o porque Sage lo trajo antes que Apiscore) solo se le añade la relación con esta empresa.
  En C# se buscaba solo por nombre.
- **Los proveedores también llevan su CIF** y **el correo de clientes y proveedores se guarda**
  (en C# se leían y se perdían).
- **Las personas de Sage llevan su DNI** como identificación.
- **Empresa desconocida: la fila se rechaza.** En C# caía en la primera empresa del fichero.
- **El plan de cuentas dice qué cuentas admiten apuntes**: las que no tienen otras colgando. En C#
  no había esa distinción.
- **Las cuentas de clientes cerradas se cierran** en su fecha de baja. En C# se guardaba la fecha
  y la cuenta seguía activa.
- **Lo que no se puede cargar se dice, fila a fila**, y lo demás sigue.

### Decisiones (aprobadas por Javier el 2026-10-08)

1. **Documentos que Parties no admite con lo que traen los ficheros se guardan como «otro
   documento», con su país.** Un pasaporte o un NIE piden fecha de caducidad en Parties y los
   ficheros no la traen; un DNI o CIF con los dígitos de control mal no pasa como tal. El número
   queda y se puede buscar por él. Sugerencia: sí; la alternativa es perder el documento.
2. **Sin país no hay documento.** Si no se sabe qué país lo expidió (tipo desconocido, nacionalidad
   que no está en la tabla), el cliente se carga sin documento y la ejecución avisa. Sugerencia: sí.
3. **Las cuentas propias en otra divisa que no sea euro no se cargan.** Tesorería solo lleva
   cuentas en euros. La fila falla diciéndolo. Sugerencia: sí por ahora; llevar divisas en
   Tesorería es un cambio de ese contexto.
4. **Segregada y abandono van en el alias de la cuenta** («BANCO SANTANDER 0022 (segregada)»).
   Tesorería no tiene dónde guardar el uso de una cuenta propia, ni el banco. Sugerencia: sí por
   ahora; si hace falta filtrar por ello, se añade el uso a Tesorería.
5. **Un IBAN es de un solo dueño.** Si Sage y Apiscore traen la misma cuenta propia para la misma
   empresa, es una sola. No se reconcilia el uso entre las dos fuentes como hacía C# (K2).
   Sugerencia: sí, mientras el uso viva en el alias.
6. **Los tipos de IVA de Sage van al catálogo común de Fiscal**, uno por código (Sage repite su
   catálogo en cada empresa). Si ya hay un tipo en vigor con ese código o con el mismo porcentaje
   y recargo, es ese y no se crea otro. Los nuevos valen desde el día de la importación, porque
   Sage no dice desde cuándo. Sugerencia: sí.
7. **Exento y no sujeto no se cargan como tipos**: en Karpo son tratamientos fiscales. La
   ejecución avisa. Sugerencia: sí.
8. **Empleado inactivo sin fecha de baja: el empleo termina el día que empezó**, con aviso. C#
   ponía el 1 de enero de 1900. Sugerencia: sí; RRHH no admite una baja anterior al alta.
9. **No se cargan**: convenio, salario e IRPF de los empleados (son del contrato de RRHH, que
   necesita más datos), condiciones de pago y comisionista de clientes (C# los metía en un texto),
   ni el banco como participante. Sugerencia: sí; cada uno es una fase propia.
10. **Una cuenta de cliente que ya existe no se toca.** Si Apiscore dice después que se bloqueó,
    la importación no la bloquea: desde que está en Karpo, su estado se lleva en Karpo.
    Sugerencia: sí; si Apiscore sigue siendo el que manda, se cambia a sincronizar el estado.
11. **El nombre de una persona se parte por el primer espacio** (nombre / apellidos), porque
    Apiscore lo trae en una sola columna. Sugerencia: sí.

### Validación

- **Dominio**: el país y el tipo de 14 documentos; un juego de ficheros de Sage con duplicados,
  empresas desconocidas, porcentajes ilegibles, oficinas repetidas y empleados sin nombre; otro de
  Apiscore con cuentas repetidas, sin titular, fechas ilegibles, columnas en otro orden y sin
  `ES_SEGREGADA`.
- **Anfitrión** (en memoria y en SQLite): los ficheros de Sage de un grupo de dos empresas y los
  de Apiscore de una entidad, cargados dos veces; lo que quedó, preguntado a Parties, RRHH,
  Contabilidad, Tesorería y el sectorial financiero (clientes por su documento, un cliente de dos
  empresas, cuentas activas, bloqueadas, abandonadas, cerradas y virtuales).
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: Sage y después Apiscore sobre la
  misma base, dos veces cada una; el cliente y la empleada que Sage trajo son los titulares de
  las cuentas de Apiscore, sin duplicarse.

## Fuente del calendario de festivos

Añadida el 2026-10-09: fuente `holidays` en `contexts/imports/domain/holidays.go`, cargador
`HolidayCalendar` en `host/loaders_holidays.go`. Carga en el calendario de Geografía (ver
[GEOGRAFIA.md](GEOGRAFIA.md), «Calendario de festivos»).

- **Un solo fichero** (rol `calendar`), separado por `;` o por `,`, con tres columnas:

  ```
  fecha;festivo;ámbito
  2026-12-25;Natividad del Señor;ES
  2026-05-02;Fiesta de la Comunidad de Madrid;ES-MD
  2026-12-07;Traslado de la Constitución;28
  2026-11-09;Nuestra Señora de la Almudena;28079
  ```

  Las columnas valen también en inglés (`date`, `name`, `place`). La fecha, como en las demás
  fuentes: `2026-12-25` o `25/12/2026`.
- **El ámbito es el código oficial del lugar:**

  | Se escribe | Qué es | Código |
  |---|---|---|
  | `ES` | un país | ISO 3166-1 |
  | `ES-MD` | una comunidad autónoma | ISO 3166-2 |
  | `28` | una provincia | INE, 2 cifras |
  | `28079` | un municipio | INE, 5 cifras |

  Un festivo vale para su ámbito y todo lo que contiene.
- **Se puede cargar las veces que haga falta.** Un día que el lugar ya tiene se deja como está:
  el calendario del año se carga cuando sale el nacional y otra vez cuando se publican los
  locales, con el mismo fichero ampliado.
- **Lo que no se puede situar falla fila a fila** (`geography.unknown_place`) y lo demás se carga.
- **Permiso:** además de ejecutar importaciones, quien importa necesita
  `Geography.Holiday.Update` y `Geography.Holiday.Read`.

### Decisiones (aprobadas por Javier el 2026-10-09)

1. **El fichero es uno neutro de tres columnas**, no el formato de ningún boletín. No existe un
   fichero oficial único: el BOE publica los nacionales y autonómicos en una tabla, cada
   comunidad los suyos y cada ayuntamiento sus dos días. Sugerencia: sí; se copia a este fichero
   una vez al año.
2. **El lugar se escribe con su código oficial**, no con su nombre. Sugerencia: sí; «Madrid» es
   municipio, provincia y comunidad, y el código no deja dudas.
3. **Provincias y municipios por código INE, sin prefijo de país.** Hoy la semilla solo trae los
   de España. Sugerencia: sí; cuando haya otro país se antepone su código.
4. **Importar no corrige ni borra**: un festivo ya declarado conserva su nombre, y un día que
   desaparece del fichero no se quita. Sugerencia: sí; borrar se hace a mano, por la ruta.
5. **No se descarga nada de internet**: el servidor no va a buscar el calendario a ninguna web
   (decisión 5 de Importación: los ficheros llegan en la petición). Sugerencia: sí.

### Validación

- **Dominio**: columnas en castellano y con BOM, fechas en dos formatos, ámbito en minúsculas o con
  espacios, nombre con coma entre comillas, día repetido, y filas sin día, sin lugar o sin nombre.
- **Geografía**: `Locate` de país, comunidad, provincia y municipio, y ocho códigos que no son
  de ningún sitio.
- **Anfitrión** (en memoria y en SQLite): el calendario de 2026 con días de país, comunidad,
  provincia y municipio y dos filas sin sitio; previsualización; el calendario de Madrid con lo
  heredado en orden; el mismo fichero ampliado con un municipio más; y el vencimiento de una
  empresa de Madrid, que salta los festivos importados.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: un fichero sobre un calendario que
  ya tenía un día declarado a mano.

## Pendiente

- ~~Cargadores reales para las clases de Personio~~: hechos, ver «Cargadores de Personio»,
  incluidos el puesto y el centro de cada persona.
- Quién supervisa a quién: el fichero trae `isSupervisor`, que se lee y no se usa (en C# tampoco).
- Contrato de RRHH (tipo, convenio, jornada, centro): el fichero no lo trae.
- Alias de cargos en el catálogo de tipos de puesto, si el fichero real los necesita.
- ~~Fuentes de Sage y de Apiscore~~: hechas, ver «Fuentes de Sage y de Apiscore». Quedan el
  contrato de RRHH (convenio, salario), las condiciones de pago de clientes y los bancos como
  participantes.
- Programar `CloseStale` al montar el servidor.
- Ejecución en segundo plano con progreso para ficheros grandes (hoy la petición espera).
- Subida de ficheros como `multipart` y lectura de XLSX.
- Seudónimos para DNI y CIF al importar en entornos que no sean producción (lo hacía C# con Sage).
- Pantalla: no existe en el frontal de C#.
- Framework: en Oracle `{str:N}` mide bytes y los contextos validan caracteres. Aquí el texto de
  los mensajes (1.000 caracteres) se guarda en una columna de 4.000 para que quepa con acentos;
  el resto de columnas de texto de todos los contextos tiene el mismo límite latente.
