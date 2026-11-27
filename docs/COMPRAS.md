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
├── application/     # casos de uso con permisos y ámbito, suscripción a Parties
├── infrastructure/  # esquema pur_* de 5 motores (con inbox desde la migración 4), mapeos, adaptador FiscalTaxes
├── maintenance.go   # el contrato de mantenimiento del front, /api/v1/suppliers
└── module.go        # composición, Consumer y rutas /api/purchases/...
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
  - `pur_supplier_block_events`, el historial de bloqueos (migración 8);
  - `pur_counters`;
  - las bandejas de salida, la auditoría y, desde la migración 4, `purchases_inbox`.
- **Motivo del bloqueo** (D-122, migración 5): el perfil de proveedor lleva `blockReason` (hasta 200
  caracteres, el `BlockReason` de Trench). Se guarda con el bloqueo y se borra al desbloquear: sin
  bloqueo, el motivo que llegue se descarta. La vista por pareja lo enseña en `blockReason`.
  Desde la migración 8 el motivo sólo lo pone la orden de bloquear (abajo).
- **Motivo obligatorio para bloquear y desbloquear** (decisión de Javier de 2026-11-14, migración
  6): bloquear exige `blockReason` no vacío (recortado) y desbloquear exige `unblockReason`; sin él,
  422 `purchases.block_reason_required` o `purchases.unblock_reason_required`. El perfil guarda
  el motivo del último desbloqueo en la columna `unblock_reason` (hasta 200), simétrica de
  `block_reason`: se guarda al desbloquear y se borra al bloquear, igual que el del bloqueo se
  borra al desbloquear. Se eligió la columna y no sólo la auditoría porque así el motivo vigente se
  lee con el perfil, sin consultar el registro de auditoría; la auditoría guarda además los dos
  motivos en su instantánea, y con ella la historia completa. Las filas anteriores conservan su
  estado sin motivo: la regla se aplica al cambiar de estado, no al leer.
- **Fusión de fichas** (D-93; [PARTIES.md](PARTIES.md) §17): Compras escucha
  `parties.relationship-transferred.v1`. Cuando la relación de proveedor de una empresa pasa de
  una ficha a otra, copia el perfil de proveedor (categoría, retención, días de pago, IBAN,
  bloqueo) de (empresa, la que cede) a (empresa, la que recibe) si ésta no tiene uno. El de la que
  cede no se toca. Al deshacer la
  fusión (`returning: true`) no se copia nada.

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

## Bloquear y desbloquear son órdenes propias (D-156 ampliada, 2026-11-15)

Como Trench en su PR 151, el bloqueo ya no se cambia con la ficha.

- **Órdenes:** `BlockSupplier` (con `Blocked` true o false), `POST /api/v1/suppliers/{id}/block` y
  `POST /api/v1/suppliers/{id}/unblock` con `{"version": n, "reason": "..."}`, los dos
  obligatorios (sin versión, 400; con otra, 409). Responden el perfil: 200, o 202 con
  `pendingApproval` si la empresa exige aprobación (abajo). Sin motivo, 422
  `purchases.block_reason_required` o `purchases.unblock_reason_required`; bloquear uno bloqueado,
  422 `purchases.supplier_already_blocked`; desbloquear uno sin bloquear, 422
  `purchases.supplier_not_blocked`. Permiso `Purchases.Supplier.Update` y ámbito de escritura.
- **El cambio general no bloquea:** `SetSupplier`, `ChangeSupplier`, `PUT /api/purchases/suppliers`
  y `PUT /api/v1/suppliers/{id}`. Si el cuerpo pide un estado de bloqueo distinto del actual, 422
  `purchases.block_change_requires_command` (el `Parties.BlockChangeRequiresCommand` de Trench); si
  coincide o no viene, se ignora, igual que `blockReason` y `unblockReason`. En los casos de uso
  `Blocked` es `*bool` y los motivos ya no están (`PUT /api/purchases/suppliers`, que rechaza
  campos desconocidos, da 400 si llegan).
- **El alta nace sin bloquear:** pedirla bloqueada es el mismo 422.
- **`DELETE /api/v1/suppliers/{id}`** sigue siendo «bloquear»: usa la orden con la versión vigente
  (motivo en el cuerpo o en `?reason=`).
- **Historial** (migración 8): `pur_supplier_block_events` (`id`, `version`, `profile_id`,
  `company`, `supplier`, `blocked`, `reason`, `user_id`, `user_name`, `occurred_at`,
  `approval_id`; índice `ix_pur_sup_block_events` por perfil y fecha). Una fila por cada bloqueo o
  desbloqueo efectivo, inmediato o al aprobar, en la misma transacción que el perfil. El usuario es
  el actor de esa transacción: con aprobación, quien aprueba; el solicitante está en la solicitud
  (`approval_id`). Las altas y fusiones anteriores no dejan fila. Se lee con
  `GET /api/v1/suppliers/{id}/block-history` (`SupplierBlockHistory`, `Purchases.Supplier.Read`,
  ámbito de lectura; 404 fuera), del más antiguo al más reciente:
  `[{id, profile, company, supplier, blocked, reason, userId, userName, at, approval}]`.
- **El perfil** guarda el motivo vigente (`block_reason`) y el del último desbloqueo
  (`unblock_reason`), como hasta ahora. Quién y cuándo del último bloqueo y desbloqueo **no** se
  copian al perfil (decisión técnica): salen del historial, y así no hay dos fuentes.
- **Front:** `blocked`, `blockReason` y `unblockReason` no son editables en
  `supplier.config.json`; las acciones nuevas están descritas en la ayuda del campo.

## Aprobación del bloqueo y el desbloqueo (D-156, paso 3; 2026-11-14)

Si la empresa tiene la acción en su lista (Parties; docs/APROBACIONES.md, paso 2), bloquear o
desbloquear un proveedor no se hace en el acto: deja una solicitud que aprueba otra persona.

- **Acciones:** `purchases.supplier.block` y `purchases.supplier.unblock`. **Permisos nuevos**
  (derivados por el motor): `Purchases.Supplier.ApproveBlock` y `Purchases.Supplier.ApproveUnblock`;
  el mismo aprueba y rechaza.
- **Composición:** `purchases.WithApprovalPolicy(policy)`; por omisión `approval.Never` (todo como
  antes). El anfitrión le pasa `h.Parties.ApprovalPolicy`.
- **Migración 7:** `pur_approvals`, la tabla del motor (`approvals.DDL`), con su índice
  `ix_pur_approvals_target`. Repositorio `sqlrepo` o en memoria por el conmutador, como el resto; la
  solicitud se guarda en la misma transacción que el cambio.
- **Qué la pide:** sólo las órdenes de bloquear y desbloquear (y el `DELETE`, que es la de
  bloquear); desde 2026-11-15 el cambio general no toca el bloqueo. Pedirla exige lo mismo que
  ejecutarla: `Purchases.Supplier.Update`, el ámbito de escritura, la versión vigente y el motivo
  (422 sin él, con los códigos de siempre, y también si ya está en ese estado). La carga útil es
  `{"supplier", "blocked", "reason"}`.
- **Respuesta: 202.** El cuerpo es el perfil **sin cambiar** (ni su versión), con `pendingApproval`
  (el id de la solicitud); el `DELETE` responde `{"pendingApproval": id}`. Así desaparecen los dos
  menores de la PR #102: la versión subía sin cambios, y un 409 por solicitud repetida tiraba los
  demás cambios del formulario.
- **Una pendiente por perfil, sea bloquear o desbloquear:** la segunda es 409
  (`purchases.approval_pending` en el detalle). Lo comprueba el motor y, en SQL, la base:
  `pur_approvals` gana `pending_key` (migración 8) con el índice único `ux_pur_approvals_pending`
  (migración 9). Desde la migración 10 la clave es la del motor común (docs/APROBACIONES.md, «Una
  sola pendiente»): `block:purchases.supplier_profile:<perfil>` mientras está pendiente y su id
  después; antes era `acción:perfil`, y bloquear y desbloquear podían convivir. La 10 recalcula las
  claves que ya hubiera (si dos pendientes chocan, la más antigua se queda la clave). El mapeo es el
  del motor (`approvals.Mapping`).
- **Rutas de las solicitudes**, junto a las del contexto:

| Ruta | Qué hace |
|---|---|
| `GET /api/purchases/approvals?company&status` | las solicitudes del ámbito de quien llama (`Purchases.Supplier.Read`); por omisión las `pending`; `status` admite `approved`, `rejected`, `cancelled` o `all` (otro, 400) |
| `POST /api/purchases/approvals/{id}/approve` `{comment?}` | aprueba y **aplica el bloqueo o el desbloqueo en la misma transacción**; 403 sin el permiso de aprobar, 422 `approval.self_resolution` si es el solicitante, 422 `approval.already_resolved` si ya está resuelta |
| `POST /api/purchases/approvals/{id}/reject` `{comment}` | rechaza; el comentario es obligatorio (422 `approval.comment_required`); el perfil no cambia |
| `POST /api/purchases/approvals/{id}/cancel` `{comment?}` | la retira; sólo el solicitante (422 `approval.not_requester`) |

Las cuatro, también bajo `/api/v1/purchases/approvals`, la forma de las rutas que llama el motor de
Angular de Karpo C# (front/README.md).

- **Ámbito del aprobador** (lo que APROBACIONES.md dejaba para el paso 3): resolver exige además el
  ámbito de escritura en la empresa de la solicitud: fuera de él, 404; de sólo lectura, 403.
- **Si el perfil cambió entretanto:** al aprobar sólo se aplican el bloqueo (o el desbloqueo) y su
  motivo, con su fila en el historial (con `approval_id`); los demás campos se quedan como estén. Si el perfil ya está en el estado pedido (p. ej. se
  bloqueó por otra vía cuando la empresa aún no exigía aprobación), la solicitud queda aprobada y el
  perfil no se toca: idempotente.
- **Auditoría:** la solicitud deja su registro (`approval.request`, al crearse y al resolverse, con
  el estado, el comentario y el evento) en `purchases_audit_log`, y el cambio del perfil el suyo, con
  el aprobador como actor. Los eventos `approval.*` no salen al outbox: la acción se aplica en la
  misma transacción y nadie más los escucha.
- **Front:** el motor de mantenimiento trata el 202 como un éxito; enseñar que queda pendiente, los
  botones de bloquear y desbloquear y la bandeja de solicitudes están por hacer.
- **Pruebas:** `TestPurchases_BlockApprovals_MemoryThenSQLite`, `TestPurchases_BlockWithoutPolicy_IsImmediate`,
  `TestPurchases_OnePendingApprovalInTheDatabase` (el índice único, en SQLite) y el final de
  `TestPurchasesContext` (integration/).

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

## El contrato de mantenimiento (front; añadido el 2026-11-14)

Los perfiles de proveedor se mantienen también por el **contrato de mantenimiento**, el que lee el
motor de Angular de Karpo C# (el contrato `crud` del lenguaje de Theros), bajo `/api/v1/suppliers`.
Es otra puerta a los mismos perfiles; la configuración de su pantalla está en
`front/angular/config/entities/purchases/supplier.config.json` (ver front/README.md). Es el mismo
contrato que el de Productos, Instalaciones, Activos y Envíos (docs/ENVIOS.md).

| Operación | Ruta | Qué hace |
|---|---|---|
| Lista | `GET /api/v1/suppliers?page&pageSize&sortBy&sortOrder&company&supplier&category&isActive&includeMergedParties&ids`, o `POST /api/v1/suppliers/search` con `{filter, pagination: {pageNumber, pageSize, orderBy, ascending}}` | la página del motor: `{items, total, totalCount, pageNumber, pageSize, totalPages}`. Sin `pageNumber`, la primera; 0 o negativo, 400. Orden sólo por `paymentDays` (también por omisión), con la identidad para deshacer empates; otro, 400. `isActive=true` deja los que no están bloqueados; los de parties fusionadas quedan fuera salvo `includeMergedParties=true` (D-100). `company` sólo estrecha el ámbito de quien llama. No hay búsqueda de texto: el nombre del proveedor es de Parties; `search` se ignora, como cualquier filtro que no es de la lista. `ids` pide varias identidades a la vez, para resolver etiquetas (ver [el filtro ids](CONTRATO-MANTENIMIENTO.md)). Pedidos por `ids` o por `supplier`, salen también los de parties fusionadas: es pedirlos por sí mismos, como `GET /{id}` (D-100, D-108) |
| Lectura | `GET /api/v1/suppliers/{id}` | el perfil |
| Alta | `POST /api/v1/suppliers` | 201 con `Location`; lo que sobra en el cuerpo se ignora. Si la empresa ya tiene perfil para ese proveedor, 422 `purchases.duplicate_supplier` (el `PUT /api/purchases/suppliers` de siempre, en cambio, lo sustituye) |
| Cambio | `PUT /api/v1/suppliers/{id}` con lo leído y su `version` | 200; lo que el cuerpo no trae, o trae como null, se queda como está. Sin `version`, 400; con una que ya no es la suya, 409. Se traduce a `ChangeSupplier` (`category`, `withholdingRate`, `paymentDays`, `iban`), sólo si algo cambia; `blocked` distinto del actual es 422 `purchases.block_change_requires_command` y los motivos se ignoran (la retención se compara por su valor y la cuenta sin espacios ni mayúsculas); aun sin cambios exige el permiso y el ámbito de escritura. Todo el cambio es una sola unidad de trabajo. Los textos se recortan. `company` y `supplier` no los cambia ningún caso de uso: cambiarlos es 400 `immutable` (se comparan por su valor: la identidad sin distinguir mayúsculas ni los espacios de los extremos) |
| Baja | `DELETE /api/v1/suppliers/{id}` | un perfil no se borra (sus facturas nombran al proveedor y el dominio no tiene baja, sólo bloqueo): DELETE lo **bloquea** con el motivo de `{"reason": "..."}` en el cuerpo o de `?reason=` (si vienen los dos, manda el del cuerpo), y a un proveedor bloqueado no se le registran facturas nuevas. 204 (202 si bloquear exige aprobación, más abajo); sin motivo, 422 `purchases.block_reason_required`; sobre uno ya bloqueado, 204 otra vez y sin versión nueva (el motivo no cambia). Siempre exige el permiso y el ámbito de escritura (`Purchases.Supplier.Update`; 403 sin él). Se desbloquea con `POST /api/v1/suppliers/{id}/unblock` |
| Bloquear, desbloquear | `POST /api/v1/suppliers/{id}/block`, `…/unblock` con `{version, reason}` | las órdenes propias (sección «Bloquear y desbloquear son órdenes propias») |
| Historial | `GET /api/v1/suppliers/{id}/block-history` | los bloqueos y desbloqueos efectivos del perfil |

- **El objeto:** `id`, `company`, `supplier`, `category`, `withholdingRate`, `paymentDays`, `iban`,
  `blocked`, `blockReason`, `unblockReason`, `version` y, en un 202, `pendingApproval`.
- **Casos de uso nuevos:** `RegisterSupplier` (alta sin sustituir), `ChangeSupplier` (por la
  identidad, con la versión leída: con ella el conflicto es definitivo y no se reintenta; sin ella,
  se reintenta como siempre), `GetSupplier`, `SupplierChangeable` y `PageSuppliers`. Los permisos
  son los de siempre (`Purchases.Supplier.Read` y `…Update`).
- **El error:** RFC 9457 con `errors` como diccionario (campo → mensajes) y `errorCodes`, en el
  mismo orden; en un 409 o un 422, `code`. Un cuerpo de más de 1 MiB es 413. El resto de la API de
  Compras sigue con la lista del framework.
- **Sin migración:** el contrato no añade columnas. La lista es una consulta paginada nueva sobre
  `pur_suppliers` (la genera `sqlrepo` a partir de la especificación: filtros por `company`,
  `supplier`, `category` y `blocked`; orden por `payment_days` e `id`); no hay SQL escrito a mano.
- **Prueba:** `TestPurchases_SupplierMaintenanceContract_MemoryThenSQLite`.
