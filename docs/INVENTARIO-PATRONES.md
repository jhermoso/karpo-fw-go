# Inventario de patrones del Fw (Go vs Karpo C#)

Documento vivo: qué patrones tácticos, estratégicos y de aplicación tiene el framework en Go,
a nivel de **contrato** y de **implementación**, y cuáles faltan respecto a `Paranoia.Karpo.Fw.*`.

Leyenda: ✅ hecho · 🟡 parcial · ❌ falta · — no aplica en Go (decisión consciente)

> Nota de clasificación: los patrones **tácticos** de DDD viven en el dominio. Los **estratégicos**
> son de límites e integración (Bounded Context, Context Map, Shared Kernel, ACL, lenguaje
> publicado). CQRS, orquestador, outbox o pipeline son patrones **de la capa de aplicación**.

## 1. Tácticos (dominio) — contratos en `pkg/domain`

| Patrón | Contrato | Implementación | Pendiente |
|---|---|---|---|
| Identificador tipado | ✅ `Identifier`, `UUIDBacked`, `LongBacked` | ✅ `UUID` v7 monótono, `LongID` | ❌ identificador derivado (`IDerivedIdentifier`) |
| Entity | ✅ `Entity[ID]` | ✅ `BaseEntity`, `SameIdentity` | |
| Value Object | 🟡 `Equaler` + guía | — (no hace falta clase base) | |
| Aggregate Root | ✅ `AggregateRoot[ID]` sellada | ✅ versión + eventos | ❌ `IValueObjectRootAggregate` |
| Domain Event | ✅ `Event`, `EventMeta` | ✅ | ❌ `DomainEventLog` |
| Domain Service | — (marcador inútil en Go) | — | |
| Factory | ✅ `Factory[T,P]` | ✅ `FactoryFunc` | |
| Specification | ✅ árbol traducible (`spec`) | ✅ memoria + SQL ×5 | ❌ filtros HTTP → spec, serialización de `Expr` |
| Repository | ✅ `Repository`, `Read/WriteRepository` | ✅ memoria, SQL ×5, hotswap | ❌ repositorio con caché |
| Unit of Work | ✅ | ✅ memoria, SQL, hotswap | |
| Errores / validación | ✅ taxonomía + `Validation` | ✅ | ❌ reglas por configuración (`IEntityConfigRulesProvider`) |
| Clock | ✅ `Clock` | ✅ real / fake | |
| Rasgos transversales (`IAuditable`, `IAuditableHashChained`, `IAuthorizable`, `ITraceable`, `IActivable`/`IToggleable`, `IExpirable`/`ITimeScoped`/`IHistoriable`, `ICodificable`, `INamed`, `IDescriptable`, `IComentable`, `IRegulated`, `IAccountable`, `INotificable`) | ❌ | ❌ | base de `BusinessEntity`; en Go como componentes componibles |
| Extensibilidad (`BusinessEntityExtensible`, `TypeRef`) | ❌ | ❌ | |
| Lenguaje ubicuo común (`Name`, `PersonalName`, `OrganizationName`, `Email`, `Telephone`, `Url`, `PostalCode`, `Percentage`, `ValidPeriod`, `DateValue`, `Actor`, `Tag`, `EventType`, `Error`) | ❌ | ❌ | `Fw.Domain/SustantivosComunes` |

## 2. Estratégicos

| Patrón | Contrato | Implementación | Pendiente |
|---|---|---|---|
| Bounded Context | ✅ `application.Module` | ✅ `hosting.Host` | 🟡 identificador, icono, descripción, configuración |
| Shared Kernel | ✅ `pkg/domain` | ✅ | |
| Eventos de integración / lenguaje publicado | ❌ | ❌ | envelope y versionado distintos de los eventos de dominio |
| Anti-Corruption Layer | ❌ | ❌ | contrato de traductor entre modelos |
| Open Host Service | 🟡 `distribution.EndpointModule` | 🟡 HTTP | |
| Mensajería entre contextos | 🟡 `application.Publisher`/`Dispatcher` | 🟡 en proceso | ❌ adaptador de broker |
| Fronteras entre subdominios | — | 🟡 `archtest` | ❌ reglas por contexto |

## 3. Capa de aplicación — contratos en `pkg/application`

| Patrón | Contrato | Implementación | Pendiente |
|---|---|---|---|
| Command / Query handlers | ✅ `Handler[In,Out]` | ✅ | |
| Pipeline / decoradores | ✅ `Middleware`, `Chain` | ✅ `pipeline.*` | |
| Idempotencia | ✅ `IdempotencyStore`, `IdempotencyKeyed` | 🟡 memoria | ❌ almacén SQL |
| Orquestador | 🟡 sin interfaz | ✅ `orchestration` | |
| Outbox | ✅ `OutboxStore`, `EventRecorder`, `EventDecoder` | ✅ `outbox`, memoria, SQL, hotswap | ❌ varias instancias (`SKIP LOCKED`/`READPAST`) |
| Publicación / suscripción | ✅ `Publisher`, `Dispatcher`, `EventHandler` | ✅ `events/inprocess`, `events.Registry` | |
| Base de datos activa (`IActiveDatabaseTargetProvider`) | ✅ | ✅ `persistence/hotswap` | |
| DTOs / mappers | 🟡 `Mapper` | 🟡 | 🟡 `ISearchQuery` |
| Proyecciones / modelos de lectura (`IAggregateProjection`) | ❌ | ❌ | |
| Autorización (`AuthorizationContext`, resolvers, `OrganizationAccessLevel`, `PermissionCodes`, `ICurrentActorResolver`) | ❌ | ❌ | 🟡 `distribution` solo propaga ids |
| Workflow (definiciones, instancias, pasos, motor) | ❌ | ❌ | contexto completo del Fw |
| Gestión de esquema / migraciones (`IDatabaseSchemaManager`) | ❌ | ❌ | |
| Importación y referencias legadas (`ImportRun`, `LegacyReference`) | ❌ | ❌ | |
| Log de auditoría (`AuditLogEntry`) | ❌ | ❌ | |
| Log | ✅ `log.Logger` | ✅ `log/vanilla` | |
| Caché | ✅ `cache.Cache` | ✅ `cache/memory` | ❌ `CatalogCache`, repositorio con caché |
| Observabilidad (trazas, métricas) | ❌ | ❌ | |
| Contenedor de dependencias | — (punto de composición) | — | |
| Guardián de arquitectura | ✅ `archtest` | 🟡 capas y contratos | ❌ nombres, screaming architecture, subdominios |

## 4. Prioridad propuesta antes de Parties

1. Lenguaje ubicuo común (`Name`, `Email`, `Telephone`, `ValidPeriod`, `Actor`...).
2. Rasgos transversales componibles (auditable, activable, vigencia...).
3. Contratos de autorización y actor en contexto.
4. Eventos de integración.
5. Gestión de esquema / migraciones por dialecto.

Pueden esperar a un segundo contexto que los necesite: Workflow, ACL, adaptador de broker.
