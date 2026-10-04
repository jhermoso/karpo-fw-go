# Contexto Inventario

Port del stock de C# al contexto `contexts/inventory`. En C# estaba repartido en tres sitios:

- **Product (ErpKernel):** `InventoryItem`, `InventoryItemStatusType`, `InventoryItemVariance` y
  `StockBalance`.
- **Shipments (ErpKernel):** `Picklist`, `ItemIssuance` y `ShipmentReceipt`.
- **Parties:** `Facility`, que hace de almacén.

El catálogo de productos está en [PRODUCTOS.md](PRODUCTOS.md).

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

- **El stock es una fila que se sobrescribe.** `StockBalance` guarda la cantidad por producto e
  instalación, sin libro de movimientos y sin valoración.
  - Las salidas, las liberaciones y las bajas de pedido se recortan a cero con `Math.Max`: un
    exceso se oculta en lugar de rechazarse.
  - El ajuste aplica un incremento con signo, sin motivo ni rastro, y no crea ninguna varianza.
  - `DeductStockAsync` no tiene endpoint ni nadie que lo llame.
- **Las reservas no se liberan nunca.** Confirmar un pedido de venta reserva stock, pero ningún
  camino lo libera ni lo descuenta: ni el envío, ni la anulación, ni la factura. Si no se indica
  la instalación al confirmar, no se comprueba el stock.
- **Recepciones y salidas no tocan el stock.** `ShipmentReceipt`, `ItemIssuance` y `Picklist` son
  altas, lecturas y borrados sin efecto.
- **`InventoryItem` no tiene cantidad,** ni número de serie ni lote, y exige un `Part` aunque sea
  un bien corriente.
- **`InventoryItemVariance` siempre guarda cantidad 0:** no tiene forma de asignar la cantidad, la
  fecha ni el comentario.
- **El coste medio es un campo que se escribe a mano.** No hay cálculo de coste medio ni FIFO.
- **El stock está modelado tres veces:** `InventoryItem`, `StockBalance` y, en el vertical,
  `CashHoldingStock`. El estado «Reservado» del primero no tiene relación con la cantidad
  reservada del segundo.
- **`stock_balance` no tiene migración** en ningún motor ni script: solo funciona donde el
  esquema se crea con `EnsureCreated`.
- **No hay almacenes ni ubicaciones:** un almacén es una `Facility` referenciada por Id.
- **Sin permisos:** cualquiera autenticado puede ajustar stock.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `StockBalance` (fila mutable, recortes a cero) | 5 | 2 | 2 | 3 | **65** | Modificar → agregado `Level` por almacén y producto: existencias, reservado, **coste medio ponderado** y punto de pedido. **Nunca queda en negativo y lo reservado no se toca**: un exceso es un 422, no un recorte. Solo cambia a través de un movimiento |
| 2 | Libro de movimientos | — | — | — | — | — | Nuevo → agregado `Movement`, **inmutable**: entrada, salida, ajuste, traspaso de salida y de entrada, con cantidad con signo, coste unitario, valor, saldo resultante, lote o serie, nota y origen. Numerado dentro de su nivel. Un error se corrige con otro movimiento |
| 3 | `AverageCost` escrito a mano | 2 | 1 | 2 | 3 | **39** | Sustituido → cada entrada recalcula el coste medio ponderado; las salidas salen a ese coste y no lo cambian |
| 4 | Reservas que nunca se liberan | 4 | 1 | 2 | 3 | **51** | Modificar → agregado `Reservation` por origen (línea de pedido), producto y almacén: retiene stock disponible, **se consume al dar salida contra ella y se libera** lo que quede. Una reserva abierta por origen |
| 5 | Ajuste sin motivo + `InventoryItemVariance` (siempre 0) | 3 | 1 | 2 | 3 | **45** | Modificar → **recuento**: se indica lo contado y el motivo, y genera un movimiento de ajuste por la diferencia. No puede quedar por debajo de lo reservado. Es un **permiso aparte** |
| 6 | `Facility` como almacén | 3 | 3 | 2 | 3 | **55** | Modificar → agregado `Warehouse` por empresa (código único, nombre, instalación opcional). Solo se cierra vacío |
| 7 | `InventoryItem` + `InventoryItemStatusType` (sin cantidad) | 2 | 1 | 1 | 2 | **30** | Retirar → el nivel y el movimiento lo sustituyen |
| 8 | Traspasos entre almacenes | — | — | — | — | — | Nuevo → dos movimientos enlazados, que llevan el coste al almacén de destino |
| 9 | `ShipmentReceipt`, `ItemIssuance`, `Picklist` (sin efecto en stock) | 2 | 1 | 2 | 3 | **39** | Aplazado → llegan con Pedidos y la fase 2 de Compras, y moverán stock a través de `Receive` e `Issue` con su origen |
| 10 | Umbrales en `StockBalance` y en el perfil del producto | 2 | 2 | 2 | 3 | **43** | Modificar → un punto de pedido por nivel y la consulta de lo que está por debajo |
| 11 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Inventory.Warehouse.Read/Update`, `Inventory.Stock.Read`, `Inventory.Stock.Move` (**mover**) e `Inventory.Stock.Adjust` (**recontar**), separados |

## Diseño

```
contexts/inventory/
├── domain/          # Warehouse, Level, Movement, Reservation, puerto Catalog
├── contracts/       # lenguaje publicado v1 y puerto Availability
├── application/     # casos de uso con permisos y ámbito, consultas
├── infrastructure/  # esquema inv_* de 5 motores, mapeos, adaptador ProductsCatalog
└── module.go        # composición y rutas /api/inventory/...
```

- **Cada cambio de stock es un movimiento**, y el nivel y su movimiento se guardan en la misma
  unidad de trabajo. Si dos operaciones chocan sobre el mismo nivel, la segunda se reintenta con
  el dato nuevo.
- **Qué se puede mover:** solo productos de la misma empresa marcados como almacenables en
  Productos. Un producto con trazabilidad exige lote o serie en entradas, salidas y traspasos. Un
  producto descatalogado ya no se recibe, pero sí se le puede dar salida.
- **Una entrada o salida con origen** (`sourceType` y `sourceId`) mueve el stock una sola vez: si
  el mismo hecho llega de nuevo, se devuelve el movimiento que ya existe.
- **Consultas:**
  - existencias por almacén y producto, con lo disponible y lo que está bajo el punto de pedido;
  - libro de movimientos por fechas, y **ficha de almacén** de un producto en orden de anotación;
  - **valoración** a coste medio;
  - reservas.
- **Puerto para otros contextos:** `Availability.Stock(empresa, producto, almacén)` devuelve
  existencias, reservado y disponible, de un almacén o de todos.
- **Evento:** `inventory.stock-moved.v1`, uno por movimiento, con cantidad, coste y valor.
- **Tablas:**
  - `inv_warehouses`;
  - `inv_levels` (único por almacén y producto);
  - `inv_movements` (secuencia única por almacén y producto);
  - `inv_reservations`;
  - las bandejas de salida y la auditoría.

## Decisiones (aprobadas por Javier el 2026-10-04)

1. **El stock se explica con un libro de movimientos inmutable.** El nivel es su saldo, y nada lo
   cambia sin dejar un movimiento. Un error se corrige con otro movimiento.
2. **Valoración a coste medio ponderado** por almacén y producto, calculado en el orden en que se
   anotan los movimientos (no por fecha). FIFO y la valoración por lote quedan fuera.
3. **El stock nunca es negativo y lo reservado no se toca:** una salida mayor que lo disponible se
   rechaza. Recontar es un permiso distinto de mover y exige motivo.
4. **El almacén es de Inventario** y puede apuntar a una instalación de Instalaciones. Las
   ubicaciones dentro del almacén quedan para la fase 2.
5. **En la fase 1 el lote o la serie se anotan en el movimiento, pero no hay saldo por lote.**
   Tampoco se contabiliza la variación de existencias: Contabilidad consumirá
   `inventory.stock-moved.v1` en la fase 2.

## Validación

- **Dominio:**
  - almacén: código sin espacios, cierre una sola vez;
  - coste medio: (100 × 2 + 300 × 3) / 400 = 2,75; una salida no lo cambia y la siguiente entrada
    sí; cantidad a cero y coste negativo rechazados;
  - salida mayor que lo disponible; reservar de más; no se toma lo reservado; salida contra
    reserva; liberar de más;
  - recuento por debajo de lo reservado, sin diferencia (sin movimiento) y con diferencia;
  - punto de pedido;
  - movimiento con valor, saldo y lote; reserva: consumir de más, liberar una sola vez.
- **Extremo a extremo** (Parties, Productos e Inventario, en memoria y en SQLite migrada, por
  HTTP):
  - almacenes: mover no es abrir almacenes (403), ajeno 404, código repetido 422;
  - entradas: contar no es mover (403), ajeno 404, **la misma entrada dos veces da un solo
    movimiento**, cantidad negativa 422, servicio 422, producto de otra empresa 422, producto con
    lote sin lote 422;
  - salida a 2,75 con su valor y saldo; salida mayor que las existencias 422;
  - reserva: una por origen (422), sin disponible suficiente 422, lo reservado no se toma (422),
    salida contra la reserva, de más 422, liberación una sola vez;
  - traspaso al mismo almacén 400; traspaso con el coste en destino y los dos movimientos
    enlazados;
  - recuento: mover no es recontar (403), sin motivo 400, con motivo; repetido no mueve nada;
  - ficha de almacén con los seis movimientos en orden;
  - punto de pedido (solo el responsable: 403), lista de lo que está por debajo, existencias,
    valoración (477,00) y disponibilidad por el puerto;
  - no se cierra un almacén con stock (422);
  - los ocho movimientos publicados.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL (junto con Productos):
  - entrada repetida por origen, lote obligatorio, coste medio, salida con decimales (150,5 a
    2,75 = 413,88);
  - reserva, salida contra ella y liberación; traspaso; recuento;
  - ficha en orden de secuencia, libro por fechas, valoración (350,63) y disponibilidad por
    almacén;
  - cierre de almacén con stock rechazado.

## Pendiente

- Fase 2:
  - saldo por lote y por número de serie, y caducidades;
  - ubicaciones dentro del almacén;
  - contabilización de la variación de existencias y del coste de ventas;
  - regularización del coste cuando la factura de compra llega con otro precio;
  - inventario físico por hojas de recuento.
- ~~Que Pedidos reserve, entregue y libere~~: hecho por eventos, ver [PEDIDOS.md](PEDIDOS.md)
  (migración 3, `inventory_inbox` e `inventory.stock-reserved.v1`).
- Que los albaranes de entrada (fase 2 de Compras) muevan el stock con su origen.
- Explosión de kits al dar salida.
- Propuesta de reposición a partir del punto de pedido.
- Importar de C#: nada; `stock_balance` no tiene tabla ni datos.
