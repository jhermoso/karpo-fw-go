## analista

- ¿Una lista de ids vacía (`?ids=` o `filter.ids: []`) significa «sin filtro» (se ignora) o «ningún resultado» (`1=0`)? AIP-231 no lo cubre y el front no lo necesita.
- Cuando hay `ids`, ¿se fuerza que la página sea al menos `len(ids)` (ignorando un `pageSize` menor) o se respeta `pageSize` y se documenta que el front debe pedir la página adecuada?
- Superar 1000 ids, ¿da 400 `ids/range` o se trunca? Y un id mal formado dentro de la lista, ¿invalida toda la petición (400 `ids/format`) o se descarta ese id sin error?
- En `/api/v1/countries`, cuya identidad es el código ISO alfa-2, ¿se incluye `ids` con códigos de país en este alcance o se deja fuera por no ser una entidad con UUID?
- ¿Conviene un auxiliar común en `pkg/distribution` para parsear y validar `ids` (GET separado por comas y POST como array o cadena), o se mantiene cada `maintenance.go` autónomo como hoy?

Respuestas del asesor a las cinco consultas de «b15-filtro-ids». Comprobado en C:/Git/Paranoia/karpo-fw-go-ids, rama feat/filtro-ids. No he ejecutado pruebas ni he escrito nada en el repositorio.

## 1. Lista vacía: «ningún resultado» si viene presente; «sin filtro» sólo si falta o es `null`

**Recomendación:** una sola regla por presencia. `filter.ids` ausente o `null`, o la URL sin `ids`, no filtra. `filter.ids: []` y `?ids=` devuelven 200 con `items: []` y `total: 0`.

Hechos:
- El framework ya fija esa semántica: un `In` sin valores se traduce a `1=0` (`pkg/persistence/sqlrepo/translate.go:409`).
- El POST ya ignora un filtro `null` (`case nil:` en el bucle de `filter` de cada `maintenance.go`).
- El GET copia hoy los parámetros con `values.Get(k)`, así que `?ids=` y la ausencia son indistinguibles; hará falta `values.Has("ids")`.
- La lista vacía no puede atajarse antes del caso de uso. Si el endpoint devuelve la página vacía sin llamar a `Search.Handle`, quien no tiene permiso recibiría 200 en vez de 403. Tiene que pasar por el caso de uso y dejar que la especificación dé `1=0`.

Opinión:
- En APIs con filtros estructurados, la lista vacía suele dar «nada» (Prisma `in: []`, Hasura `_in: []`, Rails `where(id: [])`). Lo contrario falla en abierto: un cliente con un conjunto vacío por error recibe la primera página entera.
- El punto discutible es `?ids=`. El resto del contrato ignora un filtro de texto vacío (`q.filters[f] != ""`), y django-filter hace lo mismo. Prefiero la regla única porque se documenta en una frase. Si el planificador prefiere «`?ids=` se ignora, `[]` da vacío», también es defendible; lo que no recomiendo es que `[]` devuelva todo.
- En el caso de uso, la distinción entre `nil` y lista vacía es frágil en Go. Mejor que el DTO de búsqueda lleve la presencia explícita (por ejemplo un `ByIDs bool`), con una prueba por cada rama.

## 2. Se respeta `pageSize`; no se fuerza

**Recomendación:** `ids` es un filtro más, combinado con AND con los demás, y la paginación no cambia. Se documenta que el front pide `pageSize` igual al número de ids, o 1000.

Hechos:
- El tope de ids propuesto coincide con `fw.MaxPageSize = 1000` (`pkg/domain/page.go:12`), así que una sola página siempre puede traerlos todos.
- La respuesta devuelve `pageSize`, `totalPages` y `total`. Forzar el tamaño obligaría a devolver un `pageSize` distinto del pedido o a mentir en `totalPages`.
- AIP-158 (paginación de Google) dice que el servidor puede devolver menos elementos que `page_size`, nunca más.

Opinión:
- Tampoco cambiaría el valor por defecto (20) cuando hay `ids`. Un valor por defecto que depende de otro parámetro es una regla oculta, y `total > len(items)` ya avisa al cliente.
- `ids` no debe implicar `isActive=true`: las etiquetas de claves ajenas a registros dados de baja hacen falta. Conviene decirlo en el documento.

## 3. Nunca truncar ni descartar: 400 para toda la petición

**Recomendación:**
- Más de 1000 ids: 400 con `errorCodes.ids = ["range"]`.
- Un id mal formado: 400 con `errorCodes.ids = ["format"]`, y se invalida toda la petición.
- Un elemento que no es cadena en el array del POST: `ids` con código `type`.
- La clave del diccionario es siempre `ids`, no `ids[3]`; la posición del primero malo puede ir en el mensaje.
- Un id bien formado que no existe, o que está fuera del ámbito de organización, simplemente no sale. No da 404 ni error, y así no revela si existe.

Hechos:
- Son los códigos que ya usa el contrato: `fieldError("id", "format", …)` para el id de la ruta, `"pageSize", "range"` para el tamaño y `fieldError(k, "type", …)` para un filtro que no es un valor simple (`contexts/facilities/maintenance.go`).
- Oracle admite 1000 valores por lista y el traductor ya trocea (`translate.go:411`, `MaxInList`).
- SQL Server admite 2100 parámetros por petición (comentario de `pkg/application/merged.go:47`). 1000 ids más los demás filtros caben. No cabrían si en la misma consulta entrara además una lista `NotIn` de más de unas 1000 fusionadas; es el límite que ya está anotado en `BACKLOG.md`. Hoy ningún `maintenance.go` de los siete usa `WithoutMerged`, pero el planificador debe comprobarlo en los casos de uso de proveedores y transportistas.
- Por GET, 1000 UUID son unos 37 KB de URL. Go lo acepta (`DefaultMaxHeaderBytes` es 1 MiB), pero un proxy como nginx corta por defecto en 8 KB.

Opinión:
- Truncar o descartar en silencio deja etiquetas sin resolver sin ninguna señal; es justo el fallo que B-15 quiere quitar.
- Mismo tope en GET y en POST, y se documenta que las listas largas (más de unos 200 ids) van por `POST …/search`.
- Los duplicados se eliminan sin error tras normalizar (los UUID, sin distinguir mayúsculas). El tope se cuenta sobre lo recibido, antes de eliminar duplicados, que es lo más simple de explicar.

## 4. Países: se incluyen, con códigos alfa-2

**Recomendación:** `ids` filtra por el campo `id` del contrato, sea del tipo que sea. En `/api/v1/countries` son códigos alfa-2.

Hechos:
- El contrato ya publica el alfa-2 como `id` (`MaintainedCountry{ID: c.Alpha2}`) y `GET /api/v1/countries/{id}` lo recibe en la ruta (`contexts/geography/maintenance.go:299`).
- La lista de países se filtra en memoria; el filtro son unas pocas líneas en ese bucle, sin consulta nueva.
- Existe `vocab.NewCountryCode` (`pkg/domain/vocab/country.go:28`) para validar el formato.
- El objetivo dice «todas las entidades que lo exponen», y el país es clave ajena de las direcciones.

Opinión:
- Mismos códigos de error: un código que no sean dos letras da `ids/format`; uno bien formado que no existe no sale. Se compara sin distinguir mayúsculas.
- Mismo tope de 1000, aunque sobre, para no tener una regla distinta.
- Los festivos (`HolidaysBase`, mismo fichero) entran como cualquier entidad con UUID.

## 5. Sí a un auxiliar común en `pkg/distribution`, pequeño y sin tipos de contexto

**Recomendación:** una pieza en `pkg/distribution` que haga lo que es regla del contrato: leer `ids` del GET y del POST, trocear por comas, recortar espacios, eliminar duplicados, aplicar el tope y devolver los errores `ids/type` e `ids/range` como `fw.Validation`. Cada contexto sigue convirtiendo cada cadena a su identidad (`domain.ParseFacilityID`, `vocab.NewCountryCode`…) y devuelve `ids/format`.

Hechos:
- Hoy cada `maintenance.go` es autónomo y repite `fieldError`, `readBody`, `listQuery` y el registro de rutas (siete ficheros, unas 3000 líneas).
- `pkg/distribution` ya importa `pkg/domain`, así que no se crea ninguna dependencia nueva entre capas.
- El bucle del POST rechaza hoy cualquier filtro que no sea un valor simple (`default: fieldError(k, "type", …)`). En los siete ficheros hay que sacar `ids` antes de ese `switch`, haya auxiliar o no.
- El contrato es el `crud` de Theros: lo que Theros genere compila contra este framework.

Opinión:
- Las reglas de `ids` (separador, tope, duplicados, códigos) son del contrato, no de cada contexto. Siete copias se desvían, y el juez de construcción exige mutación y cobertura sobre cada una. Una función pura en `pkg/` se prueba una vez y Theros la llama.
- No aprovecharía para sacar también `readBody` o `listQuery` a `pkg/`: es otra tarea.
- Forma orientativa (el diseño es del planificador): una función para `url.Values` y otra para el valor de `filter["ids"]`, que devuelvan `([]string, presente bool, error)`; y, si se quiere, un genérico que aplique el `parse` de cada contexto y junte el `ids/format`.
- Aceptaría en el POST tanto un array de cadenas como una cadena separada por comas, y en el GET tanto `?ids=a,b` como `?ids=a&ids=b`. Cuesta poco y evita 400 por diferencias entre serializadores.
- Es una pieza nueva de `pkg/`, así que según `docs/FORMA-DE-TRABAJO.md` §1 la tarea cuenta como mediana. Ya se lleva así.

## Dos avisos que no se preguntaron

- **Permisos y ámbito:** se heredan sólo si `ids` entra como un campo más del mismo caso de uso `Search…` de cada contexto, convertido en `Field.In(...)` dentro de la especificación. No debe haber un camino nuevo por repositorio ni un bucle de `Get`.
- **`ids` fuera del mapa de filtros:** `listQuery.filters` es `map[string]string`. `ids` debe viajar aparte (lista y presencia) y no copiarse al mapa, para que ningún contexto lo trate como filtro de texto.

## analista

- Documentación: ¿la regla común de `ids` va en `front/react/docs/analisis/02-api-karpo-go.md` §2, en un documento nuevo del backend (por ejemplo `docs/CONTRATO-MANTENIMIENTO.md`) o repetida en la sección «El contrato de mantenimiento» de cada uno de los siete documentos de contexto?
- Tope de ids: ¿se cuenta después de quitar duplicados y vacíos, como hace Karpo C# en `directory/resolve`, en lugar de antes, como se propuso al principio? ¿Se mantiene en 1000 (`MaxPageSize`) y no en 900?
- En `/api/v1/suppliers`, ¿`ids` cuenta como filtro explícito según D-108, de modo que se devuelvan los proveedores fusionados pedidos sin `includeMergedParties`?

# Respuestas del asesor — tarea «b15-filtro-ids»

Sólo he leído; no he tocado nada ni he ejecutado pruebas.

## 1. Dónde va la regla común de `ids`

**Recomendación:** un documento nuevo del backend, `docs/CONTRATO-MANTENIMIENTO.md`, con la regla común escrita una sola vez. En la sección «El contrato de mantenimiento» de cada uno de los siete documentos de contexto, sólo `ids` añadido a la lista de filtros de la fila «Lista», un enlace al documento común y lo que sea propio de esa entidad (en Compras, lo de las fusionadas de la consulta 3).

**Hechos comprobados**
- No existe hoy un documento del contrato en `docs/`. Cada contexto repite su sección y remite a otro contexto: `docs/COMPRAS.md` línea 297 dice «el mismo contrato que el de Productos, Instalaciones, Activos y Envíos (docs/ENVIOS.md)».
- La única descripción transversal es `front/react/docs/analisis/02-api-karpo-go.md` §2 (línea 99). Es una entrega de análisis del front: una foto de lo que respondía Go en una prueba.
- `CLAUDE.md` pide «un documento por contexto y por tema» en `docs/`. El objetivo de la tarea pide documentarlo «en el documento del contrato de mantenimiento y en el de cada contexto tocado».

**Opinión**
- Repetir la regla en siete sitios es como se ha llegado a que cada contexto remita a otro; con una regla que tiene tope, formato, códigos de error y casos límite, las siete copias divergirán.
- `02-api-karpo-go.md` no debe ser el sitio normativo: pertenece al carril del front y es una foto fechada. Como mucho, que el constructor avise al carril del front para que marque B-15 en `front/react/docs/PETICIONES-BACKEND.md`.
- El documento nuevo debe ser corto: operaciones comunes, forma de la página, forma del error y la regla de `ids`. Mover ahí el resto de cada contexto es otra tarea.
- Debe decir también: los `ids` se combinan con los demás filtros por intersección; un id fuera del ámbito o inexistente no aparece y no da error (no se revela su existencia); y hay que pedir `pageSize` mayor o igual que el número de ids, porque la paginación sigue aplicando (por defecto 20).

## 2. Tope de ids

**Recomendación:** sí, contar después de quitar vacíos y duplicados; mantener 1000 (`fw.MaxPageSize`), no 900; y rechazar con 400, nunca recortar.

**Hechos comprobados**
- Karpo C# (`C:\Git\Paranoia\Karpo\020-Back\020-Source\Paranoia.Karpo.ErpDetail.Distribution.Publisher\Parties\PartyDirectoryEndpoints.cs`, líneas 88-96): `Where(g => g != Guid.Empty).Distinct()` y después compara con `MaxIdsPorPeticion`. Rechaza con 400 porque recortar «sería indistinguible de que esas parties no existan».
- El 900 de C# es particular de su adaptador: su comentario (líneas 57-62) dice que EF trocea los `IN` a 900. En Go no aplica: `pkg/persistence/sqlrepo/translate.go` (`Builder.in`, línea 403) trocea la lista según `MaxInList()` del dialecto (Oracle 1000, SQL Server 2000, PostgreSQL 10000, SQLite 900) y une los trozos con `OR`.
- `MaxPageSize = 1000` está en `pkg/domain/page.go:12`. Con el tope igual al tamaño máximo de página, toda respuesta cabe en una página.
- SQL Server admite 2100 parámetros por petición, no por lista (`BACKLOG.md` líneas 83-87). 1000 ids dejan unos 1100 para ámbito, filtros y paginación.

**Opinión**
- Orden de validación: partir por comas, recortar espacios, descartar vacíos, validar cada id (400 en el campo `ids` con código `format`), quitar duplicados sobre el valor ya interpretado (un UUID en mayúsculas y en minúsculas es el mismo) y entonces contar (400 en `ids` con código `range`).
- Contar antes de deduplicar rechazaría peticiones legítimas: una página de 1000 filas con claves ajenas repetidas.
- `ids` presente pero vacío tras limpiar (`?ids=`, `filter.ids: []`): que devuelva página vacía, no «todo». Es coherente con `sqlrepo`, que traduce un `IN` vacío a `1=0`. Hoy un filtro vacío se ignora, así que el planificador debe fijarlo y probarlo.
- En `POST …/search` hoy un filtro que no es un valor simple da 400 `type` («a filter is a single value», `contexts/purchases/maintenance.go`). `filter.ids` debe ser la excepción: array de cadenas, y a mi juicio también la cadena con comas, para que las dos rutas compartan código.
- Documentar que con muchos ids se use `POST …/search`: 1000 UUID en la URL son unos 37 kB. `net/http` de Go lo admite (1 MiB de cabeceras por defecto), pero los proxies habituales cortan hacia 8 kB. Por el cuerpo son unos 39 kB, muy por debajo del tope de 1 MiB (`maxMaintenanceBody`).
- Riesgo a probar: 1000 ids más una lista larga de organizaciones del ámbito o de fusionadas (`NOT IN`) puede pasar de 2100 parámetros en SQL Server. Es el problema ya anotado en `BACKLOG.md`. Basta una prueba de integración con 1000 ids en los tres motores; no justifica bajar el tope.

## 3. `ids` y las fusionadas en `/api/v1/suppliers`

**Recomendación:** sí, los proveedores pedidos por `ids` se devuelven aunque su party esté fusionada, sin `includeMergedParties`. El fundamento más exacto es D-100 («por id se siguen leyendo»); D-108 es el argumento por analogía.

**Hechos comprobados**
- D-108 (`docs/DECISIONES-COMUNES.md:291`, `docs/PARTIES.md:776`) habla del filtro por el campo de la party (`customer=`, `supplier=`, `payee=`…). `ids` filtra por la identidad del perfil de proveedor, que no es literalmente ese caso.
- D-100 (`docs/DECISIONES-COMUNES.md:593`) dice que los registros de fusionadas quedan fuera de los listados pero «por id se siguen leyendo». `GET /api/v1/suppliers/{id}` ya lo cumple: `GetSupplier` no mira fusionadas.
- De los siete `maintenance.go`, sólo el de Compras lee `includeMergedParties`; la pregunta sólo afecta a proveedores.
- Desajuste previo: `PageSuppliers` (`contexts/purchases/application/suppliers.go:357`) pasa sólo `q.IncludeMergedParties`, sin `|| q.Supplier != ""`. El listado de facturas sí lo hace (`invoices.go:295`). Es decir, `/api/v1/suppliers?supplier=<fusionada>` hoy no cumple D-108. No he comprobado si `docs/PARTIES.md` §20 lo recoge como excepción.

**Opinión**
- `ids` es una lectura por identidad en lote. Su fin es poner etiqueta a claves ajenas ya guardadas; si una factura apunta al perfil de un proveedor fusionado y `ids` no lo trae, el front enseña un UUID desnudo. Devolver menos que N llamadas a `GET /{id}` sería incoherente.
- Implementación mínima, la de D-108: `q.IncludeMergedParties || len(q.IDs) > 0`. Como efecto añadido, no se suma el `NOT IN` de fusionadas a los 1000 parámetros en SQL Server.
- El permiso (`Purchases.Supplier.Read`) y el ámbito (`within(scopeOf(ctx), SupFieldCompany)`) no se relajan: la exención es sólo para las fusionadas.
- No es una decisión de producto nueva, se deduce de D-100; aun así conviene anotarlo en `consultas.md` y contárselo a Javier en el informe.
- El desajuste de `supplier=` conviene corregirlo en la misma línea con su prueba, o anotarlo en `BACKLOG.md` si el planificador lo considera fuera de alcance. Callarlo no.

## Lo que no he comprobado
- Qué hace Trench para lo mismo.
- Si el contrato `crud` de Karpo C# o de Theros ya tiene un filtro `ids` con otro nombre o forma. Sólo he mirado `directory/resolve`. Si existe, manda su forma (regla de datos y contrato iguales de `CLAUDE.md`); el analista debería confirmarlo.
- El número real de parámetros de las consultas con ámbito de muchas organizaciones.

## analista

- ¿Se propone alinear POST /api/parties/directory/resolve (contar tras limpiar y errorCode) en una tarea aparte con entrada en DECISIONES-COMUNES, o se deja como está?

ASESOR — tarea «b15-filtro-ids», consulta 1

Recomendación: dejar `POST /api/parties/directory/resolve` como está. No abrir tarea ni entrada en DECISIONES-COMUNES ahora; como mucho, una línea en BACKLOG.md (prioridad baja) que cubra TODOS los directorios a la vez, no sólo Parties.

Lo que sé con seguridad (leído en C:/Git/Paranoia/karpo-fw-go-ids):

1. Es otro contrato y otro público. `Directory.Resolve` es el Open Host Service entre contextos («sirve a contextos, no a usuarios», contexts/facilities/application/service.go:565; docs/PARTIES.md:129 y :819). El filtro de B-15 es contrato de mantenimiento para el front. El front no usa `directory/resolve` (sin apariciones en front/react/src ni en PETICIONES-BACKEND.md).
2. Tolerar ids mal formados allí es deliberado y está probado: contexts/parties/parties_test.go:438 envía un UUID inexistente y "junk" y espera 200 con 2 refs. Es la regla «fallar ≠ no encontrar» de las costuras (docs/EVENTOS-INTEGRACION.md:49). En B-15 el id mal formado da 400; son reglas distintas a propósito, no una incoherencia que corregir.
3. El mismo patrón (contar antes de limpiar, `fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, …)` sin errorCode) está repetido en al menos cinco sitios: contexts/parties/application/publications.go:126, details.go:167, ports.go:233; contexts/facilities/application/service.go:571; contexts/hr/application/ports.go:87 (`tooMany`); y hay `Resolve` equivalentes en geography y security. Alinear sólo Parties crearía una incoherencia nueva.
4. «Contar tras limpiar» no arregla nada real hoy: los consumidores ya trocean por `MaxDirectoryBatch` (host/datasets.go:113/203/255, host/datasets_trade.go:40, host/pairs.go:359, contexts/treasury/infrastructure/persistence.go:532, contexts/fiscal/infrastructure/persistence.go:479).
5. El 900 es «el 900 de C#» (contexts/parties/contracts/contracts.go:126): es contrato reflejado de Karpo C#. Cambiar su comportamiento observable sí pediría D-n y aviso a la sesión de C#; por eso no conviene colarlo en B-15 («un commit por tarea»).

Mi opinión:

- La única carencia real es que el 400 por exceso de lote no lleva errorCode. Es menor (lo llaman contextos, no pantallas). Si algún día se hace, que sea una tarea propia «errorCode en el límite de lote de los directorios» para parties, facilities, hr, geography y security juntos; añadir un errorCode es aditivo y no necesita D-n, salvo que además se cambie el recuento o el 900.
- Para el constructor de B-15: no implementar `filter.ids` reutilizando `Directory.Resolve`. Ese puerto no aplica permisos de usuario ni ámbito de organización y calla los ids malos; el filtro debe ir por la consulta de lista del contrato de mantenimiento (como una especificación más, tipo `WithIDs`), para heredar ámbito y permisos.
- Sobre el máximo: `domain.MaxPageSize` es 1000 (pkg/domain/page.go:12) y el directorio usa 900. Oracle admite como mucho 1000 expresiones en un `IN` (ORA-01795) y SQL Server 2100 parámetros por consulta. No he comprobado cómo se traduce `WithIDs` a SQL en cada motor; conviene que la prueba de integración (slot 61) ejercite el filtro con exactamente el máximo de ids en los tres motores. Si se quiere margen, 900 con el mismo criterio que el directorio es defendible; cualquiera de los dos vale si esa prueba pasa.

No ejecutado: ninguna compilación ni prueba (consulta de sólo lectura). No se ha tocado ningún fichero.

## juez-construccion

- Para Javier, o para el asesor si el orquestador lo prefiere: el mínimo del 80 % de §5, ¿se aplica también cuando lo que falta es código que ya había en un paquete tocado sólo con unas pocas líneas nuevas, que están cubiertas al 100 %? Si la respuesta es que no, el único hallazgo que queda es el de la documentación del orden entre el 403 y el 400, y la entrega se puede aprobar en cuanto se corrija.
- Para Javier: los mutantes que sobreviven en código anterior de un dominio tocado (por ejemplo, shipments/domain/carrier.go:64-102), ¿cuentan como «explicados» si se dice que son anteriores y se apuntan en el BACKLOG, o hay que matarlos dentro de la misma tarea?

Respuestas del asesor a las dos consultas de juez-construccion (tarea b15-filtro-ids, commit eb8577d en C:/Git/Paranoia/karpo-fw-go-ids). No he ejecutado cobertura ni gremlins: razono sobre el texto de §5 y sobre el diff.

AVISO PREVIO: las dos consultas piden interpretar criterios que fijó Javier (docs/FORMA-DE-TRABAJO.md §5). Yo doy una recomendación; no puedo cambiar el criterio. Las dos interpretaciones son técnicas y reversibles (se deshacen abriendo una tarea de pruebas), así que según CLAUDE.md se pueden tomar, anotar y contar a Javier al final, sin parar el arnés. Si el orquestador prefiere que decida él antes, son dos decisiones, ambas de sí o no.

---

CONSULTA 1 — el 80 % cuando lo que falta es código anterior

Recomendación: no bloquear la entrega por ello. Aplicar el 80 % como «trinquete»: (a) las líneas nuevas o cambiadas cubiertas (aquí, al 100 %); (b) la cobertura del paquete no baja respecto a la de origin/main; (c) el déficit anterior se apunta en BACKLOG.md, con el paquete y su cifra, como tarea de tipo «testing». Con (a), (b) y (c), el criterio se da por cumplido y el juez lo deja escrito en veredictos.md como interpretación pendiente de confirmar por Javier.

Lo que es seguro:
- El texto literal dice «Cobertura mínima del 80 % en los paquetes tocados». Leído al pie de la letra, sí se aplica al paquete entero, tenga las líneas nuevas que tenga. La lectura que recomiendo es una interpretación, no lo que pone.
- §1 del mismo documento clasifica «un filtro» como tarea pequeña. Esta tarea añade entre 3 y 12 líneas de producción por fichero en ocho contextos (git show --stat eb8577d). Obligar a subir al 80 % cada paquete tocado convertiría un filtro en una campaña de pruebas sobre seis o siete contextos, dentro de un commit que CLAUDE.md quiere «uno por tarea».
- §5 ya prevé un tipo de tarea «Testing» con criterio propio. El sitio para pagar deuda de pruebas anterior existe.

Lo que es opinión:
- Distinguir cobertura del cambio y cobertura del proyecto es la práctica habitual: Codecov separa «patch» y «project», y SonarQube aplica su puerta de calidad sólo al código nuevo («Clean as You Code») precisamente para que la deuda heredada no bloquee cambios pequeños. La condición de no bajar la cifra es lo que evita que la excepción se convierta en coladero.
- Conviene que Javier añada una frase a §5 que lo diga, para que el siguiente juez no vuelva a preguntar. Propuesta de texto para él, no para esta rama (docs/FORMA-DE-TRABAJO.md no se toca en esta tarea): «En un paquete que ya estaba por debajo del 80 %, basta con que el código nuevo esté cubierto y la cifra del paquete no baje; el déficit anterior va al BACKLOG».

Consecuencia para el veredicto: si el juez adopta esta lectura, queda el hallazgo de documentación (orden entre 403 y 400). docs/CONTRATO-MANTENIMIENTO.md línea 40 ya dice «El permiso, antes. Sin permiso de leer la lista, 403»; lo que falta es decir de forma explícita que con ids mal formados y sin permiso gana el 403. Falta además la entrada de BACKLOG del punto (c): el commit sólo añade al BACKLOG la de «errorCode en el límite de lote de los directorios».

---

CONSULTA 2 — mutantes supervivientes en código anterior del dominio tocado

Recomendación: no hay que matarlos en esta tarea, pero «son anteriores» a secas no es una explicación. Cuentan como explicados si se cumplen las tres condiciones:
1. Ningún mutante sobrevive en las líneas que añade o cambia la tarea (los XxxWithIDs y los campos XxxFieldID). Esos se matan o se justifican uno a uno como equivalentes.
2. Los anteriores quedan listados en BACKLOG.md con fichero, líneas y tipo de mutación (no «hay mutantes en shipments»), como tarea de tipo «testing».
3. El comportamiento nuevo no depende de la condición mutada.

Aplicado al ejemplo: contexts/shipments/domain/carrier.go:64-102 es la validación de ReconstituteCarrier y checkReason (límites de 120, 300, 20 y 200 caracteres, formato del código, caracteres de control). El cambio de la tarea en ese fichero son las líneas 203-204 y 216-217 (CarFieldID y CarriersWithIDs). El filtro por ids no depende de ninguno de esos límites, así que la condición 3 se cumple y van al BACKLOG.

Lo que es seguro:
- §5 dice «sobre el dominio tocado» y «un mutante superviviente sin explicar impide aprobar». No distingue código nuevo de anterior: la lectura literal los incluye, y por eso hace falta la explicación escrita y no basta con callarlos.
- La fila «Testing» de la tabla de §5 dice «Las pruebas nuevas matan mutantes que antes sobrevivían». El documento da por hecho que hay mutantes anteriores vivos y que se matan en tareas de pruebas.

Lo que es opinión:
- En la práctica de mutación, «explicado» significa mutante equivalente (no cambia el comportamiento observable) o descartado con motivo escrito. Ampliarlo a «anterior, ajeno al cambio y apuntado con su localización» es razonable; ampliarlo a «anterior» sin más vaciaría el criterio, porque casi todo dominio tocado tiene código anterior.
- Los del ejemplo son mutantes de frontera, baratos de matar con una tabla de casos (nombre de 120 y 121 caracteres, código de 20 y 21, etc.). Si el constructor quiere matarlos ya, mejor en un commit o tarea aparte, para no mezclar pruebas de validación de transportistas con el filtro ids.
- Para vueltas futuras: gremlins tiene, según recuerdo, una opción para mutar sólo lo cambiado respecto a una referencia de git (--diff); StrykerJS tiene --since e --incremental. No lo he comprobado en la versión que usa el arnés; conviene verificarlo antes de apoyarse en ello. Serviría para que el juez mida la condición 1 por separado.

---

Qué queda para aprobar, si el juez adopta ambas lecturas:
1. Documentar en docs/CONTRATO-MANTENIMIENTO.md el orden entre 403 y 400 (el hallazgo ya abierto).
2. Añadir a BACKLOG.md dos entradas: los paquetes tocados por debajo del 80 % con su cifra, y los mutantes anteriores supervivientes con fichero, líneas y tipo.
3. Dejar en docs/trabajo/b15-filtro-ids/consultas.md estas dos respuestas y en veredictos.md que son interpretaciones de §5 a confirmar por Javier. Esa carpeta no existe todavía en la rama, y §4 la pide.

Comando para comprobar la base de la consulta 1 (cobertura por paquete tocado, para comparar con origin/main):
cd C:/Git/Paranoia/karpo-fw-go-ids; go test -count=1 -cover ./pkg/application/ ./pkg/distribution/ ./contexts/shipments/... ./contexts/assets/... ./contexts/geography/...
Debería verse una línea «ok … coverage: NN.N % of statements» por paquete; las que queden por debajo de 80 son las que van al BACKLOG con su cifra.

## constructor

- ¿Confirma Javier las dos lecturas de §5 del asesor? Son el «trinquete» de cobertura (un paquete puede quedar por debajo del 80 % si no baja y tiene su entrada en BACKLOG) y qué cuenta como mutante explicado (listarlos uno a uno en BACKLOG, decir con qué se obtuvieron y que el filtro ids no depende de ellos).

## Respuesta del asesor a la consulta 1 del constructor (tarea b15-filtro-ids)

**No, Javier no las ha confirmado, y yo no puedo confirmarlas en su nombre.** El constructor no debe parar por ello: sigue con las dos lecturas como provisionales y las deja señaladas como decisión pendiente de Javier.

### Lo que sé con seguridad

- **No consta ninguna confirmación de Javier.** `docs/trabajo/b15-filtro-ids/consultas.md` (línea 28) y `veredictos.md` («Pendiente de Javier») dicen que las dos lecturas siguen pendientes. La petición de Javier que lanzó esta ejecución no las menciona.
- **Son interpretaciones mías, no texto de §5.** `docs/FORMA-DE-TRABAJO.md` §5 dice literalmente «cobertura mínima del 80 % en los paquetes tocados» y «un mutante superviviente sin explicar impide aprobar». No habla de «trinquete» ni define «explicado».
- **Los criterios de §5 los fijó Javier** (2026-11-27). Sólo él puede cambiarlos o fijar su lectura. Ni el asesor, ni el juez, ni el texto de la tarea valen como su confirmación.
- **`BACKLOG.md` (líneas 84-110 aprox.) ya lleva las dos entradas que piden las lecturas:**
  - cobertura: `hr/application` 72,9 → 73,0 % y `products/application` 77,3 → 77,6 %, medidas contra `origin/main`;
  - mutantes: listados uno a uno por dominio, con fichero, línea y tipo, la herramienta usada (mutador propio, porque gremlins v0.5.0 en Windows da todo por tiempo agotado) y la nota de que el filtro `ids` no depende de ellos.
- No he vuelto a ejecutar ni la cobertura ni el mutador: las cifras son las que constan en el repositorio.

### Mi opinión (recomendación)

1. **Seguir, no parar.** Aplicar las lecturas es reversible: si Javier las rechaza, basta con rehacer el veredicto y añadir pruebas, y no hay que deshacer código. §7 reserva las paradas para decisiones de producto, irreversibles o desviaciones de patrón, y ésta no es ninguna de las tres.
2. **El juez puede aprobar sólo de forma condicionada.** El veredicto debe decir «aprobado según las lecturas provisionales del asesor, pendientes de Javier». No debe presentarlo como cumplimiento literal de §5.
3. **Debe llegar a Javier como decisión explícita en el informe final**, antes de fusionar. Son dos preguntas:
   - ¿Vale el «trinquete»? Es decir, ¿puede un paquete tocado quedar por debajo del 80 % si no baja respecto a `origin/main` y tiene su entrada de testing en `BACKLOG.md`? La alternativa es que tocar un paquete obligue a subirlo al 80 % en la misma rama.
   - ¿Cuenta como «explicado» un mutante vivo en código anterior si se cumplen las tres condiciones (listado uno a uno en `BACKLOG.md`, herramienta declarada, y el cambio no depende de él)? La alternativa es que haya que matarlo en la misma rama.
4. **Si Javier confirma, las lecturas deben pasar a §5 de `docs/FORMA-DE-TRABAJO.md`**, para que no se vuelvan a consultar en cada tarea. Eso lo hace la sesión de orquestación en otro commit: en esta tarea ese fichero no se toca.
5. **Una cautela sobre la medida de mutación.** El mutador propio sustituye a gremlins, que es la herramienta que nombra §5 y §8. Es una desviación de herramienta razonable, porque gremlins no sirve en Windows, pero también debe constar en el informe a Javier. Puede ir como dato dentro de la segunda pregunta, sin abrir una tercera decisión.

### Qué anotar en la memoria de trabajo

En `docs/trabajo/b15-filtro-ids/consultas.md`, el constructor debe añadir: «Consulta 3 (constructor, vuelta 2): ¿confirma Javier las dos lecturas? Respuesta del asesor: no consta confirmación; el asesor no puede darla. Se sigue con las lecturas como provisionales y se elevan a Javier en el informe final, antes de fusionar.»

Ficheros citados:
- C:/Git/Paranoia/karpo-fw-go-ids/docs/FORMA-DE-TRABAJO.md (§5, §7, §8)
- C:/Git/Paranoia/karpo-fw-go-ids/docs/trabajo/b15-filtro-ids/consultas.md
- C:/Git/Paranoia/karpo-fw-go-ids/docs/trabajo/b15-filtro-ids/veredictos.md
- C:/Git/Paranoia/karpo-fw-go-ids/BACKLOG.md
