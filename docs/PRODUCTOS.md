# Contexto Productos

Port del catálogo de C# al contexto `contexts/products`. En C# estaba en
`ErpKernel/Subdominios/Product` (165 ficheros de dominio, 44 tablas), con los precios repartidos
entre ese subdominio y Parties de ErpDetail:

- **Product:** `Product` (con `Good` y `Service` como subtipos TPT vacíos), `Part`,
  `ProductCommercialProfile`, categorías, características, identificaciones, unidades de medida,
  `PriceComponent`, costes, `SupplierProduct` y seis tablas de relaciones entre productos.
- **Parties (ErpDetail):** `PriceList`, `PriceListLine`, `DiscountList`, `VatGroup` y
  `CustomerPriceGroup`.
- **Orders (ErpDetail):** el `PricingEngine`.

El stock (`InventoryItem`, `StockBalance`) se trata en [INVENTARIO.md](INVENTARIO.md).

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

- **Sin reglas:** todos los `Validate()` devuelven éxito salvo el del perfil comercial.
  - No hay unicidad de SKU, de código de barras ni de nombre.
  - El tipo de producto es un texto libre (`"Good"`, `"Service"`, `"Product"`) y a la vez un
    subtipo TPT; cambiarlo se hace con `DELETE` e `INSERT` a mano sobre la tabla hija.
  - El borrado es físico y no mira si el producto está referenciado.
- **Cada concepto, modelado varias veces y sin enlazar:**
  - **precio:** `PriceComponent`, `PriceListLine`, `ProductCommercialProfile.BaseSalesPrice` (que
    el motor ignora) y `AdvisoryServiceCatalog.UnitRate`;
  - **coste:** `EstimatedProductCost` y tres campos del perfil, ninguno calculado;
  - **códigos:** la tabla `ProductIdentification` (sin endpoint) y las columnas `Sku` y `Barcode`
    del perfil;
  - **proveedor:** `SupplierProduct` y tres campos del perfil;
  - **umbrales de stock:** el perfil y `StockBalance`.
- **Motor de precios** (`PricingEngine`):
  - el precio base no se puede dar de alta por la API (exige un tercero), así que el último nivel
    de la cascada nunca tiene dato;
  - la búsqueda en `PriceComponent` es un `FirstOrDefault` sin orden que ignora la caducidad: un
    acuerdo vencido puede ganar;
  - la vigencia y los tramos por cantidad solo se aplican en `PriceListLine`;
  - la moneda de la tarifa no se lee nunca;
  - `MinSalesPrice`, `AllowDiscounts`, `IsBlockedSales` y `VatGroupId` no se consultan.
- **Unidades de medida:** 14 sembradas; la tabla de conversiones no tiene endpoint y su factor no
  se puede asignar (siempre guarda 0).
- **Lista de materiales:** `ProductComponent` es de solo lectura por HTTP; no se pueden crear
  kits.
- **Tablas sin migración:** `product_commercial_profile` (solo un script de Postgres),
  `price_list_line` y `customer_price_group`.
- **Datos del vertical en catálogos genéricos:** tres categorías de cambio de divisa entre ocho
  genéricas que no tienen nombre.
- **Sin permisos** en ningún endpoint, y los productos no tienen empresa: solo `product_offering`
  los relaciona con una.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `Product` + `Good`/`Service` + `Part` + `ProductCommercialProfile` | 5 | 2 | 2 | 3 | **65** | Modificar → un agregado `Product` **por empresa**: SKU único, nombre, tipo (bien o servicio, **fijo**), unidad, categoría, código de impuesto de Fiscal, categoría de gasto de Compras, precio base, coste estándar, se vende, se compra, se almacena, trazabilidad (ninguna, lote o serie), bloqueos. Un servicio no se almacena. **No se borra: se descataloga** desde una fecha |
| 2 | `ProductIdentification` + `Sku`/`Barcode` del perfil | 3 | 1 | 2 | 3 | **46** | Modificar → códigos dentro del producto: EAN-13, EAN-8 y UPC-A **con dígito de control**, o código interno. **Un código identifica un solo producto** de la empresa |
| 3 | `ProductComponent` (solo lectura) y las otras cinco relaciones | 2 | 2 | 2 | 3 | **43** | Modificar → componentes del kit dentro del producto: bienes de la misma empresa, cantidad positiva, **sin ciclos** a ninguna profundidad. Sustitutos, complementos, incompatibilidades y obsolescencias, retirados (sin uso) |
| 4 | `SupplierProduct` + campos de proveedor del perfil | 3 | 2 | 2 | 3 | **52** | Modificar → proveedores dentro del producto: referencia del proveedor, plazo en días y un único preferido |
| 5 | `ProductCategory` + clasificación + rollup (categorías sin nombre) | 3 | 2 | 2 | 3 | **52** | Modificar → agregado `Category` por empresa: código único, nombre y padre. Sin semilla |
| 6 | `UnitOfMeasure` + `UnitOfMeasureConversion` (factor siempre 0) | 4 | 1 | 2 | 3 | **53** | Modificar → catálogo fijo de las 14 unidades con su dimensión y su factor. **Conversión dentro de la misma dimensión**; caja y palé no convierten |
| 7 | `PriceList` + `PriceListLine` (en Parties) | 4 | 3 | 2 | 3 | **63** | Modificar → agregado `PriceList` por empresa con sus líneas: producto, cantidad mínima, precio, descuento y vigencia. **Dos precios del mismo producto y cantidad no se solapan en el tiempo** |
| 8 | `PricingEngine` (cascada de cinco niveles) + `PriceComponent` y sus seis subtipos | 4 | 2 | 2 | 3 | **58** | Modificar → `QuoteOf`: la línea vigente de la tarifa con la mayor cantidad mínima que no supere la pedida; si no hay, el **precio base del producto**. Los precios y descuentos **por cliente** van con Pedidos. `PriceComponent` y sus subtipos, retirados |
| 9 | `DiscountList` (sin líneas), `PricingFactor`, `EstimatedProductCost`, `CostComponentType` | 1 | 2 | 2 | 2 | **33** | Retirar |
| 10 | `ProductFeature*`, `ProductQuality`, `ProductionRun`, `ProductQuote`, `ProductRequirement` | 1 | 2 | 2 | 2 | **33** | Retirar → sin uso; las características volverán si hacen falta variantes |
| 11 | `VatGroup` | 2 | 2 | 2 | 3 | **43** | Sustituido → el código de impuesto del catálogo de Fiscal |
| 12 | Cuentas contables del perfil (`SalesAccountId`…) | 1 | 2 | 1 | 2 | **30** | Retirar → las cuentas las decide el perfil del libro de Contabilidad |
| 13 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Products.Product.Read/Update` (catálogo) y `Products.PriceList.Read/Update` (precios), separados, con ámbito por empresa |

## Diseño

```
contexts/products/
├── domain/          # Product (+Barcode, Component, SupplierItem), Category, PriceList (+PriceLine, QuoteOf), Unit y Convert
├── contracts/       # lenguaje publicado v1 y puertos Catalog y Pricing
├── application/     # casos de uso con permisos y ámbito
├── infrastructure/  # esquema prd_* de 5 motores, mapeos
└── module.go        # composición y rutas /api/products/...
```

- **Puertos para los demás contextos:**
  - `Catalog.Products(ids)` devuelve lo que necesitan de un producto: empresa, SKU, nombre, tipo,
    unidad, código de impuesto, categoría de gasto, si se almacena, trazabilidad, bloqueos y
    descatalogación.
  - `Pricing.Quote(empresa, producto, tarifa, cantidad, fecha)` devuelve precio, descuento y
    precio neto, e indica si viene de la tarifa o del precio base.
- **Precios con hasta 4 decimales**; el neto se redondea a 4.
- **Búsqueda** por texto (nombre sin distinguir mayúsculas, o SKU exacto), tipo, categoría y
  código de barras.
- **Eventos:** `products.product-registered.v1` y `products.product-discontinued.v1`.
- **Tablas:**
  - `prd_products` (+ `prd_barcodes`, `prd_components`, `prd_product_suppliers`);
  - `prd_categories`;
  - `prd_price_lists` (+ `prd_price_lines`);
  - las bandejas de salida y la auditoría.

## Decisiones propuestas (pendientes de confirmar)

1. **El catálogo es de cada empresa.** Cada producto pertenece a una organización interna, con
   SKU y códigos de barras únicos dentro de ella. No hay catálogo compartido entre empresas ni
   `product_offering`.
2. **Un solo agregado `Product`** sustituye a `Product`, `Part` y el perfil comercial. El tipo
   (bien o servicio) no cambia, y un producto se descataloga, no se borra.
3. **Las tarifas viven en Productos** (no en Parties) y el precio sale de la tarifa o, si no hay
   línea vigente, del precio base. **Los precios y descuentos por cliente o grupo de clientes
   van con Pedidos**, que es quien conoce al cliente.
4. **Las unidades de medida son un catálogo fijo en código**, con conversión solo dentro de la
   misma dimensión. Añadir una unidad es un cambio de versión, no un alta de usuario.
5. **Se retiran** las características, las relaciones entre productos distintas de los
   componentes del kit, `PriceComponent`, `DiscountList` y los costes estimados. Las variantes de
   producto (talla, color) quedan para cuando haya un caso real.

## Validación

- **Dominio:**
  - dígito de control de EAN-13, EAN-8 y UPC-A;
  - producto: nombre, unidad, precios no negativos de hasta 4 decimales, servicio almacenado,
    trazabilidad sin almacenar, códigos repetidos o de tipo desconocido, componente sin producto
    o él mismo, dos proveedores preferidos, SKU normalizado, cambio de tipo, descatalogación;
  - conversiones (g a kg, l a ml, min a h, m³ a l) y las que no existen (kg a l, caja a unidad);
  - tarifa: descuento y fechas inválidos, solape, tramos por cantidad, vigencia, tarifa retirada,
    precio base;
  - categoría que es su propio padre.
- **Extremo a extremo** (Parties y Productos, en memoria y en SQLite migrada, por HTTP):
  - las 14 unidades;
  - categorías: código repetido 422, ajeno 404;
  - productos: poner precios no es catalogar (403), ajeno 404, dígito de control 400, unidad
    desconocida 400, SKU repetido 422, **código de barras de otro producto 422**, servicio
    almacenado 400;
  - kit de dos componentes; un servicio como componente 422; **ciclo de kits 422**;
  - otra empresa puede usar el mismo SKU y el mismo código;
  - búsqueda por nombre, SKU, código de barras, tipo y categoría;
  - tarifa: catalogar no es poner precios (403), código repetido 422, solape 422, descuento 400,
    producto desconocido 404;
  - cotización: tramo pequeño, tramo de 1.000 con 5 % de descuento, fuera de vigencia, sin
    tarifa, tarifa retirada, cantidad cero 400;
  - descatalogar una sola vez; el puerto `Catalog` devuelve los productos conocidos.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL (junto con Inventario):
  - producto de ida y vuelta con códigos, componentes y proveedores;
  - SKU y código de barras repetidos;
  - búsquedas, incluida la de código de barras sobre la tabla hija;
  - tarifa de ida y vuelta, solape y cotización por el puerto `Pricing`.

## Pendiente

- Precios y descuentos por cliente y grupo de clientes, con Pedidos.
- Variantes de producto (características).
- Tarifas en otras monedas.
- Precio mínimo de venta y control de descuentos máximos.
- Validar el código de impuesto contra el catálogo de Fiscal al dar de alta el producto (hoy se
  valida al facturar).
- Que Facturación y Compras tomen el código de impuesto y la categoría de gasto del producto.
- Importar de C#: los productos con su perfil comercial; las tarifas no tienen datos (0 filas).
