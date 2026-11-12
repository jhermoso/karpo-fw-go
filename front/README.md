# Front de Karpo Go

Karpo Go no tiene un front propio: usa el **motor de Angular de Karpo C#**
(`010-Front/Angular` del repositorio de Karpo), que se dirige por configuración. Aquí está la
configuración de las pantallas de los contextos de Go que hablan el **contrato de
mantenimiento** (las rutas, las formas y los errores que el motor lee; el contrato `crud` del
lenguaje de Theros).

| Contexto | Pantalla | Configuración | API |
|---|---|---|---|
| Productos | catálogo de productos: rejilla, alta, detalle, retirada | `angular/config/entities/products/product.config.json` | `/api/v1/products` (docs/PRODUCTOS.md, «El contrato de mantenimiento») |

## Cómo se ve

1. Arrancar el anfitrión de Go (`go run ./cmd/karpo`, sobre SQLite; ver docs/ANFITRION.md).
2. En una copia del motor de Angular, copiar `angular/config/entities/products/product.config.json`
   sobre `public/config/entities/products/product.config.json` y apuntar el proxy de `/api` del
   servidor de desarrollo al puerto del anfitrión de Go.
3. Entrar con un usuario de Go con `Products.Product.Read` (y `…Update` para cambiar).

El motor antepone `/api/v1` al `apiEndpoint` de la configuración (`/products`).

## Lo que falta

- Probarlo en un navegador: la prueba de Go recorre el contrato por HTTP
  (`TestProducts_MaintenanceContract_MemoryThenSQLite`), no el motor.
- Las partes del producto (códigos de barras, componentes, proveedores) no viajan por el
  contrato: se mantienen por `/api/products/products`, y un cambio por el contrato las conserva.
- Los demás contextos.
