# Evaluación del lenguaje ubicuo común (Fw C# → Go)

Evaluación de cada término de `Fw.Domain.Contracts/LenguajeUbicuo/Sustantivos` y
`Fw.Domain/LenguajeUbicuo/SustantivosComunes` antes de traducirlo. No se traduce nada por
inercia: cada término se puntúa por su firma, su implementación y su uso real en Karpo.

## Método

| Criterio | Peso | Qué mide |
|---|---|---|
| **U** Utilidad | 40 % | Uso real en los contextos (ErpKernel, ErpDetail, sectoriales; sin Fw ni tests) y necesidad en un ERP |
| **C** Corrección | 25 % | Defectos de la implementación actual |
| **D** Diseño de la firma | 20 % | Cohesión, responsabilidades, API |
| **G** Encaje en Go | 15 % | Si Go ya lo resuelve o si la forma C# no tiene sentido |

Cada criterio se puntúa de 1 a 5; la nota final es la media ponderada sobre 100.

**Decisiones:** **Mantener** (≥ 70) · **Modificar** (45–69) · **Retirar** (< 45) ·
**Mover** (útil pero no pertenece al Fw) · **Sustituido** (Go ya lo cubre con otro diseño).

Uso medido: "archivos" = ficheros de contextos que nombran el tipo; `new` = construcciones reales
(distingue un value object de una simple propiedad `string` con el mismo nombre).

## Resultados

| # | Término | Uso real | U | C | D | G | Nota | Decisión |
|---|---|---|---|---|---|---|---|---|
| 1 | `IClock` / `SystemClock` | 15 archivos | 5 | 4 | 4 | 5 | **91** | Mantener (ya hecho: `domain.Clock`) |
| 2 | `Actor` | 31 archivos | 5 | 4 | 3 | 3 | **81** | Mantener (con `PartyID` tipado) |
| 3 | `FactReference` | 69 archivos, 10 `new` | 4 | 4 | 3 | 4 | **76** | Mantener |
| 4 | `Name` (+ formatos Normal / FirstCapitalized / AllCapitalized) | 125 `new` | 5 | 2 | 2 | 3 | **67** | Modificar |
| 5 | `Url` | 0 | 2 | 4 | 4 | 5 | **67** | Modificar (envoltorio fino de `net/url`) |
| 6 | `Tag` / `TagSet` | 6 / 5 archivos | 2 | 4 | 4 | 5 | **67** | Modificar (admitir conjunto vacío) |
| 7 | Excepciones de dominio (`BusinessRule`, `Invariant`, `StateConflict`, `ConcurrencyConflict`) | 23 / 1 / 3 / 0 | 4 | 4 | 3 | 1 | 67 | Sustituido (`RuleViolationError`, `ConflictError`) |
| 8 | `DateValue` (fecha sin hora) | 2 (`DateOnly`: 136 archivos) | 4 | 2 | 3 | 4 | **66** | Modificar → `Date` civil |
| 9 | `ValidPeriod` | 0 (vigencias: `ExpirationDate` en 218 archivos) | 4 | 2 | 3 | 4 | **66** | Modificar: unificar vigencias |
| 10 | `IValidationResult` / `IValidationResultFactory` | 1.725 líneas | 5 | 3 | 2 | 1 | 66 | Sustituido (`domain.Validation` + `error`) |
| 11 | `ActorContext` | 16 usos | 4 | 4 | 2 | 1 | **63** | Modificar → `context.Context` |
| 12 | `OrganizationName` | 0 `new` | 3 | 3 | 3 | 4 | 63 | Mover a Parties |
| 13 | `Milestone` | 10 `new`, 32 archivos | 4 | 2 | 2 | 3 | **59** | Modificar → fusionar en `ValidPeriod` |
| 14 | `IdentificationNumber` + `DocumentIdentificationType` | 0 | 3 | 2 | 3 | 4 | **58** | Modificar (validadores por país) |
| 15 | `Email` | 1 `new` | 3 | 2 | 3 | 4 | **58** | Modificar |
| 16 | `FactIssuedDomainEvent` | 26 archivos | 3 | 3 | 2 | 3 | 56 | Mover a ErpKernel.Documents |
| 17 | `PostalCode` | 0 `new` (solo `string`) | 2 | 3 | 3 | 4 | **55** | Modificar cuando exista `Address` |
| 18 | `EventType` (VO) | 4 `new` | 3 | 4 | 2 | 1 | 55 | Sustituido (`Event.EventType()`) |
| 19 | `Telephone` | 0 | 3 | 1 | 3 | 4 | **53** | Modificar |
| 20 | `Percentage` | 0 `new` (se usa `decimal`) | 2 | 3 | 3 | 3 | **52** | Modificar (sobre `Decimal`) |
| 21 | `Gender` / `MaritalStatus` | 2 / 0 `new` | 2 | 3 | 2 | 4 | 51 | Mover a Parties |
| 22 | `PersonalName` | 3 `new` | 3 | 2 | 2 | 3 | 51 | Mover a Parties y rediseñar |
| 23 | `PassportNumber` | 0 | 1 | 3 | 3 | 4 | **47** | Retirar (fusionar en `IdentificationNumber`) |
| 24 | `Error` / `ErrorCode` / `ErrorMessage` / `ErrorFactory` / `IError` | 8–9 archivos | 3 | 2 | 2 | 1 | 45 | Sustituido (`FieldError`, `RuleViolationError`); se conserva la convención de códigos |
| 25 | `IRemark` | 0 | 1 | 3 | 2 | 4 | **43** | Retirar |
| 26 | `InvalidNameFormatException`, `NameLengthException` | 3 | 2 | 3 | 2 | 1 | 42 | Sustituido (`ValidationError`) |
| 27 | `IRoleAuth`, `IdRoleAuth`, `IdUserAuth` | 1 / 1 / 6 | 2 | 3 | 1 | 2 | 41 | Mover a contratos de autorización |
| 28 | `UTCDateTime` | 4 | 2 | 2 | 1 | 1 | **33** | Retirar (`time.Time` en UTC + `domain.Now`) |
| 29 | `SocialSecurityNumber` | 3 | 1 | 1 | 2 | 4 | **33** | Retirar |
| 30 | Formatos técnicos de `Name` (Pascal/Camel/Kebab/Snake) y sus conversiones | 1 uso (Pascal) | 1 | 2 | 1 | 2 | **28** | Retirar |
| 31 | `NameFactory` | 0 | 1 | 2 | 1 | 1 | **25** | Retirar |
| 32 | `Icon` | 1 (icono de `BoundedContext`) | 1 | 1 | 1 | 2 | **23** | Retirar del dominio (metadato de módulo) |

## Defectos encontrados (justifican la columna C)

- **`Email`**: hay **dos implementaciones duplicadas** (Contracts y Fw.Domain). La de Contracts
  lanza `NotImplementedException` en `Equals(IEmail)`. `Value` se pasa a minúsculas pero
  `LocalPart` y `Domain` conservan las mayúsculas originales. Solo acepta ASCII.
- **`Name`**: `Name.Empty` salta la validación y crea un estado inválido. Máximo de 100 caracteres
  en `Name` frente a 255 en `NameFactory`. Hay dos algoritmos distintos de kebab/snake case: `Name`
  parte por mayúsculas y la factoría no. El formato forma parte de la igualdad:
  `Name("Acme", Normal) != Name("Acme", FirstCapitalized)`. `AllCapitalized` rechaza nombres reales
  como «Juan de la Fuente». Mezcla un valor de negocio con utilidades de nombres técnicos
  (camel, kebab, snake) que no se usan en ningún contexto.
- **`Telephone`**: al quitar los separadores, la expresión regular toma con avidez 3 dígitos de
  prefijo: `+34 600 111 222` → país **346**. `Equals(ITelephone)` lanza `NotImplementedException`.
  La detección del tipo casi siempre devuelve `Unknown`.
- **`IdentificationNumber`**: el control por letra del CIF calcula `'A' + (control - 1)` (para
  control 0 da `'@'`); la tabla correcta es `JABCDEFGHI`. El país está fijado a `ES` con `if` en el
  constructor, y `TaxID` y `CompanyID` se solapan.
- **`SocialSecurityNumber`**: usa el formato de EE. UU. (`XXX-XX-XXXX`); el NSS español tiene 12
  dígitos. Además, `IdentificationNumber` ya cubre la Seguridad Social.
- **`UTCDateTime`**: `SpecifyKind(Utc)` sobre una hora local la **etiqueta** como UTC sin
  convertirla (desplazamiento silencioso). Guarda el offset horario del servidor dentro de un valor
  de dominio y lo incluye en la igualdad.
- **`ValidPeriod` / `Milestone`**: usan `DateTime.UtcNow` directamente, así que no se pueden
  probar e ignoran `IClock`. `ElapsedYears` compara `DayOfYear` y falla en años bisiestos.
- **`ExpirationDate`** (adjetivo `Expirable`, 218 archivos): `_minValidDate` y `_maxValidDate` son
  **estáticos**, así que se calculan una vez al arrancar el proceso y envejecen con él. `Never`
  devuelve `UtcNow + 50 años`, un valor distinto en cada llamada (dos «nunca» no son iguales). El
  operador explícito `DateTime → ExpirationDate` valida «futuro», de modo que rehidratar con él una
  fecha pasada falla; la rehidratación correcta es `UnsafeRehydrate`.
- **`PassportNumber`**: valida con `^[A-Z0-9]+$` **antes** de pasar a mayúsculas, así que rechaza
  una entrada en minúsculas que luego habría normalizado.
- **`PostalCode`**: `Value` guarda la entrada sin normalizar y `FormattedValue` la normaliza, así
  que `sw1a1aa` y `SW1A 1AA` son el mismo código pero no son iguales.
- **`Percentage`**: limitado a 0–100, así que no sirve para recargos ni variaciones de más del
  100 % ni para valores negativos. `+` recorta a 100 en silencio y oculta errores.
- **`ErrorMessage`**: la expresión regular rechaza comillas, guiones, `%` y `/`, de modo que
  «Can't» o «IVA-21%» no son mensajes válidos. `ErrorCode` expone `Layer` y `Origin`, que siempre
  están vacíos.
- **`IValidationResultFactory`**: se inyecta en cada entidad solo para construir objetos
  triviales (un localizador de servicios encubierto), y el contrato tiene una errata (`IsVaLid`).
- **`PersonalName`**: un solo apellido (en España se usan dos). Fuerza la mayúscula inicial en
  cada parte («McDonald» → «Mcdonald»). `UpdateCurrentName` lanza `NotImplementedException`.
- **`Icon`**: hace E/S de ficheros dentro del dominio. `FromFile` y `FromStream` fijan 64×64 píxeles
  sea cual sea la imagen. Es un `record struct` con `byte[]`, así que la igualdad compara
  referencias.
- **`FactIssuedDomainEvent`**: es un evento concreto de Documents/Accounting que vive en el Fw.

## Decisiones tomadas

1. Se acepta la clasificación: `PersonalName`, `OrganizationName`, `Gender` y `MaritalStatus` pasan
   al contexto Parties.
2. `Decimal`: se usa `github.com/shopspring/decimal`, con una excepción explícita y única en la regla
   de dominio puro de `archtest`.
3. Paquete: `pkg/domain/vocab`.

## Huecos que Go obliga a cubrir (en C# vienen de serie)

| Hueco | Evidencia en Karpo | Propuesta |
|---|---|---|
| **Decimal** exacto | `decimal` en 164 archivos | Tipo decimal en el dominio (**decisión pendiente**, ver abajo) |
| **Money** / **CurrencyCode** (ISO 4217) | `Currency` en 74 archivos; `Money` en MaccorpKernel | `Money{Amount Decimal; Currency}` |
| **Date** civil (sin hora ni zona) | `DateOnly` en 136 archivos | `Date{Year, Month, Day}` comparable, con JSON y SQL |
| **CountryCode** (ISO 3166-1 alfa-2) | `IsoCountryCode`; `PostalCode`, `Telephone` e `IdentificationNumber` usan `string` | Value object compartido |

## Catálogo propuesto para el Fw en Go

| Paquete | Términos |
|---|---|
| `pkg/domain` (ya existe) | `Clock`, errores, validación |
| `pkg/domain/vocab` (nuevo) | `Actor`, `Name`, `Email`, `Phone`, `URL`, `Tag`/`TagSet`, `FactReference`, `CountryCode`, `TaxID`/`IdentificationNumber` (validadores por país, empezando por ES), `Date`, `ValidPeriod` (absorbe `Milestone` y `ExpirationDate`), `Decimal`, `Money`, `CurrencyCode`, `Percentage` |
| Contexto Parties (no Fw) | `PersonalName` (nombre + dos apellidos), `OrganizationName`, `Gender`, `MaritalStatus` |
| Autorización (paso 3) | `IRoleAuth`, `IdRoleAuth`, `IdUserAuth` |
| Retirados | `UTCDateTime`, `NameFactory`, formatos técnicos de `Name`, `SocialSecurityNumber`, `PassportNumber`, `Icon`, `IRemark`; sustituidos por el diseño Go: `EventType` VO, familia `Error*`, `IValidationResult*`, excepciones de dominio |

### Guía de migración de lo sustituido (C# → Go)

| C# | Go |
|---|---|
| `_validationResultFactory.Success()` | `return nil` |
| `_validationResultFactory.Failure(msg, code)` | `v.Add(field, code, msg)` … `return v.Err()` |
| `DomainBusinessRuleException` / `DomainInvariantViolationException` | `domain.Violation(code, msg)` |
| `DomainStateConflictException` / `ConcurrencyConflictException` | `domain.Conflict(...)` |
| `new EventType(nameof(X))` | `func (X) EventType() string` |
| `UTCDateTime.Now()` | `domain.Now()` |

## Estado de la implementación en Go (`pkg/domain/vocab`)

| Tipo | Sustituye a (C#) | Qué corrige |
|---|---|---|
| `Name` + `CapitalizeWords` | `Name`, `NameFormat`, `NameFactory` | La identidad es el texto (sin formato en la igualdad); partículas en minúscula; conserva «McDonald» |
| `Email` | los dos `Email` duplicados | Un solo tipo; `LocalPart`/`Domain` derivados del valor normalizado |
| `Phone` | `Telephone` | Prefijo de país con la tabla E.164 (sin avidez); admite `00`; prefijo por defecto opcional |
| `URL` | `Url` | Envoltorio fino de `net/url` |
| `CountryCode`, `CurrencyCode` | `IsoCountryCode` y los `string` sueltos | ISO 3166-1 y ISO 4217 (con decimales por divisa) |
| `Decimal`, `Money`, `Percentage` | `decimal`, `Percentage` | Dinero con divisa, redondeo por divisa, reparto sin perder céntimos; porcentaje sin tope 0–100 ni recortes silenciosos |
| `Date` | `DateValue`, `DateOnly` | Fecha civil comparable; aritmética de meses que ajusta a fin de mes |
| `ValidPeriod` | `ValidPeriod`, `Milestone`, `ExpirationDate` | Intervalo semiabierto `[desde, hasta)`; «ahora» siempre con `domain.Now` |
| `Tag`, `TagSet` | `Tag`, `TagSet` | El conjunto puede estar vacío |
| `FactReference` | `FactReference` | — |
| `Actor` (+ `application.WithActor`/`ActorFrom`) | `Actor`, `ActorContext` | Contexto explícito en lugar de estado ambiental `AsyncLocal` |
| `Identification` + `RegisterDocumentRule` | `IdentificationNumber`, `PassportNumber`, `SocialSecurityNumber` | CIF con la tabla `JABCDEFGHI` y reglas por tipo de entidad; NSS español (control módulo 97); NIF K/L/M; normaliza antes de validar; reglas enchufables por país |

Persistencia: `sqlrepo.Row` lee `Decimal` y `Date` en todos los motores y los dialectos enlazan
las fechas civiles como fechas (`domain.DateBacked`), nunca como instantes. La batería de
conformidad incluye especificaciones sobre decimales y fechas, y pasa en SQLite, PostgreSQL,
SQL Server, Oracle (con GUIDs RFC y .NET) y MySQL.

**Hallazgo al validarlo contra Oracle real:** `go-ora` enlaza un `time.Time` como
`TIMESTAMP WITH TIME ZONE` y pone la zona de la sesión a la del cliente. Al comparar una columna
`DATE`, Oracle la convierte con esa zona y el día se desplaza. Por eso el dialecto Oracle enlaza
las fechas civiles como `TO_DATE('YYYY-MM-DD')`. Los instantes (`TIMESTAMP WITH TIME ZONE`) no se
ven afectados; si se comparten tablas del C# con columnas `DATE` o `TIMESTAMP` sin zona para
instantes, conviene fijar la zona de sesión a UTC.
