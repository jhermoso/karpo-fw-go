# Arquitectura de Karpo.Fw.Go

Este documento explica cómo se han traducido a Go los patrones tácticos y estratégicos de DDD del
framework C# (`Paranoia.Karpo.Fw.*`), y cómo se consigue una persistencia **agnóstica de la
tecnología**, con **especificaciones ejecutadas en la base de datos** y **cambio de base de datos
en caliente**.

---

## 1. Principios de traducción C# → Go

Go no tiene clases abstractas, herencia ni métodos virtuales. La regla aplicada en todo el
framework es:

| En C# | En Go | Ejemplo |
|---|---|---|
| `interface IX` (contrato) | interfaz pequeña | `domain.Repository`, `spec.Specification` |
| `abstract class` con estado | struct **embebible** | `domain.BaseAggregateRoot` |
| `abstract` + métodos `*Internal` (template method) | **composición**: tipo genérico + estrategia inyectada | `sqlrepo.Repository` + `Mapping` + `Dialect` |
| `virtual` / hooks | funciones o interfaces inyectadas | `Mapping.Hydrate`, `CustomSQL` |
| restricciones `where T : ...` | restricciones de tipos genéricos | `ID domain.Identifier`, `T domain.AggregateRoot[ID]` |
| algoritmos genéricos | **funciones libres genéricas** | `spec.And`, `application.Execute`, `domain.SameIdentity` |
| excepciones | errores tipados que envuelven centinelas | `domain.NotFound`, `errors.Is(err, domain.ErrConflict)` |
| `Expression<Func<T,bool>>` | **árbol de expresión** propio (datos) | `spec.Expr` |
| MediatR / decoradores de handlers | handlers tipados + middleware genérico | `application.Chain`, `Transactional`, `Idempotent` |

> **Trampa del embebido**: una struct embebida nunca ve los métodos de la struct que la contiene
> (no hay despacho dinámico). Por eso ninguna "clase base" del framework llama a hooks
> sobrescribibles: el comportamiento variable se inyecta.

### Correspondencia de piezas

| C# (`Fw.Domain.Contracts` / `Fw.Application*`) | Go |
|---|---|
| `IBaseIdentifier<T>`, `ILongIdentifier<T>`, `ICommonBackedIdentifier` | `domain.Identifier`, `domain.UUID`, `domain.LongID`, `domain.UUIDBacked`, `domain.LongBacked` |
| `Entity<TEntity,TId>` | `domain.BaseEntity[ID]` + `domain.SameIdentity` |
| `EntityRootAggregate`, `IHasDomainEvents` | `domain.AggregateRoot[ID]` (sellada) + `domain.BaseAggregateRoot[ID]` |
| `ValueObject<T>` | structs comparables con constructor validador (`value_object.go`) |
| `IDomainEvent`, `DomainEvent<,>` | `domain.Event` + `domain.EventMeta` embebible |
| `IFactory<...>` | `domain.Factory[T,P]` / `FactoryFunc` |
| `ISpecification<T>`, `IPagedSpecification`, `IOrderBySpecification` | `spec.Specification[T]`, `domain.PageRequest[T]`, `spec.Order[T]` |
| `IRepository`, `IReadRepository`, `IWriteRepository` | `domain.Repository`, `ReadRepository`, `WriteRepository` |
| `IUnitOfWork` + `ITransactionScope` | `domain.UnitOfWork` (`Do(ctx, fn)`) |
| `IValidationResult`, `IError`, excepciones de dominio | `domain.Validation`, `ValidationError`, `RuleViolationError`, centinelas |
| `IClock` | `domain.Clock` + `domain.SetClock` |
| `ICommandHandler`, `IQueryHandler` | `application.Handler[In,Out]` (`CommandHandler`, `QueryHandler`) |
| `IdempotencyCommandHandlerDecorator`, `IIdempotencyStore` | `application.Idempotent`, `application.IdempotencyStore` |
| `OrchestratorService` | `application.Orchestrator` + `application.Execute` |
| `OutboxEvent` | `application.Outbox`, `OutboxStore`, `OutboxRelay` |
| `IBoundedContext` | `application.Module` + `application.Host` |
| `IDtoMapper` | `application.Mapper`, `domain.MapPage` |

---

## 2. Patrones tácticos

### Identidad tipada
```go
type PartyID struct{ domain.UUID }   // no se puede pasar un OrderID donde se espera un PartyID
```
`domain.NewUUID()` genera UUID v7 **monótonos** dentro del proceso: índices B-tree compactos y
orden de creación estable (el outbox lo aprovecha).

### Agregados
`domain.AggregateRoot[ID]` está **sellada**: solo la cumplen los tipos que embeben
`BaseAggregateRoot`. Así todo agregado tiene:
- versión persistida (`Version()`, 0 = nuevo) para **concurrencia optimista**;
- buffer de eventos (`Raise`, `PendingEvents`, `ClearEvents`), con metadatos que ya incluyen
  el agregado y la versión (`NewEventMeta`).

Los agregados exponen un constructor de creación (valida, emite eventos) y uno de
**reconstitución** (`Reconstitute`, valida, sin eventos) que usa la infraestructura. No hace
falta ningún setter público.

### Errores
Los errores se clasifican con centinelas (`ErrNotFound`, `ErrConflict`, `ErrValidation`,
`ErrRuleViolation`, `ErrUnsupported`...). La capa de distribución los traduce a HTTP con
`errors.Is/As` (404, 409, 400 con errores por campo, 422...), **nunca leyendo el texto del mensaje**.

---

## 3. Especificaciones ejecutadas en la base de datos

### El problema
Un predicado `func(T) bool` solo se puede evaluar en memoria: para obtener 50 registros habría
que traer el millón. El C# lo resolvía con `Expression<Func<T,bool>>`, que EF traduce a SQL.

### La solución: un árbol de expresión propio
Cada especificación tiene **dos caras que se construyen juntas y nunca divergen**:

```go
var (
	PartyName   = spec.Text[*Party]("legal_name", (*Party).LegalName)
	PartyActive = spec.Comparable[*Party, bool]("active", (*Party).Active)
	Contacts    = spec.Collection[*Party, Contact]("contacts", (*Party).Contacts)
)

s := PartyActive.Eq(true).And(
	PartyName.ContainsFold("acme"),
	Contacts.Any(ContactKind.Eq(Email)),
)
s.IsSatisfiedBy(p) // evaluación en memoria (reglas de dominio, adaptador memory)
s.Expr()           // árbol traducible: AND(active = true, LOWER(name) LIKE ..., EXISTS(...))
```

- Los campos son **tipados** (`Text`, `Ordered`, `Comparable`, `Time`, `Optional`,
  `Collection`): no se puede comparar un `int64` con un `string` ni ordenar por un campo que no
  es ordenable.
- Los nombres de campo son **lógicos**; cada adaptador los mapea a columnas.
- Los nodos son un conjunto cerrado: `Compare`, `AndExpr`, `OrExpr`, `NotExpr`, `Const`,
  `AnyExpr` (colecciones hijas → `EXISTS`) y `CustomExpr`.

### Especificaciones de negocio a medida (`spec.Custom`)
Una regla que no se puede expresar con los nodos estándar se declara con nombre, su semántica en
memoria y sus argumentos, y **cada base de datos implementa su traducción**:

```go
// dominio
func MinContacts(n int) spec.Spec[*Party] {
	return spec.Custom("parties.min_contacts", func(p *Party) bool { return len(p.contacts) >= n }, int64(n))
}

// infraestructura (una vez, válida para todos los dialectos SQL)
Custom: map[string]sqlrepo.CustomSQL{
	"parties.min_contacts": func(b *sqlrepo.Builder, args []any) (string, error) {
		id, _ := b.Column("id"); n, _ := b.Arg(args[0])
		return fmt.Sprintf("(SELECT COUNT(*) FROM party_contacts pc WHERE pc.party_id = %s) >= %s", id, n), nil
	},
},
```

**Garantía**: si un adaptador no sabe traducir una expresión (un campo sin mapear o una custom
sin traducción), devuelve `domain.ErrUnsupported`. **Nunca** recurre a cargar datos y filtrar
en memoria.

### Cómo se comprueba
`pkg/testing/repotest` es la batería de conformidad del contrato. Su test central ejecuta 47
especificaciones (todos los operadores, colecciones, custom, combinaciones, negaciones,
escapes de `%` y `_`...) y exige que el resultado de la base de datos sea **idéntico** al de
`IsSatisfiedBy` en memoria. Además, `explain_test.go` fija el SQL generado para cada dialecto
(`Repository.Explain` permite ver el SQL de cualquier consulta).

---

## 4. Persistencia agnóstica

```
                 domain.Repository[ID,T]  (contrato, capa de dominio)
                          ▲
        ┌─────────────────┼──────────────────────┐
  memory.Repository   sqlrepo.Repository     (futuros: documentos, APIs...)
                          │  Mapping (1 por agregado, sirve para todos los motores)
                          │  Dialect
      ┌────────┬──────────┼───────────┬─────────┐
    sqlite  postgres  sqlserver    oracle     mysql      (un paquete por motor)
```

- **Contrato en el dominio** (`domain.Repository`): `Get`, `Find`, `FindPage`, `Count`,
  `Exists`, `Save` (inserta si versión 0, actualiza con concurrencia optimista), `Delete`.
- **`sqlrepo`** implementa el contrato una sola vez sobre `database/sql`. Todo lo específico de
  cada motor está en `Dialect`: marcadores (`?`, `$1`, `@p1`, `:1`), comillas, paginación
  (`LIMIT/OFFSET` u `OFFSET/FETCH`), booleanos (`BOOLEAN`, `BIT`, `NUMBER(1)`), UUID (`UUID`,
  `UNIQUEIDENTIFIER` con su orden de bytes mixto, `RAW(16)` con orden RFC u orden .NET para
  compartir tablas con los servicios C#, `CHAR(36)`), escape de `LIKE`, límite de listas `IN`
  (Oracle: 1000) y detección de claves duplicadas.
- **Los dialectos no importan drivers**: el driver lo elige el punto de composición. `archtest`
  lo verifica.
- **`Mapping`** vive en la infraestructura de cada contexto delimitado: columnas, campos lógicos,
  tablas hijas (guardadas con estrategia *replace* y cargadas por lotes, sin N+1),
  `Dehydrate`/`Hydrate` y traducciones custom.
- **Unidad de trabajo**: `db.Do(ctx, fn)` abre una transacción ligada al `ctx`; repositorios y
  outbox la comparten. Si falla, también se deshace la versión en memoria del agregado.

### Diferencias semánticas entre motores (a tener en cuenta)
| Aspecto | Comportamiento |
|---|---|
| `Contains/StartsWith/EndsWith/Eq` sobre texto | siguen la **collation** del motor (SQL Server y MySQL no distinguen mayúsculas por defecto; SQLite tampoco en `LIKE`). Para no distinguir mayúsculas en todos los motores usa `EqualFold`/`ContainsFold` |
| Espacios finales | SQL Server y MySQL los ignoran en `=` |
| Cadena vacía | Oracle la trata como `NULL` |
| Orden de textos | depende de la collation; el desempate siempre es por identidad |

### Añadir otra tecnología
1. Si es SQL: implementar `sqlrepo.Dialect` en un paquete nuevo y pasar
   `sqlconformance.Run(t, db)`.
2. Si no lo es (p. ej. documentos): implementar `domain.Repository` traduciendo `spec.Expr` a su
   lenguaje de consulta y pasar `repotest.Run`. El árbol es un conjunto cerrado de nodos, así que
   el traductor es un `switch` finito.

---

## 5. Cambio de base de datos en caliente (`pkg/persistence/hotswap`)

```go
sw := hotswap.New(sqlserverDB)                                   // backend inicial
parties := hotswap.Repository(sw, infrastructure.RepositoryFactory) // domain.Repository estable
outbox  := hotswap.Outbox(sw, infrastructure.OutboxFactory)
svc     := application.NewService(parties, sw /* UnitOfWork */, ...)

// ... en caliente:
err := sw.Swap(ctx, postgresDB)
```

Garantías (cubiertas por tests):
- cada operación **arrienda** el backend actual mientras dura;
- una unidad de trabajo **fija** su backend en el `ctx`: todas sus operaciones van a la misma
  base de datos aunque entre medias se haga un `Swap`;
- `Swap` redirige inmediatamente las operaciones nuevas, **espera** a que terminen las que usan
  el backend anterior y después lo **cierra**. Si vence el `ctx`, el redireccionamiento se
  mantiene y el cierre se hace al drenar;
- los adaptadores se construyen perezosamente por backend (`Bind`), así que se puede cambiar
  **de tecnología** (memoria → SQLite → SQL Server → PostgreSQL...), no solo de servidor;
- tráfico concurrente con 20 cambios seguidos: cero fallos.

**Fuera de alcance**: mover los datos. `Swap` cambia conexiones de forma atómica y segura; la
replicación, la doble escritura o la carga previa del destino son operativas (herramientas del
motor, CDC, o un proceso de backfill antes del `Swap`).

---

## 6. Capa de aplicación

- **Handlers tipados** (`Handler[In,Out]`) y **middleware genérico** (`Chain`): `Validating`,
  `Transactional`, `RetryOnConflict`, `Idempotent`, `Logging`. Sin mediador por reflexión: el
  compilador comprueba cada par comando → resultado.
- **Orchestrator**: carga el agregado **dentro** de la transacción, ejecuta el comportamiento,
  guarda con concurrencia optimista y registra los eventos **en la misma transacción**
  (outbox). `application.Execute` devuelve un resultado tipado.
- **Outbox + Relay**: entrega *at-least-once* con reintentos, `correlation_id` y `causation_id`.
  Los eventos se deserializan con `events.Registry` y se suscriben con tipo
  (`events.Subscribe[E]`).
- **Módulos** (`Module`, `Host`): equivalen a `IBoundedContext`; declaran dependencias
  (`Fw ← ErpKernel ← ErpDetail ← ...`), se ordenan y detectan ciclos.

---

## 7. Guardián de arquitectura

`pkg/testing/archtest` verifica en cada `go test`:
1. **Dominio puro** (lista blanca): solo biblioteca estándar y el propio árbol `pkg/domain`.
2. **Aplicación**: sin distribución, sin persistencia, sin `database/sql`, sin terceros.
3. **Distribución**: no alcanza la persistencia.
4. **Persistencia**: agnóstica de drivers (sin dependencias de terceros).
5. **Eventos**: independientes de aplicación y persistencia.

Los contextos delimitados pueden reutilizarlo (ver `examples/parties/parties_test.go`).

---

## 8. Ejemplo completo

`examples/parties` es un contexto delimitado completo (dominio, aplicación, infraestructura,
HTTP) cuyo test de extremo a extremo arranca sobre memoria, **cambia en caliente a SQLite** con
el servicio en marcha, repite el escenario (validaciones 400, reglas 422, idempotencia, búsquedas
con especificaciones de colección y custom, paginación) y entrega los eventos del outbox a
suscriptores tipados.

## 9. Pruebas contra servidores reales

`integration/` es un módulo aparte (los drivers no entran en el framework). Con Docker:

```powershell
./integration/run.ps1 -Down
```

levanta PostgreSQL 17, SQL Server 2022, Oracle 23 Free y MySQL 8.4 y ejecuta en todos ellos:

- la batería de conformidad completa (las 47 especificaciones dan en SQL exactamente lo mismo que
  en memoria), también en Oracle con GUIDs en orden .NET (`oracle.WithDotNetGUIDs`);
- el contexto Parties (tabla hija, especificación custom con `COUNT`, NIF único en la base de datos);
- `TestHotSwapAcrossEngines`: un único repositorio de dominio que cambia en caliente
  PostgreSQL → SQL Server → Oracle → MySQL con una unidad de trabajo abierta en cada cambio.

Todo ello pasa en verde contra los cuatro motores.
