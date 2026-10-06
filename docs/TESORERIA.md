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

## Extractos y conciliación

Fase 2 de Tesorería, aplazada por la decisión 4: lo que dice el banco que pasó en cada cuenta, y
su punteo contra lo que la empresa le envió.

En C#, `BankStatementLine` apuntaba a `financial_account` y no a una cuenta bancaria, el enlace con
lo conciliado se ponía a mano y el estado «Matched» no se asignaba nunca (fila 5 de la tabla de
resultados, nota 49). No había lector de ficheros del banco.

- **Extracto** (`Statement`): una cuenta, un periodo, saldo inicial y final, y sus movimientos
  (fecha, fecha valor, importe con signo, concepto y referencia).
  - **Cuadra o no entra:** saldo inicial más movimientos es el saldo final.
  - **Continuidad:** los periodos de una cuenta no se solapan, y el saldo inicial es el saldo
    final del extracto anterior (422 si no).
- **Entrada:**
  - **Norma 43** (Cuaderno 43 de la AEB, el formato en que los bancos españoles dan los
    extractos): registros 11 (cuenta), 22 (movimiento), 23 (conceptos), 33 (totales) y 88 (fin).
    Se comprueban los totales de debe y haber y el saldo final de cada cuenta. Admite UTF-8 e
    ISO 8859-1. La cuenta del fichero se localiza por banco, oficina y número entre las cuentas de
    la organización. **Un fichero entra entero o no entra.**
  - **A mano**, por JSON, para bancos sin Norma 43.
- **Conciliación** de cada movimiento contra una de tres cosas:

  | Contra | Movimiento | Condiciones |
  |---|---|---|
  | `remittance` (remesa de adeudos) | abono | generada o liquidada, de esa cuenta, por el **mismo importe** |
  | `transfer-order` (orden de transferencias) | cargo | generada o liquidada, de esa cuenta, por el mismo importe |
  | `other` | cualquiera | una **nota** que lo explique (comisiones, intereses, impuestos) |

  Una remesa o una orden explica **un solo movimiento** (se busca en los movimientos de todos los
  extractos). Un movimiento conciliado se puede liberar y volver a conciliar.
- **Conciliación automática:** a cada movimiento pendiente se le asigna la remesa o la orden de su
  cuenta con el mismo importe y fecha a **cinco días** como mucho, solo si hay **exactamente una**
  candidata libre. Si hay varias o ninguna, decide una persona.
- **Permisos:** `Treasury.Statement.Read`, `Import` y `Reconcile` (**registrar lo que dice el banco
  y explicarlo están separados**).
- **Tablas:** `trs_statements` (+ `trs_statement_lines`), migración 4.

### Decisiones propuestas (pendientes de confirmar)

1. **El formato de entrada es Norma 43**, más el alta a mano. camt.053 (el XML europeo) se añade
   cuando un banco lo exija. Sugerencia: sí; Norma 43 es lo que entregan los bancos españoles.
2. **Un extracto debe cuadrar y continuar al anterior**, o no se registra. Sugerencia: sí; así un
   fichero repetido, saltado o truncado se detecta al entrar y no al cerrar el mes.
3. **En esta fase se concilia contra lo que Tesorería envió al banco** (remesas y órdenes de
   transferencia) **y con una nota para lo demás**. Conciliar contra cobros y pagos sueltos
   (transferencias recibidas de clientes, recibos domiciliados de proveedores) es el siguiente
   paso, con puertos a Cobros y Pagos. Sugerencia: sí.
4. **Conciliar no cambia nada más**: no liquida la remesa ni ejecuta la orden, ni genera asientos.
   Es un punteo. Sugerencia: sí por ahora; que el abono del banco liquide la remesa
   automáticamente es cómodo, pero conviene decidirlo junto con las devoluciones por fichero.
5. **La conciliación automática solo actúa cuando hay una única candidata** por importe exacto y
   fecha a cinco días. Sugerencia: sí; prefiero dejar un movimiento pendiente a puntearlo mal.
6. **Las comisiones y demás movimientos «otros» no se contabilizan todavía**: la nota los explica,
   pero el asiento (626, 669…) se hace a mano en Contabilidad. Sugerencia: sí en esta fase; el
   siguiente paso es que la nota lleve una categoría y Contabilidad asiente desde un evento.

### Validación

- **Dominio:** extracto que no cuadra, periodo invertido, movimiento fuera del periodo, importe
  cero o con tres decimales; conciliar (línea inexistente, sin nota, sin referencia, tipo
  inválido, una sola vez), liberar, y el evento al conciliar el último movimiento.
- **Norma 43:** fichero con saltos de línea de Windows, saldos, fechas, importes con signo,
  conceptos del registro 23, ISO 8859-1; rechazo del fichero que no cuadra, sin totales, con un
  registro desconocido, vacío, en otra divisa o con un movimiento antes de la cuenta.
- **Extremo a extremo** (Tesorería sola, con los demás contextos simulados; remesas y orden
  generadas por sus casos de uso; en memoria y en SQLite migrada):
  - fichero: conciliar no es importar (403), ajeno 404, vacío 400, descuadrado 422, cuenta de
    otro 422; mayo registrado; repetido 422;
  - junio a mano: no continúa mayo 422, no cuadra 400; junio y julio;
  - automática: importar no es conciliar (403), ajeno 404; dos de tres movimientos; repetida no
    hace nada;
  - a mano: tipo inválido 400, sin nota 422, línea inexistente 422, la comisión con su nota, ya
    conciliado 422;
  - liberar (una vez) y reconciliar: una orden no es una remesa, remesa de otra cuenta, otro
    importe y, por fin, la suya; la misma remesa en julio 422;
  - junio: la remesa del día 20 queda fuera de la ventana automática, pero se puede asignar a mano;
  - búsquedas (todos, pendientes, otra cuenta), ficha con sus movimientos, ajeno 404.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: fichero Norma 43, solape, hueco de
  saldos, automática, «ya conciliado» buscado en las filas hijas, importe distinto, ida y vuelta
  de los movimientos en orden, liberar y reasignar, y búsqueda de extractos con pendientes.

## Pendiente

- Fase 2:
  - ~~extractos (Norma 43) y conciliación~~: hechos, ver «Extractos y conciliación» (migración 4);
  - conciliación contra cobros y pagos sueltos, camt.053, y asiento de comisiones;
  - ~~transferencias (pain.001)~~: hechas con el contexto de Pagos, ver [PAGOS.md](PAGOS.md)
    (órdenes de transferencia, migración 3);
  - caja;
  - ficheros de devoluciones (pain.002 / camt.054) en lugar del alta manual.
- Calendario TARGET2 para validar la fecha de cobro (D+1 hábil).
- Tipos de uso del vertical de juego, a Maccorp.
- Importar de C#: cuentas propias (`BankAccount` y `OwnBankAccount` fusionadas) sin los
  `MandateReference` corruptos.
