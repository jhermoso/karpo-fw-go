# Contexto Cobros

Port de lo que en C# tocaba los cobros al contexto `contexts/receivables`. En C# estaba repartido
en cuatro sitios:

- **Payments** de ErpKernel: `Payment`, `PaymentApplication` y `PaymentMethodType`.
- **Parties** de ErpDetail: `PaymentCondition` y el riesgo del perfil comercial (`MaxRisk`,
  `BlockOrders`, `DefaultPaymentCondition`).
- **FinancialKernel:** cuentas bancarias y extractos.
- **Accounting:** solo metadatos de cuentas.

El contexto consume `billing.invoice-issued.v1` de Facturación (decisiones aprobadas en
[FACTURACION.md](FACTURACION.md)).

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

- **No existe una cartera de cobros:** no hay vencimientos, ni plazos, ni saldo pendiente, ni
  impagados.
  - `PaymentCondition` define plazos, días fijos, meses sin pago y festivos, pero **nadie la
    convierte en fechas**; ni pedidos, ni facturas, ni cobros la leen.
  - El estado «Paid» de la factura está sembrado y **nada lo asigna**.
- **La aplicación de cobros solo limita por el lado del cobro:**
  - una factura se puede cobrar de más con varios cobros;
  - no se comprueba que el pagador sea el cliente de la factura;
  - un cobro puede reducirse por debajo de lo aplicado, y se puede modificar o borrar después de
    emitido.
- **Riesgo:**
  - `ExistingAccumulatedRisk` nunca se rellena en producción;
  - `MaxRisk = 0` significa «sin límite» en `PricingEngine` y «límite 0» en
    `SalesOrderWorkflow`;
  - el riesgo se compara pedido a pedido, nunca contra la deuda abierta.
- **SEPA:** no hay mandatos, remesas ni ficheros pain o Norma 19. Además, la importación
  sobrescribe `MandateReference` con `PC=…;COM=…`.
- **Cuentas bancarias:** hay tres validadores de IBAN y solo uno comprueba el MOD-97.
  `OwnBankAccount` duplica `BankAccount` y `PartyBankAccount`.
- **Sin conciliación ni asientos de cobro.**
- **Sin permisos** en ningún endpoint.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `PaymentCondition` (sin calculadora ni consumidores; plazos > 1 con 0 días entre ellos) | 3 | 2 | 3 | 3 | **54** | Modificar → agregado `Terms` del vendedor, con **`Schedule` real**: días hasta el primer plazo y entre plazos, mes comercial, hasta 3 días fijos (ajustados a fin de mes), meses sin pago (también de diciembre a enero), festivos hacia atrás hasta `BackwardDays` o hacia delante, y reparto al céntimo con el resto en el último plazo. Varios plazos exigen días entre ellos |
| 2 | Vencimientos, cartera, estado «Paid» | — | — | — | — | — | Sustituido → agregado `Receivable` (su Id es el de la factura), **abierto al consumir `billing.invoice-issued.v1`** (inbox), con sus plazos. El calendario sale del vencimiento explícito de la factura; si no hay, de las condiciones del perfil de crédito; si tampoco, un plazo en la fecha de emisión. Una rectificativa (crédito) vence en el acto. `ReceivableSettled` sustituye a «Paid» |
| 3 | `Payment` + `PaymentApplication` (tope solo del cobro; mutable tras emitir) | 4 | 2 | 3 | 3 | **59** | Modificar → agregado `Collection` con sus aplicaciones. **Tope en los dos lados**: el cobro no se aplica por encima de su importe, y el plazo no se cobra por encima de lo abierto. Mismo vendedor, **pagador = cliente de la factura**. Se revierte una aplicación o se anula el cobro entero; nunca se borra |
| 4 | Rectificativa sin compensación | — | — | — | — | — | Sustituido → **compensación**: un cobro de tipo `offset` con importe 0 cuyas aplicaciones (+ en la factura, − en el crédito) deben cuadrar a cero |
| 5 | `MaxRisk`, `BlockOrders` del perfil comercial (0 con dos significados; sin deuda abierta) | 3 | 1 | 2 | 3 | **46** | Modificar → agregado `CreditProfile` por vendedor y cliente: condiciones por defecto, **límite explícito o sin límite** (0 es un límite) y bloqueo. El puerto `Credit.Exposure` devuelve lo abierto, lo vencido, el límite, el disponible y el bloqueo. Sustituye a `ExistingAccumulatedRisk` |
| 6 | `PaymentMethodType` (4 filas) | 3 | 3 | 3 | 4 | **64** | Modificar → enumerado de dominio: efectivo, transferencia, tarjeta, cheque, domiciliación y compensación |
| 7 | `BankAccount`, `OwnBankAccount`, `PartyBankAccount`, `BankStatementLine`, conciliación | 3 | 2 | 2 | 3 | **51** | Fuera de Cobros → contexto Tesorería (cuentas, extractos, conciliación, remesas SEPA y mandatos) |
| 8 | `MandateReference` sobrescrito por la importación | 1 | 1 | 1 | 2 | **22** | Retirar. Los mandatos SEPA serán un agregado de Tesorería o de Cobros; no se importan los valores corruptos |
| 9 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Receivables.Terms.*`, `Receivables.Credit.*`, `Receivables.Receivable.Read` y `Receivables.Collection.*` |

## Diseño

```
contexts/receivables/
├── domain/          # Terms (+Schedule, Calendar), Receivable (+Installment), Collection (+Allocation), CreditProfile, eventos
├── contracts/       # Credit (exposición) + lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito, suscripción a Facturación, CreditPort
├── infrastructure/  # esquema rec_* de 5 motores (con inbox), mapeos
└── module.go        # composición, Consumer y rutas /api/receivables/...
```

- **Ámbito:** todo pertenece al vendedor. Fuera de su ámbito, 404 uniforme; con solo lectura, 403.
- **Aplicar un cobro** cambia dos agregados en la misma unidad de trabajo: el cobro (lo aplicado)
  y la cuenta por cobrar (lo abierto del plazo). Ambos usan concurrencia optimista y reintento.
- **Festivos:** puerto `Calendar`; por defecto no hay ninguno, porque C# no tenía calendario.
- **Lenguaje publicado:**
  - `receivables.collection-allocated.v1`, con medio, pagador, factura, plazo e importe
    (Contabilidad: 572 contra 430, o 430 contra 430 en una compensación);
  - `receivables.allocation-reversed.v1`;
  - `receivables.receivable-settled.v1`: la factura está cobrada.
- **Tablas:**
  - `rec_terms`, `rec_credit_profiles`;
  - `rec_receivables` (+ `rec_installments`), con lo abierto y el indicador de saldada para
    consultas;
  - `rec_collections` (+ `rec_allocations`);
  - las bandejas de salida, la auditoría y `receivables_inbox`.

## Decisiones (aprobadas por Javier el 2026-09-28)

1. **Las condiciones de pago y el riesgo viven en Cobros** (`Terms` y `CreditProfile`), no en el
   perfil comercial de Parties. Pedidos consultará la exposición por el puerto `Credit`.
2. **Una cuenta por cobrar por factura, con sus plazos dentro** (y no un agregado por plazo). Así
   la suma de los plazos igual al total es un invariante del agregado.
3. **Prioridad del calendario de vencimientos:** vencimiento explícito de la factura →
   condiciones del perfil de crédito → emisión. Las condiciones se fijan al emitir la factura;
   si cambian después, no afectan a lo ya emitido.
4. **Sin límite es explícito.** `MaxRisk = 0` pasa a ser un límite de 0; «sin límite» es la
   ausencia de límite.
5. **Tesorería es un contexto aparte:**
   - cuentas bancarias con IBAN MOD-97;
   - extractos y conciliación;
   - remesas SEPA y mandatos;
   - efectivo.

   Cobros publica los hechos y Tesorería los ejecuta.

## Validación

- **Dominio:**
  - calendario 30/60/90 con el resto en el último plazo;
  - mes comercial con ajuste a fin de mes;
  - días fijos con salto de mes y ajuste al 31;
  - agosto sin pagos, con y sin día fijo; periodo de diciembre a enero;
  - festivos hacia atrás o hacia delante;
  - crédito inmediato;
  - cobro por encima de lo abierto o con signo contrario rechazado; saldada; reversión y su
    límite;
  - aplicación por encima del cobro; anulación que devuelve las aplicaciones;
  - compensación que cuadra;
  - límite 0 frente a sin límite.
- **Extremo a extremo** (Parties, Fiscal, Facturación y Cobros sobre el mismo backend, en
  memoria y en SQLite migrada, por HTTP):
  - condiciones 30/60 al día 10, con vista previa;
  - Facturación emite y Cobros abre las cuentas por cobrar por inbox:
    - por las condiciones del cliente;
    - por el vencimiento explícito;
    - rectificativa como crédito;
  - exposición: 145,20 abiertos, 24,20 vencidos y 54,80 disponibles;
  - cobro de 100 aplicado a dos facturas;
  - cobro ya aplicado del todo: 422; factura saldada: 422; factura de otro cliente: 422;
  - compensación que supera lo abierto: 422; compensación válida;
  - reversión y anulación, y la exposición vuelve a su valor;
  - ajeno: 404;
  - eventos v1.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con Parties, Fiscal, Facturación y
  Cobros migrados en la misma base:
  - días fijos y diciembre sin pago guardados y recuperados;
  - cuenta por cobrar abierta por la bandeja de entrada SQL, con tres plazos;
  - aplicaciones con tope;
  - compensación;
  - exposición y vencido por fecha;
  - anulación;
  - búsqueda por el indicador de saldada.

## Pendiente

- Contexto Tesorería (decisión 5) y, con él:
  - la conciliación de extractos contra cobros;
  - las remesas SEPA (pain.008) de los plazos domiciliados;
  - las devoluciones e impagados.
- Reclamación de deuda (dunning), provisión y baja por incobrable (art. 80 LIVA: rectificativa
  R2 o R3 desde Facturación).
- Pedidos: comprobar el riesgo con `Credit.Exposure` al confirmar un pedido.
- Calendario de festivos por territorio (puerto `Calendar`).
- Importar de C# y Sage: condiciones de pago (Sage `CodigoCondiciones`, hoy perdidas), riesgo y
  cobros históricos como aplicaciones.
