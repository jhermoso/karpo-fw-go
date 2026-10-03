# Contexto Contabilidad

Port de la contabilidad de C# al contexto `contexts/accounting`. En C# estaba repartida en dos
peldaños:

- **ErpKernel:** `AccountingTransaction` (raíz de una jerarquía TPT con una docena de subtipos),
  `TransactionDetail` (línea con importe y un carácter `DebitCreditFlag`),
  `AccountingTransactionType`, `AccountingPeriod` con `PeriodType` y `GeneralLedgerAccount`. Además,
  unas 30 clases del UDM v1 (presupuestos, amortizaciones, notas…) sin persistencia.
- **ErpDetail:** `AccountingPlan` (plan contable por organización interna, inspirado en
  PlanCuentas de Sage) y `GlAccountProfile` (perfil PGC de cada cuenta: cabecera, cuenta de cierre,
  tipo de cuenta de tercero, IVA, 347, retenciones, efectos y analítica).

Aplica la decisión 5 de [COBROS.md](COBROS.md) y de [TESORERIA.md](TESORERIA.md) (aprobadas el
2026-09-28): cada contexto publica sus hechos y Contabilidad los contabiliza.

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

- **Nada contabiliza.** Ningún contexto genera asientos: ni facturas, ni cobros, ni nóminas.
- **Los asientos no cuadran por construcción:**
  - `AccountingTransaction.Validate()` devuelve siempre éxito;
  - no se comprueba que debe = haber ni que haya al menos dos líneas;
  - el lado de cada línea es un `char` libre (`DebitCreditFlag`) y el importe puede ser negativo.
- **Sin numeración** de asientos ni ejercicio: solo `TransactionDate` y `EntryDate`.
- **Periodos sin cierre:** `AccountingPeriod` es el periodo genérico del UDM (tipo, número, parte);
  nada impide contabilizar en un periodo ya presentado.
- **Tres representaciones de la cuenta:** `GeneralLedgerAccount` (jerarquía por `Parent`),
  `AccountingPlan` y `GlAccountProfile` (TET 1:1). El código PGC vive en el perfil, no en la cuenta.
- **Perfil sobredimensionado:** `GlAccountProfile` tiene 20 campos (prorrata, 347, efectos
  en riesgo, impagados, analítica…) que nadie lee.
- **Sin plan sembrado:** el plan de cada empresa entra por la importación de Sage.
- **Sin permisos:** 28 códigos `Accounting.*` definidos y ninguno comprobado.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `AccountingPlan` + `GeneralLedgerAccount` + código en `GlAccountProfile` | 4 | 2 | 2 | 3 | **59** | Modificar → agregado `Account` por empresa: código de 1 a 12 dígitos (el primero de 1 a 9) **único en el plan**, nombre, **cuenta de detalle o de cabecera**, activa o dada de baja. La jerarquía sale del prefijo del código y la naturaleza (balance 1–5, resultados 6–7, patrimonio 8–9) del primer dígito |
| 2 | `GlAccountProfile` (20 campos) | 2 | 3 | 2 | 3 | **47** | Modificar → solo `Postable`. El resto (cuenta de cierre, 347, prorrata, efectos, analítica) espera a quien lo use; el tipo de tercero viaja en la línea (`Party`) |
| 3 | `AccountingTransaction` + `TransactionDetail` sin validar | 5 | 1 | 2 | 3 | **66** | Modificar → agregado `Entry`: fecha, descripción, **origen** (tipo, id y clave), **2 o más líneas, cada una solo al debe o solo al haber, en céntimos, y Σ debe = Σ haber**. Número correlativo **por empresa y ejercicio** tomado en la misma unidad de trabajo. Los importes negativos de un borrador pasan al otro lado y las líneas a cero se quitan |
| 4 | Subtipos TPT (`SalesAcctgTrans`, `ReceiptAccountingTransaction`, `PaymentAccountingTransaction`…) | 2 | 2 | 1 | 2 | **37** | Retirar → el **origen** del asiento (tipo de evento + id) dice de dónde viene sin una tabla por subtipo |
| 5 | `AccountingPeriod` + `PeriodType` | 3 | 2 | 2 | 3 | **51** | Modificar → el agregado `Ledger` (uno por empresa) fija el **mes de inicio del ejercicio**, calcula el ejercicio y periodo de cada fecha y **cierra y reabre periodos**. Nada se contabiliza en un periodo cerrado |
| 6 | Anulación de asientos | — | — | — | — | — | Nuevo → **contraasiento**: un asiento con las líneas invertidas, enlazado en los dos sentidos. Un asiento se anula una vez y un contraasiento no se anula |
| 7 | Contabilización automática | — | — | — | — | — | Nuevo → **perfil de contabilización** en el `Ledger` (rol → cuenta y código de impuesto → cuenta de IVA repercutido) y suscripciones a `billing.invoice-issued.v1`, `receivables.collection-allocated.v1` / `allocation-reversed.v1`, `treasury.direct-debit-collected.v1` / `returned.v1` y `payroll.payslip-approved.v1` / `cancelled.v1`. Cada hecho se contabiliza **una sola vez** (origen único por empresa) |
| 8 | Presupuestos, amortizaciones, notas y demás andamiaje UDM sin persistencia | 1 | 2 | 2 | 2 | **33** | Retirar |
| 9 | Consultas | — | — | — | — | — | Nuevo → diario paginado, **sumas y saldos** y **mayor de una cuenta** con saldo acumulado, por rango de fechas |
| 10 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Accounting.Account.*`, `Accounting.Ledger.*` y `Accounting.Entry.Read/Create/Reverse`, con ámbito por empresa (404 fuera, 403 en solo lectura) |

## Diseño

```
contexts/accounting/
├── domain/          # Account (+Nature), Ledger (+Period, Role), Entry (+Line, Source, Draft, Post, Reversal), Counter
├── application/     # casos de uso con permisos y ámbito, contabilizador (post / reverse), suscripciones, consultas
├── infrastructure/  # esquema acc_* de 5 motores, mapeos, bandeja de entrada
└── module.go        # composición, Consumer y rutas /api/accounting/...
```

- **Qué contabiliza cada hecho** (las cuentas salen del perfil del `Ledger` de la empresa):

  | Hecho | Debe | Haber |
  |---|---|---|
  | Factura emitida | clientes (total, con el cliente) | ventas (base); IVA repercutido de cada código; recargo de equivalencia |
  | Cobro aplicado | caja (efectivo), bancos (transferencia, tarjeta, cheque) o efectos al cobro (domiciliación) | clientes (con el pagador) |
  | Aplicación anulada | contraasiento del cobro aplicado | |
  | Adeudo cobrado | bancos | efectos al cobro |
  | Adeudo devuelto | efectos al cobro | bancos |
  | Nómina aprobada | sueldos (bruto); Seguridad Social a cargo de la empresa | Seguridad Social acreedora (trabajador + empresa); retenciones IRPF; otras deducciones; remuneraciones pendientes (neto) |
  | Nómina anulada | contraasiento de la nómina | |
  | Factura recibida ([COMPRAS.md](COMPRAS.md)) | gasto de cada categoría (600, 621–629) e IVA soportado deducible (472) | proveedores (lo que se paga, con el proveedor) y retenciones |
  | Factura recibida anulada | contraasiento de la factura | |
  | Pago aplicado ([PAGOS.md](PAGOS.md)) | proveedores, remuneraciones pendientes o retenciones (con el beneficiario) | bancos, o caja si es en efectivo |
  | Aplicación de pago anulada | contraasiento del pago aplicado | |

  Así, una domiciliación cobrada y luego devuelta deja **efectos al cobro a cero** y la deuda del
  cliente otra vez abierta, igual que en Cobros.
- **Idempotencia en dos capas:** la bandeja de entrada descarta el mismo mensaje y el índice único
  `(empresa, tipo de origen, id de origen)` descarta un segundo mensaje sobre el mismo hecho. Un
  asiento manual es su propio origen.
- **Un contraasiento nunca precede a lo que anula:** si el hecho llega con fecha anterior, toma la
  del asiento original.
- **Si la empresa no tiene libro** o le falta una cuenta del perfil, el evento falla y se reintenta:
  no se pierde, y se contabiliza en cuanto se configura.
- **Tablas:**
  - `acc_accounts` (código único por empresa);
  - `acc_ledgers` (uno por empresa) + `acc_ledger_roles`, `acc_ledger_taxes` y `acc_closed_periods`;
  - `acc_entries` (número único por empresa y ejercicio; origen único por empresa) +
    `acc_entry_lines`;
  - `acc_counters`;
  - las bandejas de salida, la auditoría y `accounting_inbox`.

## Decisiones (aprobadas por Javier el 2026-09-28)

1. **Plan por empresa sin semilla PGC.** Cada empresa da de alta sus cuentas (o las importa de
   Sage). La jerarquía es la del prefijo del código y solo las cuentas de detalle admiten apuntes.
2. **El perfil de contabilización vive en el libro de cada empresa** (rol → cuenta, código de
   impuesto → cuenta de IVA). Los contextos de origen no conocen cuentas contables.
3. **Asientos siempre cuadrados, numerados por empresa y ejercicio, inmutables**: se corrigen con
   contraasiento, nunca editándolos. Periodos cerrables y reabribles por quien tenga
   `Accounting.Ledger.Update`.
4. **Fase 1 no contabiliza** las compensaciones de Cobros (no mueven dinero ni saldo de clientes
   en conjunto) ni las presentaciones de Fiscal (el modelo 111 se liquida desde retenciones al
   pagarse, que es de Pagos). **Por la nómina se usa un único neto a pagar**, sin cuenta por
   trabajador.
5. **Fase 2:** compras y proveedores (cuando exista su contexto), IVA soportado, inmovilizado y
   amortizaciones, analítica, regularización y cierre del ejercicio (asiento de pérdidas y
   ganancias y de apertura), libros oficiales y SII / Verifactu desde Contabilidad.

## Validación

- **Dominio:**
  - código de cuenta y naturaleza por el primer dígito;
  - asiento: mínimo dos líneas, solo debe o solo haber, céntimos, cuadre;
  - borrador: negativos al otro lado y líneas a cero fuera;
  - ejercicio y periodo según el mes de inicio; periodo cerrado rechazado; reapertura;
  - contraasiento: líneas invertidas, una sola vez, no se anula un contraasiento.
- **Extremo a extremo** (Parties, Fiscal, Facturación, Cobros, Tesorería y Contabilidad sobre el
  mismo backend, en memoria y en SQLite migrada, con un único broker; Nóminas se simula con sus
  mensajes):
  - plan: código duplicado 422, código con 0 inicial 400, solo lectura 403, ajeno 404, consulta
    por prefijo con la naturaleza;
  - un libro por empresa (422);
  - dos facturas al 21 %: clientes 181,50, ventas −150,00, IVA −31,50, con el cliente en la línea;
  - cobro por transferencia; domiciliación cobrada (clientes 0, bancos 181,50, efectos 0) y
    devuelta (clientes 121,00, bancos 60,50, efectos 0); mayor de clientes con saldo acumulado;
  - nómina entregada tres veces (mismo mensaje dos veces y otro mensaje del mismo hecho): un solo
    asiento, sin línea de otras deducciones a cero; anulada dos veces: un solo contraasiento;
  - asientos manuales: descuadrado 400, cuenta de cabecera o fuera del plan 422, solo lectura
    403; contraasiento enlazado, repetido 422, contraasiento de contraasiento 422;
  - periodo cerrado 422 y reabierto 201;
  - doce asientos numerados del 1 al 12 sin huecos y sumas y saldos a cero.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con los cinco contextos migrados en
  la misma base:
  - plan y libro de ida y vuelta (roles, cuentas de IVA y periodos cerrados ordenados);
  - factura de 82,64 + 21 % (99,99) y cobro en efectivo pasando por `accounting_inbox`;
  - anulación del cobro con su contraasiento;
  - nómina con reentregas; asiento manual en periodo cerrado rechazado; asiento, contraasiento y
    líneas de ida y vuelta; mayor, diario sin huecos y filtro por fechas.

## Pendiente

- Todo lo de la decisión 5 (fase 2).
- Pagos: contabilizar el pago de nóminas, de retenciones (111) y de la Seguridad Social.
- IVA de caja y recargo de equivalencia soportado.
- Multimoneda: hoy todo es EUR.
- Importar de C#: el plan de cada empresa desde `AccountingPlan` + `GlAccountProfile` (código y
  cabecera) y, si se quiere histórico, los asientos cuadrados de `AccountingTransaction`.
