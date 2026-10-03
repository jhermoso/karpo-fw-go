# Contexto Nóminas

Port de Nóminas de C# al contexto `contexts/payroll`. En C#, Nóminas vivía repartida en dos
sitios:

- **`ErpKernel.Domain.Payroll`**, esquema `rrhh`, alojada en la rebanada de RRHH: `Payslip`,
  `PayslipLine`, `RateType` y `DeductionType`.
- **Parties de ErpDetail**, esquema `parties`: `EmployeePayrollAccount`, las
  `CompanySocialSecurityAccount` (con sus pagos y representantes), la parte retributiva de
  `EmployeeProfile` y los generadores de los modelos 111 y 190.

Aplica la decisión 3 de [RRHH.md](RRHH.md) (aprobada el 2026-09-27): lo retributivo sale de RRHH y
de Parties.

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

- **No hay motor de cálculo.**
  - El bruto (`GrossAmount`) y el neto (`NetAmount`) de la nómina los envía el cliente.
  - El importe de cada línea también lo envía el cliente.
  - Ni las cotizaciones (trabajador o empresa), ni sus bases y topes, ni la retención de IRPF se
    calculan en ningún sitio.
  - La única aritmética es un **15 % de IRPF fijo** en los generadores de los modelos 111 y 190.
- **Las líneas no cuadran con la nómina.** Los totales no se derivan de las líneas. Además:
  - una nómina emitida se puede modificar y borrar físicamente;
  - el estado es un entero «mágico» (0 y 2);
  - `Validate()` acepta cualquier cosa.
- **La línea no distingue IRPF de Seguridad Social.** `Withholding` mezcla los dos, y el modelo
  190 suma las cotizaciones como si fueran IRPF retenido.
- **Los conceptos son texto libre**, sin catálogo. `DeductionType` no tiene importe ni tipo.
  `RateType` guarda los tramos de la **escala anual** de IRPF (no una tabla de retenciones), con
  huecos entre tramos y sin año. Además, el nombre `RateType` se usa también para la tarifa de
  WorkEffort y TimeEntry.
- **Las cuentas de nómina** (`EmployeePayrollAccount`):
  - guardan el reparto del neto, pero **nada lo aplica**;
  - el validador ignora las fechas de vigencia;
  - `ValidFrom` se guarda como `0001-01-01`.
- **La CCC** no valida su formato y no está enlazada con ningún empleado ni con ninguna nómina. En
  `EmployeeProfile` es un texto suelto.
- **`EmployeeProfile.BaseSalary`** no dice si es mensual o anual, y la importación de SAGE guarda
  en él el bruto mensual. Ni ese campo ni el porcentaje de IRPF los lee nada.
- **Modelos 111 y 190:**
  - confunden el Id de la organización interna con el de su rol;
  - usan la clave G en lugar de la A;
  - el NIF y el nombre son marcadores de posición;
  - el fichero no sigue el formato de la AEAT.
- **Los endpoints de Nóminas del lado de Parties no exigen permisos**, solo autenticación.
- **Emitir una nómina da 500** en la rebanada de RRHH, porque Documents no está alojado allí.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `Payslip` (cabecera con bruto y neto enviados por el cliente, estado entero, modificable tras emitir) | 4 | 1 | 2 | 3 | **54** | Modificar → agregado `Payslip` con **las líneas dentro**. Tipos ordinaria, extra y finiquito; estados borrador, aprobada y anulada. **Inmutable al aprobarse**: se corrige anulando y creando otro borrador. Solo se descartan borradores |
| 2 | `PayslipLine` (agregado aparte; `Amount` enviado por el cliente; `Withholding` mezcla IRPF y SS) | 4 | 1 | 2 | 3 | **54** | Modificar → hijo `Line` con una copia del concepto (clase, bases y clave del 190). El importe se da directo, se **calcula** como cantidad × precio o como base × porcentaje (al céntimo, redondeando los medios hacia fuera de cero), o es cero si la línea es informativa |
| 3 | Totales | — | — | — | — | — | Sustituido → **derivados de las líneas**: bruto, bases de cotización y de retención, SS del trabajador, IRPF, otras deducciones, neto, coste de empresa. Para aprobar hace falta al menos un devengo; el neto no puede ser negativo, la retención no puede superar su base y la SS no puede superar el bruto |
| 4 | `DeductionType` (8 filas) + conceptos en texto libre | 3 | 2 | 2 | 3 | **51** | Modificar → catálogo `Concept` con clase (devengo, SS del trabajador, IRPF, otra deducción, coste de empresa, informativo), marcas de cotizable y tributable, y clave del 190. Semilla de 26 conceptos: las 8 deducciones con **sus GUID de C#**, más devengos, la SS de la empresa y líneas informativas |
| 5 | `RateType` (escala de IRPF sin año, con huecos; nombre compartido con WorkEffort) | 2 | 1 | 2 | 3 | **36** | Retirar de Nóminas. El nombre `RateType` vuelve a su significado original (tarifa de WorkEffort) cuando se porte ese contexto |
| 6 | `EmployeePayrollAccount` (reparto del neto que nada aplica) | 3 | 2 | 3 | 3 | **54** | Modificar → hijos `Split` del perfil: porcentaje, importe o resto, con prioridad, embargo e IBAN. **Invariantes con fechas de vigencia:** un modo por reparto, una sola cuenta de resto a la vez, porcentajes ≤ 100 en cada instante. **`Distribute` reparte de verdad:** primero los embargos, después por prioridad, y el resto a la cuenta de resto; da error si sobra dinero sin cuenta |
| 7 | Parte retributiva de `EmployeeProfile` (`BaseSalary` sin periodicidad, IRPF %, CCC como texto) | 3 | 2 | 1 | 3 | **47** | Modificar → agregado `Profile`, uno por relación laboral. Grupo de cotización (1–11), tipo de retención, salario pactado con periodicidad y número de pagas (12, 14 o 15), y CCC por Id, del mismo empleador y activa. Lo de RRHH (tipo de contrato, convenio, horas) se quedó en `Contract` |
| 8 | `CompanySocialSecurityAccount` + `Payment` | 3 | 2 | 3 | 3 | **54** | Modificar → agregado `EmployerAccount`. CCC con **dígitos de control** (mod 97), única mientras está activa, régimen de 4 dígitos, CNAE y forma de pago (la domiciliación exige IBAN). La baja en la SS es `Deactivate` |
| 9 | `CompanySocialSecurityAccountRepresentative` | 2 | 3 | 3 | 3 | **50** | Aplazado (ver Pendiente) |
| 10 | Mod111, Mod190, `OfficialFormSubmission`, `OfficialFormCounter` | 3 | 1 | 2 | 2 | **46** | Fuera de Nóminas → contexto Fiscal, que consumirá `payroll.payslip-approved.v1`, con la base y la retención separadas y la clave del 190. **Desaparece el 15 % fijo** |
| 11 | `IssueAsync` a través de Documents (500 si no está alojado) | 3 | 2 | 2 | 3 | **52** | Sustituido → la aprobación es un hecho de Nóminas y se publica. La numeración de documentos queda en Documents, que puede consumir el evento |
| 12 | Entidades antiguas `PayCheck`, `PayHistory`, `PayGrade`, `SalaryStep`, `PayrollPreference`, `Deduction` | 1 | 1 | 2 | 2 | **28** | Retirar |
| 13 | Endpoints de Nóminas del lado de Parties sin permisos | — | — | — | — | — | Sustituido → `Payroll.Payslip.*` (con **`Approve` separado** de preparar), `Payroll.Profile.*`, `Payroll.EmployerAccount.*`, `Payroll.Catalog.Read` y `Payroll.Remittance.Read` |

## Diseño

```
contexts/payroll/
├── domain/          # Payslip (+Line), Totals, Concept, Profile (+Split, Distribute), EmployerAccount (CCC), eventos
├── contracts/       # Remittance + lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito, puerto Employments (RRHH)
├── infrastructure/  # esquema pay_* de 5 motores, semilla de conceptos, mapeos, adaptador del Staff de RRHH
└── module.go        # composición y rutas /api/payroll/...
```

- **Ámbito:** todo pertenece al empleador. Fuera de su ámbito, 404 uniforme; con acceso de solo
  lectura, 403.
- **Puerto consumido:** `Employments.EmploymentOn(persona, empleador, fecha)`, implementado por
  `infrastructure.HRStaff` sobre el `Staff` de RRHH.
  - Una nómina exige una relación laboral vigente al inicio o al final del periodo.
  - Guarda una **copia** del número de empleado, el convenio y el centro de trabajo del contrato
    principal, y del grupo de cotización, el tipo de retención y la CCC del perfil.
  - Una sola nómina no anulada por relación laboral, tipo y periodo.
- **Puerto expuesto:** `Remittance.NetPayments(nóminas)` da el neto de cada nómina aprobada
  repartido entre las cuentas del perfil vigentes en la fecha de pago. Es lo que usará Tesorería
  para las transferencias SEPA.
- **Lenguaje publicado:**
  - `payroll.payslip-approved.v1`: empleado, empleador, periodo, fecha de pago, CCC, clave del 190
    y todos los totales como texto decimal exacto;
  - `payroll.payslip-cancelled.v1`.
  - Consumidores previstos: Fiscal (111 y 190), Contabilidad (640, 642, 476, 4751 y 465) y
    Documents.
- **Tablas:**
  - `pay_concepts`, `pay_employer_accounts`;
  - `pay_profiles` (+ `pay_profile_splits`);
  - `pay_payslips` (+ `pay_payslip_lines`);
  - las dos bandejas de salida y la auditoría.
  - Los importes se guardan como texto decimal exacto.
  - El bruto y el neto se guardan también en la cabecera para informes; al leer, se recalculan
    desde las líneas.

## Decisiones (aprobadas por Javier el 2026-09-27)

1. **Sin motor de cálculo por ahora.** Nóminas registra, valida, totaliza, aprueba y publica, pero
   no calcula cotizaciones ni retenciones. El motor necesita tablas legales que cambian cada año:
   - tipos por contingencia;
   - bases mínimas y máximas por grupo de cotización;
   - MEI;
   - desempleo según el tipo de contrato;
   - FOGASA y AT/EP por CNAE;
   - el procedimiento de retención del IRPF.

   Ninguna de estas tablas existe en C# ni en la semilla. Propongo hacerlo como fase 2: un
   servicio de dominio que genere las líneas a partir del perfil y del convenio, con reglas
   versionadas por año y verificadas contra casos reales.
2. **Los modelos 111 y 190 van a un contexto Fiscal**, alimentado por
   `payroll.payslip-approved.v1`, y no por lectura directa de las tablas de Nóminas.
3. **La CCC vive en Nóminas** (`EmployerAccount`), no en Parties. Es dato de inscripción del
   empleador en la SS, y solo lo usan Nóminas y los seguros sociales.
4. **Aprobar es un permiso aparte** (`Payroll.Payslip.Approve`) de preparar la nómina. Así se
   separan funciones.
5. **Los representantes de la CCC** (administrador, firmante) **se aplazan** hasta portar los
   seguros sociales (RLC/RNT/SILTRA).

## Validación

- **Dominio:**
  - catálogo válido y sin GUID repetidos;
  - líneas por cantidad × precio y por base × porcentaje, con redondeo al céntimo; línea
    informativa a cero;
  - totales de una nómina real: 2.105,25 de bruto, 131,58 de SS, 315,79 de IRPF, 1.657,88 de neto
    y 2.602,09 de coste de empresa;
  - aprobada inmutable, anulación con motivo, neto negativo rechazado;
  - reparto con embargo, 30 % y resto (200 / 497,36 / 960,52); dos cuentas de resto y porcentajes
    por encima de 100 rechazados;
  - CCC con dígitos de control.
- **Extremo a extremo** (Parties, Instalaciones, RRHH y Nóminas sobre el mismo backend, en
  memoria y en SQLite migrada, por HTTP):
  - CCC con dígitos erróneos: 400; duplicada: 422; empleador ajeno: 404;
  - perfil único por relación laboral; perfil de alguien no contratado: 422;
  - borrador con la copia de RRHH y del perfil; nómina duplicada: 422;
  - líneas por código de concepto (sin distinguir mayúsculas); concepto desconocido: 400;
  - preparar no es aprobar: 403;
  - aprobada inmutable y no borrable;
  - `Remittance` con el reparto;
  - anular libera el periodo; descartar un borrador;
  - evento v1 consumido con inbox;
  - auditoría con el actor.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con Parties, Instalaciones, RRHH y
  Nóminas migrados en la misma base:
  - semilla de conceptos;
  - decimales exactos (15,5 %, 30.000,50, 3,5 × 18,333);
  - totales y reparto recalculados al leer;
  - duplicado y búsqueda por fechas en SQL;
  - anulación que libera el periodo.

## Pendiente

- Fase 2: motor de cálculo (decisión 1).
- ~~Contexto Fiscal (modelos 111 y 190)~~: hecho en [FISCAL.md](FISCAL.md). Contabilidad (asiento de nóminas) y Tesorería (remesa SEPA
  con `Remittance`).
- Representantes de la CCC y seguros sociales (RLC/RNT/SILTRA).
- Catálogo de regímenes de la SS y de claves y subclaves del 190.
- Importar de C# (SAGE): el perfil desde `EmployeeProfile` y `EmployeePayrollAccount`
  (**`BaseSalary` es mensual**), las CCC y las nóminas históricas como aprobadas.
