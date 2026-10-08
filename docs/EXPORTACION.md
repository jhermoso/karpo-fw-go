# Contexto Exportación de listados

Port de los trabajos de exportación de C# (`ExportService`, `ExportJobProcessor`,
`ExportJobStore` y las rutas `/api/exports/*`) al contexto `contexts/exports`.

Sirve para **llevarse un listado a un fichero** (CSV o XLSX): se pide, un trabajador lo escribe en
segundo plano y quien lo pidió lo descarga. Es lo único de lo que quedaba por portar que usa el
frontal: la rejilla lo llama cuando el listado tiene más filas de las que maneja el navegador.

> **Alcance.** Están los trabajos, los escritores de CSV y XLSX, el almacén de ficheros y las
> rutas. Desde el 2026-10-08 el anfitrión ofrece **nueve listados reales** (los de Parties que usa
> la rejilla, empleados y cuentas de clientes) y hace de trabajador: llama a `RunNext` y `Purge`
> en cada ronda de tareas. Ver «Listados de Parties».

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

Unas 1.130 líneas. Dos caminos sobre el mismo servicio: los trabajos (`/api/exports/*`) y siete
rutas síncronas (`GET /api/<entidad>/export`). Solo exporta listados de Parties.

- **Todo vive en la memoria de un proceso.** Los trabajos son un diccionario y la cola un canal:
  al reiniciar, todos pasan a «no existe» y sus ficheros quedan huérfanos; con dos instancias, el
  estado y la descarga caen en la equivocada.
- **Los ficheros van a la carpeta temporal** de la máquina, sin cifrar, y la limpieza solo se
  ejecuta cuando llega un trabajo nuevo: un servidor en reposo no limpia nunca.
- **Cancelar un trabajo en cola no hace nada** y responde «cancelado»; el trabajo se ejecuta.
  Cancelado y fallido son el mismo estado.
- **Sin límites:** ni de filas, ni de trabajos por persona. La cola admite diez y la petición
  número once se queda colgada.
- **El XLSX se construye entero en memoria**, con todas las celdas como texto.
- **El CSV no protege contra fórmulas** (`=`, `+`, `-`, `@` se escriben tal cual), y su salto de
  línea depende del sistema operativo.
- **El «cursor» es paginación por desplazamiento**, pese a lo que dice el comentario: cada lote
  es más lento que el anterior y, si los datos cambian a mitad, se saltan o repiten filas.
- **El mensaje de la excepción llega tal cual a quien pidió el fichero.**
- **Campos que nadie usa:** columnas a medida, filtro de empleados, filtro de relaciones (en los
  trabajos siempre va vacío) e `include` (lanza consultas para columnas que no se exportan). La
  columna CIF/NIF de clientes sale siempre vacía. `TotalRows` no se rellena nunca.
- **Un tipo de listado desconocido** se acepta con el permiso de Parties, se encola y falla
  después. Un formato que no sea `csv` ni `xlsx` produce un XLSX con extensión `.csv`.
- **Cuatro de las siete rutas síncronas no piden permiso** (personas, empleados, roles y
  relaciones), y su política de tiempo máximo no está definida en ninguna parte.
- **Lo que sí está bien** (endurecido en SEC-17): el trabajo es de quien lo crea, a los demás se
  les responde lo mismo que si no existiera, y el fichero se genera con el ámbito de empresas de
  quien lo pidió. Aunque si falta una pieza opcional, ese ámbito se omite en silencio.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | Propiedad del trabajo y ámbito capturado (SEC-17) | 5 | 4 | 3 | 4 | **84** | Mantener → el trabajo guarda quién lo pidió y qué empresas veía; 404 uniforme para los demás. Si no se puede reconstruir esa vista, el trabajo falla (no se exporta sin ámbito) |
| 2 | Pedir, consultar, cancelar y descargar | 5 | 2 | 3 | 3 | **71** | Mantener → las mismas cuatro operaciones, sobre un trabajo que se guarda |
| 3 | Escritura de CSV | 4 | 2 | 3 | 4 | **66** | Modificar → marca UTF-8, CRLF siempre y protección contra fórmulas |
| 4 | Escritura de XLSX (OpenXml, en memoria) | 4 | 2 | 3 | 3 | **63** | Modificar → escritor propio que va escribiendo según lee; los números, como números |
| 5 | Permiso según el tipo de listado, a mano | 4 | 2 | 2 | 3 | **59** | Modificar → cada listado declara su permiso; uno desconocido es un 400 |
| 6 | `ExportJobStore` + canal + servicio en segundo plano | 4 | 1 | 2 | 3 | **54** | Modificar → agregado `Job` en la base de datos; un trabajador del anfitrión llama a `RunNext` |
| 7 | `switch` por tipo con columnas fijas (solo Parties) | 4 | 2 | 1 | 2 | **52** | Modificar → puerto `Dataset`: clave, permiso, columnas, filtros y páginas |
| 8 | Paginación por desplazamiento disfrazada de cursor | 3 | 2 | 2 | 3 | **51** | Modificar → el listado devuelve su propio cursor; cómo pagina es cosa suya |
| 9 | Rutas síncronas `GET /api/<entidad>/export` | 3 | 1 | 2 | 3 | **46** | Modificar → un solo camino: el trabajo |
| 10 | Limpieza al llegar un trabajo; carpeta temporal | 3 | 1 | 2 | 3 | **46** | Modificar → puerto `Files` (carpeta compartida o depósito) y `Purge` para una tarea |
| 11 | Columnas a medida, `include`, filtros por tipo | 1 | 1 | 1 | 3 | **26** | Retirar → filtros como pares nombre y valor que declara cada listado |

## Diseño en Go

- **Listado** (`application.Dataset`): lo escribe el anfitrión sobre las consultas del contexto
  dueño. Dice su clave, el permiso que exige leerlo, sus columnas (campo, cabecera y tipo), los
  filtros que entiende, y devuelve las filas página a página con su cursor.
- **Trabajo** (`domain.Job`): listado, formato, título, filtros, **quién lo pidió y qué empresas
  veía en ese momento**, estado, filas, fichero y hasta cuándo se guarda. Estados: `queued`,
  `processing`, `completed`, `failed`, `cancelled`, `expired`.
- **Pedir** (`Start`): hace falta `Exports.Job.Create` **y** el permiso del listado. Cinco
  trabajos en espera o en curso por persona como mucho.
- **Escribir** (`RunNext`, para un trabajador con `Exports.Job.Run`): toma el trabajo que más
  lleva esperando y llama al listado **como quien lo pidió**, con sus empresas de entonces y sin
  más permiso que el del listado. Entre página y página mira si lo han cancelado; cada diez
  páginas anota por dónde va. Más de un millón de filas, y falla pidiendo que se filtre.
- **Entregar** (`Download`): a quien lo pidió o a un administrador global; a cualquier otro, 404.
- **Limpiar** (`Purge`): borra los ficheros que llevan guardados 24 horas (el trabajo pasa a
  `expired`) y da por perdidos los que llevan más de 30 minutos escribiéndose.
- **Ficheros** (`application.Files`): una carpeta (`DiskFiles`) o lo que el anfitrión ponga. La
  clave del fichero la genera el contexto; nunca viene de una petición.

Cómo se escribe:

- **CSV:** UTF-8 con su marca (para que la hoja de cálculo lea los acentos), comas, CRLF en
  cualquier sistema, comillas solo donde hacen falta. Lo que empieza por `=`, `+`, `-`, `@` y no
  es un número va tras un apóstrofo.
- **XLSX:** un libro con una hoja, escrito sobre la marcha. Textos como textos (nunca fórmulas),
  números como números (hasta quince cifras; más, como texto para no redondear). Sin estilos.
  El título se ajusta a lo que admite el nombre de una hoja.
- **Valores:** fechas `AAAA-MM-DD`, booleanos «Sí» / «No», vacío para lo que no hay.
- **Si falla:** a la persona se le dice que no se pudo escribir; el motivo real lo recibe el
  trabajador. Una regla (demasiadas filas) sí se le cuenta.

Además:

- **Permisos** (`application/service.go`): `Exports.Job.Read` (ver y descargar los propios),
  `Exports.Job.Create` (pedir y cancelar) y `Exports.Job.Run` (el trabajador).
- **Rutas:** `GET /api/exports/datasets`, `POST /api/exports` (202), `GET /api/exports`,
  `GET /api/exports/{id}`, `POST /api/exports/{id}/cancel`, `GET /api/exports/{id}/download`,
  `POST /api/exports/run-next` y `POST /api/exports/purge`.
- **Auditoría:** cada trabajo queda en el registro del contexto, con quién lo pidió. No publica
  eventos de integración.
- **Almacenamiento:** `exp_jobs` (+ `exp_job_filters`, `exp_job_scope`) y la auditoría.

## Decisiones (aprobadas por Javier el 2026-10-08)

1. **Un contexto genérico con listados que ofrece el anfitrión**, no atado a Parties. Sugerencia:
   sí; exportar facturas o pedidos será escribir un listado, no tocar el contexto.
2. **Los trabajos se guardan en la base de datos** y los escribe un trabajador del anfitrión.
   Sugerencia: sí; sobreviven a un reinicio y funcionan con varias instancias.
3. **Los ficheros van a un almacén compartido y se guardan 24 horas** (en C#, 30 minutos desde
   que se pedía, en la carpeta temporal). Sugerencia: sí; el plazo es un ajuste.
4. **El fichero se escribe con la vista de quien lo pidió cuando lo pidió.** Si después gana o
   pierde empresas, ese fichero no cambia. Si esa vista no se puede reconstruir, el trabajo
   falla. Sugerencia: sí.
5. **Un solo camino: se retiran las exportaciones síncronas** (`GET /api/<entidad>/export`).
   Sugerencia: sí; cuatro de siete no pedían permiso y duplicaban lo mismo.
6. **CSV con protección contra fórmulas**, marca UTF-8 y CRLF fijo; el separador sigue siendo la
   coma. Sugerencia: sí. La rejilla, cuando exporta en el navegador, usa punto y coma: si
   prefieres unificar a punto y coma (lo que abre Excel en español sin preguntar), es un cambio
   de una línea.
7. **XLSX escrito por el propio contexto**, sin librería: una hoja, sin estilos, números como
   números. Sugerencia: sí; evita una dependencia y no carga el fichero en memoria. Cabeceras en
   negrita o anchos de columna serían una mejora posterior.
8. **Límites:** cinco trabajos a la vez por persona y un millón de filas por fichero. Sugerencia:
   sí.
9. **Los errores internos no se enseñan** a quien pidió el fichero. Sugerencia: sí.
10. **Rutas y estados cambian respecto a C#:** `POST /api/exports` en vez de `/exports/start`,
    estados en minúscula y `cancelled` propio. **El frontal actual hay que adaptarlo** (espera
    `Queued`, `Completed`… y los nombres de campo de C#). Sugerencia: sí, por coherencia con el
    resto de contextos; si prefieres no tocar el frontal, puedo mantener la forma de C#.
11. **Los filtros son pares nombre y valor que declara cada listado.** Desaparecen las columnas a
    medida y `include`, que nadie usaba. La búsqueda rápida y el orden de la rejilla siguen sin
    aplicarse al fichero, como en C#. Sugerencia: sí.
12. **Cada exportación queda en la auditoría** (quién, qué listado, cuándo), que en C# solo se
    anotaba en el navegador. Sugerencia: sí; son datos personales saliendo del sistema.

## Validación

- **Dominio:**
  - CSV carácter a carácter: marca, CRLF, comillas, comas, saltos de línea dentro de un valor,
    espacios al borde, números, fechas, booleanos, vacíos y las cuatro formas de fórmula;
  - XLSX abierto como zip: cinco partes, nombre de hoja saneado, textos y números, celdas vacías
    omitidas, un número de veinte cifras como texto;
  - trabajo: petición inválida (listado, formato, sin solicitante, filtro repetido), se toma una
    vez, el progreso solo crece, completar, caducar una vez, cancelado mientras se escribía,
    error recortado.
- **Extremo a extremo** (con un doble de listado que filtra por empresa; en memoria y en SQLite
  migrada):
  - listados visibles según el permiso;
  - pedir: el trabajador no pide (403), sin permiso del listado 403, listado desconocido 400,
    formato 400, filtro desconocido 400;
  - se le concede otra empresa después de pedir y el fichero no la incluye;
  - pedir no es escribir (403); el fichero, byte a byte, con sus cabeceras de descarga;
  - otro usuario: 404 en consulta, cancelación y descarga; el trabajador no lee (403); un
    administrador global sí descarga; tres apuntes de auditoría;
  - XLSX filtrado con título; quien no ve ninguna empresa recibe solo la cabecera;
  - cancelar en cola (dos veces no cambia nada) y el trabajador lo pasa de largo;
  - seis trabajos a la vez: 422;
  - cancelado mientras se escribe: para en la página siguiente y no deja fichero;
  - listado que falla: 500 para el trabajador, «no se pudo escribir» para la persona;
  - más de un millón de filas: falla diciéndolo;
  - trabajo colgado: a los 31 minutos se da por perdido y, cuando su trabajador vuelve, sigue así;
  - a las 25 horas no queda ningún fichero y la descarga da 422;
  - mis trabajos, los últimos primero; los de todos, solo un administrador global.
- **`DiskFiles`:** crear, leer, borrar (dos veces) y claves que intentan salir de la carpeta.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: trabajo de ida y vuelta con filtros,
  ámbito, acentos e instantes; fichero escrito por un trabajador distinto de quien lo pidió;
  administrador global; límite de trabajos; lista larga con progreso anotado y cancelada a
  mitad; caducidad con el reloj adelantado; listados y auditoría.

## Listados de Parties (en el anfitrión)

Añadidos el 2026-10-08, en `host/datasets.go`. Son los listados que la rejilla del frontal pide
exportar hoy; con ellos **la exportación ya saca datos reales**.

| Listado | De dónde sale | Columnas | Permiso |
|---|---|---|---|
| `parties` | búsqueda de Parties | Nombre, Tipo, Estado | `Parties.Party.Read` |
| `persons` | lo mismo, solo personas | Nombre, Tipo, Estado | `Parties.Party.Read` |
| `organizations` | lo mismo, solo organizaciones | Nombre, Tipo, Estado | `Parties.Party.Read` |
| `internal-organizations` | lo mismo, con rol de organización interna | Nombre, Tipo, Estado | `Parties.Party.Read` |
| `customers` | lo mismo, con rol de cliente | Nombre, Tipo, CIF/NIF, Estado | `Parties.Party.Read` |
| `party-roles` | los roles de cada participante, una fila por rol | Participante, Tipo de rol, Fecha inicio, Fecha expiración, Activo | `Parties.Party.Read` |
| `party-relationships` | las relaciones de cada participante, cada una una vez | Tipo de relación, Origen, Destino, Fecha inicio, Fecha expiración, Estado, Observaciones | `Parties.Relationship.Read` |
| `employees` | empleos de RRHH, con el nombre de Parties | Nombre, Número empleado, Fecha contratación, Fecha baja, Activo | `HR.Employment.Read` |

Más `customer-accounts`, del sectorial financiero, que ya estaba.

- **Mismas columnas y cabeceras que en C#**, con dos diferencias: el CIF/NIF de clientes ahora sí
  sale (en C# la columna existía y quedaba siempre vacía), y las relaciones llevan fecha de
  inicio en lugar de prioridad, que en Go no existe.
- **Filtros** de los listados de Parties: `name`, `document`, `organization`, `active` y `role`.
  De `employees`: `organization`, `number` y `active`. En C# los filtros de relaciones y de
  roles se ignoraban; aquí se aplican.
- **Cada fichero lleva lo que puede ver quien lo pidió.** Los listados leen por las búsquedas del
  contexto dueño, con el ámbito de esa persona. Filtrar por otra empresa estrecha lo que ve,
  nunca lo ensancha.

Decisiones (aprobadas por Javier el 2026-10-08):

1. **`employees` sale de RRHH**, no de Parties: una fila por empleo, así que quien trabaja para
   dos empresas sale dos veces, y pide el permiso de RRHH. En C# salía de Parties (personas con
   rol de empleado) con datos del perfil de empleado. Sugerencia: sí; el número y las fechas son
   de RRHH.
2. **`legal-organizations` no se ofrece.** En C# filtraba por un tipo de participante
   «organización legal» que en Go no existe como tipo aparte. Sugerencia: sí; si la rejilla lo
   usa, se cubre con `organizations` y el filtro `role`.
3. **Las relaciones se leen participante a participante**, porque Parties no tiene una búsqueda
   de relaciones. El fichero es correcto pero tarda más que los demás. Sugerencia: sí por ahora;
   si se exportan a menudo, lo propio es añadir esa búsqueda a Parties.
4. **Cabeceras y valores en castellano, fijos** («Activo», «Persona», «Organización»), como en
   C#. Sugerencia: sí, hasta que haya idioma por usuario.

Validación:

- **Anfitrión** (en memoria y en SQLite), tras una importación real de dos empresas con su gente
  y un cliente con NIF: los nueve listados ofrecidos; cada fichero comparado línea a línea
  (apellido con coma entre comillas, filtros por empresa y por nombre, el NIF del cliente,
  empleados con sus fechas y quien trabaja en dos empresas dos veces, roles, y relaciones
  contadas por tipo); quien solo ve una empresa recibe solo lo de esa empresa, también cuando
  filtra por otra; sin el permiso del listado, 403; filtro que el listado no tiene, 400.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: personas, empleados y relaciones
  exportados a través del anfitrión.

## Pendiente

- ~~Listados de Parties y el trabajador~~: hechos, ver «Listados de Parties» y
  [ANFITRION.md](ANFITRION.md). Siguen los de facturas, pedidos, movimientos…
- Un tiempo máximo por trabajo de exportación (hoy solo lo corta la limpieza a los 30 minutos).
- Adaptar el frontal a las rutas y estados nuevos (decisión 10).
- Un almacén en depósito de objetos para despliegues sin disco compartido.
- Total de filas para mostrar un porcentaje (el listado tendría que saber contarse).
- Cabeceras en el idioma de quien pide (hoy las pone el listado).
- Estilos en el XLSX y fechas como fechas.
- Aviso cuando el fichero está listo (un evento, si llega a haber notificaciones).
