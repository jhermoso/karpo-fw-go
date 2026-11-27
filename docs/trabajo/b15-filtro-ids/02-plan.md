# B-15: filtro por varios ids. Plan aprobado (segunda versión)

Petición B-15 del front React: un filtro por varios identificadores en el contrato de
mantenimiento (`GET /api/v1/<entidad>?ids=a,b,c` y `POST …/search` con `filter.ids`), para
resolver de una vez las etiquetas de claves ajenas. El contrato resultante está en
[`docs/CONTRATO-MANTENIMIENTO.md`](../../CONTRATO-MANTENIMIENTO.md).

El análisis aprobado (tercera versión) no se guardó en esta carpeta; este plan parte de él.

## 1. Contextos afectados

| Contexto | Ruta | Cambio |
|---|---|---|
| assets | `/api/v1/assets` | Dominio: `AstFieldID`, `AssetsWithIDs`. Aplicación y distribución |
| facilities | `/api/v1/facilities` | Aplicación y distribución (`WithIDs` ya existía) |
| hr | `/api/v1/work-centers` | Aplicación y distribución (`WorkCentersWithIDs` ya existía) |
| products | `/api/v1/products` | Dominio: `ProductsWithIDs`. Aplicación y distribución |
| purchases | `/api/v1/suppliers` | Dominio: `SupFieldID`, `SuppliersWithIDs`. Con `ids` y con `supplier=` salen también las fusionadas |
| shipments | `/api/v1/carriers` | Dominio: `CarFieldID`, `CarriersWithIDs`. Aplicación (`PageCarriers`) y distribución |
| geography | `/api/v1/holidays`, `/api/v1/countries` | Dominio: `HolidayFieldID`, `HolidaysWithIDs`. Festivos sin ámbito; países, alfa-2 en memoria |
| pkg/application | — | `ParseIDs` genérico: interpreta, quita repetidos y aplica el tope |
| pkg/distribution | — | `IDsFromQuery`, `IDsFromFilter`: sólo leen las cadenas |

No se tocan: parties (`directory/resolve` es otro contrato), los alias `/approvals`, migraciones
y esquema, Theros y Karpo C# (sólo reciben aviso).

## 2. Patrones

- Especificación `XFieldID = spec.Comparable("id", …)` y `XWithIDs(ids...) = XFieldID.In(ids...)`.
- El filtro va en la consulta de lista existente; no hay consulta nueva.
- Formato y tope en la aplicación, con `fw.Validation` (campo `ids`, códigos `format` y `range`),
  como los demás filtros de identidad. Distribución sólo lee y da `type`.
- `XWithIDs` entra detrás del término de ámbito, dentro del manejador que ya comprueba el permiso.
  Festivos no tienen ámbito: sólo el permiso.
- Desviación: países no usa especificación (catálogo de ~250 filas filtrado en memoria); su
  validación alfa-2 vive en el manejador.

Sin librerías de terceros.

## 3. Decisiones (técnicas y reversibles)

- **D-a.** Tope de 1000 (`fw.MaxPageSize`), contado después de limpiar. Con 1000 hay un solo `IN`
  en los tres motores (Oracle trocea a partir de 1001). Si SQL Server fallase, bajar a 900.
- **D-b.** Ids mal formados: 400 `ids/format`. El directorio sigue descartándolos en silencio;
  alinearlo queda en `BACKLOG.md`.
- **D-c.** La identidad cero da `ids/format`.
- **D-d.** `ids` vacío tras limpiar: 200 con página vacía (`IN ()` se traduce a `1=0`); sin permiso,
  403.
- **D-e.** `ids` se combina con AND y no cambia `pageSize`.
- **D-f.** En suppliers, `ids` y `supplier=` incluyen las fusionadas (D-100, D-108).
- **D-g.** Interpretación y tope en `pkg/application`; distribución sólo lee.

## 4. Tareas

| # | Tarea | Modelo |
|---|---|---|
| T1 | `pkg/distribution/ids.go` y `pkg/application/ids.go` con pruebas | Sonnet, medio |
| T2 | Dominio: `XFieldID`/`XWithIDs` y `ProductsWithIDs` | Haiku, bajo |
| T3 | assets, facilities, hr, products, shipments: aplicación y distribución | Sonnet, medio |
| T4 | purchases (fusionadas) y geography (festivos, países) | Sonnet, alto |
| T5 | Documentación | Haiku, medio |
| T6 | Integración con 1000 ids (slot 61), cobertura, mutación, Comprobar y commit | Opus, medio |

## 5. Pruebas

Unitarias de lectura y de `ParseIDs` (vacíos, espacios, repetidos con distinta forma, `junk`,
UUID cero, 1000 pasa, 1001 distintos falla, 1001 con un repetido pasa, orden de primera
aparición); dominio por cada `XWithIDs`; HTTP por GET y POST en las seis listas con ámbito y en
festivos (pedidos, inexistente, otra organización, `format`, `range`, vacío, 403, AND, UUID cero);
suppliers con fusionadas; países (`es`, `XX`, `ESP`, `1A`); integración con 1000 ids en los
motores; mutación y cobertura del 80 % en los paquetes tocados.
