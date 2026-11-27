# B-15: filtro por varios ids. Plan (segunda versión, con los hallazgos del juez)

Carpeta `C:/Git/Paranoia/karpo-fw-go-ids`, rama `feat/filtro-ids`. El plan parte del análisis aprobado (tercera versión). En esta versión no he ejecutado nada ni he vuelto a leer código: tomo como comprobadas las referencias que da el juez.

Qué cambia respecto a la primera versión:
- **La interpretación, la deduplicación y el tope pasan a la aplicación**, como el resto de filtros de identidad.
- Se corrige lo que decía de Oracle y del troceo.
- Festivos no tienen ámbito.
- Se decide qué pasa con la identidad cero.
- Se usan los nombres reales de las consultas.
- Cada tarea lleva su modelo.

`ProductsWithIDs` no existe; sólo está `ProdFieldID` (`product.go:347`), así que hay que añadirlo.

## 1. Contextos afectados

| Contexto | Ruta | Cambio |
|---|---|---|
| assets | `/api/v1/assets` | Dominio: `AstFieldID` y `AssetsWithIDs`. Aplicación y distribución |
| facilities | `/api/v1/facilities` | Aplicación y distribución (`WithIDs` ya existe) |
| hr | `/api/v1/work-centers` | Aplicación y distribución (`WorkCentersWithIDs` ya existe) |
| products | `/api/v1/products` | Dominio: `ProductsWithIDs`. Aplicación y distribución |
| purchases | `/api/v1/suppliers` | Dominio: `SupFieldID` y `SuppliersWithIDs`. Aplicación: con `ByIDs` y con `supplier=` salen también las fusionadas. Distribución |
| shipments | `/api/v1/carriers` | Dominio: `CarFieldID` y `CarriersWithIDs`. Aplicación y distribución |
| geography | `/api/v1/holidays`, `/api/v1/countries` | Dominio: `HolidayFieldID` y `HolidaysWithIDs`. Aplicación: `Holidays.Page`. Distribución: el registro común de lista y búsqueda; en países, un filtro alfa-2 en memoria |
| pkg/application | — | Nuevo `ParseIDs` genérico: interpreta, quita duplicados y aplica el tope, con `fw.Validation` |
| pkg/distribution | — | Nuevos `IDsFromQuery` e `IDsFromFilter`: sólo leen las cadenas |

**No se tocan:** parties (`directory/resolve` es otro contrato), los alias `/approvals`, las migraciones y el esquema, Theros y Karpo C# (que sólo reciben un aviso).

## 2. Artefactos y patrones obligatorios

**`pkg/domain`**
- Identidades: las de cada contexto, con su `Parse…ID`. No hay ninguna nueva.
- Especificaciones: `XFieldID = spec.Comparable("id", func(x *X) XID { return x.ID() })` y `XWithIDs(ids ...XID) spec.Spec[*X] { return XFieldID.In(ids...) }`, como en `facilities/domain/facility.go:370` y `hr/domain/ids.go:51`. `…FieldIDOrder` se queda para desempatar el orden.
- Agregados, eventos y rasgos: sin cambios. Países no usa `vocab.NewCountryCode`, porque un alfa-2 inexistente no es un error de formato.

**`pkg/application`**
- CQRS: el filtro va en la **consulta de lista que ya existe**. No hay consulta nueva.
- Validación: la misma que el resto de filtros de identidad. El formato se valida en la aplicación con `fw.Validation`, campo `"ids"` y código `format`, como hacen `facilities/application/service.go:520-525`, los `parseID` de assets (`:99`), products (`:151`), purchases (`:157`), shipments (`:135`) y hr (`:182`), y `geography/application/holidays.go:339`. El tope también se aplica aquí, en un solo sitio, de modo que cubre a cualquier llamador de la consulta (HTTP, una plantilla de Theros u otro canal), no sólo a la entrada HTTP.
- Permisos y ámbito: `XWithIDs` entra en `parts` detrás del término de ámbito y dentro del manejador que ya llama a `guard`/`require`. **Salvo festivos**, que no tienen ámbito: allí sólo cuenta `require(ctx, PermHolidayRead)`.
- Idempotencia y transacción: no aplican, porque es una lectura.

**Distribución:** sólo lee las cadenas y comprueba la forma del JSON (código `type`), igual que hace hoy con los demás filtros (`facilities/maintenance.go:248`).

**Rutas:** no hay ninguna nueva. Se aceptan dos entradas en las que ya existen:
- `GET …?ids=a,b,c`, que también acepta `ids=a&ids=b`;
- `POST …/search` con `{"filter":{"ids":[…]}}`, que también acepta una cadena con comas.

**Desviación única: países no usa especificación.** Es un catálogo de unos 250 registros que el manejador ya filtra en memoria. Montarle un repositorio con especificaciones sólo para esto sería complejidad innecesaria. Su validación alfa-2 vive en el manejador, como el resto de su filtrado.

(La desviación 1 de la primera versión desaparece: ya no hay validación de formato en distribución.)

## 3. Librerías de terceros

Ninguna; basta la biblioteca estándar.

| Candidata | Veredicto |
|---|---|
| Biblioteca estándar (`net/url`, `strings`) | **Elegida**: coste cero y suficiente |
| `gorilla/schema` (BSD-3) | Descartada: no resuelve `a,b` ni el JSON |
| `go-playground/validator` (MIT) | Descartada: no da nuestros errorCodes |

Gremlins se ejecuta con `go run …@versión` y no entra en el `go.mod`.

## 4. Consecuencias de cada decisión (tres niveles)

**D-a. Tope de 1000 (`fw.MaxPageSize`), contado después de limpiar y aplicado en la aplicación**
1. Una página completa del front se resuelve en una sola llamada, y las claves repetidas no se rechazan.
2. Con 1000 ids, el traductor emite **un solo `IN`** en los tres motores: Oracle trocea a partir de 1001 (`MaxInList` 1000, `oracle/oracle.go:66`), SQL Server a partir de 2001 (`sqlserver.go:38`) y PostgreSQL a partir de 10001. Por eso la integración con 1000 ids no ejercita el troceo. Lo que prueba es:
   - que Oracle acepta un `IN` de exactamente su máximo;
   - que SQL Server se queda por debajo de 2100 parámetros con 1000 ids más el ámbito y el resto de filtros;
   - que el tipo UUID de la identidad se compara bien en los tres motores.

   El troceo ya lo cubren las pruebas de `Builder.in`, y con el tope de 1000 nunca se llega a él.
3. Si SQL Server falla, se baja la constante común a 900 (un solo cambio) y se anota en el documento. Theros y el front leen el tope del documento. No hay migración que deshacer.

**D-b. Los ids mal formados dan 400 `ids/format`; el directorio los descarta en silencio**
1. El front ve el error, en lugar de etiquetas que faltan sin explicación.
2. Conviven dos reglas: la del usuario y la de las costuras entre contextos. El documento común explica por qué.
3. Alinear el directorio pediría una D-n y avisar a C#. Queda una línea de prioridad baja en `BACKLOG.md`.

**D-c. La identidad cero da `ids/format`**
1. Es lo mismo que hace hoy cualquier filtro de identidad: el `parseID` de cada aplicación exige `err == nil && !IsZero()`. Aunque los `Parse…ID` del dominio la aceptan (por ejemplo `purchases/domain/events.go:50`), `ParseIDs` la rechaza.
2. Una clave ajena sin valor no se cuela como un id que no encuentra nada: el front debe quitarla antes de pedir.
3. Hay una prueba unitaria en `ParseIDs` y otra HTTP en una entidad.

**D-d. Tras limpiar, `ids` vacío da 200 con página vacía y pasa por el caso de uso**
1. Falla en cerrado (`In()` vacío se traduce a `1=0`, `translate.go:409`).
2. El 403 sin permiso se mantiene, y lo prueba un caso negativo.
3. Ningún repositorio tiene que tratar el vacío aparte.

**D-e. `ids` se combina con AND y no cambia `pageSize`**
1. Si se piden 50 ids con `pageSize` 20, llegan 20. El documento lo dice.
2. Se respeta AIP-158.
3. Theros no necesita ningún caso especial.

**D-f. En suppliers, `ByIDs` y `supplier=` incluyen las fusionadas (D-100, D-108)**
1. `ids` equivale a N llamadas a `GET /{id}`.
2. El `NOT IN` de fusionadas no se suma a los parámetros.
3. Se corrige `PageSuppliers` (`suppliers.go:357`) con la misma regla que `invoices.go:295`. Es una línea, lleva su prueba y se anota en `COMPRAS.md`. Es reversible.

**D-g. Interpretación y tope en `pkg/application`; distribución sólo lee**
1. Hay un solo sitio que interpreta. Los errores llevan campo y código (`fw.Validation`) y salen por el mapeo de errores que ya tiene cada `maintenance.go`. Desaparecen `IDsError` y la conversión a `fieldError` en cada contexto.
2. Theros genera la misma llamada en su plantilla y hereda el tope sin hacer nada más.
3. `pkg/application` no gana ninguna dependencia, porque la pieza es genérica sobre `parse func(string) (T, error)`.

## 5. Diseño detallado

### 5.1 `pkg/distribution/ids.go` (nuevo; sólo lectura)

```go
// IDsFromQuery lee ?ids=a,b y ?ids=a&ids=b; present=false si la clave no aparece.
// Usa v["ids"], no v.Get: "?ids=" cuenta como presente y vacío.
func IDsFromQuery(v url.Values) (raw []string, present bool)

// IDsFromFilter lee y borra filter["ids"]: []any de cadenas o una cadena con comas.
// nil o ausente => present=false. Un elemento que no es cadena => fw.Validation con ("ids","type").
func IDsFromFilter(filter map[string]any) (raw []string, present bool, err error)
```

Ninguna de las dos parte ni recorta: devuelven los valores tal como llegan. Para el error `type` se usa el mismo constructor de error de campo que ya emplea el `default:` de `maintenance.go:248`. Si ese constructor es local de cada contexto, `IDsFromFilter` devuelve `fw.Validation` y cada manejador lo escribe por la vía normal.

### 5.2 `pkg/application/ids.go` (nuevo)

```go
// ParseIDs parte por comas, recorta, descarta vacíos, interpreta con parse
// (fallo o identidad cero => v.Add("ids","format", ...)), quita duplicados por el
// valor interpretado y, si quedan más de fw.MaxPageSize, añade ("ids","range").
// Devuelve los ids en orden de primera aparición; con errores, nil.
func ParseIDs[T interface{ comparable; IsZero() bool }](v *fw.Validation, raw []string, parse func(string) (T, error)) []T
```

- Si alguna identidad no tiene `IsZero()`, se sustituye la restricción por una comparación con `var zero T`; las dos son válidas, y el constructor elige.
- Con varios ids malos se añade un solo error `format`, que nombra el primero. Así no salen mil errores.
- El orden de los pasos es el del análisis: la validación de formato va antes de quitar duplicados y de contar.

### 5.3 Dominio

| Fichero | Añadido |
|---|---|
| `contexts/assets/domain/asset.go` | `AstFieldID`, `AssetsWithIDs` |
| `contexts/purchases/domain/supplier.go` | `SupFieldID`, `SuppliersWithIDs` |
| `contexts/shipments/domain/carrier.go` | `CarFieldID`, `CarriersWithIDs` |
| `contexts/geography/domain/holiday.go` | `HolidayFieldID`, `HolidaysWithIDs` |
| `contexts/products/domain/product.go` | `ProductsWithIDs` sobre `ProdFieldID` |

Si el mapeo SQL de alguno no registra el campo comparable `"id"`, se registra junto al de orden.

### 5.4 Aplicación

Consultas con su nombre real:

| Contexto | Consulta |
|---|---|
| facilities | `SearchFacilities` (`facilities/application/service.go:124`) |
| assets | La consulta de lista de `assets/application/service.go:442` |
| hr | `SearchWorkCenters` (`hr/maintenance.go:183`, `hr/application/workcenters.go:229`) |
| products | La de `products/application/products.go:299` |
| purchases | `PageSuppliers` (`suppliers.go:334-357`) |
| shipments | **`PageCarriers`** (`shipments/application/shipments.go:479`). No `SearchCarriers`, que devuelve `[]CarrierDTO` sin paginar |
| geography | `Holidays.Page` (`holidays.go:331-345`) |

A cada una se le añade:

```go
IDs   []string // tal como llegan; los interpreta la aplicación
ByIDs bool     // true si el filtro llegó, aunque esté vacío
```

En el manejador, en la misma validación donde hoy se interpretan los demás filtros de identidad:

```go
var ids []domain.XID
if q.ByIDs { ids = application.ParseIDs(v, q.IDs, domain.ParseXID) }
// … v.Err() como hoy …
if q.ByIDs { parts = append(parts, domain.XWithIDs(ids...)) } // tras el ámbito
```

Casos particulares:
- **suppliers:** `includeMerged := q.IncludeMergedParties || q.ByIDs || q.Supplier != ""`.
- **holidays:** no hay término de ámbito, así que `HolidaysWithIDs` entra en `parts` sin más, detrás de `require(ctx, PermHolidayRead)`.

### 5.5 Distribución (cada `contexts/*/maintenance.go`)

- **GET:** `if raw, ok := distribution.IDsFromQuery(r.URL.Query()); ok { q.IDs, q.ByIDs = raw, true }`. `ids` no entra en `q.filters`.
- **POST `…/search`:** se llama a `IDsFromFilter` antes del bucle de `body.Filter`. La función borra la clave, así que el `default:` de la línea 248 no ve el array. Si devuelve un error, se escribe con la vía normal de errores de validación.
- **Países** (`geography/maintenance.go:249-296`): en este único caso el manejador valida los ids.
  - `ParseIDs` se usa con `parse = canonAlpha2`: exactamente dos letras ASCII, que se devuelven en mayúsculas; si no, error.
  - Dentro del bucle, antes de ordenar y paginar: `if byIDs && !set[c.Alpha2] { continue }`.
  - `XX` da 200 vacío; `ESP` y `1A` dan 400 `ids/format`.

**Mensajes (en inglés):**

| Código | Mensaje |
|---|---|
| `type` | "ids must be strings" |
| `format` | "invalid id: <valor>" |
| `range` | "at most 1000 ids" |

### 5.6 Documentación

- **`docs/CONTRATO-MANTENIMIENTO.md` (nuevo y corto):**
  - las dos formas de enviar `ids`;
  - presente frente a ausente;
  - el vacío da una página vacía;
  - el orden de validación;
  - la identidad cero da `format`;
  - los errorCodes `type`, `format` y `range`;
  - el tope de 1000;
  - el AND con los demás filtros y con el ámbito (festivos sólo tienen permiso);
  - `pageSize` mayor o igual al número de ids;
  - los inexistentes o fuera de ámbito se omiten sin error;
  - las listas largas, por POST (8 KB);
  - la diferencia con `directory/resolve`.
- **Los siete documentos de contexto** (`ACTIVOS`, `INSTALACIONES`, `RRHH`, `PRODUCTOS`, `COMPRAS`, `ENVIOS`, `GEOGRAFIA`): `ids` en la fila «Lista», con un enlace al documento común.
  - `COMPRAS.md`: las fusionadas con `ids` y con `supplier=` (D-108).
  - `GEOGRAFIA.md`: el alfa-2 en países y que festivos no tiene ámbito.
- **`docs/ARQUITECTURA.md`:** un enlace al documento nuevo, si tiene índice.
- **`BACKLOG.md`:** una línea de prioridad baja: «errorCode en el límite de lote de los directorios (parties, facilities, hr, geography, security)».
- **Avisos** al carril del front (para que marque B-15) y a la sesión de Theros. `PETICIONES-BACKEND.md` no se edita aquí.

## 6. Reparto en tareas

| # | Tarea | Depende de | Modelo y esfuerzo | Por qué |
|---|---|---|---|---|
| T1 | `pkg/distribution/ids.go` (lectura y `type`) y `pkg/application/ids.go` (`ParseIDs` con `format`, la identidad cero, la deduplicación y `range`), con sus pruebas | — | Sonnet, medio | Piezas puras con casos límite claros |
| T2 | Dominio: los cuatro `XFieldID`/`XWithIDs` y `ProductsWithIDs`, con pruebas en memoria | — | Haiku, bajo | Mecánico, con el patrón a la vista |
| T3 | Aplicación y distribución de assets, facilities, hr (`SearchWorkCenters`), products y shipments (`PageCarriers`), con pruebas HTTP | T1, T2 | Sonnet, medio | Repetitivo; un solo agente para que salga uniforme |
| T4 | purchases (fusionadas y `supplier=`) y geography (festivos sin ámbito; países alfa-2) | T1, T2 | Sonnet, alto | Los dos casos con reglas propias |
| T5 | Documentación (§5.6) | T3, T4 | Haiku, medio | Texto ya decidido |
| T6 | Integración con 1000 ids (slot 61, y después `docker compose -p integration-s61 down -v`), cobertura, gremlins y la batería de Comprobar; después, el commit | T3, T4, T5 | Opus, medio | Ejecutar y juzgar resultados en tres motores, y decidir si se baja a 900 |

T1 y T2 van en paralelo, y T3 y T4 también. Un solo commit en la rama, sin push. Los ficheros `.claude/agents/` y `docs/FORMA-DE-TRABAJO.md` no se tocan.

## 7. Pruebas

**Unitarias de distribución:**
- ausente;
- `?ids=`, que cuenta como presente;
- `ids=a&ids=b`;
- `null`, que no filtra;
- un array con un número, que da `type`;
- una cadena con comas en el JSON;
- que la clave se borra del filtro.

**Unitarias de `ParseIDs`:**
- `a,,b` con espacios;
- duplicados con distinta forma del mismo UUID, que se quitan después de interpretar;
- `"junk"`, que da `format`;
- **el UUID cero, que da `format`**;
- exactamente 1000, que pasa;
- 1001 distintos, que da `range`;
- 1001 con un duplicado, que pasa;
- se conserva el orden de primera aparición.

**Dominio:** cada `XWithIDs` selecciona sólo los ids dados, y ninguno con la lista vacía.

**HTTP, GET y POST, en los seis listados con ámbito:**
- 200 sólo con los pedidos;
- un id inexistente se omite;
- **un id de otra organización se omite**;
- `ids/format` y `ids/range` dan 400 con errorCodes;
- vacío da 200 con página vacía;
- sin permiso, 403 también con la lista vacía;
- el AND con otro filtro;
- el UUID cero da `ids/format` en una entidad.

**Festivos:** las mismas pruebas **sin la de otra organización**, que no aplica porque no hay ámbito.

**Casos propios:**
- suppliers: la fusionada sale con `ids` y con `supplier=`, y no sale sin filtro;
- países: `es` sale; `XX` da 200 vacío; `ESP` y `1A` dan 400.

**Integración** (slot 61): exactamente 1000 ids sobre una entidad con ámbito, en PostgreSQL, SQL Server y Oracle. Comprueba lo que se dice en D-a.2: un `IN` único en el máximo de Oracle, los parámetros de SQL Server con el ámbito, y la comparación del UUID. No ejercita el troceo. Si SQL Server falla, se baja a 900 y se anota.

**Mutación** (gremlins) sobre `pkg/application`, `pkg/distribution` y los dominios tocados. No puede quedar ningún mutante vivo sin explicar en `ParseIDs`: el `>` frente a `>=` del tope, el orden entre deduplicar y contar, y la comprobación de la identidad cero.

**Cobertura:** 80 % o más en los paquetes tocados.

## 8. Decisiones para Javier

Ninguna. Todas son técnicas y reversibles: el tope de 1000 con 900 de reserva, los errorCodes, la identidad cero, las fusionadas y `supplier=`. Quedan anotadas aquí y se le cuentan en el resultado.

## 9. Consultas al asesor

Ninguna.
