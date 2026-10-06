# Contexto Compras

Port de la parte de compras de C# al contexto `contexts/purchases`, más sus efectos en Pagos,
Contabilidad y Fiscal.

La fase 1 es la **factura recibida**, que cierra el ciclo que dejó abierto la decisión 2 de
[PAGOS.md](PAGOS.md) (aprobada el 2026-10-03): una obligación de pago sin base, IVA soportado ni
gasto «hasta que exista un contexto de Compras».

En C# la compra estaba repartida en cuatro subdominios, sin flujo entre ellos:

- **Orders:** `PurchaseOrder`, `PurchaseOrderItem` y `PurchaseAgreement`.
- **Shipments:** `PurchaseShipment` y `ShipmentReceipt`.
- **Invoicing:** la fila de semilla «Purchase Invoice» de `InvoiceType`.
- **Parties (ErpDetail):** `SupplierRelationshipCommercialProfile` y los catálogos `VatGroup`,
  `VatIndicator`, `PaymentCondition` y `OwnBankAccount`.

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

- **No hay circuito de compra.**
  - Ningún código crea un `PurchaseOrder`: `/api/orders` siempre construye un `Order` base, así
    que la fila de la tabla `purchase_order` nunca se escribe.
  - `PurchaseOrderItem`, `PurchaseShipment`, `PurchaseReturn` y `PurchaseAgreement` no están en el
    modelo de EF.
  - No hay vista de pedidos, albaranes ni facturas en Angular.
- **La factura recibida no existe como tal.**
  - Es una fila de semilla de `InvoiceType` con `IsSales = 0`, que ningún código consulta.
  - No tiene número del proveedor ni fecha de registro; `IssueAsync` le daría un número de
    nuestra propia serie.
  - **`InvoiceApplicationService.CreateAsync` no guarda los terceros** (`BilledFrom` y
    `BilledTo`): toda factura se graba con `Guid.Empty`, así que en una factura recibida se
    perdería el proveedor.
- **Sin impuestos en compras:**
  - las líneas no tienen base, tipo ni cuota;
  - no hay IVA soportado, deducibilidad, prorrata ni inversión del sujeto pasivo;
  - la `WithholdingTaxPercent` del proveedor se guarda y no se aplica: el 111 y el 190 solo leen
    nóminas (hay un TODO en `Mod111GeneratorService.cs:137`);
  - `IsVatDeductible`, `GeneralProRata`, `IsExcludedFrom347`, `VatGroup` y `VatIndicator` son
    datos que nadie lee.
- **Recepción mal formada:** `ShipmentReceipt` exige motivo de rechazo aunque se acepte todo, no
  enlaza con la línea del albarán y no comprueba las cantidades. No hay casación pedido, albarán
  y factura.
- **Sin rectificativas de compra:** «Credit Memo», «Debit Memo» y «Exenta» están sembradas con
  `IsSales = 1`.
- **Sin permisos:** ningún endpoint de compras los comprueba, y no hay códigos de compras en el
  catálogo.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `Invoice` + `InvoiceType` «Purchase Invoice» (pierde los terceros, sin impuestos) | 4 | 1 | 1 | 3 | **50** | Modificar → agregado `Invoice` (factura recibida): número del proveedor, fecha de emisión (devengo), fecha de registro, vencimiento, líneas con **categoría de gasto**, base y código de impuesto o tratamiento, desglose calculado por el motor de **Fiscal** con la jurisdicción de la empresa, **total impreso comprobado contra el desglose**, retención, cuentas de abono. **Registro de facturas recibidas** numerado por empresa y año (`FR-2026-000001`). Única por proveedor y número. **Rectificativa** en negativo que apunta a la original. Se anula con motivo |
| 2 | `SupplierRelationshipCommercialProfile` (retención, IVA y cuentas que nadie lee) | 3 | 2 | 2 | 3 | **52** | Modificar → agregado `SupplierProfile` por empresa y proveedor con lo que se **usa al registrar**: categoría por defecto, % de retención, días de pago, cuenta de abono y bloqueo. Las cuentas contables no están aquí: las decide el perfil del libro de Contabilidad |
| 3 | `PurchaseOrder`, `PurchaseOrderItem`, `PurchaseAgreement`, RFQ y requisiciones | 1 | 1 | 2 | 2 | **30** | Aplazado → fase 2, con Productos e Inventario. Sin flujo en C# |
| 4 | `PurchaseShipment` + `ShipmentReceipt` | 1 | 1 | 1 | 2 | **26** | Aplazado → fase 2: albarán de entrada y casación pedido, albarán y factura |
| 5 | `VatGroup` / `VatIndicator` | 2 | 2 | 2 | 3 | **43** | Sustituido → los tipos y tratamientos del catálogo de **Fiscal** (`G21`, `R10`, exentos y no sujetos), los mismos que usa Facturación |
| 6 | `PaymentCondition` (sin cálculo) | 2 | 2 | 3 | 3 | **49** | Modificar → días de pago del perfil; los plazos múltiples, en la fase 2 (Cobros ya los tiene) |
| 7 | `Credit Memo` / `Debit Memo` sembrados como ventas | — | — | — | — | — | Sustituido → factura recibida rectificativa (importe negativo y `corrects`) |
| 8 | IVA soportado, retención de profesionales, 111/190 | — | — | — | — | — | Nuevo → el evento lleva el **gasto por categoría** (con el IVA no deducible repartido), el **IVA deducible** y la **retención** (clave G del 190). Contabilidad asienta, Pagos debe lo que se paga y **Fiscal añade la retención al 111 y al 190** junto a las de nómina |
| 9 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Purchases.Invoice.Read/Register/Cancel` (**registrar y anular están separados**) y `Purchases.Supplier.Read/Update` |

## Diseño

```
contexts/purchases/
├── domain/          # Invoice (+Line, Breakdown, PayTo, Expenses), SupplierProfile, Counter, puerto Taxes
├── contracts/       # lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito
├── infrastructure/  # esquema pur_* de 5 motores, mapeos, adaptador FiscalTaxes
└── module.go        # composición y rutas /api/purchases/...
```

- **El ciclo completo**, sin escrituras entre contextos. Compras publica
  `purchases.invoice-registered.v1` y cada contexto reacciona:
  - **Pagos** debe al proveedor el total menos la retención, al vencimiento, en las cuentas de la
    factura. **Se retira el alta manual** de facturas de proveedor de Pagos, como preveía su
    decisión 2. Las rectificativas, que son abonos del proveedor, no generan obligación: la
    compensación queda para la fase 2.
  - **Contabilidad** carga el gasto de cada categoría (600, 621–629) y el IVA soportado (472), y
    abona al proveedor (400) y la retención (4751), en la fecha de registro. Tiene diez roles
    nuevos en el perfil: `input-tax`, `purchases`, `rent`, `repairs`, `professional-services`,
    `transport`, `insurance`, `advertising`, `supplies` y `other-services`. Después llegó la
    categoría `fixed-asset` (la compra de un inmovilizado va al 21x, no a gasto): ver
    [ACTIVOS.md](ACTIVOS.md).
  - **Fiscal** guarda la retención con clave G y fecha de la factura, de modo que el 111 del
    trimestre y el 190 del año la incluyen.
- **Anulación:** `purchases.invoice-cancelled.v1` retira la obligación si está sin pagar, genera
  el contraasiento y anula la retención.
- **IVA no deducible:** se reparte entre las categorías en proporción a sus bases; la última se
  queda el redondeo.
- **Tablas:**
  - `pur_invoices` (+ `pur_invoice_lines`, `pur_invoice_taxes`, `pur_invoice_accounts`);
  - `pur_suppliers`;
  - `pur_counters`;
  - las bandejas de salida y la auditoría.

## Decisiones (aprobadas por Javier el 2026-10-04)

1. **La fase 1 de Compras es la factura recibida**, con su registro numerado. Pedidos, albaranes
   de entrada y la casación de los tres van a la fase 2 con Productos e Inventario.
2. **El IVA de la factura recibida lo calcula el motor de Fiscal** con la jurisdicción de la
   empresa compradora, y **el total impreso por el proveedor debe coincidir** con el calculado.
   Si no coincide, no se registra (422).
3. **Las categorías de gasto sustituyen a las cuentas contables** en Compras. Contabilidad las
   traduce con su perfil (decisión 2 de [CONTABILIDAD.md](CONTABILIDAD.md)). El IVA no deducible
   es una marca por factura; **la prorrata va a la fase 2**.
4. **La retención de fase 1 es solo la de profesionales** (clave G del 190, en el 111 de la fecha
   de la factura). Los alquileres (115/180) y otros rendimientos van a la fase 2.
5. **Pagos deja de dar de alta facturas de proveedor a mano**: toda obligación de proveedor nace
   de una factura recibida. Las rectificativas (abonos del proveedor) no generan obligación hasta
   que se implemente la compensación.

## Validación

- **Dominio:**
  - número del proveedor, fechas (registro no anterior a la emisión, vencimiento no anterior),
    líneas numeradas con categoría, base en céntimos y código o tratamiento;
  - desglose que cubre las líneas, total impreso, retención solo con servicios profesionales,
    cuentas que suman lo que se paga;
  - rectificativa en negativo; anulación con motivo y una sola vez;
  - reparto del IVA no deducible entre categorías con redondeo;
  - perfil del proveedor y contador.
- **Extremo a extremo** (Parties, Fiscal, Compras, Pagos y Contabilidad sobre el mismo backend,
  con un único broker, en memoria y en SQLite migrada):
  - perfil del asesor: solo lectura 403, categoría inválida 400;
  - factura del asesor con los valores de su perfil (vencimiento a 30 días, 15 % de retención,
    cuenta): ajeno 404, solo lectura 403, total impreso erróneo 422, duplicada 422;
  - papelería con dos tipos (21 % y 10 %), retención sin servicios profesionales 422, factura no
    deducible y rectificativa;
  - Pagos: tres obligaciones (ninguna por la rectificativa);
  - Contabilidad: gasto por categoría, IVA soportado, retención y proveedores, con las sumas y
    saldos cuadrados;
  - Fiscal: el 111 del 3T con la retención de clave G;
  - anulación (registrar no es anular: 403; una sola vez): obligación retirada, contraasiento y
    retención anulada en el 111; nueva alta con el siguiente número del registro;
  - registro por fechas, ajeno 404, listado de perfiles.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con los cinco contextos migrados en
  la misma base:
  - perfil de ida y vuelta (versión 2 tras modificarlo);
  - facturas con el motor de Fiscal (333,33 al 21 % con 7 % de retención; dos tipos; dos cuentas
    de abono; no deducible; rectificativa), de ida y vuelta con líneas, desglose y cuentas;
  - obligaciones, asientos y 111 por las bandejas `payments_inbox`, `accounting_inbox` y
    `fiscal_inbox`;
  - anulación y registro por proveedor.

## Pendiente

- Fase 2:
  - pedidos de compra, albaranes de entrada y casación de los tres;
  - prorrata general y especial;
  - inversión del sujeto pasivo y adquisiciones intracomunitarias (autorrepercusión);
  - importaciones (DUA);
  - recargo de equivalencia soportado (empresas en ese régimen);
  - retenciones de alquileres (115/180);
  - plazos múltiples de pago.
- Fiscal:
  - el 303, con el IVA soportado y el repercutido;
  - el libro registro de facturas recibidas;
  - el 347 y el 349;
  - el SII.
- Compensar las rectificativas de proveedor en Pagos.
- Importar de C#: los perfiles de proveedor (`SupplierRelationshipCommercialProfile`) con su
  retención, días de pago y cuenta. Las facturas recibidas no tienen datos (0 filas).
