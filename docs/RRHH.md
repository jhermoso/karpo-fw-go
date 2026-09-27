# Contexto RRHH

Port del subdominio RRHH de C# (`ErpKernel.*\Subdominios\RRHH`, más las piezas de RRHH que vivían
en Parties de ErpDetail) al contexto `contexts/hr`. Aplica las decisiones del mapa de contextos
aprobado el 2026-09-27 (ver [PARTIES.md](PARTIES.md) e [INSTALACIONES.md](INSTALACIONES.md)): el
`WorkCenter` referencia `FacilityID` y las personas y organizaciones siguen en Parties.

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

- El código vive solo en el monolito `ErpKernel.*`. Los proyectos de la «rebanada»
  `Paranoia.Karpo.ErpKernel.RRHH.*` enlazan esos ficheros con comodines. La rebanada tiene
  20 referencias a Parties («enfoque A») y dos `DbContext` sobre la misma cadena de conexión.
- **No hay eventos de dominio** en RRHH, en nóminas, en `LaborContract` ni en `Employee`.
- `Validate()` devuelve `Success()` siempre, en todas las entidades de RRHH. Las únicas
  comprobaciones reales son las excepciones de los setters.
- El agregado `Position` está a medio hacer:
  - sus fechas y flags no se pueden fijar;
  - `Activate`, `Deactivate` y `Terminate` no hacen nada;
  - las peticiones y los DTO llevan campos que la entidad no tiene;
  - `PositionFulfillment` no tiene fecha de inicio;
  - la estructura jerárquica no puede marcar la línea principal ni terminarla;
  - `BuildNode` (el organigrama) puede recurrir sin fin si hay un ciclo.
- Hay conceptos duplicados:
  - `ConvenioColectivo` (RRHH) y `CollectiveAgreement` (Parties);
  - el tipo de contrato está en `LaborContract` y en `EmployeeProfile`;
  - las horas están en `WorkSchedule`, `EmployeeProfile` y `PositionTypeClass`;
  - la cuenta de nómina y el CCC de la Seguridad Social se repiten entre `EmployeeProfile` y
    las entidades de nóminas.
- Los Id de organización son de tipos distintos: `IdPartyRole`, `IdInternalOrganization` e
  `IdParty`, según la entidad.
- Mucho código muerto: `Benefit`, `Classification`, `Employment.cs`, `Performance`, `Training`,
  `PayrollLegacyEntities`, `Roles.cs` y el filtro `IsVacant`.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `Position` + catálogos `PositionType`, `PositionTypeClass`, `ClassificationType`, `PositionStatus` | 4 | 2 | 2 | 3 | **59** | Modificar → agregado `Position` con periodo previsto, flags, estado y cierre reales. Catálogos sembrados con los GUID de C# (6 estados, 9 clases, 57 tipos, 23 enlaces tipo-clase) |
| 2 | `PositionFulfillment` sin fecha de inicio ni «un titular a la vez» | 4 | 1 | 2 | 3 | **54** | Modificar → hijo `Fulfillment` con periodo, **un titular a la vez**, titular = empleado de la organización en esa fecha; `Vacate` real |
| 3 | `PositionReportingStructure` + `BuildNode` recursivo | 3 | 1 | 2 | 3 | **46** | Modificar → hijo `ReportingLine` con periodo y línea principal única (la nueva sustituye a la anterior), **sin ciclos** (recorrido acotado a 20 niveles); organigrama por niveles, una consulta por nivel |
| 4 | Estado «Vacant» guardado | 2 | 1 | 2 | 3 | **36** | Retirar → la vacante se **deriva** (activa y sin titular): `IsVacantAt`, `VacantAt` (NOT EXISTS en SQL) |
| 5 | `WorkCenter` con la dirección copiada como texto y el Id `IdPartyRole` | 4 | 3 | 2 | 3 | **64** | Modificar → agregado `WorkCenter` que referencia `FacilityID` (instalación existente, activa y del mismo empleador), código único por empleador y una única sede activa por empleador |
| 6 | `ConvenioColectivo` (RRHH) + `CollectiveAgreement` (Parties) | 4 | 3 | 2 | 4 | **67** | Modificar → **un solo catálogo** `Agreement`: ámbito nacional, autonómico o provincial, o de empresa con su organización; 8 convenios sembrados |
| 7 | `LaborContract` (en Parties) | 5 | 3 | 3 | 4 | **79** | Mantener, movido → hijo `Contract` de `Employment`: fechas civiles, convenio en vigor, centro del empleador abierto, **un contrato principal a la vez** |
| 8 | Campos de RRHH en el rol `Employee` de Parties (número, alta y baja, categoría, grupo salarial) | 5 | 3 | 2 | 3 | **72** | Mantener, movido → agregado `Employment` (persona + empleador). Parties conserva el rol `Employee` y la relación `Employment` (afiliación), que es requisito para contratar |
| 9 | `Terminate` de empleado (solo ponía una fecha) | 4 | 1 | 3 | 3 | **57** | Modificar → la baja cierra los contratos abiertos y deja los puestos que ocupaba la persona en el empleador (misma transacción) |
| 10 | `EmployeeProfile` (duplica tipo de contrato, horas, cuenta y CCC) | 3 | 2 | 1 | 2 | **44** | Retirar → lo de RRHH va a `Employment`/`Contract`; lo retributivo, a Nóminas |
| 11 | `WorkSchedule` | 3 | 3 | 3 | 3 | **60** | Modificar, **aplazado** (ver Pendiente) |
| 12 | `RateType`, `DeductionType`, `EmployeePayrollAccount`, `CompanySocialSecurityAccount`, Mod190 | 3 | 3 | 3 | 4 | **63** | Fuera de RRHH → contexto Nóminas |
| 13 | Código muerto de la raíz (`Benefit`, `Classification`, `Employment.cs`, `Performance`…) | 1 | 2 | 2 | 2 | **32** | Retirar |
| 14 | `Validate()` que siempre acepta | 1 | 1 | 1 | 1 | **20** | Sustituido → invariantes en los agregados y `fw.Validation` |
| 15 | Ausencia de eventos | — | — | — | — | — | Sustituido → 12 eventos de dominio y 6 de integración v1 |
| 16 | Políticas `RRHH.<Recurso>.<Acción>` por endpoint | 5 | 4 | 4 | 4 | **88** | Mantener → `HR.Position.*`, `HR.Employment.*`, `HR.WorkCenter.*`, `HR.Catalog.Read` con `pipeline.RequirePermission` |

## Diseño

```
contexts/hr/
├── domain/          # Position, Employment (+Contract), WorkCenter, Agreement, catálogos, eventos, especificaciones
├── contracts/       # Staff, Positions + lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito, puertos Organizations y Facilities
├── infrastructure/  # esquema hr_* de 5 motores, semilla, mapeos, adaptadores a Parties y Facilities
└── module.go        # composición y rutas /api/hr/...
```

- **Ámbito:** un puesto pertenece a su organización interna, calculada desde la unidad
  (departamento, división…) con `OrganizationHierarchy.InternalOrganizationOf` de Parties. Una
  relación laboral y un centro de trabajo pertenecen a su empleador. Fuera del ámbito, 404
  uniforme; con acceso de solo lectura, 403.
- **Puertos consumidos** (propiedad de RRHH, con adaptadores ACL en `infrastructure`):
  - `Organizations`: la organización interna de una unidad y si una persona está afiliada al
    empleador. Usa `Membership` y `OrganizationHierarchy` de Parties.
  - `Facilities`: la instalación existe, está activa y es del empleador. Usa el `Directory` de
    Instalaciones.
- **Puertos expuestos** (`contracts`):
  - `Staff.EmploymentsOn(personas, fecha)`: las relaciones laborales vigentes, con el contrato
    principal en esa fecha. Es lo que necesita Nóminas.
  - `Positions.Resolve` y `Positions.HeldBy`: titular y superior actuales.
- **Lenguaje publicado:** `hr.employee-hired.v1`, `hr.employee-terminated.v1`,
  `hr.contract-started.v1`, `hr.contract-ended.v1`, `hr.position-filled.v1` y
  `hr.position-vacated.v1`.
- **Tablas:**
  - catálogos: `hr_position_statuses`, `hr_position_classes`, `hr_position_types`,
    `hr_position_type_classes` y `hr_agreements`;
  - agregados: `hr_positions` (+ `hr_position_holders` y `hr_position_reporting`),
    `hr_work_centers` y `hr_employments` (+ `hr_contracts`);
  - las dos bandejas de salida y la auditoría.
  - Las horas y los decimales se guardan como texto exacto.
  - Los hijos se ordenan en Go, por el orden GUID de SQL Server.

## Decisiones propuestas (pendientes de confirmar)

1. **`Employment` vive en RRHH** y absorbe los campos de RRHH del rol `Employee`. Parties conserva
   la persona, el rol y la relación `Employment` (afiliación). Contratar exige que la persona esté
   afiliada al empleador: la afiliación es lo que la hace visible para los usuarios del empleador.
2. **Un único catálogo de convenios** (fusión de `ConvenioColectivo` y `CollectiveAgreement`). Los
   convenios de empresa llevan su organización y solo los ve su ámbito.
3. **Nóminas queda fuera:** `RateType`, `DeductionType`, las cuentas de nómina y de la Seguridad
   Social, Mod190 y la parte retributiva de `EmployeeProfile`.
4. **La baja no termina la afiliación en Parties.** La propuesta es que Parties consuma
   `hr.employee-terminated.v1` y cierre la relación `Employment`. Así se evita una llamada
   síncrona entre contextos.
5. **`WorkSchedule` se aplaza** hasta tener un consumidor (control horario o Nóminas).

## Validación

- **Dominio:**
  - catálogos sembrados (6, 9, 57, 23 y 8) y convenios válidos;
  - un titular a la vez, vacante derivada, línea principal única, no reportar a sí mismo, cierre
    que deja el puesto;
  - alta limitada a un año vista;
  - contrato dentro de la relación, un principal a la vez, baja que cierra los contratos;
  - centro cerrado que no puede ser sede.
- **Extremo a extremo** (Parties, Geografía, Instalaciones y RRHH sobre el mismo backend, en
  memoria y en SQLite migrada, por HTTP):
  - sede única y código único;
  - instalación de otra organización: 404;
  - contratar a alguien no afiliado: 422; contratarlo dos veces: 422; número de otra persona: 422;
  - empleador ajeno: 404; lector sin escritura: 403;
  - el puesto de un departamento pertenece a la organización interna;
  - titular no contratado: 422; ciclo en la jerarquía: 422;
  - organigrama de tres niveles, búsqueda de vacantes y de puestos de una persona;
  - `Staff` con el contrato principal;
  - un centro con contratos vigentes no se cierra;
  - la baja cierra los contratos y deja el puesto vacante;
  - eventos v1 consumidos con inbox, y auditoría con el actor.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con los cuatro contextos migrados en
  la misma base:
  - catálogos sembrados, ida y vuelta de fechas civiles y horas decimales;
  - EXISTS y NOT EXISTS sobre los hijos;
  - organigrama, baja en cascada, `Staff` y cierre del centro.

## Pendiente

- Confirmar las decisiones 1–5.
- Casos de uso para los convenios de empresa (ahora solo hay semilla) y para `WorkSchedule`.
- Catálogos para los códigos que siguen siendo texto libre acotado: tipo de contrato, causa de
  baja, categoría y grupo salarial.
- Si una unidad cambia de organización interna, sus puestos conservan la anterior. Es el mismo
  pendiente que la reafiliación en Parties.
- Importar los datos de C#: el rol `Employee`, `LaborContract` y `WorkCenter`, con estos casos
  de uso.
- Contexto Nóminas, consumidor de `Staff` y de los eventos v1.
