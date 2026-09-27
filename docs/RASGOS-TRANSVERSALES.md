# Evaluación de los rasgos transversales (Fw C# → Go)

Evaluación de los «adjetivos» de `Fw.Domain.Contracts/LenguajeUbicuo/Adjetivos` y de la clase base
`BusinessEntity` (`Fw.Domain/DDD Patterns/Tacticos/01Entity`) antes de traducirlos. Se usa el
mismo método que en [LENGUAJE-UBICUO.md](LENGUAJE-UBICUO.md).

## Método

| Criterio | Peso |
|---|---|
| **U** Utilidad (uso real en contextos, sin Fw ni tests) | 40 % |
| **C** Corrección de la implementación | 25 % |
| **D** Diseño de la firma | 20 % |
| **G** Encaje en Go | 15 % |

Puntuación de 1 a 5 por criterio; nota ponderada sobre 100. **Mantener** ≥ 70 · **Modificar** 45–69 ·
**Retirar** < 45 · **Mover** · **Sustituido**.

Para medir el uso se distinguen las **declaraciones** (la interfaz aparece en la herencia de un
contrato) de las **llamadas reales** a sus métodos, porque `BusinessEntity` hace que cientos de
contratos declaren interfaces que nadie invoca.

## Hallazgo principal

`BusinessEntity` implementa ocho interfaces (`IToggleable`, `IInternalValidatable`, `ILogable`,
`IAuditable`, `IAuthorizable`, `ITrazable`, `IExtensible`, `IHasDomainEvents`) y la heredan 512
archivos. Pero:

| Rasgo | Declaraciones | Llamadas reales |
|---|---|---|
| `IAuthorizable` | 374 | `AuthorizeRole` 0 · `IsAuthorized` 0 |
| `ITrazable` | 377 | `GetTraceId` 0 |
| `IAuditable` | 387 | `GetAuditTrail`, `GetCreatedBy`, `GetLastModifiedBy` 0 (solo `EfUnitOfWork`) |
| `ILogable` | 521 | 165 implementaciones repetitivas de `GetLogEntityName`; `LogEntityEvent` 0 |
| `IExtensible` | vía `BusinessEntity` | `RegisterProperty` 0 |

La auditoría que sí funciona la escribe **`EfUnitOfWork`** a partir del change tracker de EF
(diferencias de campos, actor, canal, procedencia de importación), no la entidad.

## Resultados

| # | Rasgo | Uso real | U | C | D | G | Nota | Decisión |
|---|---|---|---|---|---|---|---|---|
| 1 | `INamed` / `CurrentName` | 44 / 241 | 5 | 4 | 4 | 4 | **88** | Mantener → `traits.Named` |
| 2 | `IToggleable` / `IActivable` (`Enable`/`Disable`) | 276 decl., 77 llamadas, `IsActive` 1.219 | 5 | 4 | 3 | 4 | **84** | Mantener → `traits.Activation` |
| 3 | `IDescriptable` / `Description` | 102 / 636 | 5 | 3 | 3 | 4 | **79** | Mantener → `vocab.Description` + `traits.Describable` |
| 4 | `IValidator<T>` | 10 | 3 | 4 | 4 | 4 | **72** | Mantener → `domain.Validator[T]` |
| 5 | Vigencia: `IExpirable`, `ExpirationDate` (211), `StartingDate`, `ExpirationInfo`, `ITimeScoped`, `IHistoriableStartEnd`, `ITimeBounded`, `StartTermDate` | 211 archivos | 5 | 2 | 2 | 4 | **70** | Modificar: siete tipos solapados → `traits.Validity` sobre `vocab.ValidPeriod` |
| 6 | `ITesteable` | 20 decl., 71 usos de `IsTest` | 3 | 4 | 3 | 4 | **68** | Modificar → `traits.TestFlag` |
| 7 | `IAuditable` (quién y cuándo creó y modificó) | `SetCreatedByUser` 23 | 5 | 2 | 2 | 3 | **67** | Modificar → `traits.Audited`, sellado por la aplicación |
| 8 | `ImportProvenance` / `ImportContext` | 5 | 3 | 4 | 3 | 2 | **62** | Modificar → `application.WithImportProvenance` |
| 9 | `OperationChannel` | usado por la auditoría de EF | 3 | 4 | 3 | 2 | **62** | Modificar → `application.WithChannel` |
| 10 | Regulación: `IRegulated`, `IRegulationSource`, `ICountryDelimited`, `IRegionDelimited`, `IsoRegionCode` | 2 / 3 / 0 / 0 / 1 | 2 | 3 | 3 | 4 | **55** | Modificar → `vocab.RegulationSource`, `vocab.RegionCode`, `traits.Regulated` |
| 11 | `IComentable` / `Remark` | 20 / 58 | 3 | 2 | 2 | 4 | **54** | Modificar → `vocab.Remark` (sin texto por defecto) |
| 12 | `IInternalValidatable` (validar después de construir) | 386 decl., 48 llamadas | 4 | 3 | 1 | 1 | 54 | Sustituido: agregados siempre válidos + `domain.Validation` |
| 13 | `BusinessEntity` (clase base) | 512 | 5 | 2 | 1 | 1 | 57 | Sustituido por composición de rasgos |
| 14 | `BoundedContextIdentifierAttribute` | 1 | 2 | 3 | 3 | 2 | **49** | Modificar → metadatos de `application.Module` (pendiente) |
| 15 | Rastro de auditoría dentro de la entidad (`AuditEntry`, `GetAuditTrail*`) | 0 (solo `EfUnitOfWork`) | 2 | 2 | 1 | 2 | 36 | Mover → `application.AuditLog`, escrito por el orquestador |
| 16 | `ILogable`, `LoggingComponent`, `IInternalLogger`, `LoggerBase` | 165 impl. repetitivas; 0 usos | 2 | 3 | 1 | 1 | **38** | Retirar (en Go, `slog.LogValuer` si hace falta) |
| 17 | `IAuditableHashChained` | 0 | 1 | 2 | 2 | 2 | **32** | Retirar (pendiente: un almacén de auditoría a prueba de manipulación) |
| 18 | `BusinessEntityEvent`, `Evnt*`, `BusinessEntityEventFactory` | 0 | 1 | 2 | 2 | 2 | **32** | Retirar |
| 19 | `EncryptedAttribute` | 0 | 1 | 2 | 2 | 2 | **32** | Retirar (pendiente: cifrado en el mapeo de persistencia) |
| 20 | `IHistoriable` (array de hitos) | 9 decl. | 1 | 2 | 1 | 3 | **31** | Retirar |
| 21 | Marcadores vacíos: `ICodificable`, `IAccountable`, `INotificable`, `IRecuperable` | 0 | 1 | 3 | 1 | 1 | **30** | Retirar |
| 22 | `IAuthorizable` + `AuthorizationComponent` (lista de permisos en la entidad) | 374 decl., 0 llamadas | 1 | 2 | 1 | 2 | **28** | Retirar → contratos de autorización (paso 3) |
| 23 | `ITrazable` + `TraceComponent` | 377 decl., 0 llamadas | 1 | 2 | 1 | 1 | **25** | Retirar → observabilidad (OpenTelemetry) |
| 24 | `IExtensible`, `TypeRef`, `DomainTypeDiscoveryService`, aprobación | 0 | 1 | 2 | 1 | 1 | **25** | Retirar (pendiente: atributos extendidos declarativos) |
| 25 | Atributos de formato (`PascalCase`, `CamelCase`...) y sus validadores | 0 | 1 | 2 | 1 | 1 | **25** | Retirar |

> Nota (2026-09-27, ADR 0010 del C#): en C# `IInternalLogger`/`LoggerBase` son la infraestructura de log del Fw y se mantienen; `ILogable` queda reducido a un log por entidad guardada en `EfUnitOfWork`. En Go la decisión no cambia: el log es `pkg/log` y no forma parte de la entidad.

## Defectos encontrados (justifican la columna C)

- **La auditoría de la entidad siempre registra `Actor.System`.** `CreateAuditComponent` fija
  `UserRole = Actor.System` y `RecordAuditChange` usa `UserRole`. El propio `EfUnitOfWork` lo
  documenta y tiene que corregirlo leyendo `ActorContext`. Por el mismo motivo, los eventos
  `OnEnabled`/`OnDisabled` de `BusinessEntity` se atribuyen siempre al sistema.
- **Dos `AuthorizationComponent` con semánticas distintas.** El de `Fw.Domain.Contracts`
  interpreta la ausencia de operaciones como el comodín `*`; el de `Fw.Domain` ignora el comodín
  y no hace nada. Además, ninguno se persiste: los permisos se pierden al recargar la entidad.
- **`TraceComponent` guarda en cada instancia** el nombre de la máquina, el proceso, el hilo, el
  consumo de memoria y las recolecciones del GC. Usa un identificador de sesión `[ThreadStatic]`,
  que en código asíncrono se comparte entre peticiones que reutilizan el mismo hilo.
- **`Sha256HashProvider.Equals` lanza `NotImplementedException`**, y la semilla del primer eslabón
  de la cadena es aleatoria, así que la cadena no se puede verificar desde fuera.
- **`Remark.Default` es el texto «Party role»:** un concepto de Parties dentro del Fw.
- **Vigencia:** siete tipos que se solapan, todos con `DateTime.UtcNow` directo y sin reloj
  inyectable (`ExpirationDate` ya aparece en la evaluación del lenguaje ubicuo).
- **`IExtensible`** registra métodos como delegados en tiempo de ejecución: no se pueden
  persistir ni auditar, y `TypeRef.Resolve` recorre todos los ensamblados por reflexión.

## Diseño en Go

Cada agregado **compone** solo los rasgos que necesita; ningún rasgo lanza eventos ni lee estado
ambiental.

```go
type Party struct {
	domain.BaseAggregateRoot[PartyID]
	traits.Activation // IsActive / Activate / Deactivate (indican si hubo cambio)
	traits.Audited    // quién y cuándo creó y modificó (lo sella el orquestador)
	...
}

func (p *Party) Deactivate() {
	if p.Activation.Deactivate() { // el agregado decide qué evento lanza
		p.Raise(PartyDeactivated{EventMeta: p.NewEventMeta()})
	}
}
```

| Rasgo Go | Qué hace |
|---|---|
| `traits.Activation` | Estado activo/inactivo; su valor cero es «activo», como el valor por defecto del C# |
| `traits.Validity` | Vigencia sobre `vocab.ValidPeriod` (semiabierta, con el reloj del dominio): `ExpireAt`, `Reopen`, `ExpiresWithin` |
| `traits.Audited` + `traits.Stamp` | Sello de creación y modificación. La interfaz está **sellada**: solo lo escribe el orquestador, con el actor del contexto |
| `traits.Snapshotter` + `traits.Diff` | El agregado expone sus campos auditables; el orquestador calcula las diferencias (lo que en C# hacía el change tracker) |
| `traits.TestFlag` | Marca de datos de prueba |
| `traits.Named`, `Describable`, `Commentable`, `Regulated` | Interfaces de capacidad (solo lectura) |
| `domain.Validator[T]`, `domain.ValidateAll` | Validaciones externas o configurables |

**Contexto de la operación** (`application`): `WithActor`/`ActorFrom`, `WithChannel`/`Channel` y
`WithImportProvenance`/`ImportProvenanceFrom`. Sustituyen a `ActorContext`, `OperationChannel` e
`ImportContext`, que usaban `AsyncLocal`. `distribution.TenantActorContext` los rellena desde las
cabeceras `X-Actor-ID`, `X-Actor-Name` y `X-Channel` del gateway de confianza.

**Registro de auditoría** (`application.AuditLog`): con `orchestration.WithAuditLog`, cada alta,
modificación y baja escribe un `AuditRecord` en la **misma transacción** que el cambio. El registro
incluye actor, canal, procedencia de importación, correlación, versión, campos cambiados y eventos
lanzados. Hay implementaciones en memoria, SQL (`audit_log` con DDL para los cinco motores) y
`hotswap`. Las columnas del sello se mapean con `sqlrepo.WithAuditColumns`,
`sqlrepo.AuditStampValues` y `Row.AuditStamp`.

## Validación

- Tests unitarios de cada rasgo, del sellado, del cálculo de diferencias y del rollback del
  registro de auditoría junto con la operación.
- Batería de conformidad del `AuditLog` SQL (`sqlconformance.RunAuditLog`) y viaje completo del
  sello en Parties: pasan en SQLite, PostgreSQL, SQL Server, Oracle y MySQL.
- Ejemplo Parties de extremo a extremo: el actor llega por cabecera HTTP, se sella la
  modificación y el rastro recoge actor, canal, campo cambiado y evento.
- **Defecto encontrado al validar en MySQL:** un agregado sin sellar guardaba la fecha cero de Go;
  MySQL la rechaza y los demás motores la aceptaban en silencio como año 0001. Ahora se guarda
  `NULL`.
