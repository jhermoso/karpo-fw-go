# B-15: construcción

Rama `feat/filtro-ids`. Vuelta 1: commit `eb8577d`. Vuelta 2: el commit siguiente.

## Qué se hizo (vuelta 1)

- `pkg/distribution/ids.go`: `IDsFromQuery` (`?ids=a,b` y la clave repetida; `?ids=` es «presente
  y vacío») e `IDsFromFilter` (lee y borra `filter.ids`; array o cadena con comas; un elemento que
  no es cadena da `ids/type`).
- `pkg/application/ids.go`: `ParseIDs[T comparable]`: parte, recorta, interpreta, quita repetidos;
  mal formado o identidad cero dan un solo `ids/format`; más de 1000 tras limpiar, `ids/range`.
- Dominio: `AstFieldID`/`AssetsWithIDs`, `SupFieldID`/`SuppliersWithIDs`,
  `CarFieldID`/`CarriersWithIDs`, `HolidayFieldID`/`HolidaysWithIDs`, `ProductsWithIDs`.
- Aplicación: `IDs` y `ByIDs` en `SearchFacilities`, `SearchAssets`, `SearchWorkCenters`,
  `SearchProducts`, `PageSuppliers`, `PageCarriers` y `PageHolidays`, detrás del ámbito. Suppliers:
  con `ids` o `supplier=` salen también las fusionadas.
- Distribución: las 7 listas por GET y `POST …/search`; países con `canonAlpha2` en memoria.
- Pruebas: unitarias, en memoria por `XWithIDs`, `testkit.CheckIDsFilter` (seis listas con ámbito y
  festivos, en memoria y SQLite), países, suppliers con fusionadas, e
  `integration/ids_filter_test.go` (1000 ids pasan y 1001 fallan en postgres, supabase,
  sqlserver, oracle y mysql; slot 61, después `docker compose -p integration-s61 down -v`).
- Documentación: `docs/CONTRATO-MANTENIMIENTO.md`, las filas «Lista» de los siete documentos de
  contexto, el enlace en `ARQUITECTURA.md` y la línea del directorio en `BACKLOG.md`.

Desviaciones: sin subagentes (no había herramienta Agent); `IDs []string` hace no comparables las
consultas de lista (se cambió un mapa en una prueba de integración); `testkit.CheckIDsFilter` es
pieza nueva; en países el 400 de `ids` va antes del 403; `ParseIDs` usa `comparable` con
`var zero T`.

## Vuelta 2: hallazgos del juez

1. **Orden 403/400.** `CONTRATO-MANTENIMIENTO.md` dice ahora el orden real: `type` (distribución)
   y el `format`/`range` de países van antes del permiso; en las demás listas, `format` y `range`
   van después. No se ha movido código.
2. **Países sin permiso.** `contexts/geography/maintenance_test.go` (`idsFilter`): `?ids=` sin
   permiso da 403; `?ids=ESP`, 400 `ids/format`; `POST` con `[1]`, 400 `ids/type`.
3. **Cobertura.** Entrada de testing en `BACKLOG.md` con `hr/application` (72,9 → 73,0 %) y
   `products/application` (77,3 → 77,6 %).
4. **Mutantes anteriores.** Listados uno a uno en `BACKLOG.md`. Gremlins en Windows da todo por
   tiempo agotado, así que se usó un mutador propio con `go test -overlay`:

   | Dominio | Muertos | Vivos |
   |---|---|---|
   | assets | 73 | 16 |
   | facilities | 31 | 13 |
   | geography | 4 | 30 |
   | hr | 66 | 23 |
   | products | 129 | 44 |
   | purchases | 59 | 27 |
   | shipments | 12 | 153 |

   Ninguno está en líneas de B-15, que no tienen operadores; el filtro `ids` no depende de esas
   condiciones.
5. **Memoria de trabajo.** Esta carpeta: `02-plan.md`, `03-construccion.md`, `consultas.md`,
   `veredictos.md`. El análisis no se guardó y no se reconstruye.
