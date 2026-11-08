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
| 13 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Products.Product.Read/Update` (catálogo), `Products.PriceList.Read/Update` (tarifas) y `Products.SpecialPrice.Read/Update` (precios especiales, D-148), separados, con ámbito por empresa |

## Diseño

```
contexts/products/
├── domain/          # Product (+Barcode, Component, SupplierItem), Category, PriceList y SpecialPrices (+PriceLine), QuoteOf, Unit y Convert
├── contracts/       # lenguaje publicado v1 y puertos Catalog y Pricing
├── application/     # casos de uso con permisos y ámbito
├── infrastructure/  # esquema prd_* de 5 motores, mapeos
└── module.go        # composición y rutas /api/products/...
```

- **Puertos para los demás contextos:**
  - `Catalog.Products(ids)` devuelve lo que necesitan de un producto: empresa, SKU, nombre, tipo,
    unidad, código de impuesto, categoría de gasto, si se almacena, trazabilidad, bloqueos y
    descatalogación.
  - `Pricing.Quote(QuoteRequest{empresa, cliente, producto, tarifa, cantidad, fecha})` devuelve
    precio, descuento y precio neto, y de qué nivel de la cadena sale (ver «Precios especiales y
    tarifa general»).
- **Precios con hasta 4 decimales**; el neto se redondea a 4.
- **Búsqueda** por texto (nombre sin distinguir mayúsculas, o SKU exacto), tipo, categoría y
  código de barras.
- **Eventos:** `products.product-registered.v1` y `products.product-discontinued.v1`.
- **Tablas:**
  - `prd_products` (+ `prd_barcodes`, `prd_components`, `prd_product_suppliers`);
  - `prd_categories`;
  - `prd_price_lists` (+ `prd_price_lines`);
  - `prd_special_prices` (+ `prd_special_price_lines`);
  - las bandejas de salida y la auditoría.

## Precios especiales y tarifa general (D-148, plan Sage T6)

**Cadena del precio de venta**, de más a menos prioritaria; gana el primer nivel con una línea
vigente para la cantidad (la de mayor cantidad mínima que no la supere):

| # | Nivel | `Quote.Source` | De dónde sale |
|---|---|---|---|
| 1 | Precio especial del cliente para el producto | `special` | `SpecialPrices` de la empresa y el cliente |
| 2 | Tarifa del cliente | `customer_list` | la tarifa que traen las condiciones (`Terms`) de Orders |
| 3 | Tarifa general de la empresa | `default_list` | el ajuste `default_price_list` del perfil de la empresa en Parties |
| 4 | Precio base del producto | `base` | `Product.BasePrice` |

- Un nivel gana entero: si el precio especial tiene línea, da precio y descuento aunque la tarifa
  del cliente tenga un tramo por cantidad más barato. Una tarifa retirada no da precio.
- **El descuento del cliente** (`Terms.Discount`) no entra en la cadena: Orders lo aplica encima de
  la línea, encadenado con el del nivel que ganó (precio × (1 − dto. del nivel) × (1 − dto. del
  cliente), no la suma de los dos), también sobre un precio especial.
- **Precios especiales:** agregado `SpecialPrices`, uno por empresa y cliente (índice único
  `ux_prd_special_prices_customer`), con líneas como las de la tarifa: producto, cantidad mínima,
  precio, descuento opcional y vigencia desde/hasta. Dos líneas del mismo producto y cantidad no se
  solapan en el tiempo. Se crea con su primer precio. El cliente es una party de Parties y no se
  comprueba su rol, igual que el proveedor de un producto.
- **Tarifa general:** no es una marca de la tarifa, sino un ajuste de la empresa,
  `default_price_list`, en `internal_organization_profile` de Parties, junto a la zona horaria y los
  ajustes de D-144, con el mismo nombre que en Karpo C# (docs/PARTIES.md §25). Products lo lee por
  el puerto `application.CompanySettings` (`DefaultPriceList(empresa)`), que cablea el anfitrión
  (`host.ProductsSettings`, sobre `contracts.CompanyProfiles` de Parties) con
  `products.WithSettings`, igual que Billing lee sus ajustes. Se lee en cada cotización. Si no hay
  puerto, la empresa no tiene el ajuste, o la tarifa no existe, es de otra empresa o está retirada,
  ese nivel se salta. Parties ya la comprueba al guardarla (con el puerto `contracts.PriceLists` de
  Products, `Module.PriceLists`), pero se puede retirar después. Una sola tarifa
  general por empresa sale del propio esquema (una columna del perfil).
- **`GET /quote?customer=` no conoce los `Terms` de Orders:** la tarifa del cliente la pasa quien
  llama en `priceList`; sin ella, ese nivel se salta. Con `customer` se pide además
  `Products.SpecialPrice.Read` (403 sin él), porque la respuesta enseña su precio especial. El
  puerto `Pricing`, de servicio a servicio, no pide permisos.
- **Cambios de comportamiento:** una tarifa del cliente sin línea vigente cae ahora a la tarifa
  general, si la empresa tiene una, y no directamente al precio base. `Quote.Source` deja de valer
  `list` y pasa a `customer_list` (los demás valores: `special`, `default_list`, `base`).
- **API** (`/api/products`):

  | Método y ruta | Permiso | Qué hace |
  |---|---|---|
  | `GET /special-prices?company=&customer=` | `Products.SpecialPrice.Read` | los precios especiales del cliente (lista vacía si no tiene) |
  | `POST /special-prices` | `Products.SpecialPrice.Update` | alta de un precio especial |
  | `POST /special-prices/remove` | `Products.SpecialPrice.Update` | baja de un precio (producto, cantidad mínima y fecha desde) |
  | `GET /quote?company=&customer=&product=&priceList=&quantity=&on=` | `Products.PriceList.Read` (y `Products.SpecialPrice.Read` con `customer`) | la cadena completa |

  No hay verbo `Delete`: como en las líneas de tarifa, borrar un precio es cambiar los precios del
  cliente (`Update`).
  La tarifa general se fija en el perfil de la empresa: `PUT /api/profiles/internal-organizations/{id}`
  con `defaultPriceList` (docs/PARTIES.md §25).
- **Migración 4** de Products, en los cinco motores: las dos tablas nuevas, con los decimales ya
  como `{dec:18,4}` y `{dec:5,2}`. Los ajustes de la empresa van en la migración 35 de Parties.
- **Precio automático de la factura directa:** el mismo perfil guarda `automatic_pricing`
  (`automaticPricing`, nulo es sí). Facturación pide el precio con `Pricing.Quote` para la línea
  con producto de una factura directa (docs/FACTURACION.md, «Precio automático»).
- **Orders** pasa el cliente al puerto: `Catalog.Price` recibe el cliente del pedido o del
  presupuesto. Nada más cambia allí.

**Diferencias con `price_component` de Trench.** Trench guarda el precio de un cliente en
`price_component` con `Party`, `Product`, `Price` (18,2), `Percent` (9,4) y `ExpirationDate`, sin
empresa y sin tramos:

| Trench (`price_component`) | Go (`prd_special_price_lines` y cabecera) | Por qué |
|---|---|---|
| `Party` | `customer` | el nivel es sólo de clientes |
| `Product` | `product_id` | como `prd_price_lines` |
| `Price` (18,2) | `unit_price` (18,4) | los cuatro decimales de los precios de Products |
| `Percent` (9,4) | `discount` (5,2) | como `prd_price_lines.discount` |
| `ExpirationDate` | `valid_to` | más `valid_from`: un precio pactado empieza un día |
| — | `company` | el catálogo es de cada empresa |
| — | `min_quantity` | tramos por cantidad, como en la tarifa |
| `ProductCategory`, `ProductFeature`, `Uom`, `Comments`, `IsActive` | — | categoría: ver el hueco previsto; el resto, sin uso |

Se usan los nombres de `prd_price_lines` y no los de Trench para que una línea de precio especial y
una de tarifa sean el mismo tipo (`PriceLine`) y la misma lectura.

**Hueco previsto** (sin construir):

- **Descuento por familia:** la cadena es una lista de niveles (`domain.Level`) con una interfaz
  `Prices.PriceOf(producto, cantidad, fecha)` que recibe el producto entero, categoría incluida. Un
  descuento por familia es otro tipo de línea (categoría en lugar de producto) en los precios
  especiales o en la tarifa, o un nivel nuevo entre los que hay, sin tocar `QuoteOf` ni el contrato.
- **Otras monedas:** la tarifa y los precios especiales ya guardan su moneda (hoy sólo `EUR`), y la
  petición es un `QuoteRequest`: la moneda entra como un campo más y cada nivel mira sólo las listas
  de esa moneda.

## Decisiones (aprobadas por Javier el 2026-10-04)

1. **El catálogo es de cada empresa.** Cada producto pertenece a una organización interna, con
   SKU y códigos de barras únicos dentro de ella. No hay catálogo compartido entre empresas ni
   `product_offering`.
2. **Un solo agregado `Product`** sustituye a `Product`, `Part` y el perfil comercial. El tipo
   (bien o servicio) no cambia, y un producto se descataloga, no se borra.
3. **Las tarifas viven en Productos** (no en Parties) y el precio sale de la tarifa o, si no hay
   línea vigente, del precio base. **Los precios y descuentos por cliente o grupo de clientes
   van con Pedidos**, que es quien conoce al cliente. *Revisado por D-148 (2026-11-07):* el precio
   especial por cliente y producto vive en Productos, que es quien da precios; el descuento general
   del cliente sigue en las condiciones de Pedidos.
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
  - precios especiales: cliente y moneda, precio negativo, vigencia, solape, tramo, baja;
  - la cadena: el especial gana al tramo de la tarifa del cliente, y un caso por nivel;
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
  - precios especiales (D-148): sólo con su permiso (403), ajeno 404, descuento 400, producto
    desconocido 404, solape 422, consulta y baja; la cadena por `GET /quote`, un caso por nivel, con
    la tarifa general de un puerto falso; la tarifa general se salta si no hay, no existe, es de otra
    empresa o está retirada;
  - descatalogar una sola vez; el puerto `Catalog` devuelve los productos conocidos.
- **Anfitrión** (memoria y SQLite): la tarifa general sale del perfil de la empresa en Parties; sin
  ajuste o retirada, el precio base.
- **Pedidos:** un pedido y un presupuesto con precio especial y el descuento del cliente
  encadenado (0,09 × 0,95 × 0,90), y otro cliente sin especial que toma el tramo de su tarifa.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL (junto con Inventario):
  - producto de ida y vuelta con códigos, componentes y proveedores;
  - SKU y código de barras repetidos;
  - búsquedas, incluida la de código de barras sobre la tabla hija;
  - tarifa de ida y vuelta, solape y cotización por el puerto `Pricing`;
  - tarifa general sacada del perfil de Parties y precios especiales de ida y vuelta, su solape, la cadena
    completa por el puerto `Pricing` y la baja de un precio especial.

## Pendiente

- Descuento por familia (categoría) y por grupo de clientes (D-148 deja el hueco).
- Que una línea de factura sin pedido tome el precio de la cadena, configurable por empresa
  (D-148, Facturación).
- Variantes de producto (características).
- Tarifas y precios especiales en otras monedas.
- Precio mínimo de venta y control de descuentos máximos.
- Validar el código de impuesto contra el catálogo de Fiscal al dar de alta el producto (hoy se
  valida al facturar).
- Que Facturación y Compras tomen el código de impuesto y la categoría de gasto del producto.
- Importar de C#: los productos con su perfil comercial; las tarifas no tienen datos (0 filas).
