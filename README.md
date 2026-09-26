# Karpo.Fw.Go

[![License: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/jhermoso/karpo-fw-go)](https://goreportcard.com/report/github.com/jhermoso/karpo-fw-go)

Framework base en Go para la plataforma **Karpo**: traducción idiomática del framework C#
(`Paranoia.Karpo.Fw.*`) basada en **Domain-Driven Design**, **Clean Architecture**, **CQRS**,
**inversión de dependencias** y **bajo consumo de recursos**.

Puntos clave:
- **Contratos DDD en el dominio**, con implementaciones genéricas por composición (Go no tiene
  clases abstractas): identidad tipada, agregados con concurrencia optimista y eventos,
  errores tipados.
- **Especificaciones como árbol de expresión**: la misma especificación se evalúa en memoria y
  se **traduce a SQL** (filtros, `EXISTS` sobre colecciones hijas, paginación y orden en la
  base de datos). Las reglas a medida se traducen por motor.
- **Persistencia agnóstica**: un contrato de repositorio en el dominio y una implementación SQL
  genérica con **un paquete por motor** (SQLite, PostgreSQL, SQL Server, Oracle, MySQL), sin
  importar drivers.
- **Cambio de base de datos en caliente** sin cortar transacciones en curso.
- **Batería de conformidad**: toda implementación del repositorio debe demostrar que cada
  especificación devuelve en la base de datos exactamente lo mismo que en memoria.

📖 Diseño completo: [docs/ARQUITECTURA.md](docs/ARQUITECTURA.md) · Inventario de patrones: [docs/INVENTARIO-PATRONES.md](docs/INVENTARIO-PATRONES.md)

---

## 🏛️ Capas

$$\text{Distribution} \longrightarrow \text{Application} \longrightarrow \text{Domain} \longleftarrow \text{Persistence (adaptadores)}$$

```
pkg/
├── domain/               # CONTRATOS DE DOMINIO (= Fw.Domain.Contracts; puro: solo biblioteca estándar)
│   ├── identifier.go     # Identifier, UUID v7 monótono, LongID, UUIDBacked/LongBacked
│   ├── entity.go         # Entity[ID], BaseEntity, SameIdentity
│   ├── aggregate.go      # AggregateRoot[ID] (sellada), BaseAggregateRoot: versión + eventos
│   ├── event.go          # Event, EventMeta
│   ├── errors.go         # Taxonomía de errores + Validation (notificación)
│   ├── value_object.go   # Guía de value objects idiomáticos
│   ├── factory.go        # Factory[T,P]
│   ├── repository.go     # ReadRepository / WriteRepository / Repository / UnitOfWork
│   ├── page.go           # PageRequest[T], Page[T]
│   └── spec/             # Especificaciones: árbol de expresión + campos tipados
│
├── application/          # CONTRATOS DE APLICACIÓN (= Fw.Application.Contracts)
│   ├── cqrs.go           # Handler[In,Out], Middleware, Chain
│   ├── ports.go          # Publisher, Dispatcher, EventHandler, EventRecorder, EventDecoder,
│   │                     # OutboxStore/OutboxMessage, IdempotencyStore, Validatable...
│   ├── module.go         # Module, Starter, Stopper (bounded contexts)
│   ├── context.go, dto.go
│   │   ── implementaciones ──
│   ├── pipeline/         # Validating, Transactional, RetryOnConflict, Idempotent, Logging
│   ├── orchestration/    # Orchestrator + Execute (carga→comportamiento→guardado→eventos)
│   ├── outbox/           # Recorder (outbox transaccional) + Relay
│   └── hosting/          # Host (ciclo de vida de módulos por dependencias)
│
├── log/, cache/, time/   # contratos transversales (implementaciones en subpaquetes)
├── distribution/         # DISTRIBUCIÓN (HTTP, RFC 9457, correlación, health)
├── events/               # Registry + suscripción tipada; inprocess/ (Dispatcher en memoria)
│
├── persistence/          # ADAPTADORES
│   ├── memory/           # Repositorio, UoW con rollback, outbox, idempotencia en memoria
│   ├── sqlrepo/          # Repositorio SQL genérico + traductor de especificaciones
│   │   ├── sqlite/ postgres/ sqlserver/ oracle/ mysql/   # un dialecto por motor
│   │   └── sqlconformance/                                # conformidad para dialectos
│   └── hotswap/          # Cambio de backend en caliente
│
└── testing/
    ├── archtest/         # Guardián de arquitectura (contratos solo importan contratos)
    ├── repotest/         # Batería de conformidad del contrato de repositorio
    └── testkit/          # Arnés: reloj falso, bus, store y outbox en memoria

examples/parties/         # Contexto delimitado completo (dominio→HTTP) con cambio en caliente
integration/              # Módulo aparte: pruebas contra PostgreSQL, SQL Server, Oracle, MySQL
```

---

## 🚀 Uso rápido

```go
// Dominio
type PartyID struct{ domain.UUID }

var (
	LegalName = spec.Text[*Party]("legal_name", (*Party).LegalName)
	Active    = spec.Comparable[*Party, bool]("active", (*Party).Active)
)

// Aplicación: la especificación se ejecuta en la base de datos
page, err := parties.FindPage(ctx,
	Active.Eq(true).And(LegalName.ContainsFold("acme")),
	domain.NewPageRequest(1, 50, LegalName.Asc()))

// Composición: cualquier motor, intercambiable en caliente
sw := hotswap.New(sqlserver.Open(sqlDB))
parties := hotswap.Repository(sw, infrastructure.RepositoryFactory)
// ...
sw.Swap(ctx, postgres.Open(pgDB))
```

---

## 🧪 Compilar y probar

Requisitos: **Go 1.27+**.

```powershell
go test ./...                      # unitarias, arquitectura, conformidad en memoria y SQLite, e2e
go test ./... -cover
./integration/run.ps1 -Down        # (Docker) conformidad + cambio en caliente en PostgreSQL, SQL Server, Oracle y MySQL
```

---

## 🗺️ Roadmap

Consulta [BACKLOG.md](BACKLOG.md).

## 📄 Licencia

**GNU General Public License v2.0** (GPL-2.0). Consulta [LICENSE](LICENSE).
