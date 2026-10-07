# Contexto Importación de datos

Port de la importación de C# (`OrganizationImportService`, sus lectores de Personio, Apiscore y
Sage, `import_run` y `legacy_reference`) al contexto `contexts/imports`.

Sirve para **cargar en Karpo lo que hoy vive en otros sistemas**: se le dan unos ficheros, dice qué
haría con ellos (vista previa), lo hace (ejecución) y deja escrito qué pasó línea a línea y en qué
se convirtió cada clave del sistema de origen.

> **Alcance de esta fase.** Está el motor, el registro de ejecuciones, las referencias y la fuente
> Personio. **Todavía no hay cargadores reales**: quien escribe en Parties o en RRHH es una pieza
> del anfitrión que aún no existe (en las pruebas la hace un doble). Hasta que se escriban, una
> importación lee y valida, pero no crea nada en los demás contextos.

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

## Decisiones propuestas (pendientes de confirmar)

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

## Pendiente

- **Cargadores reales** sobre Parties, Instalaciones y RRHH para las cinco clases de Personio: es
  lo que falta para que una importación cree algo.
- Fuentes de Sage (CSV por concepto: clientes, proveedores, IVA, plan de cuentas, bancos) y de
  Apiscore, con sus clases de registro y sus cargadores (Parties, Fiscal, Contabilidad, Tesorería).
- Programar `CloseStale` al montar el servidor.
- Ejecución en segundo plano con progreso para ficheros grandes (hoy la petición espera).
- Subida de ficheros como `multipart` y lectura de XLSX.
- Seudónimos para DNI y CIF al importar en entornos que no sean producción (lo hacía C# con Sage).
- Pantalla: no existe en el frontal de C#.
- Framework: en Oracle `{str:N}` mide bytes y los contextos validan caracteres. Aquí el texto de
  los mensajes (1.000 caracteres) se guarda en una columna de 4.000 para que quepa con acentos;
  el resto de columnas de texto de todos los contextos tiene el mismo límite latente.
