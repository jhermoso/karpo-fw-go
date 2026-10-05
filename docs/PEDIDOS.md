# Contexto Pedidos

Port de los pedidos de venta de C# al contexto `contexts/orders`, más lo que necesita de
Inventario para reservar, dar salida y liberar el stock.

En C# estaban repartidos en cuatro sitios:

- **Orders (ErpKernel):** `Order`, `SalesOrder`, `OrderItem`, roles, estados, ajustes, `Quote` y
  una treintena de clases de andamiaje (`Agreement`, `OrderTerm`, `SalesTax`…) sin tablas.
- **Orders (ErpDetail):** `OrderManagementProfile`, el `PricingEngine` y el
  `SalesOrderWorkflowApplicationService` (borrador, recálculo, confirmación).
- **Parties (ErpDetail):** `CustomerRelationshipCommercialProfile` y `CustomerPriceGroup`.
- **Shipments e Invoicing (ErpKernel):** `Shipment`, `DeliveryNote`, `ItemIssuance` y la
  facturación bajo demanda desde un pedido o un envío.

Aplica decisiones ya aprobadas:

- La 2 de [COBROS.md](COBROS.md): el riesgo del cliente vive en Cobros y Pedidos lo consulta.
- La 3 de [PRODUCTOS.md](PRODUCTOS.md): los precios salen de Productos; los descuentos por cliente
  van con Pedidos.
- Las 1 y 3 de [INVENTARIO.md](INVENTARIO.md): el stock solo cambia con un movimiento y lo
  reservado no se toca.

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

- **El flujo de pedido solo funciona con repositorios falsos.** Sus pruebas pasan, pero contra el
  modelo real fallaría por cuatro motivos independientes:
  - las líneas son `SalesOrderItem`, un tipo que no está en el modelo de EF ni tiene tabla;
  - el tipo de pedido y los estados usan identificadores de otros catálogos (una prioridad y dos
    estados de relación entre terceros), con clave foránea física que los rechazaría;
  - la columna `Status` del perfil de gestión no existe en el DDL ni en la migración;
  - `stock_balance`, `price_list_line` y `customer_price_group` no tienen tabla.
- **Tres estados sin sincronizar:** el del catálogo (que cualquiera cambia con un `PUT`), el del
  perfil de gestión (también modificable) y un histórico que nadie escribe. En preparación,
  entregado y cancelado no se asignan nunca. No hay operación de cancelar.
- **El descuento se puede aplicar dos veces:** el precio guardado en la línea ya lleva todos los
  descuentos, y a su lado se guarda otra vez el descuento del cliente. El precio original y el
  descuento de tarifa se pierden.
- **El «riesgo» no es un control de crédito:** compara el límite solo con el importe de ese
  pedido, sin contar lo que el cliente ya debe. Un límite de 0 significa cosas distintas en el
  motor y en el flujo.
- **Reservas que no se liberan.** La confirmación reserva stock solo si se indica la instalación;
  si no, no comprueba nada. No queda registro de qué pedido reservó qué, y nada libera ni consume
  la reserva. Dos líneas del mismo producto se pisan.
- **Nada une pedido, envío, albarán y stock.**
  - `Shipment` no referencia ningún pedido.
  - El albarán es una cabecera sin líneas, editable y borrable después de emitido.
  - `ItemIssuance` no mueve stock.
  - `BlockDelivery`, `ServeComplete` y `GroupDeliveryNotes` no se leen.
- **El perfil del cliente se busca sin mirar la empresa:** un cliente de dos empresas recibe uno
  cualquiera.
- **Dos numeraciones:** la confirmación fabrica un número con los 8 primeros caracteres del Id, y
  `POST /issue` usa una serie real, pero no escribe el número en el pedido.
- **La facturación desde el pedido ignora el pedido:** el precio viene en la petición (o es 0), no
  mira el estado ni el cliente, siempre es «Exenta» y la misma mercancía se puede facturar por la
  línea del pedido y por la del envío.
- **Sin permisos** en ningún endpoint de pedidos, envíos ni facturas.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `Order` + `SalesOrder` + `OrderManagementProfile` (tres estados) | 5 | 1 | 2 | 3 | **61** | Modificar → agregado `Order` con **un único estado** y transiciones propias: borrador → confirmado → entregado, cerrado (servido en parte) o cancelado (sin entregas). Confirmado ya no se edita |
| 2 | `OrderItem` / `SalesOrderItem` (precio ya descontado + descuento repetido) | 5 | 1 | 2 | 3 | **61** | Modificar → línea con **precio sin descuentos**, descuento de tarifa, y precio neto e importe **derivados** con el descuento del cliente del pedido. Cada descuento, una vez. Lleva lo reservado y lo entregado |
| 3 | `PricingEngine` (cascada de cinco niveles) | 4 | 2 | 2 | 3 | **58** | Sustituido → el precio lo da Productos (`Pricing.Quote`: tarifa del cliente o precio base); Pedidos añade el descuento del cliente |
| 4 | `CustomerRelationshipCommercialProfile` (lado de ventas) + `CustomerPriceGroup` | 4 | 2 | 2 | 3 | **58** | Modificar → agregado `Terms` **por empresa y cliente**: tarifa, descuento, bloqueo de pedidos y **bloqueo de entregas (que ahora sí se aplica)**. Los grupos de precio, retirados: la tarifa ya agrupa |
| 5 | `MaxRisk` contra el importe del pedido | 3 | 1 | 2 | 3 | **45** | Sustituido → al confirmar se consulta el crédito de **Cobros** (`Credit.Exposure`), que cuenta todo lo que el cliente debe: bloqueado o sin crédito disponible suficiente, no se confirma |
| 6 | Reserva en la confirmación (contador sin dueño, sin liberación) | 4 | 1 | 1 | 3 | **47** | Sustituido → **reserva por eventos**: el pedido pide stock, Inventario retiene lo que hay por línea de pedido y lo comunica; lo que falta queda pendiente y se vuelve a pedir. **Cancelar o cerrar libera** |
| 7 | `DeliveryNote` (cabecera sin líneas) + `Shipment` + `ItemIssuance` | 4 | 1 | 1 | 3 | **47** | Modificar → agregado `Delivery` (**albarán**) del pedido, con líneas, numerado e **inmutable**. Solo se entrega lo pendiente y, en lo almacenable, lo que Inventario retiene. Inventario da la salida contra la reserva |
| 8 | Numeración: seudonúmero al confirmar y `POST /issue` | 3 | 2 | 2 | 3 | **51** | Modificar → contador por empresa, serie y año: `PED-2026-000001` al confirmar y `ALB-2026-000001` al entregar |
| 9 | `OrderRole`, `OrderItemRole`, `OrderStatus`, `OrderAdjustment` y sus tipos | 2 | 2 | 2 | 2 | **40** | Retirar → el cliente y la empresa están en el pedido; el histórico lo da la auditoría |
| 10 | `Quote` (cabecera sin líneas, sin conversión a pedido) | 2 | 2 | 2 | 3 | **43** | Aplazado → fase 2: presupuestos que se convierten en pedido |
| 11 | Facturación bajo demanda desde pedido o envío | 4 | 1 | 1 | 3 | **47** | Aplazado → siguiente paso: Facturación prepara la factura desde el albarán publicado (`orders.delivery-issued.v1`), con sus precios |
| 12 | Andamiaje sin tablas (`Agreement*`, `OrderTerm`, `SalesTax`, `Fee`…) | 1 | 1 | 1 | 2 | **22** | Retirar |
| 13 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Orders.Order.Read/Update` (tomar el pedido), `Confirm`, `Deliver` y `Cancel`, **separados**, y `Orders.Terms.Read/Update` |

## Diseño

```
contexts/orders/
├── domain/          # Order (+Line), Delivery (+DeliveryLine), Terms, Counter, puertos Catalog y CreditCheck
├── contracts/       # lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito, suscripción a Inventario
├── infrastructure/  # esquema ord_* de 5 motores, mapeos, bandeja de entrada, adaptadores ProductsCatalog y ReceivablesCredit
└── module.go        # composición, Consumer y rutas /api/orders/...

contexts/inventory/  # + suscripción a Pedidos (migración 3: inventory_inbox), inventory.stock-reserved.v1
```

- **El ciclo completo, sin escrituras entre contextos:**
  1. El pedido se abre en borrador con la tarifa y el descuento del cliente de ese día. Cada
     línea se valora con Productos.
  2. Al confirmar se comprueba el bloqueo y el crédito, se numera y se publica
     `orders.stock-requested.v1` con las líneas almacenables.
  3. Inventario retiene lo que haya disponible, hasta lo pedido, en una reserva por línea de
     pedido, y publica `inventory.stock-reserved.v1`. Pedidos lo anota en la línea. Si no hay
     nada, no publica nada: el pedido ve que le falta y puede **volver a pedirlo**.
  4. El albarán entrega cantidades dentro de lo retenido (o de lo pendiente, en servicios) y
     publica `orders.delivery-issued.v1`. Inventario da la salida contra la reserva, a coste
     medio, con la nota «Albarán …».
  5. Cancelar (sin entregas) o cerrar (con entregas) publica `orders.order-closed.v1` e Inventario
     libera lo que el pedido aún retuviera.
- **Precio de la línea:** `neto = precio × (1 − dto. tarifa) × (1 − dto. cliente)`, a 4 decimales;
  el importe, a céntimos. El pedido no lleva impuestos: los calcula Facturación.
- **Idempotencia:** las bandejas de entrada descartan mensajes repetidos, y la salida de un
  albarán es única por albarán y línea.
- **Tablas:**
  - `ord_orders` (+ `ord_order_lines`);
  - `ord_deliveries` (+ `ord_delivery_lines`), con número único por empresa;
  - `ord_terms` (único por empresa y cliente);
  - `ord_counters`;
  - las bandejas de salida, la auditoría y `orders_inbox`.

## Decisiones (aprobadas por Javier el 2026-10-05)

1. **Un pedido tiene un solo estado** y, una vez confirmado, no se edita: se sirve, se cierra o se
   cancela. Cambiar un pedido confirmado es cancelarlo y hacer otro.
2. **La reserva de stock es asíncrona y parcial.** Confirmar no falla por falta de stock: el
   pedido queda con lo que falta a la vista y se puede volver a pedir. No hay reserva
   «todo o nada».
3. **Solo se entrega lo que Inventario retiene para ese pedido.** El albarán pertenece a Pedidos,
   lleva líneas y precios y no se modifica; una devolución será otro documento (fase 2).
4. **Las condiciones del cliente (tarifa, descuento, bloqueos) viven en Pedidos**, por empresa y
   cliente. **El límite de crédito sigue en Cobros**, y el pedido se compara con el crédito
   disponible por su importe sin impuestos.
5. **Facturar desde el albarán es el siguiente paso** y lo hará Facturación consumiendo
   `orders.delivery-issued.v1`. Presupuestos, devoluciones, portes y comisiones quedan para la
   fase 2.

## Validación

- **Dominio:**
  - precio con los dos descuentos, una vez cada uno (0,08 × 0,95 × 0,90 = 0,0684); numeración de
    líneas; cantidad y descuento inválidos;
  - confirmar un pedido vacío; pedido confirmado que ya no cambia;
  - petición de stock con lo que falta; retención limitada a lo pendiente; nada que pedir;
  - entregas: de un borrador, sin retención, de más, línea repetida, parcial y total;
  - cancelar con y sin entregas, motivo obligatorio, una sola vez; retención tardía ignorada;
    cerrar con y sin entregas;
  - condiciones del cliente y contador.
- **Extremo a extremo** (Parties, Productos, Inventario, Cobros y Pedidos sobre el mismo backend,
  con un único broker, en memoria y en SQLite migrada, por HTTP):
  - condiciones: vender no es fijar condiciones (403), descuento inválido 400;
  - borrador: ajeno 404, entregar no es vender (403); producto bloqueado o de otra empresa 422;
    cantidad cero 400; línea añadida y quitada; precios de tarifa y base;
  - confirmar: vender no es confirmar (403), **crédito de Cobros superado 422**, confirmado una
    sola vez, número `PED-2026-000001`;
  - entregar antes de que Inventario retenga: 422; retención de las 600 unidades que hay, con 400
    pendientes;
  - albarán de todo lo entregable: vender no es entregar (403), fecha anterior al pedido 422,
    `ALB-2026-000001` con sus importes; salida en la ficha de almacén con la nota del albarán;
  - nueva petición sin stock (nada), entrada de 250, nueva petición (retiene 250), entrega de más
    422, segundo albarán;
  - un pedido servido se cierra, no se cancela (422); vender no es cerrar (403); cierre;
  - segundo pedido con precio base, retención y **cancelación que libera el stock**;
  - cliente bloqueado para pedidos y para entregas (422 cada uno);
  - búsquedas por estado y por pedido; ajeno 404.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con los cinco contextos migrados en
  la misma base:
  - condiciones de ida y vuelta (versión 2);
  - pedido con crédito superado, confirmación, y pedido de ida y vuelta con líneas, retención y
    cantidades con decimales;
  - albarán de ida y vuelta; stock retenido, entregado y liberado al cerrar, pasando por
    `inventory_inbox` y `orders_inbox`;
  - ficha de almacén con la salida del albarán; búsquedas.

## Pendiente

- **Facturar desde el albarán** (decisión 5).
- Fase 2:
  - presupuestos y su conversión en pedido;
  - devoluciones de cliente y albaranes de abono;
  - portes, recargos y comisiones;
  - cambios de precio manuales con permiso propio;
  - fecha de entrega prevista y reparto por almacenes;
  - servir completo y agrupar albaranes;
  - pedidos de compra (fase 2 de Compras).
- Comparar el pedido con el crédito por su importe con impuestos.
- Reserva automática al entrar stock de un producto con pedidos pendientes.
- La reserva de divisa del vertical (`CurrencyReservation`), a Maccorp.
- Importar de C#: nada; no hay pedidos, envíos ni albaranes en las semillas.
