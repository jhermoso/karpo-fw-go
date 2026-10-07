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
| 10 | `Quote` (cabecera sin líneas, sin conversión a pedido) | 2 | 2 | 2 | 3 | **43** | Aplazado → hecho en la fase 2: ver «Presupuestos (fase 2)» |
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

## Presupuestos (fase 2)

Port de `Quote` de C# (`/api/quotes`) como una pieza más de Pedidos: una oferta a un cliente, con
productos, cantidades y precios que se mantienen hasta un día, y que al aceptarse se convierte en
pedido.

### Estado del C#

Unas 665 líneas útiles: una cabecera con siete rutas. La única operación con sustancia es
«emitir», que pide un número a Documentos.

- **Sin líneas.** `quote_item` tiene tabla, pero nada la lee ni la escribe, y sus columnas
  obligatorias apuntan a conceptos que no existen (`RequestId`, `SkillTypeId`).
- **Sin importes:** ni totales, ni descuentos, ni impuestos.
- **No se convierte en pedido.** `order_item.QuoteId` existe y nadie lo escribe.
- **El estado es un número** (0, 1, 2) sin reglas: un presupuesto emitido se puede editar, borrar
  (dejando su documento y su número huérfanos) o devolver a «enviado».
- **La validez solo se guarda:** nada caduca, y actualizar sin enviarla la borra.
- **El número lo puede poner quien llama** al crear, saltándose la serie.
- **Sin permisos ni empresa:** cualquier usuario autenticado lee, cambia, borra o emite los
  presupuestos de cualquier empresa.
- **Sin pantalla** en el frontal.

### Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `IssueAsync` (número `PRE-AAAA-NNNNNN`, una sola vez) | 4 | 4 | 3 | 3 | **73** | Mantener → `SendQuote` numera con la serie `PRE` de la empresa y el año, en la misma transacción |
| 2 | `ValidUntilDate` (solo guardada) | 3 | 2 | 3 | 3 | **55** | Modificar → la validez se cumple: treinta días por defecto, y `ExpireQuotes` para una tarea |
| 3 | `Quote` (cabecera) | 3 | 2 | 2 | 3 | **51** | Modificar → agregado `Quote` **con líneas valoradas** como las de un pedido |
| 4 | `MarkAsSentAsync` (pone el estado a 1 desde cualquiera) | 2 | 2 | 2 | 3 | **43** | Retirar → enviar y numerar son un solo paso |
| 5 | Estado numérico sin reglas | 3 | 1 | 1 | 3 | **42** | Retirar → `draft`, `sent`, `accepted`, `rejected`, `expired`, `withdrawn`, con sus transiciones |
| 6 | Editar y borrar lo emitido | 2 | 1 | 2 | 3 | **38** | Retirar → solo cambia el borrador; lo demás se retira con un motivo, no se borra |
| 7 | `QuoteItem` (tabla huérfana) | 2 | 1 | 1 | 3 | **34** | Retirar → `QuoteLine` |
| 8 | Andamiaje (`QuoteRole`, `QuoteTerm`, `Proposal`, `Request*`, `ProductQuote`) | 1 | 1 | 1 | 2 | **22** | Retirar |
| 9 | Rutas sin permisos ni empresa | — | — | — | — | — | Sustituido → `Orders.Quote.Read`, `Update`, `Send` y `Resolve`, dentro del ámbito de la empresa |
| 10 | Conversión en pedido | — | — | — | — | — | No existía → `AcceptQuote` crea el pedido |

### Diseño

- **`Quote`**: empresa, cliente, fecha, validez, tarifa y descuento del cliente (los de sus
  condiciones al abrirlo), referencia, notas y líneas. Cada línea se valora como la de un pedido:
  precio de Productos, descuento de la tarifa y, encima, el del cliente.
- **Ciclo:**
  - `draft`: se le añaden y quitan líneas y se le cambian fechas, referencia y notas.
  - `sent`: recibe su número (`PRE-AAAA-NNNNNN`) y ya no cambia.
  - `accepted`: dentro de su validez; nace un **pedido en borrador con los precios ofertados**,
    aunque la tarifa haya cambiado desde entonces, y el presupuesto guarda cuál es.
  - `rejected` (con motivo), `expired` (pasó su último día) y `withdrawn` (lo retira la empresa,
    siendo borrador o enviado).
- **Validez:** el último día cuenta. Pasado ese día no se envía ni se acepta. Un envío que falla
  no consume número.
- **El pedido es un pedido más:** el bloqueo del cliente y el crédito se comprueban al
  confirmarlo, como siempre.
- **Permisos:** `Orders.Quote.Read`; `Update` (preparar y retirar); `Send` (comprometer a la
  empresa); `Resolve` (aceptar, rechazar, caducar).
- **Rutas:** `POST/GET /api/orders/quotes`, `GET/PUT /api/orders/quotes/{id}`, y
  `POST …/{id}/lines`, `…/lines/remove`, `…/send`, `…/accept`, `…/reject`, `…/withdraw`, más
  `POST /api/orders/quotes/expire-due`.
- **Eventos publicados:** `orders.quote-sent.v1` y `orders.quote-closed.v1` (con el pedido cuando
  se acepta). Un borrador retirado no publica nada.
- **Almacenamiento:** `ord_quotes` y `ord_quote_lines` (migración 4 de Pedidos).

### Decisiones (aprobadas por Javier el 2026-10-08)

1. **Los presupuestos viven en Pedidos**, no en un contexto propio. Sugerencia: sí; comparten las
   condiciones del cliente, los precios y el numerador, y acaban en un pedido.
2. **Enviar es numerar**, y lo enviado no cambia. La serie `PRE` la lleva Pedidos por empresa y
   año, como `PED` y `ALB`; en C# la serie la elegía quien llamaba y la numeraba Documentos.
   Sugerencia: sí; Documentos puede registrar el presupuesto al oír `orders.quote-sent.v1`.
3. **Aceptar crea el pedido en borrador a los precios ofertados**, en la misma transacción.
   Sugerencia: sí; es para lo que sirve un presupuesto. El pedido aún puede retocarse antes de
   confirmarse, y ahí pasa el control de crédito.
4. **La validez se cumple:** treinta días si no se dice otra cosa, último día incluido; vencido,
   ni se envía ni se acepta, y una tarea lo cierra. Sugerencia: sí. Queda programar la tarea.
5. **La aceptación lleva la fecha de hoy:** no se puede fechar atrás para saltarse la validez.
   Sugerencia: sí; si el cliente aceptó a tiempo y se anota tarde, se hace un presupuesto nuevo.
6. **No se borra nada:** se retira (borrador o enviado) o se rechaza, con su motivo. Sugerencia:
   sí.
7. **Cuatro permisos separados:** preparar, enviar, resolver y leer. Sugerencia: sí; quien
   prepara una oferta no tiene por qué poder comprometer a la empresa.
8. **El pedido no guarda de qué presupuesto viene** en esta fase: el vínculo está en el
   presupuesto y en el evento. Sugerencia: sí por ahora; añadirlo al pedido es una columna y un
   filtro, y lo dejo en pendientes.

### Validación

- **Dominio:** borrador limpio y con treinta días; validez anterior a la fecha; líneas valoradas
  (10 − 10 % − 5 % = 8,55); cambio que no vale deja todo igual; enviar sin líneas, sin número y
  vencido; lo enviado no cambia; el último día cuenta; aceptar tarde, sin pedido y dos veces;
  caducar; retirar un borrador sin avisar a nadie; lo enviado siempre tiene número.
- **Extremo a extremo** (Pedidos con un doble de Productos; en memoria y en SQLite migrada):
  - borrador: solo lectura 403, ajeno 404, validez de ayer 400; toma el 5 % del cliente;
  - líneas: cantidad cero 400; no vendible, sin código de impuesto y desconocido 422; quitar y
    volver a añadir; total 215,65;
  - cambio de fechas, referencia y notas; validez pasada 400;
  - enviar: preparar no es enviar (403); `PRE-AAAA-000001`; dos veces 422; después no cambia
    nada (422);
  - sube la tarifa y el cliente acepta: preparar no es aceptar (403), ajeno 404; pedido en
    borrador con 8,55 y 215,65, su almacén y su referencia; dos veces 422; el pedido se confirma
    con su `PED`;
  - rechazo con motivo (largo 422; dos veces 422); borrador retirado sin número; enviado retirado;
  - validez: hoy no caduca nada; al día siguiente no se acepta ni se envía (422), la tarea cierra
    uno y la segunda vez ninguno; el envío fallido no gastó número (`…000006`);
  - lectura y búsquedas por empresa, estado y cliente; ajeno, lista vacía;
  - doce eventos publicados.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: presupuesto de ida y vuelta con
  líneas, fechas y decimales (10,1234 → 8,6555 → 21,64), numeración sin huecos tras un envío
  fallido, aceptación con su pedido en la misma unidad de trabajo, rechazo, borrador retirado
  sin número, caducidad con el reloj adelantado, búsquedas y bandeja de salida.

## Pendiente

- ~~Facturar desde el albarán~~: hecho, ver [FACTURACION.md](FACTURACION.md). Facturación prepara
  el borrador y el albarán anota su factura al emitirse.
- Fase 2:
  - ~~presupuestos y su conversión en pedido~~: hecho, ver «Presupuestos (fase 2)»; quedan
    programar `ExpireQuotes`, guardar en el pedido el presupuesto del que viene, registrar
    el presupuesto en Documentos y las revisiones de una oferta ya enviada;
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
