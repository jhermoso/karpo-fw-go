# Contexto Tesorería

Port de lo bancario de C# al contexto `contexts/treasury`. En C# estaba repartido en tres sitios:

- **FinancialKernel:** `BankAccount`, `BankAccountUse`, `FinancialAccount`, `AccountTransaction` y
  `BankStatementLine`.
- **Parties de ErpKernel y ErpDetail:** `PartyBankAccount`, `OwnBankAccount` y la
  `MandateReference` del perfil comercial.
- **Cobros**, que no existía.

Aplica la decisión 5 de [COBROS.md](COBROS.md) (aprobada el 2026-09-28): Cobros publica los
hechos y Tesorería los ejecuta.

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

- **Nada de SEPA:**
  - no hay mandatos, remesas ni ficheros pain.008 o pain.001;
  - no hay Norma 19, 34 ni 43;
  - no existe el identificador de acreedor.
- **La `MandateReference` del perfil comercial** es un texto libre que la importación de Sage
  sobrescribe con `PC=…;COM=…`.
- **Tres validadores de IBAN:** solo el del flujo de alta de cuentas de Parties comprueba el
  MOD-97. `BankAccount` y `OwnBankAccount` solo miran la longitud.
- **`OwnBankAccount`** (Parties) duplica `BankAccount` + `PartyBankAccount` + `BankAccountUse`
  (FinancialKernel), y no están enlazados.
- **Extractos sin cuenta ni conciliación:**
  - `BankStatementLine` apunta a `financial_account`, no a `bank_account`, porque la herencia
    prevista nunca se hizo;
  - no hay conciliación: el enlace se pone a mano y el estado «Matched» nunca se asigna.
- **Tipos de uso de cuenta:** la semilla de 10 filas es del vertical de juego (PLAYER, AGENT…).
- **Sin permisos** en ningún endpoint bancario.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `BankAccount` (IBAN sin MOD-97) + `OwnBankAccount` (duplicado) | 4 | 2 | 2 | 3 | **59** | Modificar → agregado `Account` de una organización interna: IBAN validado con MOD-97 (`vocab.IBAN`) y único, BIC de 8 u 11 caracteres, alias, EUR, uso para cobros o pagos, sufijo del identificador de acreedor, apertura y cierre |
| 2 | `BankAccountUse` + 10 tipos de uso del vertical de juego | 2 | 3 | 3 | 3 | **50** | Modificar → indicadores de uso para cobros y pagos. Los tipos del vertical de juego, a Maccorp |
| 3 | `MandateReference` libre (sobrescrita por la importación) | 1 | 1 | 1 | 2 | **22** | Sustituido → agregado `Mandate`: acreedor, deudor, IBAN, referencia única por acreedor en caracteres SEPA, esquema CORE o B2B, firma, **FRST o RCUR según el uso**, **caducidad a los 36 meses sin uso** y revocación |
| 4 | Remesas, pain.008, identificador de acreedor | — | — | — | — | — | Nuevo → agregado `Remittance`, que se **propone** con los plazos pendientes de Cobros (`Collectable`): mandato usable del esquema, sin repetir plazos en remesas vivas y contando los plazos sin mandato. Borrador → generada (fija el acreedor, el identificador AT-02 y el FRST o RCUR, y registra el uso de los mandatos) → liquidada → devoluciones con motivo ISO. Genera el **fichero pain.008.001.02** |
| 5 | `BankStatementLine`, `AccountTransaction`, `FinancialAccount` y la conciliación manual | 3 | 1 | 2 | 3 | **49** | Aplazado → fase 2: extractos (Norma 43 / camt.053) y conciliación contra cobros, remesas y pagos |
| 6 | `PartyBankAccount` (cuentas de terceros) | 3 | 3 | 3 | 3 | **60** | Se queda en Parties por ahora; la cuenta del deudor viaja en su mandato |
| 7 | Tres validadores de IBAN | — | — | — | — | — | Sustituido → un único `vocab.IBAN` (MOD-97) |
| 8 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Treasury.Account.*` y `Treasury.Mandate.*`; en las remesas, **preparar** (`Update`), **generar** (`Generate`) y **liquidar** (`Settle`) son permisos separados |

## Diseño

```
contexts/treasury/
├── domain/          # Account, CreditorID, ValidBIC, Mandate, Remittance (+Item), pain.008, puertos Receivables e Identities
├── contracts/       # lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito
├── infrastructure/  # esquema trs_* de 5 motores, mapeos, adaptadores ReceivablesDueItems y PartiesIdentities
└── module.go        # composición y rutas /api/treasury/... (el fichero se sirve como application/xml)
```

- **El ciclo completo, sin llamadas síncronas de escritura entre contextos:**
  1. Facturación emite y publica `billing.invoice-issued.v1`.
  2. Cobros abre la cuenta por cobrar con sus plazos.
  3. Tesorería consulta los plazos pendientes (`Collectable.DueItems`), propone y genera la remesa
     y el banco la carga.
  4. Tesorería publica `treasury.direct-debit-collected.v1`, uno por adeudo. Cobros registra un
     cobro por domiciliación con referencia igual al end-to-end y lo aplica al plazo. Si el plazo
     ya se cobró por otra vía, el cobro queda sin aplicar para que lo revise una persona.
  5. Si hay devolución, `treasury.direct-debit-returned.v1` hace que Cobros anule ese cobro y el
     plazo vuelve a estar pendiente.
- **Identificador de acreedor (AT-02):** país + dígitos de control ISO 7064 MOD 97-10 sobre el
  NIF + sufijo de la cuenta + NIF. Por ejemplo, `ES30000A58818501`. El NIF sale de Parties
  (`TaxIdentities`).
- **Nombres en el fichero:** se transliteran al juego de caracteres SEPA (sin tildes ni eñes) y
  se recortan a 70 caracteres. El agente del deudor va como `NOTPROVIDED`, porque no se exige
  BIC desde 2016.
- **Tablas:**
  - `trs_accounts` (IBAN único);
  - `trs_mandates` (referencia única por acreedor);
  - `trs_remittances` (+ `trs_remittance_items`);
  - las bandejas de salida y la auditoría.

## Decisiones (aprobadas por Javier el 2026-09-28)

1. **Los mandatos viven en Tesorería.** Cobros no necesita el mandato para saber qué se debe; lo
   necesita quien remesa.
2. **Remesa por propuesta.** Se genera desde los plazos pendientes y se puede editar antes de
   generar. Un plazo no puede estar en dos remesas vivas. Una remesa generada solo se liquida o
   se anula.
3. **La liquidación publica un evento por adeudo** y **Cobros registra los cobros**; Tesorería no
   escribe en Cobros.
4. **Fase 1 sin extractos ni conciliación** (Norma 43 / camt.053), **sin transferencias
   (pain.001)**, porque no hay contexto de Pagos, **y sin caja**. Quedan como fase 2.
5. **Las cuentas de terceros** (`PartyBankAccount`) **se quedan en Parties.** Las cuentas propias
   son las de Tesorería.

## Validación

- **Dominio:**
  - identificador de acreedor (con y sin sufijo) y BIC;
  - cuenta con BIC inválido; cuenta abierta hasta su cierre;
  - mandato: caracteres SEPA, caducidad a los 36 meses, FRST y luego RCUR, revocación;
  - remesa: importe positivo, un plazo una sola vez, secuencia obligatoria, congelada al
    generar, liquidación, devolución anterior a la liquidación rechazada o repetida, no se anula
    si está liquidada;
  - **pain.008 parseado:** espacio de nombres, número de adeudos y suma de control, bloques FRST
    y RCUR, esquema, fecha, identificador de acreedor, end-to-end, nombre transliterado y
    `NOTPROVIDED`.
- **Extremo a extremo** (Parties, Fiscal, Facturación, Cobros y Tesorería sobre el mismo
  backend, en memoria y en SQLite migrada, por HTTP):
  - IBAN duplicado: 422; IBAN con dígitos erróneos: 400; ajeno: 404;
  - mandato duplicado: 422;
  - propuesta con Ana y el plazo de Carl contado como «sin mandato»;
  - preparar no es generar, ni generar es liquidar: 403;
  - fichero con MndtId, FRST y el nombre transliterado;
  - una segunda propuesta no repite a Ana;
  - la liquidación salda la factura en Cobros; la devolución la reabre;
  - una tercera remesa con Ana ya sale como RCUR y el mandato registra su último uso.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con los cinco contextos migrados en
  la misma base:
  - cuenta con sufijo `001` (identificador `ES30001A58818501`) y mandato duplicado;
  - propuesta, generación y fichero a partir de la remesa guardada;
  - liquidación y devolución pasando por `receivables_inbox`, con la cuenta por cobrar saldada y
    reabierta.

## Pendiente

- Fase 2:
  - extractos (Norma 43 / camt.053) y conciliación automática;
  - ~~transferencias (pain.001)~~: hechas con el contexto de Pagos, ver [PAGOS.md](PAGOS.md)
    (órdenes de transferencia, migración 3);
  - caja;
  - ficheros de devoluciones (pain.002 / camt.054) en lugar del alta manual.
- Calendario TARGET2 para validar la fecha de cobro (D+1 hábil).
- Tipos de uso del vertical de juego, a Maccorp.
- Importar de C#: cuentas propias (`BankAccount` y `OwnBankAccount` fusionadas) sin los
  `MandateReference` corruptos.
