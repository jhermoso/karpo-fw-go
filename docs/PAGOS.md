# Contexto Pagos

Port de los pagos de C# al contexto `contexts/payments`, más las **órdenes de transferencia
(pain.001)** en Tesorería y la **contabilización de los pagos** en Contabilidad.

En C# solo existía `ErpKernel/Subdominios/Payments`:

- `Payment` (cabecera con estado numérico);
- `PaymentApplication` (aplicación a facturas);
- el catálogo `PaymentMethodType`.

Los endpoints estaban archivados bajo `Invoicing/`. Nada de esto estaba en ErpDetail ni en los
núcleos sectoriales.

Aplica decisiones ya aprobadas:

- La 2 de [NOMINAS.md](NOMINAS.md): el neto se reparte con el puerto `Remittance` de Nóminas.
- La 5 de [COBROS.md](COBROS.md) y la 3 de [TESORERIA.md](TESORERIA.md): cada contexto publica,
  Tesorería ejecuta y nadie escribe en otro contexto.
- La 2 de [CONTABILIDAD.md](CONTABILIDAD.md): los contextos de origen no conocen cuentas contables.

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

- **Todos los `Validate()` devuelven éxito.**
  - `Payment.SetAmount` acepta cero y negativos.
  - Solo se comprueba que la suma de aplicaciones no supere el pago, y esa regla está repetida en
    `EnsureNotOverAppliedAsync` y en `EfPaymentSettlement`.
- **Sin ciclo de vida:**
  - el estado es un `int` (0 al crear, 2 al emitir, el 1 no se usa);
  - un pago emitido se puede editar o borrar, y el borrado arrastra en cascada sus aplicaciones;
  - editar el importe puede dejarlo por debajo de lo ya aplicado.
- **Una factura se puede pagar de más**, y nada la marca como pagada aunque el estado «Paid» está
  sembrado.
- **No hay obligaciones de pago:**
  - sin facturas de proveedor (solo una fila de semilla «Purchase Invoice» que ningún código mira);
  - sin vencimientos, sin pago de nóminas, sin pago de impuestos;
  - sin remesas de pago ni ficheros pain.001 o Norma 34.
- **El medio de pago está dos veces:** `PaymentMethodCode` (obsoleto) y `PaymentMethodTypeId`.
- **Sin moneda** en el pago.
- **El movimiento de dinero está modelado dos veces**, sin enlace entre ellas: `Payment` y
  `AccountTransaction` de FinancialKernel.
- **Andamiaje muerto:**
  - `IPayment` no lo implementa nadie;
  - `Disbursement`, `Receipt` y `PaymentAccountingTransaction` no tienen tablas ni servicios;
  - las referencias a `Payment` de `PayCheck` y `Deduction` en RRHH no se persisten.
- **Defectos de acceso a datos:**
  - `Take(1000)` antes de `OrderBy` en los listados;
  - tablas y semillas duplicadas en `dbo` y en `payments`;
  - `ON DELETE CASCADE` en EF pero no en el DDL.
- **Permisos:** 12 códigos `Payments.*` generados, ninguno comprobado y ninguno sembrado.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `Payment` (estado `int`, importe sin control, editable tras emitir) | 4 | 1 | 2 | 3 | **56** | Modificar → agregado `Payment`: empresa, beneficiario (parte) **o autoridad fiscal**, fecha, importe positivo en céntimos, EUR, medio, referencia. **Nunca se borra ni se edita: se anula**, y la anulación revierte sus aplicaciones |
| 2 | `PaymentApplication` (a facturas, sin tope por factura) | 4 | 2 | 3 | 3 | **63** | Modificar → aplicaciones dentro del pago, **a obligaciones** (`Payable`) del mismo beneficiario. Se aplica primero en la obligación, así que nunca se paga de más ni queda una aplicación fantasma |
| 3 | `PaymentMethodType` (4 filas) + `PaymentMethodCode` | 3 | 2 | 2 | 3 | **51** | Modificar → enumerado `cash`, `transfer`, `card`, `check`, `direct-debit` (cargo del proveedor en nuestra cuenta), como en Cobros |
| 4 | Obligaciones de pago | — | — | — | — | — | Nuevo → agregado `Payable`: **factura de proveedor** (alta manual), **neto de nómina** (de `payroll.payslip-approved.v1`, con el reparto por IBAN de Nóminas) e **impuesto** (de `fiscal.filing-submitted.v1`, con vencimiento legal: día 20 del mes siguiente al periodo, 30 de enero para el último; el 190 es informativo y no genera nada). Origen único por empresa, cuentas de abono cuya suma es el importe, vencimiento no anterior a la emisión. **Solo se retira sin pagos** (nómina anulada, modelo revertido, error de alta) |
| 5 | Transferencias, pain.001, Norma 34 | — | — | — | — | — | Nuevo en **Tesorería** → agregado `TransferOrder`: se **propone** con las obligaciones no pagadas que tienen cuenta (`contracts.Payable`), sin repetir las que ya están en órdenes vivas y contando las que no tienen cuenta. Borrador → generada → ejecutada → rechazos con motivo ISO. **Fichero pain.001.001.03** |
| 6 | Emisión con número de documento (`IDocumentIssuer`, tipo `PAYMENT`) | 2 | 3 | 2 | 2 | **46** | Retirar → el pago no necesita serie propia: su referencia es el end-to-end de la transferencia, el NRC del impuesto o el número de cheque |
| 7 | `Disbursement`, `Receipt`, `PaymentAccountingTransaction`, `IPayment`, referencias de RRHH | 1 | 1 | 2 | 2 | **30** | Retirar |
| 8 | `AccountTransaction` (FinancialKernel) como segundo modelo de movimiento | 2 | 2 | 2 | 3 | **43** | Retirar aquí → el movimiento bancario lo contabiliza Contabilidad desde los eventos; los extractos quedan para la fase 2 de Tesorería |
| 9 | Contabilización | — | — | — | — | — | Nuevo en **Contabilidad** → `payments.payment-allocated.v1`: debe en la cuenta de lo que se debía (proveedores, remuneraciones pendientes o retenciones, con el beneficiario en la línea) contra bancos o caja; `payments.allocation-reversed.v1` genera el contraasiento. Nuevo rol `suppliers` en el perfil |
| 10 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Payments.Payable.Read/Update` (**registrar lo que se debe**) y `Payments.Payment.Read/Update` (**pagar**) separados. En Tesorería, `Treasury.Transfer.Update/Generate/Settle` separados, como en las remesas |

## Diseño

```
contexts/payments/
├── domain/          # Payable (+PayTo, TaxDue), Payment (+Allocation), puerto NetPaySplits
├── contracts/       # lenguaje publicado v1 y puerto Payable (lo que se paga por transferencia)
├── application/     # casos de uso con permisos y ámbito, suscripciones a Nóminas, Fiscal y Tesorería
├── infrastructure/  # esquema pay_* de 5 motores, mapeos, bandeja de entrada, adaptador PayrollNetPay
└── module.go        # composición, Consumer y rutas /api/payments/...

contexts/treasury/   # + TransferOrder y pain.001 (migración 3), adaptador PaymentsPayables, rutas /api/treasury/transfers
contexts/accounting/ # + rol suppliers y suscripción a los pagos
```

- **El ciclo completo, sin escrituras entre contextos:**
  1. Nóminas y Fiscal publican; Pagos crea las obligaciones. Las facturas de proveedor se dan de
     alta a mano.
  2. Tesorería consulta lo que se paga por transferencia (`Payable.DueForTransfer`), propone y
     genera la orden, y el banco la ejecuta.
  3. Tesorería publica `treasury.transfer-executed.v1`, uno por transferencia. Pagos registra un
     pago por transferencia con referencia igual al end-to-end y lo aplica a su obligación. Si la
     obligación se pagó por otra vía, el pago queda sin aplicar para que lo revise una persona.
  4. Un rechazo (`treasury.transfer-rejected.v1`) anula ese pago y la obligación vuelve a estar
     pendiente.
  5. Pagos publica `payments.payment-allocated.v1` y Contabilidad lo contabiliza.
- **Una obligación con varias cuentas** (el neto de una nómina repartido, un embargo…) genera una
  transferencia por cuenta. El end-to-end es el Id de la obligación y el número de cuenta.
- **Una obligación pagada en parte** no entra en propuestas de transferencia: decide una persona.
- **Idempotencia:**
  - obligaciones únicas por origen (índice `company, source_type, source_id`);
  - pagos de transferencias únicos por referencia;
  - bandeja `payments_inbox`.
- **Tablas:**
  - `pay_payables` (+ `pay_payable_accounts`);
  - `pay_payments` (+ `pay_allocations`);
  - las bandejas de salida, la auditoría y `payments_inbox`;
  - en Tesorería, `trs_transfer_orders` (+ `trs_transfers`).

## Decisiones (aprobadas por Javier el 2026-10-03)

1. **Pagos registra obligaciones y pagos; Tesorería ejecuta las transferencias.** El pain.001
   vive en Tesorería, junto al pain.008, y Pagos no conoce ficheros bancarios.
2. **Las facturas de proveedor se dan de alta en Pagos solo como obligación de pago** (número,
   fechas, importe, cuentas), sin base, IVA soportado ni contabilización del gasto, hasta que
   exista un contexto de **Compras** que las reciba y publique. Hasta entonces el asiento de la
   factura de proveedor se hace a mano en Contabilidad.
3. **Las cuentas de abono viajan en la obligación**: las de la nómina salen del reparto de
   Nóminas y las del proveedor se indican al darla de alta. Las cuentas bancarias de terceros de
   Parties quedan para cuando se porten.
4. **El impuesto se debe a la autoridad fiscal, no a una parte**, con el vencimiento legal del
   periodo. Se paga a mano (NRC o domiciliación del modelo), no por transferencia SEPA.
5. **Un pago nunca se borra ni se edita: se anula.** Una obligación con pagos no se retira:
   primero se anulan sus pagos. Si llega la anulación de una nómina ya pagada, la obligación se
   deja como está para que una persona gestione el reintegro.

## Validación

- **Dominio:**
  - obligación: beneficiario o autoridad según el tipo, importe en céntimos, cuentas que suman el
    importe, vencimiento, EUR;
  - pagar de más, en parte y del todo; revertir; retirar con y sin pagos; cuentas de una
    obligación cerrada;
  - pago: beneficiario xor autoridad, aplicar de más, desaplicar, anular una sola vez;
  - vencimiento de los modelos (mensuales, trimestrales, cierre del año, el anual sin pago);
  - orden de transferencia: importes, duplicados, end-to-end de 34 caracteres, congelada al
    generar, fecha de ejecución, rechazos y anulación;
  - **pain.001 analizado** (espacio de nombres, número y suma de control, `TRF`, fecha, BIC del
    ordenante, nombres transliterados, importes).
- **Extremo a extremo** (Parties, Pagos, Tesorería y Contabilidad sobre el mismo backend, en
  memoria y en SQLite migrada; Nóminas y Fiscal se simulan con sus mensajes):
  - alta de factura de proveedor: pagar no es registrar (403), ajeno 404, duplicada 422, IBAN
    erróneo 400;
  - nómina entregada tres veces: una obligación con el reparto en dos cuentas; otra nómina
    anulada antes de pagarse queda retirada;
  - modelo 111 del 3T con vencimiento el 20 de octubre; el 190 no genera nada;
  - propuesta con 3 transferencias (2.074,70) y 2 obligaciones sin cuenta; una segunda propuesta
    no repite nada;
  - pain.001 por HTTP con los nombres transliterados;
  - ejecución: factura y nómina saldadas, proveedores y remuneraciones al debe contra bancos;
  - rechazo de una cuenta de la nómina: la obligación vuelve a deber 469,70 y el asiento se anula;
  - impuesto pagado a la AEAT; pago en efectivo: registrar no es pagar (403), pagar de más 422,
    otro beneficiario 422, retirar una obligación con pagos 422;
  - anulación del pago en efectivo con su contraasiento, y retirada de la obligación ya sin pagos.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con Parties, Pagos, Tesorería y
  Contabilidad migrados en la misma base:
  - obligaciones de ida y vuelta, con dos cuentas de abono; modelo 111 recibido dos veces por la
    bandeja;
  - orden propuesta, generada y fichero desde la orden guardada;
  - ejecución y rechazo pasando por `payments_inbox` y `accounting_inbox`;
  - pagos de ida y vuelta, impuesto y anulación con sus saldos contables.

## Pendiente

- Contexto de **Compras**: recepción de facturas de proveedor con base, IVA soportado y gasto;
  sustituirá el alta manual.
- Pago de la **Seguridad Social** (RLC/RNT) cuando Nóminas publique sus liquidaciones.
- Cuentas bancarias de terceros en Parties como fuente de las cuentas de abono.
- Agrupar en un solo pago varias obligaciones del mismo beneficiario en una transferencia.
- Ficheros de rechazos (pain.002 / camt.054) en lugar del alta manual; Norma 34 si algún banco
  lo exige.
- Pagarés y confirming.
- Importar de C#: los `Payment` y `PaymentApplication` existentes, que hoy no tienen datos
  (0 filas en las semillas).
