# Contexto Facturación

Port de Invoicing de C# al contexto `contexts/billing`. En C#:

- **Invoicing** vivía en ErpKernel: `Invoice`, `InvoiceItem`, `CreditNote`, los catálogos de tipo
  y estado, y la facturación a demanda desde albaranes y pedidos.
- **La numeración** estaba en Documents: `DocumentSeries` e `IDocumentIssuer`.
- **Los perfiles comerciales** estaban en Parties.

El cálculo de impuestos sigue la decisión del 2026-09-28 (punto 6 de
[BACKLOG.md](../BACKLOG.md)): se hace **por país**, en jurisdicciones de Fiscal, y **por régimen
o sector** dentro de cada una.

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

- **`CreateAsync` no guarda el emisor ni el destinatario.** Todas las facturas se crean con
  `Guid.Empty`, y ninguna prueba lo detecta.
- **Una factura emitida sigue siendo modificable y borrable:**
  - no hay máquina de estados;
  - `IssueAsync` no pasa la factura a «emitida»;
  - borrar una factura deja su número en Documents.
- **No hay impuestos, ni moneda, ni vencimientos:**
  - la línea es `Quantity × UnitPrice` sin redondear;
  - la exención se modela como tipo de factura («Exenta»), que se busca **por nombre**.
- **Numeración:**
  - la factura no tiene organización, así que se puede numerar con la serie de otra empresa;
  - no se comprueba el año de la serie;
  - no hay series sembradas;
  - los pedidos se numeran por su cuenta (`PED-yyyy-guid8`).
- **La nota de abono no tiene líneas:**
  - no se comprueba que exista la factura original;
  - las causas son texto libre;
  - se solapa con los tipos «Credit Memo» y «Debit Memo».
- **Los precios no pasan del pedido a la factura:** una factura a demanda sale con precio 0.
- **No hay permisos:** los `Invoicing.*` están definidos pero no se exige ninguno.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `Invoice` (cabecera sin vendedor, sin impuestos, emisor y destinatario sin guardar) | 4 | 1 | 2 | 3 | **54** | Modificar → agregado `Invoice` con **las líneas dentro**: vendedor (organización interna), cliente, fechas de operación, emisión y vencimiento, moneda (EUR en esta fase), recargo de equivalencia del cliente y copia de las identidades fiscales y del desglose al emitir |
| 2 | `InvoiceItem` (agregado aparte, sin redondeo) | 4 | 2 | 2 | 3 | **59** | Modificar → hijo `Line`: cantidad, precio, descuento, **código de tipo o tratamiento**; neto al céntimo redondeando los medios hacia fuera de cero |
| 3 | Estado sin máquina, emitida mutable y borrable | 4 | 1 | 2 | 3 | **54** | Modificar → borrador o emitida. **La emitida es inmutable** y solo se corrige con una rectificativa; solo se descartan borradores |
| 4 | Tipo «Exenta» + `VatGroup`/`VatIndicator` sin usar | 2 | 1 | 1 | 2 | **30** | Sustituido → cada línea lleva su código de tipo o de tratamiento, y **Fiscal calcula** (`TaxEngine`) con la jurisdicción del vendedor: España con el régimen general, cuota por tipo sobre la base agregada, exentas y recargo |
| 5 | `DocumentSeries` + `IDocumentIssuer` (sin organización ni año; sin series sembradas) | 4 | 2 | 3 | 3 | **63** | Modificar → agregado `Series` de Facturación: vendedor, código, año y tipo (**las rectificativas van en series propias**). El número se toma **en la misma unidad de trabajo** que la emisión (sin huecos), con concurrencia optimista; formato `CÓDIGO-AAAA-NNNNNN` |
| 6 | `CreditNote` (cabecera sin líneas) + tipos «Credit/Debit Memo» | 3 | 1 | 2 | 3 | **46** | Modificar → factura **rectificativa** (`Kind = corrective`) por diferencias. Comprueba la original: emitida y del mismo vendedor; copia el cliente y el recargo de la original; causa **R1–R5** |
| 7 | Facturación a demanda (albaranes y pedidos, `order_item_billing`) | 4 | 2 | 3 | 3 | **59** | Aplazado → cuando se porten Pedidos y Envíos (los enlaces facturados y el índice «una línea no se factura dos veces») |
| 8 | `Payment` y `PaymentApplication` | 3 | 2 | 3 | 3 | **54** | Fuera de Facturación → contexto Cobros/Tesorería, que consumirá `billing.invoice-issued.v1` |
| 9 | Catálogos `InvoiceType` e `InvoiceStatusType` | 2 | 2 | 2 | 3 | **42** | Retirar → enumerados del dominio |
| 10 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Billing.Invoice.Read/Create/Update/Issue` (**emitir va aparte** de preparar) y `Billing.Series.Read/Update` |

## Diseño

```
contexts/billing/
├── domain/          # Invoice (+Line, Breakdown, Identity), Series, eventos, puertos Taxes e Identities
├── contracts/       # lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito
├── infrastructure/  # esquema bil_* de 5 motores, mapeos, adaptadores FiscalTaxes y PartiesIdentities
└── module.go        # composición y rutas /api/billing/...

contexts/fiscal/
├── domain/jurisdiction.go   # Jurisdiction, Assessment, Breakdown, RateBook (núcleo independiente del país)
├── jurisdictions/es/        # España: régimen general (el resto, en BACKLOG §6)
└── application/engine.go    # TaxEngine: perfil del vendedor → jurisdicción de su país → cálculo
```

- **Ámbito:** todo pertenece al vendedor. Fuera de su ámbito, 404 uniforme; con solo lectura, 403.
- **Emisión, en una unidad de trabajo:**
  1. las identidades fiscales, desde Parties (`TaxIdentities`): el NIF del vendedor debe ser
     válido y el del cliente, existir;
  2. el desglose, desde Fiscal, en la fecha de devengo (la de operación, o la de emisión);
  3. el número de la serie: mismo vendedor, mismo año y mismo tipo (ordinaria o rectificativa);
  4. la congelación de la factura.

  Si algo falla, no se consume número.
- **Invariantes al emitir:**
  - al menos una línea;
  - ordinaria con neto positivo; rectificativa con neto distinto de cero;
  - el desglose cubre exactamente el neto de las líneas;
  - el vencimiento no es anterior a la emisión.
- **Vista previa:** `GET /api/billing/invoices/{id}/taxes?on=` calcula el desglose de un
  borrador sin emitirlo.
- **Lenguaje publicado:** `billing.invoice-issued.v1` con número, clase, factura rectificada y
  causa, NIF de las partes, fechas, totales y desglose por tipo. Lo consumirán Fiscal (libros,
  303, SII y Verifactu), Contabilidad y Cobros.
- **Tablas:**
  - `bil_series`, con índice único por vendedor, código y año, y la fecha de la última factura
    emitida (`last_issue_date`, migración 7);
  - `bil_invoices` (FK a sí misma para la rectificada), con índice único por serie y número
    (migración 6), y con `bil_invoice_lines` y `bil_invoice_taxes`;
  - las bandejas de salida y la auditoría.

## Decisiones (aprobadas por Javier el 2026-09-28)

1. **Las series viven en Facturación**, no en un contexto Documents genérico. La numeración sin
   huecos exige tomar el número en la misma transacción que la emisión. Si más adelante hay
   Documents, recibirá el evento para archivar y generar el PDF.
2. **Solo euros por ahora.** La moneda se guarda, pero no se aceptan otras hasta que haya tipos
   de cambio y reglas de redondeo por moneda.
3. **Sin factura simplificada en esta fase:** el cliente debe tener NIF. La simplificada (hoy
   «ticket») llegará con Verifactu.
4. **La rectificativa es solo por diferencias.** La rectificación por sustitución se añadirá con
   el SII y Verifactu, que la distinguen.
5. **El recargo de equivalencia lo indica el borrador.** Pasará al perfil comercial del cliente
   cuando se porte (hoy está en Parties de C#, sin ese dato).

## Validación

- **Dominio:**
  - serie: numeración, otro vendedor, otro año, otro tipo, serie cerrada; un intento rechazado
    no consume número;
  - líneas: descuento y redondeo, negativas solo en rectificativas;
  - emisión: desglose que no cuadra, NIF del vendedor, vencimiento anterior;
  - inmutable tras emitir; rectificativa con causa R1–R5.
- **Jurisdicción España:**
  - base agregada por tipo (30,09 × 21 % = 6,32, frente a 6,33 redondeando línea a línea);
  - reducido, exento y recargo;
  - IGIC en Canarias, sin IVA allí;
  - tipo aún no vigente;
  - base sin redondear, línea sin código, tratamiento desconocido, código en una línea exenta;
  - bases negativas.
- **Extremo a extremo** (Parties, Fiscal y Facturación sobre el mismo backend, en memoria y en
  SQLite migrada, por HTTP):
  - series duplicadas y ajenas;
  - factura con tres tipos (170,09 + 15,32 = 185,41);
  - preparar no es emitir: 403; serie rectificativa para una ordinaria: 422;
  - emitida, inmutable y no borrable; ajeno: 404;
  - recargo de equivalencia (5,20);
  - serie de otro año rechazada sin hueco;
  - rectificativa R1 en su serie (−6,32 de cuota), que no puede rectificar un borrador;
  - búsqueda;
  - eventos v1.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con Parties, Fiscal y Facturación
  migrados en la misma base:
  - cantidades y precios exactos (2,5 × 19,999 − 12,5 % = 43,75);
  - desglose con recargo congelado y recuperado;
  - rectificativa con FK a sí misma;
  - búsqueda por fecha de emisión.

## Facturar desde el albarán (añadido el 2026-10-05)

Aplica la decisión 5 de [PEDIDOS.md](PEDIDOS.md): Pedidos publica el albarán y Facturación lo
factura, sin que ningún contexto escriba en el otro.

- **Qué hace Facturación al recibir `orders.delivery-issued.v1`:** prepara un **borrador** de
  factura para ese cliente con las líneas del albarán, con su precio, su descuento encadenado
  (D-149) y su código de impuesto. La fecha de operación es la del albarán y la descripción cita el albarán y el pedido.
  Una persona lo revisa y lo emite en una serie; la emisión sigue siendo un permiso aparte.
- **La factura recuerda su origen** (tipo, Id y número del documento). Hay un borrador por
  albarán: un mensaje repetido no crea otro.
- **Un borrador que viene de un albarán no se descarta** (422): lo entregado se factura, y un
  error se corrige con una rectificativa después de emitir.
- **Al emitirse,** `billing.invoice-issued.v1` lleva el origen. Pedidos anota en el albarán la
  factura que lo cobra y deja de listarlo como pendiente de facturar. Cobros abre la deuda como
  con cualquier factura, y esa deuda cuenta para el crédito del siguiente pedido.
- **Pedidos no deja vender un producto sin código de impuesto** (422), porque su albarán no se
  podría facturar.
- **Cambios de esquema:** en Facturación, migración 3 (columnas `source_type`, `source_id` y
  `source_ref` en `bil_invoices`) y migración 4 (`billing_inbox`); en Pedidos, migración 3
  (`invoice_id`, `invoice_number` e `invoiced` en `ord_deliveries`).

### Decisiones (aprobadas por Javier el 2026-10-06)

1. **Cada albarán genera un borrador de factura, automáticamente.** No se factura el pedido sino
   lo entregado. Desde D-146, salvo el de un cliente con facturación mensual, que espera al cierre
   del periodo.
2. **El borrador se emite a mano,** en la serie y fecha que decida quien factura. Facturar
   automáticamente al entregar queda fuera.
3. **Un borrador nacido de un albarán no se descarta;** sí se pueden editar sus líneas antes de
   emitir. Desde D-146, sólo desaparece al unirse a otro.
4. ~~Una factura por albarán~~. **Sustituida por D-146** (Javier, 2026-11-07): varios albaranes de
   un cliente en una factura, configurable y con los dos modos, a mano y por periodo. Ver
   «Varios albaranes en una factura».

### Validación

- **Extremo a extremo** (Parties, Fiscal, Productos, Inventario, Cobros, Pedidos y Facturación
  sobre el mismo backend, en memoria y en SQLite migrada):
  - un producto sin código de impuesto no se puede añadir al pedido (422);
  - pedido de 60 unidades y 1 hora, albarán de 95,00 y un único borrador con su origen, fecha de
    operación, líneas, precios y el mismo neto que el albarán;
  - el borrador no se descarta (422); el albarán figura como pendiente de facturar;
  - emisión (114,95): el albarán queda con su factura y ya no está pendiente;
  - Cobros debe 114,95 y deja 85,05 de crédito: un pedido de 110,00 no se confirma (422) y uno de
    75,00 sí.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con los siete contextos migrados en
  la misma base, incluidas las migraciones que añaden columnas: borrador desde el albarán por
  `billing_inbox`, origen de ida y vuelta, descarte rechazado, emisión, albarán facturado por
  `orders_inbox` y deuda abierta en Cobros.

## Varios albaranes en una factura (D-146, plan Sage T8; añadido el 2026-11-07)

Decisión de Javier del 2026-11-07 («1 c configurable»): varios albaranes de un cliente en una sola
factura, de los dos modos. Por defecto sigue un borrador por albarán.

- **La factura recuerda todos sus orígenes** en la tabla hija `bil_invoice_sources` (`invoice_id`,
  `line_no`, `source_type`, `source_id`, `source_ref`), migración 10, que copia el origen de las
  facturas que ya lo tenían **antes** de crear el índice único `ux_bil_invoice_sources_source`
  sobre (`source_type`, `source_id`), todo en la misma migración. Si dos facturas tuvieran el mismo
  documento, la creación del índice falla con un error de clave repetida. En PostgreSQL, SQL Server
  y SQLite la migración entera se deshace y no se marca como hecha; en Oracle y MySQL, donde el DDL
  no es transaccional, queda **sucia** (`ErrSchemaDirty`): hay que quitar el duplicado y la tabla a
  medio hacer y forzarla (`Migrator.Force`). Un duplicado así sólo puede venir de antes de D-146, de
  un mismo albarán entregado dos veces a la vez, que pasó las dos la comprobación de origen antes
  de que ninguna guardara su borrador. El índice impide que un albarán esté en dos facturas; el
  choque es un conflicto (se reintenta). Las columnas `source_type`, `source_id` y `source_ref`
  de `bil_invoices` **se conservan** con el primer origen, para quien ya las lea; quitarlas exigía
  borrar un índice y columnas en cinco dialectos sin ganar nada.
- **La factura y el evento:** `sources` (`type`, `id`, `ref`) lleva todos; `sourceType`, `sourceId` y
  `sourceRef` siguen con el primero. `billing.invoice-issued.v1` añade `sources` y conserva
  `sourceType` y `sourceId`: es compatible hacia atrás. Pedidos marca facturado **cada** albarán de
  `sources`. Documentos sigue enlazando sólo el primero (pendiente en BACKLOG).
- **A mano:** `POST /api/billing/invoices/merge` con `{"invoices": [id1, id2, …]}` une en el
  primero los demás borradores: las líneas y los orígenes de todos; la fecha de operación pasa a
  ser la última de todas (el final del periodo que cubren) y la descripción cita los albaranes
  por orden de número («Albaranes A-1, A-2»); si la lista pasa de 200 caracteres, sólo el menor, el
  mayor y cuántos
  («Albaranes ALB-2026-000101 a ALB-2026-000140 (40)»), que siempre cabe en los 500 de la
  descripción: la lista entera está en los orígenes. Los demás se borran en la misma transacción: es el único modo de quitar
  un borrador de albarán. Permiso: el de editar borradores, `Billing.Invoice.Update` (el de
  descartar). Rechazos (422): menos de dos (`billing.merge_too_few`), alguno emitido
  (`billing.invoice_not_draft`), alguno que no viene de albarán (`billing.merge_unsourced`),
  clientes o vendedores distintos (`billing.merge_other_customer`), un cálculo distinto, redondeo o
  decimales congelados al preparar, D-144 (`billing.merge_calculation`), y unos con recargo de
  equivalencia y otros sin él (`billing.merge_surcharge`). El mismo borrador dos veces es un 400.
- **Por periodo:** la periodicidad es del cliente en cada empresa, en las condiciones de venta de
  Pedidos (`Terms.billingPeriodicity`, `per_delivery` o `monthly`; [PEDIDOS.md](PEDIDOS.md)), que es
  donde ya están la tarifa, el descuento y los bloqueos, y donde Trench tiene `BillingPeriodicity`.
  Pedidos la pone en cada `orders.delivery-issued.v1`, así Facturación no consulta a Pedidos ni
  ningún contexto escribe en otro. El albarán de un cliente `monthly` no tiene borrador: queda en
  `bil_pending_deliveries` (+ `bil_pending_delivery_lines`), migración 11, con lo necesario para
  facturarlo; su Id es el del albarán, así que no queda pendiente dos veces, y uno que ya está en
  una factura tampoco. Al llegar se prepara su borrador sin guardarlo, para validar sus líneas: un
  albarán que no se podría facturar (una línea sin código de impuesto) se rechaza ahora, como el de
  un cliente por albarán, en vez de quedar pendiente y hacer fallar el cierre de su cliente. Cada
  línea pendiente guarda su precio y su descuento encadenado (`bil_pending_delivery_lines.discount`,
  migración 13, D-149; nulo en las de antes, que guardaban el precio neto).
- **Pendientes:** `GET /api/billing/pending-deliveries?seller=…&customer=…&upTo=…&page=…&size=…`
  los lista (Id del albarán en `delivery`, cliente, número, pedido, fecha, número de líneas y neto),
  por fecha y número; con `upTo`, sólo los de hasta ese día: lo que facturaría un cierre. Permiso: el de leer facturas, `Billing.Invoice.Read`, y sólo los de los vendedores del
  ámbito de quien lee.
- **Cierre del periodo:** `POST /api/billing/periods/close` con `{"seller", "upTo", "customer"?}`
  prepara para cada cliente un borrador con todos sus albaranes pendientes hasta `upTo`, incluido,
  y los quita de pendientes. Permiso: el de preparar borradores, `Billing.Invoice.Create`.
  - **Cliente a cliente**, cada uno en su propia transacción: si uno falla, sus albaranes siguen
    pendientes y los demás se cierran. La respuesta es `{"drafts": [...], "failed": [{"customer",
    "code", "message"}]}`. Una línea que la factura no admite da `billing.close_invalid_line` a ese
    cliente (sólo un pendiente guardado antes de validarse al llegar); una regla del borrador, su
    propio código; un conflicto que agota los reintentos, `billing.close_conflict`, con un mensaje
    genérico y el detalle en el log. Cualquier otro error (la base de datos, una cancelación) no es
    del cliente: corta el cierre y la ruta responde con ese error; los clientes ya cerrados se
    quedan cerrados (cada uno en su transacción) y un cierre posterior sigue con los demás.
  - **Como mucho 500 líneas por borrador:** si los albaranes de un cliente pasan de ahí, el cierre
    prepara varios borradores, sin partir ningún albarán (por fecha y número, cada borrador se llena
    con los albaranes que caben enteros).
  - **Un pendiente que ya está en una factura** (llegó como mensual y otra vez como por albarán, o
    se facturó de otro modo) se borra al cerrar sin preparar otro borrador.
  - **Idempotente:** un segundo cierre no encuentra nada y devuelve `{"drafts": [], "failed": []}`.
    Dos cierres a la vez: cada uno lee los pendientes del cliente dentro de su transacción y los
    borra antes de facturarlos. El pendiente que el otro ya borró no está (NotFound) y se salta; si
    los dos lo borran a la vez, choca la versión del pendiente o el índice único de los orígenes,
    que es un conflicto: la transacción del cliente se deshace y se reintenta, ya sin nada
    pendiente. Ninguno de los dos acaba en 404 ni duplica un borrador.
  - **Un albarán que llega tarde,** con fecha anterior a un corte ya cerrado, queda pendiente y entra
    en el cierre siguiente, con su fecha; la fecha de operación del borrador es la del último
    albarán.
  - Los borradores del cierre se calculan con los ajustes del vendedor al cerrar (D-144) y no se
    descartan, como los de un albarán.
  - **Cierre programado** (`CloseDuePeriods`): en cada ronda de tareas del anfitrión
    (`Host.RunChores`), cada empresa cierra su periodo hasta el último día del mes anterior,
    contado en su zona horaria (la guardada o la de su país): a las 23:30 UTC del 31 de octubre ya
    es 1 de noviembre en Madrid y octubre se cierra. Como el cierre es idempotente, sólo prepara
    algo la primera ronda después de acabar el mes; los albaranes del mes en curso siguen
    pendientes. Un albarán de un mes ya cerrado que llega tarde va en su propio borrador en la ronda
    siguiente (se puede unir a mano al del cierre, si aún es un borrador). Los clientes que no se
    pueden cerrar salen como fallo de la ronda (`billing period of …`) y se reintentan en la
    siguiente. `RunChores` cuenta los borradores en `PeriodDrafts`. La ruta sigue sirviendo para
    cerrar a mano otro corte. Prueba: `TestBilling_CloseDuePeriods`.
- **Códigos de error de D-146** (422 salvo que se diga): `billing.merge_too_few`,
  `billing.invoice_not_draft`, `billing.merge_unsourced`, `billing.merge_other_customer`,
  `billing.merge_calculation`, `billing.merge_surcharge`, `billing.too_many_lines` (la unión pasa de
  500 líneas), `billing.sourced_draft` (descartar un borrador de albarán); en `failed` del cierre,
  `billing.close_invalid_line` y `billing.close_conflict`. Validación (400): `invoices` repetido o
  mal formado, `upTo` que falta (cierre) o mal formado (listado), `seller` o `customer` mal formados,
  un albarán mensual con una línea que la factura no admite (el mensaje se rechaza); `billingPeriodicity` distinta de
  `per_delivery` o `monthly` en las condiciones de Pedidos.
- **Diferencia con Trench:** Trench enlaza **líneas** (`shipment_item_billing`, `order_item_billing`:
  línea de envío o de pedido con línea de factura, con `IsActive` y un índice único sobre la línea
  activa). Aquí el enlace es por **documento**, porque el albarán de Pedidos es inmutable y se
  factura entero, y es una sola tabla para cualquier tipo de origen. La invariante es la misma: un
  albarán no se factura dos veces (índice único).
- **Pruebas:** de dominio (`TestInvoice_Merge`, `TestPendingDelivery`, `TestTermsAndCounter`,
  `TestOrder_ConfirmHoldDeliver`); de aplicación con HTTP (`TestBilling_MergeDeliveryDrafts`,
  `TestBilling_ClosePeriod`, con el listado de pendientes y un albarán tardío;
  `TestBilling_ClosePeriodPerCustomer`: 40 albaranes con la descripción corta, 600 líneas en dos
  borradores, un albarán mensual sin código de impuesto rechazado al llegar, un pendiente
  defectuoso que hace fallar sólo a su cliente, el listado con `upTo`, un pendiente ya facturado y
  40 borradores unidos a mano; `TestCloseFailure`, qué errores anota el cierre y cuáles lo cortan); de extremo a extremo en memoria y en SQLite migrada
  (`TestOrders_DeliveryNotesAreGrouped_MemoryThenSQLite`): dos albaranes unidos a mano y emitidos
  (127,05), los dos facturados en Pedidos y la deuda en Cobros; una clienta mensual con tres
  albaranes, ningún borrador hasta el cierre, uno con los tres al cerrar y ninguno más en un
  segundo cierre; emitido, los tres facturados. En los motores, `TestOrdersInvoicing` lo repite
  con las migraciones nuevas.

## Números que no se repiten y fechas que no retroceden (plan Sage, T0; añadido el 2026-11-03)

Tareas T0-G1 y T0-G2 del plan de Karpo C# para incorporar lo aprendido de Sage (D-127), confirmada por
Javier el 2026-11-03.

- **Índice único** `ux_bil_invoices_number` sobre (`series_id`, `invoice_number`), migración 6.
  Los borradores no tienen serie ni número; en SQL Server, que trata los NULL como iguales, el
  índice lleva el filtro `WHERE invoice_number IS NOT NULL` (`{whereNotNull}`). Es la segunda
  barrera: la primera sigue siendo la versión de la serie.
- **Un número repetido es un conflicto:** el mapeo declara el índice, el choque es
  `fw.ErrConflict` y la emisión se reintenta con un número nuevo.
- **La emisión se reintenta hasta 30 veces** (el resto de órdenes, 5). De varias emisiones a la
  vez en una serie, cada vuelta sólo la gana una: con veinte emisores, la última pierde diecinueve
  veces. Con 5 intentos, la prueba de veinte emisiones fallaba en los cuatro motores.
- **Prueba:** `TestBillingContext` emite veinte borradores a la vez en una serie: se emiten todos,
  del 1 al 20, sin repetidos ni huecos; después, un número repetido escrito sin pasar por la serie
  choca con el índice. En MySQL esa parte tarda unos 30 segundos; en los demás, segundos.
- **La fecha de emisión no retrocede en una serie** (T0-G2): la serie guarda la de su última
  factura y rechaza una anterior con 422 `billing.series_date_backwards`, sin gastar número. La
  misma fecha vale. La migración 7 añade `last_issue_date` y la rellena con la fecha de la última
  factura emitida en cada serie.
- **Sin fecha, la de hoy en la zona de la empresa** (decisión de Javier, 2026-11-05): la emisión
  sin fecha toma el día en la zona horaria del vendedor, no en UTC; a las 22:30 UTC del 14 de
  octubre ya es día 15 en Madrid. La zona se pide por empresa (`Zones.Location(ctx, vendedor,
  país)`, en el anfitrión `host.CompanyZones`), en este orden (D-133): la que la empresa guarda en
  su perfil de Parties (`internal_organization_profile.time_zone`, docs/PARTIES.md §25); si no
  guarda ninguna, la principal del país de su identidad fiscal en Geography; si no, UTC. Una
  empresa de Canarias que guarda `Atlantic/Canary` emite el 31 de diciembre a las 23:30, hora
  canaria, con fecha del 31: ya no sale con fecha del año siguiente ni pide la serie de ese año.
  Una que no la guarda sigue tomando la de Madrid. Los demás «hoy» de Go (pedidos, presupuestos,
  cobros, contabilidad…) toman también el de la empresa, con el mismo objeto del anfitrión
  (docs/ARQUITECTURA.md, «Hoy es el de la empresa»); la vista previa de impuestos sin fecha usa la
  misma que la emisión. La zona del perfil se lee en cada emisión (una fila por su clave): quien la guarda a
  las 23:28 del 31 de diciembre y factura a las 23:30 ya saca fecha del 31. Sólo la zona de cada
  país se recuerda 5 minutos, como el lugar de una empresa en el calendario de cobros: un cambio
  en Geography llega en ese tiempo, sin reiniciar.
- **Bases existentes:** si una ya tuviera números repetidos en una serie, la migración 6 fallaría
  y habría que corregirlos antes. Karpo Go no tiene bases vivas.

## Factura del plan Sage (T1; desde el 2026-11-03)

Tareas de Go del tramo T1 del plan de Karpo C# (D-127). Karpo C# llega primero a la factura real
(D-131); Go sigue las mismas reglas.

- **G1, descripción de la operación obligatoria al emitir:** la exige el SII
  (`DescripcionOperacion`, REQ-FAC-029). Un borrador se prepara sin ella, pero no se emite: 422
  `billing.description_required`. El borrador de un albarán ya la trae («Albarán N (pedido M)»),
  y el de una rectificativa sin descripción toma «Rectificación de la factura N». Ojo: `PUT
  …/details` sustituye la cabecera entera, así que cambiar sólo el vencimiento sin reenviar la
  descripción la borra, y la emisión dará 422 hasta que se vuelva a poner.
- **G2, domicilio fiscal de las dos partes:** se congela al emitir, como el NIF y el nombre
  (migración 8: `seller_street`, `seller_postal_code`, `seller_locality`, `seller_region`,
  `seller_address_country` y los mismos de `customer_`). Lo da el contrato `TaxIdentities` de
  Parties en `address`: el contacto postal de todos con uso de facturación, si no el de por
  defecto, si no cualquiera; los de una empresa no cuentan (D-122). La factura y
  `billing.invoice-issued.v1` lo llevan en `sellerAddress` y `customerAddress`, que faltan si la
  party no tiene ninguno. La provincia que el mismo contrato da a Fiscal se sigue eligiendo
  aparte (el primer contacto español con código postal de cinco cifras), así que, si el de
  facturación es extranjero, puede salir de otro contacto.
- **Domicilio fiscal obligatorio para emitir (D-147, desde el 2026-11-07):** lo pide el art. 6 del
  RD 1619/2012 en la factura completa. La regla es del dominio (`RequireAddresses`, que usa
  `Invoice.Issue` junto a la de la descripción) y se comprueba al emitir, que es cuando se congela;
  la aplicación la comprueba también antes de tomar número, para bloquear la serie menos tiempo. Un
  domicilio vale si tiene al menos calle y país (sin contar espacios); si no, 422
  `billing.seller_address_required` (emisor) o `billing.customer_address_required` (cliente). Se
  arregla dando a la party un contacto postal y volviendo a emitir; el rechazo no consume número.
- **La rectificativa** no lo exige, pero lo lleva. Al emitirla, el **nombre y el NIF** salen de
  Parties ese día, como en la ordinaria; el **domicilio**, de cada parte, el congelado en la factura
  que rectifica, no el que Parties tenga hoy. Si la original no lo tenía (emitida antes de D-147),
  el de Parties hoy, porque el art. 15 pide que la rectificativa cumpla el art. 6; y si tampoco hay,
  se emite igualmente: una factura antigua se tiene que poder corregir.
- **Pendiente:**
  - exigir también la localidad (hoy sólo calle y país);
  - `fiscalAddress` (Parties) elige el contacto vigente hoy, no el vigente en la fecha de emisión:
    una factura emitida con fecha pasada congela el domicilio de hoy.

## Los ajustes de la empresa (D-128 a D-130, D-144; desde el 2026-11-06)

La empresa vendedora guarda sus ajustes en su perfil de Parties (docs/PARTIES.md §25). Billing no
importa Parties: los pide por su puerto `Settings` (`billing.WithSettings`), que en el anfitrión es
`host.BillingSettings`, sobre `contracts.CompanyProfiles`. Sin ajuste, lo de siempre.

- **Redondeo de la línea** (`line_rounding`): `once`, una sola vez al final (cantidad × precio −
  descuento, al céntimo); `gross_then_discount`, el bruto (cantidad × precio) al céntimo y después
  el neto con el descuento, también al céntimo. Los dos redondean los medios hacia fuera de cero.
  Ejemplo donde dan distinto: 1 × 0,125 con un 50 % da 0,06 una sola vez (0,0625) y 0,07 con el
  bruto antes (0,13 y 0,065).
- **Decimales del precio** (`price_decimals`, 0 a 6; sin ajuste, 4): un precio con más decimales
  es un error del campo `unitPrice` (400), como antes con 4. El precio de la línea pasa de (18,4) a
  (20,6) en la migración 9: caben 6 decimales y siguen cabiendo las mismas 14 cifras enteras. El
  borrador de un albarán de Orders lleva al menos 4 decimales del precio, los del pedido, aunque la
  empresa pida menos: redondear el precio perdería dinero (1,2345 × 1000 daría 1230,00 y no
  1234,50) y rechazarlo repetiría el evento sin fin.
- **Dónde se aplica y dónde se congela:** la forma de redondear y los decimales se toman de la
  empresa **al preparar el borrador** (a mano o desde un albarán) y se quedan en la
  factura: `bil_invoices.line_rounding` y `price_decimals` (migración 9; las facturas que ya había
  se marcan `once` y 4, que es como se calcularon). Todas las líneas de una factura se calculan
  igual, lo que se ve en el borrador es lo que se emite, y una factura emitida no cambia aunque la
  empresa cambie después sus ajustes (el matiz de D-144). Por qué al preparar y no al emitir:
  recalcular al emitir cambiaría importes que alguien ya revisó, y con menos decimales obligaría a
  redondear precios escritos a mano o a rechazar la emisión. Consecuencia: un borrador preparado
  antes del cambio se emite con el cálculo de antes; para el nuevo, se descarta y se prepara otro.
  La factura los devuelve en `lineRounding` y `priceDecimals`. **Una rectificativa toma el cálculo
  de la original**, no el de hoy: así anula exactamente sus importes y admite sus precios aunque la
  empresa haya pasado a menos decimales.
- **Patrón del código de serie** (`invoice_series`): si la empresa lo tiene, el código de una serie
  nueva, **tal como se escribe** (sin los espacios de los extremos), debe cumplirlo entero; si no,
  422 `billing.series_code_pattern`. Después se guarda en mayúsculas, como siempre: con
  `F[A-Z]/[0-9]{2}`, `FA/26` vale y `fa/26` no; con `f[a-z]`, `fx` vale y se guarda `FX`. Así una
  empresa puede admitir la barra (`FA/26`, y el número `FA/26-2026-000001`). Sin patrón, la regla
  fija de siempre: sin barras. Siempre, con patrón o sin él, de 1 a 20 caracteres (los de la
  columna) y sin espacios ni caracteres de control, aunque el patrón los admita. Las series que ya
  existen no se revisan al cambiar el patrón.
- **Qué patrones:** son expresiones regulares RE2 (las de Go): no hay *lookaround* (`(?=…)`,
  `(?<!…)`) ni retroreferencias (`\1`). Para que el mismo patrón valga en Karpo C# y en Trench, hay
  que usar el subconjunto común a .NET: clases, alternativas, grupos y repeticiones, como
  `F[A-Z]{1,3}/[0-9]{2}`.
- **Vencimientos** (`installments_timing`): con `with_issue`, la emisión entrega
  `billing.invoice-issued.v1` a Receivables dentro de su propia transacción
  (`billing.WithInstallmentsInTransaction`, en el anfitrión el consumidor de Receivables y su
  backend). Si ese backend no es el de Billing, `Compose` lo descarta, lo deja en el log y los
  vencimientos nacen después. Ver docs/COBROS.md.
- **Prueba:** `TestBilling_CompanySettings` (memoria y SQLite; también la rectificativa después de
  cambiar el ajuste), `TestBilling_DeliveryKeepsThePriceOfTheOrder`, `TestInvoice_Calculation`,
  `TestSeries_CodePattern` y la migración 9 con datos en SQLite (`TestCalculationMigrationInSQLite`);
  en los cuatro motores, `TestBillingContext` (precio de 6 decimales, serie con barra y factura
  emitida que no cambia) y `TestBillingCalculationMigration` (la migración 9 conserva los precios,
  también los de 14 cifras enteras); de punta a punta con el perfil de Parties,
  `TestBillingSettings_FromThePartiesProfile` en `host`.
- **El cliente gana a la empresa** (cascada cliente > empresa de D-144, D-150): las condiciones del
  cliente en Pedidos (`PUT /api/orders/terms`) pueden llevar `lineRounding` y `priceDecimals`; el
  que lleven gana al de la empresa, campo a campo, y el que no, sigue a la empresa. Facturación los
  lee por el contrato `CustomerTerms` de Pedidos (el mismo puerto de la tarifa, D-148) al preparar
  el borrador: la factura directa, el de un albarán (que sigue con al menos 4 decimales) y el de un
  cierre de periodo; el pendiente de un cliente mensual se enseña con el mismo cálculo. Como los de
  la empresa, se congelan en el borrador. Sin ese puerto (Facturación sola), manda la empresa.
  Los borradores de un cierre se preparan todos con el mismo cálculo, tomado una vez. Dos
  borradores de albaranes preparados antes y después de cambiar las condiciones del cliente
  tienen cálculos distintos y no se pueden unir (422 `billing.merge_calculation`).
  Prueba: `TestBilling_CustomerCalculationOverTheCompany`; en Pedidos, el guardado y la lectura en
  memoria y SQLite (`TestOrders_DeliveryNotesAreGrouped_MemoryThenSQLite`) y la validación
  (`TestTermsAndCounter`).

## PDF de la factura (plan Sage, T1-G6; añadido el 2026-11-07)

`GET /api/billing/invoices/{id}/pdf` devuelve `application/pdf` (en línea, con el número como nombre
de fichero) de una factura **emitida**. Permiso `Billing.Invoice.Read` y el ámbito de siempre: fuera
del vendedor, 404. Un borrador no tiene PDF: 422 `billing.invoice_not_issued`.

- **Qué lleva** (art. 6 del RD 1619/2012, con lo que la factura congeló al emitir): número con su
  serie; fecha de expedición; fecha de la operación si es distinta; vencimiento si lo hay; nombre,
  NIF y domicilio fiscal del emisor y del destinatario (T1-G2; el domicilio sale si la factura lo
  congeló, y desde D-147 una ordinaria siempre lo lleva); descripción de la operación (T1-G1);
  las líneas (concepto, cantidad, precio, descuento e importe); el desglose por tipo (base, tipo y cuota, y el recargo si lo hay); base, cuota, recargo
  y total; la mención de cada exención o no sujeción con su clave (E1–E6, N1–N2) y el artículo de
  la Ley 37/1992 que corresponde a la clave; en la rectificativa, el número y la fecha de la
  factura rectificada y la causa R1–R5. Importes al estilo español (1.234,56 €), precios con sus
  decimales y al menos dos.
- **Cómo:** el maquetado está en la aplicación de Billing (`application/invoice_pdf.go`), con los
  textos en constantes en un solo sitio. El PDF lo escribe `pkg/render/pdf`, un generador mínimo con
  la biblioteca estándar: A4, Helvetica y Helvetica-Bold con WinAnsiEncoding, texto, líneas y
  rectángulos, flujos sin comprimir. Si las líneas no caben, sigue en otras páginas con la cabecera
  de las líneas, o la del desglose, repetida y «Página n de m». La respuesta lleva
  `Cache-Control: no-store` y `X-Content-Type-Options: nosniff`, y el nombre en `filename*` si el
  número no es ASCII. El dominio no sabe nada de PDF. Sin migraciones.
- **Prueba:** `TestBilling_InvoicePDF` (número, fechas, NIF y domicilio de las dos partes, total,
  mención de la exención, rectificativa; borrador 422, sin permiso 403, fuera del ámbito 404),
  `TestRenderInvoice_SeveralPages`, `TestSpanishNumbers` y las del generador (`pkg/render/pdf`:
  cabecera, xref leída objeto a objeto, longitudes de los flujos, `%%EOF`, WinAnsi).
- **Pendiente:**
  - el **QR de VERI*FACTU** (T9): hay un hueco de unos 30 × 30 mm arriba a la derecha, vacío;
  - la **leyenda registral** del emisor (Registro Mercantil, tomo, folio, hoja): Parties no la guarda;
  - el **logotipo** de la empresa: no hay dónde guardarlo;
  - **idiomas:** sólo español; los textos están en constantes para traducirlos;
  - la **descripción del tratamiento** de Fiscal no se congela en la factura: la mención usa la
    clave y el artículo de la Ley del IVA que le corresponde; en IGIC sólo sale la clave, porque las
    referencias de la Ley 37/1992 no valen allí;
  - el **país** del domicilio sale con su código (ES), no con su nombre.
  - la mención de una exención o no sujeción con una clave que no es del SII (E1–E6, N1–N2) no cita
    el precepto: sólo la clave, hasta que la factura congele la descripción del tratamiento;
  - el PDF no lleva `/ID` en el trailer, que hará falta para PDF/A;
  - las letras latinas que WinAnsi no tiene salen con su letra base (Łódź sale «Lódz»); otros
    alfabetos salen como «?», hasta que haya fuentes incrustadas.

## Casos compartidos con Karpo C# (plan Sage, T1-G7; añadido el 2026-11-07)

Karpo C# y Karpo Go no comparten código, sino especificación y casos (D-1). Los casos del cálculo
de la factura están en [`contracts/billing/calculation-cases.json`](../contracts/billing/calculation-cases.json),
en la raíz del repositorio y no en un `testdata` de Billing, porque **son contrato, no una prueba
interna**: Karpo C# copia el fichero tal cual a su repositorio y lo pasa por su propio cálculo.

- **Qué es:** un JSON neutral respecto al lenguaje, con las claves en inglés y en camelCase, como
  el contrato HTTP (`unitPrice`, `lineRounding`, `priceDecimals`); los textos (reglas, descripciones,
  pendientes) van en español. Lleva la versión del formato (`format`), las reglas (`rules`: cómo se
  calcula el neto con cada `lineRounding`, la clave del desglose, los decimales del precio contados
  por valor, `surchargeRate` a cero sin recargo, que `date` es a la vez la vigencia del tipo y la
  base de los plazos, y que `rejection.field` es el campo del contrato HTTP), el catálogo de Fiscal
  que usan los casos (`catalog`: G21, R10 y SR4 con su recargo; E1, N1 y N2), la lista `pending` de
  lo que aún no cubre y, por caso, un `id` estable, una `description`, la entrada (`input`: ajustes
  de D-144, líneas con `quantity`, `unitPrice`, `discount` y `taxCode` o `treatment`, recargo de
  equivalencia, plazos) y lo esperado (`expected`: neto por línea, desglose por grupo con base,
  tipo, cuota y recargo, totales y plazos, o el campo que se rechaza). Todos los importes son
  cadenas decimales; los códigos son los del catálogo de Fiscal de Go, y C# los traduce si los
  suyos difieren.
- **Qué cubre:** una línea al 21 %; varios tipos con la cuota por grupo, no por línea; el medio
  céntimo alejándose de cero en neto y cuota, también en una rectificativa; `once` frente a
  `gross_then_discount` con un céntimo de diferencia; precios de 6 y de 2 decimales, el rechazo de
  uno con más y la aceptación de "0.120" con 2; líneas exentas y no sujetas junto a sujetas; el
  recargo de equivalencia; y plazos truncados al céntimo con el resto en el último (D-145: 100.01
  en tres da 33.33 + 33.33 + 33.35); y el descuento de la línea encadenado con el del cliente
  (`descuento-encadenado`, D-149: 1000 × 1 con un 5 % y un 2,5 % dan 926,25). Con `lineRounding` =
  `once`, la prueba comprueba además que el importe de una línea de pedido de Orders da el mismo
  neto. Lo que falta (retención, más de dos descuentos, cascada cliente > empresa, plazos de una
  rectificativa…), en `pending`.
- **`customerDiscount`** (D-149): campo opcional de una línea de entrada, el descuento del cliente.
  Se encadena con `discount` y se redondea a 4 decimales; sin él, `discount` vale tal cual. Se
  añadió sin cambiar `format` (sigue en 1) porque es opcional y no cambia ningún caso anterior;
  Karpo C# tiene que admitirlo al leer el fichero.
- **La prueba:** `contracts/billing/calculation_cases_test.go` pasa cada caso por el dominio de
  Billing, el `TaxEngine` de Fiscal con la jurisdicción española (a través del adaptador
  `FiscalTaxes`) y las condiciones de pago de Receivables. Lee el fichero sin admitir campos
  desconocidos (un campo nuevo es un cambio de `format`), y un importe esperado que falte hace
  fallar la prueba en vez de contar como cero.
- **Regla:** el fichero es de las dos sesiones. **Quien cambia, añade o quita un caso avisa a la
  otra sesión** (Karpo C# o Karpo Go) con el `id` y el porqué; un caso no se ajusta al código: si
  el código no da lo acordado, se anota como divergencia y se decide.
- **Cómo se corren:** `go test -count=1 -run TestSharedCalculationCases -v ./contracts/billing/`
  (un subtest por `id`).

## Precio automático (D-148; añadido el 2026-11-08)

Una línea de una factura directa (sin pedido) puede llevar un producto de Products, y entonces toma
sola el precio, como el pedido de Orders. Es un ajuste de la empresa vendedora: `automatic_pricing`
en su perfil de Parties (docs/PARTIES.md §25), por omisión sí.

- **API:** `POST /api/billing/invoices/{id}/lines` admite `product` (el id de un producto de
  Products). La línea devuelve `product` y `priceSource`, de dónde salió su precio: `special`,
  `customer_list`, `default_list` o `base` (los niveles de la cadena de docs/PRODUCTOS.md) o
  `manual` (escrito). Una línea sin producto no trae ninguno de los dos y **funciona como siempre**.
- **Con precio automático**, Billing pide a Products `Pricing.Quote` con la cadena completa: la
  empresa vendedora, el cliente, el producto, la cantidad, la fecha de la factura (la de la
  operación o, si el borrador no la tiene, hoy en la zona de la empresa) y la tarifa del cliente,
  que está en sus condiciones (`Terms`) de Orders. El precio se redondea a los decimales de la
  factura (`priceDecimals`, D-144: 0.125 con 2 decimales da 0.13) y el neto se calcula con su
  `lineRounding`, como el de un precio escrito. Una tarifa del cliente que Products no encuentra o
  que es de otra empresa se salta: la general y, si no, el precio base.
- **Se fija al añadir la línea:** el precio, el descuento y la comprobación de producto retirado
  usan la fecha de la operación del borrador en ese momento o, si no tiene, la de hoy. No se
  recalculan si después cambia la fecha del borrador ni al emitir: para otro precio, se quita la
  línea y se vuelve a añadir.
- **El descuento del cliente** (`Terms.Discount`) se encadena con el del nivel que gana, igual que
  en el pedido: 5 % y 10 % dan 14,5 % (0,95 × 0,90), no 15 %. La regla está en un solo sitio,
  `vocab.ChainedDiscount`, que usan Orders y Billing. La línea guarda un solo descuento, el
  encadenado, con los 4 decimales de su columna: dos descuentos de 2 decimales pueden dar hasta 6
  (12,5 y 7,25 dan 18,84375) y se redondea a 18,8438.
- **El neto coincide con el del pedido** para lo mismo (D-149): Orders calcula el importe de la
  línea como Billing con `once`, con el descuento encadenado a 4 decimales sobre la línea entera.
  1000 unidades a 1 € con un 5 % y un 2,5 % dan 926,25 en el pedido, en el albarán y en la
  factura. Sólo una empresa con `gross_then_discount` puede ver un céntimo de diferencia entre el
  albarán y su factura, porque la factura se calcula con su ajuste.
- **Lo que no se escribe sale del producto:** la descripción, su nombre; el código de impuesto, el
  suyo si la línea no trae ni código ni tratamiento.
- **Lo escrito manda** (sobrescritura explícita): si la línea trae `unitPrice`, el precio y el
  descuento son los escritos (sin descuento, si no se escribe ninguno; ni el del nivel ni el del
  cliente) y `priceSource` es `manual`; si sólo trae `discount`, el precio es el automático y el
  descuento escrito sustituye al encadenado. Por qué: quien escribe un precio pactado no espera que
  se le sume un descuento que no ve, y un valor vacío es fácil de distinguir de un cero.
- **Con `automatic_pricing` a false**, el precio se escribe y es obligatorio (sin él, 400 en
  `unitPrice`; sin producto, como siempre, un precio vacío es cero). El producto sólo da la
  descripción y el código de impuesto, y `priceSource` es `manual`. No se pide ningún precio.
- **Rectificativa:** el precio de una línea con producto se escribe siempre, como con
  `automatic_pricing` a false: corrige lo que se facturó, no lo que costaría hoy. Tampoco se mira si
  el producto sigue a la venta: se puede corregir la venta de un producto ya retirado.
- **Rechazos (422):** `billing.unknown_product`, el producto no existe o es de otra empresa;
  `billing.product_discontinued`, retirado en la fecha de la factura; `billing.product_not_for_sale`,
  no está a la venta o tiene las ventas bloqueadas; `billing.automatic_pricing_unavailable`, Billing
  no tiene los puertos (abajo). Un `product` que no es un id, 400.
- **Puertos** (`billing.WithPricing`), del dominio de Billing, cableados en el anfitrión como los de
  D-144:
  - `domain.Catalog` (`Product`, `Price`), en el anfitrión `infrastructure.ProductsCatalog` sobre
    los contratos `Catalog` y `Pricing` de Products;
  - `domain.CustomerTerms`, en el anfitrión `infrastructure.OrdersTerms` sobre el contrato nuevo
    de Orders `contracts.CustomerTerms` (`Terms(empresa, cliente)` → `TermsRef{PriceList,
    Discount}`), de sólo lectura, que Orders expone en `Module.Terms`. Billing no lee tablas de
    Orders;
  - el ajuste, por el puerto `Settings` de D-144: `CompanySettings.ManualPricing`, que
    `host.BillingSettings` copia de `ManualPricing` del contrato de Parties (el valor cero, también
    sin perfil, es el automático). Sin puerto `Settings`, el valor por omisión: automático.

  Sin `Catalog`, una línea con producto se rechaza siempre; sin `CustomerTerms`, la que pide el
  precio automático. En los dos casos, 422 `billing.automatic_pricing_unavailable`.
- **Migración 12** de Billing, en los cinco motores: `bil_invoice_lines.product` `{uuid}` y
  `price_source` `{str:20}`, nulos. Sin clave ajena: el producto es de otro contexto. Las líneas que
  ya hay no tienen producto.
- **El borrador de un albarán** trae el precio y el descuento encadenado del pedido (D-149) y sus
  líneas no guardan el producto.
- **Prueba:** `TestBilling_AutomaticPricing` (Products y Orders de mentira: un caso por nivel con la
  tarifa del cliente y la fecha de la operación en la petición, el descuento encadenado, los
  decimales de la factura, el precio y el descuento escritos, `automatic_pricing` a false con y sin
  precio, el producto ajeno, desconocido, retirado o bloqueado por HTTP, la línea sin producto y
  sin puertos); `TestChainedDiscount` (`pkg/domain/vocab`); de punta a punta, en memoria y en
  SQLite, `TestBilling_AutomaticPricingFromProductsAndOrders` en `host` (tarifa general en el
  perfil, un cliente con precio especial y 10 % de descuento y otro sin nada, dos facturas de dos
  líneas emitidas con sus totales); en los cuatro motores, `TestBillingContext` (la línea con
  producto vuelve con su producto y su nivel de las columnas nuevas).
- **Pendiente:** el descuento por familia y otras monedas (D-148), que entran por la cadena de
  Products sin tocar Billing; que el borrador de un albarán guarde también el producto.

## Importe como el del pedido y el albarán (D-149; añadido el 2026-11-08)

Acuerdo técnico de Karpo Go y Karpo C#: el importe de la línea de pedido y de albarán se calcula
como el de la factura. Lo que cambia en Facturación:

- **`orders.delivery-issued.v1`** trae en cada línea `unitPrice` (el precio del pedido, 4
  decimales) y `discount` (el descuento encadenado de la tarifa y del cliente, 4 decimales), además
  de `netPrice`, que se queda informativo. El borrador y el pendiente mensual toman el precio y el
  descuento: la factura sale igual que el albarán al céntimo (con `once`). Un evento de antes, sin
  `unitPrice`, se factura como siempre: `netPrice` sin descuento.
- **Compatibilidad:** los campos son nuevos y el viejo se conserva. Un consumidor antiguo los
  ignora y sigue facturando a `netPrice` (con la diferencia de céntimos de antes); uno nuevo lee un
  evento antiguo sin ellos.
- **Migración 13:** `bil_pending_delivery_lines.discount` `{dec:7,4}`, nula (sin descuento), como la
  de `bil_invoice_lines`. Los pendientes que ya hay guardan el precio neto y siguen sin descuento,
  que es como llegaron.
- **La API** escribe el descuento de una línea con al menos 2 decimales y todos los que tenga, hasta
  4 (`7.375`, no `7.38`): con 2 no se podría rehacer el neto. La regla es `vocab.StringMinPlaces`,
  la misma del PDF y del albarán de Pedidos.
- **El neto de la lista de pendientes** se calcula con el cálculo de la empresa vendedora
  (`lineRounding`), como el borrador que prepararía el cierre.
- **Pruebas:** `TestBilling_DeliveryWithChainedDiscount` (SQLite: borrador, evento antiguo,
  pendiente mensual y cierre); de punta a punta, `TestOrders_ChainedDiscountLikeTheInvoice_MemoryThenSQLite`
  en `contexts/orders` (pedido, albarán y factura emitida, 926,25 y 1120,76 con IVA); el caso
  `descuento-encadenado`; en los motores, el final de `TestOrdersInvoicing`.

## Pendiente

- Todo el cálculo de impuestos que falta, por país y por sector: punto 6 de
  [BACKLOG.md](../BACKLOG.md).
- ~~Facturación desde Pedidos~~: hecha desde el albarán, ver la sección anterior; varios albaranes
  en una factura, hecho (D-146). Queda la rectificativa que reabra un albarán.
- Del D-146: programar el cierre del periodo (hoy se lanza por su ruta) y que Documentos enlace la
  factura con todos sus albaranes (hoy, con el primero: lee `sourceId`, no `sources`).
- ~~Plazos de vencimiento y cobros~~: hechos en [COBROS.md](COBROS.md). Las remesas quedan para Tesorería.
- Facturas recibidas (compras), con el mismo `TaxEngine` y la deducibilidad.
- ~~Dirección fiscal del cliente en la factura~~: hecha (T1-G2), y obligatoria para emitir (D-147).
- PDF de la factura: hecho (T1-G6); falta el QR de VERI*FACTU (T9), la leyenda registral, el logotipo y
  otros idiomas.
- Importar las facturas de C# como emitidas, con sus números de Documents.
