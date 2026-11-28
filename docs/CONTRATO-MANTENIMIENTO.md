# Contrato de mantenimiento: el filtro `ids`

El contrato de mantenimiento es el que leen los motores de rejilla del front: `/api/v1/<entidad>`,
con la lista por `GET` y por `POST …/search`, la ficha por `GET /{id}`, el alta, el cambio y la
baja. Cada contexto describe el suyo en su documento. Aquí se fija lo que es común a todos: el
filtro `ids`, que pide varias identidades en una sola llamada para resolver de una vez las
etiquetas de las claves ajenas de una página (petición B-15 del front React).

## Dónde está

| Ruta | Contexto |
|---|---|
| `/api/v1/assets` | [Activos](ACTIVOS.md) |
| `/api/v1/facilities` | [Instalaciones](INSTALACIONES.md) |
| `/api/v1/work-centers` | [RRHH](RRHH.md) |
| `/api/v1/products` | [Productos](PRODUCTOS.md) |
| `/api/v1/suppliers` | [Compras](COMPRAS.md) |
| `/api/v1/carriers` | [Envíos](ENVIOS.md) |
| `/api/v1/holidays`, `/api/v1/countries` | [Geografía](GEOGRAFIA.md) |

## Cómo se envía

- `GET …?ids=a,b,c`, o la clave repetida: `?ids=a&ids=b`.
- `POST …/search` con `{"filter": {"ids": ["a", "b"]}}`, o una cadena con comas:
  `{"filter": {"ids": "a,b"}}`.

Una lista larga va mejor por `POST`: 1000 identidades son unos 37 KB, y muchos servidores y
proxies cortan la URL hacia los 8 KB.

## Qué hace

- **Presente o ausente.** Sin la clave (o con `"ids": null` en `POST`) no se filtra. Con la clave,
  se filtra aunque llegue vacía: `?ids=` o `"ids": []` dan **200 con una página vacía**, no la
  lista entera.
- **Limpieza.** Se parte por comas, se recortan los espacios y se descartan los vacíos; después se
  quitan los repetidos (también los que sólo difieren en mayúsculas).
- **Y con lo demás.** `ids` se combina con AND con los otros filtros y con el ámbito de
  organización del usuario. Un id que no existe, o que es de otra organización, se omite sin
  error. Los festivos no tienen ámbito: basta el permiso de leerlos.
- **El permiso va primero.** Sin permiso de leer la lista, 403, también con la lista vacía. El
  permiso se comprueba **antes** de leer o validar nada, para no dar información a quien no puede
  leer: sin permiso, 403 aunque el cuerpo no sea JSON, un id no sea cadena (`{"ids":[1]}`), los
  ids estén mal formados, la página esté fuera de rango o una bandera de países (`eu`,
  `eurozone`, `sepa`) no valga. En la distribución, `GET` y `POST …/search` van envueltos en
  `distribution.RequirePermission` con el permiso de lectura de la lista; la aplicación lo vuelve
  a comprobar.
- **La página no cambia.** `ids` no toca `pageSize`: si se piden 50 con `pageSize=20`, llegan 20.
  Quien resuelve etiquetas pide un `pageSize` mayor o igual que el número de ids.
- **Países.** Los ids son códigos alfa-2 (dos letras, sin distinguir mayúsculas). Un código bien
  formado que no existe (`XX`) sólo no está; `ESP` o `1A` son 400.

## Errores

400 con el diccionario de errores de siempre, en el campo `ids`. Se valida en este orden:

| Código | Cuándo | Mensaje |
|---|---|---|
| `type` | En `POST`, un elemento que no es una cadena (`[1]`, `true`) | `ids must be strings` |
| `format` | Un valor que no es una identidad de la entidad, o la identidad cero (`00000000-0000-0000-0000-000000000000`). Se informa sólo del primero | `invalid id: <valor>` |
| `range` | Más de **1000** identidades distintas después de limpiar (los repetidos no cuentan) | `at most 1000 ids` |

El tope es `MaxPageSize` (1000), el tamaño máximo de una página. Con 1000 identidades la consulta
es un solo `IN` en PostgreSQL, SQL Server y Oracle (Oracle trocea a partir de 1001); lo comprueba
`integration/ids_filter_test.go` en los motores.

## Dónde vive

- `pkg/distribution`: `IDsFromQuery` e `IDsFromFilter` sólo leen las cadenas (y dan `type`).
- `pkg/application`: `ParseIDs` interpreta, quita repetidos y aplica el tope (`format` y
  `range`). Está en la aplicación para que valga para cualquier llamador de la consulta, no sólo
  para HTTP.
- Cada contexto: la consulta de lista lleva `IDs` y `ByIDs`, y su dominio la especificación
  `…WithIDs`, que entra detrás del término de ámbito.
- `pkg/testing/testkit`: `CheckIDsFilter` prueba el filtro en una lista por HTTP.

## Diferencia con `directory/resolve`

Los directorios entre contextos (por ejemplo, el de Parties) también resuelven varias
identidades, pero son una costura entre contextos y no una entrada del usuario: allí un id mal
formado se descarta en silencio. En el contrato de mantenimiento el front debe saber que ha
mandado algo mal, y recibe `ids/format`. Alinear los directorios está en `BACKLOG.md`.
