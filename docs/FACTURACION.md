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
  factura para ese cliente con las líneas del albarán, a su precio neto y con su código de
  impuesto. La fecha de operación es la del albarán y la descripción cita el albarán y el pedido.
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
   lo entregado.
2. **El borrador se emite a mano,** en la serie y fecha que decida quien factura. Facturar
   automáticamente al entregar queda fuera.
3. **Un borrador nacido de un albarán no se descarta;** sí se pueden editar sus líneas antes de
   emitir.
4. **Una factura por albarán.** Agrupar varios albaranes de un cliente en una factura
   (facturación periódica) queda para la fase 2.

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
  contabilidad…) siguen en UTC; la vista previa de impuestos sin fecha usa la misma que la
  emisión. La zona del perfil se lee en cada emisión (una fila por su clave): quien la guarda a
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
  facturación es extranjero, puede salir de otro contacto. **No se exige para emitir:** el reglamento pide el domicilio en la
  factura completa, pero exigirlo es una regla fiscal que queda para quien valide lo fiscal (plan,
  §7).

## Pendiente

- Todo el cálculo de impuestos que falta, por país y por sector: punto 6 de
  [BACKLOG.md](../BACKLOG.md).
- ~~Facturación desde Pedidos~~: hecha desde el albarán, ver la sección anterior. Queda agrupar
  varios albaranes en una factura y la rectificativa que reabra un albarán.
- ~~Plazos de vencimiento y cobros~~: hechos en [COBROS.md](COBROS.md). Las remesas quedan para Tesorería.
- Facturas recibidas (compras), con el mismo `TaxEngine` y la deducibilidad.
- ~~Dirección fiscal del cliente en la factura~~: hecha (T1-G2); falta decidir si se exige para emitir.
- Importar las facturas de C# como emitidas, con sus números de Documents.
