## B-15: filtro por varios ids (análisis, tercera versión, con los hallazgos del juez y las respuestas del asesor)

Carpeta `C:/Git/Paranoia/karpo-fw-go-ids`, rama `feat/filtro-ids`. Sólo he leído código. No he ejecutado pruebas ni he escrito código.

### 1. Situación de partida

**Petición.** Está en `front/react/docs/PETICIONES-BACKEND.md:29` (B-15, P1, T1). Hoy el front resuelve las etiquetas de las claves ajenas leyendo una página de 1000 filas o pidiendo los registros de uno en uno. Pide `filter.ids` o `?ids=a,b,c`.

**Superficie.** Cada `contexts/*/maintenance.go` expone una sola entidad, salvo el de geography, que expone dos:

| Fichero | Ruta |
|---|---|
| assets | `/api/v1/assets` |
| facilities | `/api/v1/facilities` |
| hr | `/api/v1/work-centers` |
| products | `/api/v1/products` |
| purchases | `/api/v1/suppliers` |
| shipments | `/api/v1/carriers` |
| geography | `/api/v1/countries` (sólo lectura; `id` = alfa-2) y `/api/v1/holidays` (UUID), con un registro común de lista y búsqueda (`geography/maintenance.go:160-180`) |

Cómo se leen hoy los filtros:
- El GET copia los parámetros a `q.filters` con `values.Get`. Por eso no distingue `?ids=` de que `ids` no llegue.
- El POST `…/search` devuelve 400 `type` cuando un filtro no es un valor simple (`facilities/maintenance.go:248`). Un array `ids` caería ahí.
- Ningún DTO `Search…` o `Page…` lleva una lista de ids.

**Fuera de alcance.** Las rutas `/api/v1/<contexto>/approvals` de financial, orders, purchases, receivables y shipments (por ejemplo `purchases/module.go:29,176`) son alias de la cola de aprobaciones, no del contrato de mantenimiento. B-15 no las toca.

**Ámbito de organización en los seis listados con especificación (comprobado).** El ámbito es el primer término de `parts` y se combina con los filtros con `spec.And`:

| Contexto | Primer término de `parts` | Dónde |
|---|---|---|
| assets | `within(scopeOf(ctx), AstFieldCompany)` | `assets/application/service.go:442` |
| facilities | `scopeOf(ctx).spec()` | `facilities/application/service.go:513` |
| hr | `within(…, WCFieldEmployer)` | `hr/application/workcenters.go:229` |
| products | `within(…, ProdFieldCompany)` | `products/application/products.go:299` |
| purchases | `within(…, SupFieldCompany)` | `purchases/application/suppliers.go:334` |
| shipments | `within(…, CarFieldCompany)` | `shipments/application/shipments.go:481` |

El ámbito sale de `EffectiveOrganizations`.

**Campo de identidad en el dominio (comprobado).** Para filtrar por ids hace falta un campo comparable del tipo de la identidad:
- **Ya lo tienen tres:**
  - facilities: `FieldID` y `WithIDs` (`facilities/domain/facility.go:356,370`).
  - hr: `WCFieldID` y `WorkCentersWithIDs` (`hr/domain/workcenter.go:130`, `hr/domain/ids.go:51`).
  - products: `ProdFieldID` (`products/domain/product.go:347`).
- **No lo tienen cuatro.** Sólo tienen un `spec.Ordered("id", …string)`, que sirve para desempatar el orden:
  - assets: `AstFieldIDOrder` (`asset.go:361`).
  - carriers: `CarFieldIDOrder` (`shipments/domain/carrier.go:202`).
  - suppliers: `SupFieldIDOrder` (`purchases/domain/supplier.go:170`).
  - holidays: `HolidayFieldIDOrder` (`geography/domain/holiday.go:85`).

  En esos cuatro hay que declarar `XFieldID = spec.Comparable("id", …XID)` y `XWithIDs(ids ...XID)`, con el mismo patrón que facilities, hr, parties y security. Es dominio puro y no cambia el esquema.

**Geography funciona de otra manera (comprobado):**
- **Países.** No pasan por una especificación. El manejador lee los unos 250 con `ListCountries` y los filtra, ordena y pagina en memoria (`geography/maintenance.go:249-296`). No hay `parts`, ni ámbito de organización, ni `Field.In`.
- **Festivos.** Usan `Holidays.Page` (`geography/application/holidays.go:331-335`) con `require(ctx, PermHolidayRead)`. Tienen permiso, pero no ámbito de organización.
- **Códigos de país.** `vocab.NewCountryCode` (`pkg/domain/vocab/country.go:28`) también rechaza un código bien formado que no esté en la lista ISO.

**Fusionadas (comprobado):**
- `/api/v1/suppliers` aplica `WithoutMerged` según `includeMergedParties` (`purchases/maintenance.go:167`, `purchases/application/service.go:56`).
- `PageSuppliers` (`suppliers.go:357`) sólo pasa `q.IncludeMergedParties`. `invoices.go:295`, en cambio, añade `|| q.Supplier != ""`. Por eso hoy `supplier=<fusionada>` no cumple D-108.
- `/api/v1/carriers` no filtra fusionadas.

**Framework:**
- `In` vacío se traduce a `1=0` (`pkg/persistence/sqlrepo/translate.go:409`).
- `Builder.in` trocea las listas según `MaxInList()` del dialecto: Oracle 1000, SQL Server 2000, PostgreSQL 10000, SQLite 900.
- `fw.MaxPageSize = 1000` (`pkg/domain/page.go:12`).
- SQL Server admite 2100 parámetros por petición (`BACKLOG.md:83-87`, `pkg/application/merged.go:47`).

**Precedente propio en Go: `POST /api/parties/directory/resolve`.**
- Rutas en `contexts/parties/distribution/http.go:53` y `:307`. Lo implementa `Directory.Resolve` (`parties/application/publications.go:125`).
- El tope es `contracts.MaxDirectoryBatch = 900` («el 900 de C#», `parties/contracts/contracts.go:126-127`).
- Cuenta **antes** de limpiar.
- Descarta en silencio los ids mal formados y los vacíos (`ParsePartyID` con `err == nil && !IsZero`).
- Si se supera el tope devuelve `fw.ErrValidation` sin campo ni errorCode.
- No aplica permisos de usuario ni ámbito.
- Descartar los ids malos es deliberado y está probado: `parties_test.go:438` espera 200 con 2 refs al enviar `"junk"`. Es la regla «fallar ≠ no encontrar» de las costuras (`docs/EVENTOS-INTEGRACION.md:49`).
- El mismo patrón se repite en `parties/application/details.go:167`, `ports.go:233`, `facilities/application/service.go:571` y `hr/application/ports.go:87`, y hay `Resolve` equivalentes en geography y security.
- Es un Open Host Service entre contextos, que «sirve a contextos, no a usuarios» (`docs/PARTIES.md:129`, `:819`). El front no lo usa.

**Karpo C#.** `PartyDirectoryEndpoints.cs` tiene tope 900, porque su adaptador EF trocea a 900. Quita los Guid vacíos y los duplicados antes de contar, devuelve 400 si se supera el tope sin recortar, va por POST por el límite de 8 KB de nginx y no aplica ámbito. No he encontrado `ids` en su contrato `crud`.

**Trench.** No tiene un filtro HTTP por lista de ids de la propia entidad. Sólo lectura por lote interna: `RoleTypeApplicationService.GetByIdsAsync` (`:58`). `PartyFilterBuilder.cs:304` limpia las listas con `Where(g != Guid.Empty).Distinct()`.

**Theros.** Genera el contrato `/api/v1` (`contract_test.go:65`). Hay que avisar a su sesión. Desde aquí no se toca.

**Decisiones comunes:**
- D-100 (`DECISIONES-COMUNES.md:593`): los registros de fusionadas no salen en los listados, pero «por id se siguen leyendo».
- D-108 (línea 291; `PARTIES.md:776`): un filtro explícito por la party trae sus registros aunque esté fusionada.

**Documento del contrato.** No hay uno común en `docs/`. Cada contexto tiene su sección «El contrato de mantenimiento» (`INSTALACIONES.md:86`, `ACTIVOS.md`, `GEOGRAFIA.md`, `RRHH.md`, `PRODUCTOS.md`, `COMPRAS.md`, `ENVIOS.md`). `front/react/docs/analisis/02-api-karpo-go.md` §2 es un análisis del carril del front, no un documento normativo.

### 2. Fuentes y patrones

| Fuente | Qué dice | Uso |
|---|---|---|
| Karpo C#, `directory/resolve` | Limpia y quita duplicados antes de contar; 400 si se supera el tope; POST para listas largas | Se adopta el orden y el 400. No se adoptan el 900 ni la falta de ámbito |
| Go, `Directory.Resolve` | 900, cuenta antes de limpiar, calla los ids malos, sin errorCode | Otro contrato y otro público; no se reutiliza (§3.7) |
| Trench, `PartyFilterBuilder` | Quita vacíos y duplicados | Confirma el orden de limpieza |
| Prisma `in: []`, Hasura `_in`, Rails | Una lista vacía no devuelve nada | Falla en cerrado, igual que el `1=0` del traductor |
| Google AIP-158 | Una respuesta puede traer menos elementos que la página, nunca más | No se fuerza `pageSize` |
| Google AIP-231 | Tope y error si se supera (lo cito de memoria) | Respalda el 400 frente a recortar |
| RFC 9110 y nginx | Los proxies cortan la cabecera hacia 8 KB | Las listas largas, por POST |

### 3. Hechos y opiniones

Los hechos están en §1. Lo que sigue son opiniones.

1. **Presencia.** Si `ids` no llega o llega `null`, no se filtra. Si llega vacío después de limpiar (`[]` o `?ids=`), la respuesta es 200 con una página vacía. El caso vacío pasa igualmente por el caso de uso, para que el 403 se siga dando. El DTO lleva `IDs []string` y `ByIDs bool`.
2. **Combinación con otros filtros.** `ids` se combina con AND con los demás filtros y con el ámbito. No cambia la paginación (por defecto 20): el documento dirá que se pida `pageSize` mayor o igual al número de ids. No implica `isActive`.
3. **Orden de validación**, paso a paso:
   1. Partir por comas.
   2. Recortar espacios.
   3. Descartar los vacíos.
   4. Validar el formato de cada id. Si alguno falla, 400 `ids/format`. Si un elemento no es una cadena, 400 `ids/type`.
   5. Quitar duplicados sobre el valor ya interpretado.
   6. Contar. Si hay más de 1000, 400 `ids/range`.

   Un id bien formado que no existe, o que queda fuera del ámbito, no aparece en la respuesta y no da error.
4. **Tope: 1000** (`MaxPageSize`). Así toda la respuesta cabe en una página. El traductor trocea por dialecto, de modo que Oracle (1000 por `IN`) queda cubierto. El asesor considera defendibles tanto 900 como 1000, siempre que la integración pase con exactamente el máximo en los tres motores. Esa prueba decide: si SQL Server pasa de 2100 parámetros con un ámbito grande, se baja a 900 y se anota.
5. **Diferencias con `Directory.Resolve`, a propósito, y sin alinearlo:**
   - **El 400 frente a descartar los ids mal formados.** El directorio sirve a contextos con la regla «fallar ≠ no encontrar». El contrato de mantenimiento sirve al usuario y ya responde 400 con errorCode cuando un filtro está mal formado.
   - **Contar después de limpiar.** Así se evita rechazar una página legítima de 1000 filas con claves repetidas. Es el orden de Karpo C#.
   - **1000 frente a 900.** El 900 del directorio es contrato reflejado de C# y sirve para otra cosa.
   - **Por qué no se cambia el directorio en B-15.** Cambiarlo es otro contrato, afecta al menos a cinco sitios y pediría una D-n y aviso a C#. Se deja como está. Como mucho, una línea de prioridad baja en `BACKLOG.md`: «errorCode en el límite de lote de los directorios (parties, facilities, hr, geography, security)».
6. **Auxiliar común en `pkg/distribution`.** Una función pura que lee `url.Values` (`?ids=a,b` y `?ids=a&ids=b`) y `filter["ids"]` (un array o una cadena con comas). Devuelve `([]string, presente, error)`, con los pasos 1 a 3 aplicados y el error `ids/type`. Cada contexto interpreta el id con su tipo, quita duplicados, cuenta y produce `ids/format` o `ids/range`; esos tres pasos pueden ir en un segundo auxiliar genérico. `ids` no se copia a `q.filters`.
7. **Ámbito y permisos.** En los seis listados con especificación se añade `XWithIDs(ids...)` a `parts` en el mismo caso de uso `Search…` o `Page…`, de modo que el ámbito y el `guard` se heredan.
   - En assets, carriers, suppliers y holidays primero hay que declarar `XFieldID` comparable y `XWithIDs` (§1).
   - No se reutiliza `Directory.Resolve`, que no aplica ni permisos ni ámbito.
   - Se descarta comparar cadenas contra la columna UUID con el campo `Ordered`, porque no está comprobado que funcione en los tres motores y el tipo de la identidad es más claro.
8. **Geography:**
   - **Festivos.** `HolidayWithIDs` en `parts` de `Holidays.Page`. Sólo hay permiso `PermHolidayRead`, porque no hay ámbito de organización que respetar.
   - **Países.** El filtro es un conjunto de alfa-2 aplicado en memoria, dentro del bucle de `maintenance.go:249-296`, antes de ordenar y paginar. El formato se valida como dos letras ASCII, sin distinguir mayúsculas, y se normaliza a mayúsculas. **No** se usa `vocab.NewCountryCode`: un código bien formado pero inexistente, como `XX`, simplemente no aparece (200). Así se cumple la regla del punto 3. `1A` o `ESP` dan 400 `ids/format`. El tope es el mismo, 1000.
9. **Fusionadas.** En `/api/v1/suppliers`, `ids` trae también los perfiles de parties fusionadas, con `q.IncludeMergedParties || q.ByIDs`, por D-100 y D-108: así `ids` equivale a N llamadas a `GET /{id}`, y además el `NOT IN` de fusionadas no se suma a los parámetros. Carriers no necesita nada. El desajuste de `supplier=` (`suppliers.go:357`) lo decide el planificador: se corrige en esa misma línea, con su prueba, o se anota en `BACKLOG.md`.
10. **Documentación:**
    - Un documento nuevo y corto, `docs/CONTRATO-MANTENIMIENTO.md`, con la regla común.
    - En los siete documentos de contexto, `ids` en la fila «Lista» y un enlace al documento común.
    - `COMPRAS.md`: además, lo de las fusionadas.
    - `GEOGRAFIA.md`: además, el alfa-2.
    - Avisos: al carril del front, para que marque B-15, y a la sesión de Theros.
11. **Tamaño.** Mediana, porque hay una pieza nueva en `pkg/`, cuatro campos de dominio nuevos y ocho listados. La integración en los tres motores (slot 61) se hace con exactamente 1000 ids, porque la consulta cambia.

**Sin comprobar:**
- El número real de parámetros de 1000 ids más un ámbito grande en SQL Server. Lo dirá la integración.
- Si `PARTIES.md` §20 recoge el caso `supplier=` de proveedores como excepción a D-108.
- Si el contrato `crud` de Karpo C# tiene un filtro equivalente con otro nombre.
- Cómo traduce Theros los filtros de su plantilla.
- Que `ProdFieldID` tenga ya un `ProductsWithIDs`. Si no lo tiene, se añade como en hr.
