# Contexto Fiscal

Port de lo fiscal de C# al contexto `contexts/fiscal`. En C# estaba repartido en cinco sitios:

- **Parties de ErpDetail:** `VatGroup`, `VatIndicator`, `FiscalActivity`,
  `OfficialTaxFormSubscription`, `OfficialFormSubmission`, `OfficialFormCounter` y los
  generadores de los modelos 111 y 190.
- **Accounting de ErpDetail:** los indicadores fiscales de `GlAccountProfile`.
- **ErpKernel:** `SalesTax`, `TaxDue` y el tipo de factura «Exenta».
- **AdvisoryKernel:** `TaxFiling`, `TaxFilingMandate` y `FiscalYearConfig`.
- **La importación de Sage.**

Aplica la decisión 2 de [NOMINAS.md](NOMINAS.md) (aprobada el 2026-09-27): los modelos 111 y 190
se alimentan de `payroll.payslip-approved.v1`, no de leer las tablas de Nóminas.

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

- **El IVA no se calcula en ningún sitio.** Las facturas no tienen columnas de impuestos
  (`LineTotal = Quantity × UnitPrice`, sin redondeo), y los pedidos solo llevan importes netos.
- **No hay tipos sembrados** de IVA, IGIC ni IPSI. Solo llegan importados de Sage, y la
  importación pierde el territorio, lo que hace colisionar los códigos de IGIC. Además, el código
  admite 5 caracteres en `VatGroup` y 3 en `VatIndicator`.
- **Faltan SII, Verifactu, TicketBAI, los libros registro y los modelos 303, 390, 347, 349, 115,
  180, 200 y 202.** Solo aparecen como texto en comentarios.
- **Los modelos 111 y 190:**
  - el 111 estima siempre un 15 % fijo y el 190 suma líneas de retención, así que los dos **no
    cuadran** entre sí;
  - usan claves distintas (A en el 111, G en el 190);
  - los NIF son marcadores de posición («PENDIENTE-NIF»);
  - fechan por el inicio del periodo de la nómina, no por la fecha de pago;
  - revertir una presentación no libera el periodo;
  - el contador no está protegido frente a concurrencia (lee, incrementa y escribe sin bloqueo).
- **Dos agregados paralelos de «formulario presentado»:** `OfficialFormSubmission` (con su
  contador) y `TaxFiling` (con la serie de Documents). Cada uno numera por su cuenta.
- **El NIF está en cuatro sitios** y los generadores no usan ninguno.
- **Ningún endpoint fiscal exige permisos**, solo autenticación.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `VatGroup` (tipo por organización, sin territorio ni vigencia) | 3 | 2 | 2 | 3 | **51** | Modificar → `TaxRate`: impuesto (IVA, IGIC o IPSI) coherente con el territorio (el IGIC solo en Canarias, el IPSI en Ceuta y Melilla), **vigencia**, recargo de equivalencia solo en IVA y **un tipo por código a la vez**. Catálogo **común**, no por empresa (ver decisión 1) |
| 2 | `VatIndicator` (`IsExempt` e `IsNonSubject` a la vez; código de 3 caracteres) | 3 | 2 | 2 | 3 | **51** | Modificar → `Treatment`: sujeto, exento o no sujeto (excluyentes), por territorio, código de hasta 5 caracteres |
| 3 | `FiscalTerritory` (enum) + `Territory` como texto libre | 3 | 3 | 3 | 4 | **62** | Modificar → un solo `Territory` en todo el contexto |
| 4 | Parte fiscal de `InternalOrganizationProfile` (mes de inicio del ejercicio, prorrata, sectores diferenciados) | 3 | 3 | 3 | 3 | **60** | Modificar → agregado `Taxpayer`, uno por organización interna, con `FiscalYearOf` |
| 5 | `FiscalActivity` (una principal; no comprueba desde ≤ hasta) | 3 | 3 | 3 | 3 | **60** | Modificar → hijo `Activity`: una principal a la vez (la nueva sustituye a la anterior); comprueba desde ≤ hasta |
| 6 | `OfficialTaxFormSubscription` (todo texto libre) | 3 | 2 | 2 | 3 | **51** | Modificar → hijo `Obligation`: modelo conocido con sus periodicidades válidas (el 190 solo anual, el 111 mensual o trimestral…), una obligación por modelo y año |
| 7 | `OfficialFormSubmission` + `TaxFiling` (duplicados) | 4 | 2 | 2 | 3 | **59** | Modificar → un agregado `Filing`: borrador, presentado o revertido. El borrador se regenera; **revertir libera el periodo**; solo se descartan borradores |
| 8 | `OfficialFormCounter` (sin protección de concurrencia) | 3 | 1 | 3 | 3 | **50** | Modificar → agregado `Counter` por declarante, modelo y año, con concurrencia optimista y reintento. Hay un índice único. Presentar con problemas no gasta número |
| 9 | Generadores del 111 y del 190 (15 %, lectura de las tablas de Nóminas, NIF de marcador, clave G) | 4 | 1 | 2 | 2 | **50** | Modificar → proyección `Withholding` alimentada por `payroll.payslip-approved.v1` y `-cancelled.v1` (inbox; una anulación la saca). Se **fecha por el pago**. `Summarize` agrupa por perceptor y clave; **el 111 y el 190 salen de los mismos datos y cuadran**. NIF y provincia se obtienen de Parties (`TaxIdentities`), y **no se presenta** si faltan o no pasan el control |
| 10 | `Mod190PerceptorLine` | 3 | 2 | 3 | 3 | **54** | Modificar → hijo `Recipient` del `Filing` (NIF, nombre, provincia, clave, pagos, percepciones, retenciones) |
| 11 | `rate_type` / `deduction_type` como tabla de retenciones | 2 | 1 | 2 | 3 | **36** | Retirar. Es la escala anual y no una tabla de retenciones (decisión 5 de NOMINAS.md). Las retenciones son hechos de Nóminas |
| 12 | `SalesTax`, `SalesTaxLookup`, `TaxDue`, tipo de factura «Exenta» | 1 | 1 | 2 | 2 | **28** | Retirar. La exención es un tratamiento de la línea, no un tipo de factura |
| 13 | `TaxFilingMandate`, `FiscalYearConfig` (Advisory) | 2 | 2 | 2 | 3 | **42** | Fuera de Fiscal → contexto Asesoría, cuando se porte |
| 14 | Endpoints fiscales sin permisos | — | — | — | — | — | Sustituido → `Fiscal.Catalog.Read/Update`, `Fiscal.Taxpayer.Read/Update`, `Fiscal.Filing.Read/Create/Submit` (**presentar va aparte** de preparar) |

## Diseño

```
contexts/fiscal/
├── domain/          # TaxRate, Treatment, Taxpayer (+Activity, Obligation), Withholding, Filing (+Recipient), Counter, Period
├── contracts/       # Rates + lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito, suscripción a Nóminas, RateLookup
├── infrastructure/  # esquema fis_* de 5 motores (con inbox), mapeos, adaptador TaxIdentities de Parties
└── module.go        # composición, Consumer y rutas /api/fiscal/...
```

- **Ámbito:** el contribuyente y sus presentaciones pertenecen a la organización. Fuera de su
  ámbito, 404 uniforme. El catálogo de tipos es común y solo exige permiso.
- **Periodos AEAT:** meses 01–12, trimestres 1T–4T y año 0A, sobre el año natural.
- **En Parties:** contrato nuevo `TaxIdentities`.
  - Da el documento fiscal: primero el principal; si no, TXID, después NIDN (DNI) y después ARNU
    (NIE).
  - Da la provincia del contacto postal español vigente (primero el de facturación, después el
    predeterminado).
  - Fiscal lo consume con el adaptador `PartiesIdentities`.
- **Puerto expuesto:** `Rates.RateOn(impuesto, territorio, código, fecha)` para Facturación.
- **Lenguaje publicado:** `fiscal.filing-submitted.v1` (número y totales) y
  `fiscal.filing-reverted.v1`, para Contabilidad (liquidar la cuenta 4751) y Tesorería.
- **Tablas:**
  - `fis_tax_rates`, `fis_treatments`;
  - `fis_taxpayers` (+ `fis_activities` y `fis_obligations`);
  - `fis_withholdings`;
  - `fis_filings` (+ `fis_filing_lines`), `fis_counters`;
  - las bandejas de salida, la auditoría y `fiscal_inbox`.
  - **Sin semilla:** C# no tenía datos legales y aquí no se inventan.

## Decisiones (aprobadas por Javier el 2026-09-28)

1. **Catálogo de tipos común**, no por empresa. En C# era por organización porque venía de Sage.
   La importación deduplicará por impuesto, territorio, código y vigencia.
2. **Fase 1 sin IVA calculado** ni modelos 303, 390, 347 y 349, SII, Verifactu ni libros
   registro. Necesitan facturas con impuestos por línea (contexto Facturación), que en C# no
   existen. Fiscal ya ofrece `Rates` para cuando se porte Facturación.
3. **Sin fichero AEAT por ahora.** El formato posicional del 111 y el 190 se hará cuando haya
   casos oficiales con los que validarlo. El C# generaba un CSV que no era el formato oficial.
4. **Presentar es un permiso aparte** (`Fiscal.Filing.Submit`) de preparar la presentación.
5. **`TaxFilingMandate` y `FiscalYearConfig` van al futuro contexto de Asesoría.**

## Validación

- **Dominio:**
  - IGIC fuera de Canarias rechazado; recargo solo en IVA; solapamiento y fin de vigencia;
  - ejercicio que empieza en julio;
  - actividad principal única; obligaciones con periodicidad válida y sin solapar;
  - periodos AEAT (1T, 02, 0A) y rechazo de códigos inválidos;
  - `Summarize` sin las anulaciones;
  - problemas de NIF y provincia; presentar, regenerar, revertir;
  - contador.
- **Extremo a extremo** (Parties, Instalaciones, RRHH, Nóminas y Fiscal sobre el mismo backend,
  en memoria y en SQLite migrada, por HTTP):
  - el catálogo exige su permiso; IGIC en territorio común: 400; tipo solapado: 422;
    `RateOn` antes y después del cambio de tipo;
  - cinco nóminas aprobadas y una anulada pasan de Nóminas a Fiscal por inbox;
  - 111 del 1T: dos perceptores, 5.600,00 de percepciones y 765,00 de retenciones, sin la nómina
    anulada ni la de abril;
  - el tramitador no presenta: 403; presentar dos veces el periodo: 422; ajeno: 404;
  - 190 con Bea sin NIF ni provincia: 422 al presentar; se completa en Parties, se regenera y se
    presenta;
  - numeración por modelo; revertir el 111 libera el periodo y la nueva presentación lleva el
    número 2;
  - eventos v1 consumidos con inbox.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con los cinco contextos migrados en
  la misma base:
  - tipos con vigencia y recargo;
  - contribuyente con hijos y prorrata;
  - retenciones por `fiscal_inbox`;
  - 111 y 190 con fechas y decimales en SQL (15,5 % → 652,62);
  - contador, reversión y regeneración;
  - NIF y provincia desde Parties.

## Pendiente

- Cálculo de impuestos por país y por sector: ver el punto 6 de [BACKLOG.md](../BACKLOG.md).
- ~~Contexto Facturación~~: hecho en [FACTURACION.md](FACTURACION.md), que calcula con el `TaxEngine` y la jurisdicción España. Sobre él quedan el 303, el 390, el 347, el
  349, los libros registro, SII y Verifactu.
- Fichero AEAT del 111 y del 190 (decisión 3).
- Modelos 115 y 180 (arrendamientos) cuando haya datos de alquileres.
- Importar de C# y Sage: `VatGroup` y `VatIndicator` (con el territorio), `FiscalActivity`,
  `OfficialTaxFormSubscription` y las presentaciones históricas.
