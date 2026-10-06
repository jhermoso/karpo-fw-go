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

📖 Diseño completo: [docs/ARQUITECTURA.md](docs/ARQUITECTURA.md) · Inventario de patrones: [docs/INVENTARIO-PATRONES.md](docs/INVENTARIO-PATRONES.md) · Lenguaje ubicuo: [docs/LENGUAJE-UBICUO.md](docs/LENGUAJE-UBICUO.md) · Rasgos: [docs/RASGOS-TRANSVERSALES.md](docs/RASGOS-TRANSVERSALES.md) · Autorización: [docs/AUTORIZACION.md](docs/AUTORIZACION.md) · Seguridad: [docs/SEGURIDAD.md](docs/SEGURIDAD.md) · Integración: [docs/EVENTOS-INTEGRACION.md](docs/EVENTOS-INTEGRACION.md) · Esquema: [docs/ESQUEMA-MIGRACIONES.md](docs/ESQUEMA-MIGRACIONES.md) · Parties: [docs/PARTIES.md](docs/PARTIES.md) · Geografía: [docs/GEOGRAFIA.md](docs/GEOGRAFIA.md) · Instalaciones: [docs/INSTALACIONES.md](docs/INSTALACIONES.md) · RRHH: [docs/RRHH.md](docs/RRHH.md) · Nóminas: [docs/NOMINAS.md](docs/NOMINAS.md) · Fiscal: [docs/FISCAL.md](docs/FISCAL.md) · Facturación: [docs/FACTURACION.md](docs/FACTURACION.md) · Cobros: [docs/COBROS.md](docs/COBROS.md) · Tesorería: [docs/TESORERIA.md](docs/TESORERIA.md) · Contabilidad: [docs/CONTABILIDAD.md](docs/CONTABILIDAD.md) · Pagos: [docs/PAGOS.md](docs/PAGOS.md) · Compras: [docs/COMPRAS.md](docs/COMPRAS.md) · Productos: [docs/PRODUCTOS.md](docs/PRODUCTOS.md) · Inventario: [docs/INVENTARIO.md](docs/INVENTARIO.md) · Pedidos: [docs/PEDIDOS.md](docs/PEDIDOS.md)

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
│   ├── spec/             # Especificaciones: árbol de expresión + campos tipados
│   ├── vocab/            # Lenguaje ubicuo común: Name, Email, Phone, URL, Date, ValidPeriod,
│   │                     # Decimal, Money, Percentage, CountryCode, CurrencyCode, Tag, Actor...
│   └── traits/           # Rasgos componibles: Activation, Validity, Audited, TestFlag, Snapshotter...
│
├── application/          # CONTRATOS DE APLICACIÓN (= Fw.Application.Contracts)
│   ├── cqrs.go           # Handler[In,Out], Middleware, Chain
│   ├── ports.go          # Publisher, Dispatcher, EventHandler, EventRecorder, EventDecoder,
│   │                     # OutboxStore/OutboxMessage, IdempotencyStore, Validatable...
│   ├── module.go         # Module, Starter, Stopper (bounded contexts)
│   ├── integration.go    # IntegrationEvent, Envelope, Translator, MessageSender/Handler, InboxStore
│   ├── schema.go         # SchemaMigrator (Status, Migrate, Verify)
│   ├── context.go, dto.go
│   ├── authz/            # Contrato de autorización v1: Context, Grant, Permission, Resolver...
│   │   ── implementaciones ──
│   ├── pipeline/         # Validating, Transactional, RetryOnConflict, Idempotent, Logging
│   ├── orchestration/    # Orchestrator + Execute (carga→comportamiento→guardado→eventos)
│   ├── outbox/           # Recorder (outbox transaccional) + Relay
│   ├── hosting/          # Host (ciclo de vida de módulos por dependencias)
│   ├── authorization/    # Resolver genérico sobre authz.Directory, Authenticators (varias formas de
│   │                     # autenticarse) y directorio en memoria para pruebas; el real está en contexts/security
│   └── messaging/        # Eventos de integración: Recorder (traducción), Relay, Consumer (inbox)
│
├── log/, cache/, time/   # contratos transversales (implementaciones en subpaquetes)
├── distribution/         # DISTRIBUCIÓN (HTTP, RFC 9457, correlación, health, Authorize)
│   └── jwtauth/          # Autenticación JWT HS256 (solo biblioteca estándar)
├── events/               # Registry + suscripción tipada; inprocess/ (Dispatcher en memoria)
├── messaging/inprocess/  # Transporte de eventos de integración en memoria (monolito modular)
│
├── persistence/          # ADAPTADORES
│   ├── memory/           # Repositorio, UoW con rollback, outbox, idempotencia en memoria
│   ├── sqlrepo/          # Repositorio SQL genérico + traductor de especificaciones + Migrator
│   │   ├── sqlite/ postgres/ sqlserver/ oracle/ mysql/   # un dialecto por motor
│   │   └── sqlconformance/                                # conformidad para dialectos
│   └── hotswap/          # Cambio de backend en caliente
│
└── testing/
    ├── archtest/         # Guardián de arquitectura (contratos solo importan contratos)
    ├── repotest/         # Batería de conformidad del contrato de repositorio
    └── testkit/          # Arnés: reloj falso, bus, store y outbox en memoria

examples/parties/         # Ejemplo del framework: contexto completo (dominio→HTTP) con cambio en caliente
contexts/security/        # Contexto Security (usuarios, roles, catálogo de permisos, acceso por organización, sesiones, identidades externas; authz.Directory real): ver docs/SEGURIDAD.md
contexts/parties/         # Contexto Parties real (port de ErpKernel.Parties): ver docs/PARTIES.md
contexts/geography/       # Contexto Geografía y referencia (semilla de Karpo embebida): ver docs/GEOGRAFIA.md
contexts/facilities/      # Contexto Instalaciones (ubicación propia, jerarquía, ámbito): ver docs/INSTALACIONES.md
contexts/hr/              # Contexto RRHH (puestos, relaciones laborales, contratos, centros de trabajo): ver docs/RRHH.md
contexts/payroll/         # Contexto Nóminas (nóminas con totales derivados, conceptos, perfil y reparto del neto, CCC): ver docs/NOMINAS.md
contexts/fiscal/          # Contexto Fiscal (tipos por territorio, contribuyente, modelos 111 y 190 alimentados por Nóminas): ver docs/FISCAL.md
contexts/billing/         # Contexto Facturación (facturas con desglose de Fiscal, series sin huecos, rectificativas, borradores desde los albaranes de Pedidos): ver docs/FACTURACION.md
contexts/receivables/     # Contexto Cobros (condiciones y vencimientos, cartera por factura, cobros y compensaciones, riesgo): ver docs/COBROS.md
contexts/treasury/        # Contexto Tesorería (cuentas, mandatos SEPA, remesas con pain.008, órdenes de transferencia con pain.001, cobros y devoluciones hacia Cobros): ver docs/TESORERIA.md
contexts/accounting/      # Contexto Contabilidad (plan por empresa, libro con perfil y cierre de periodos, asientos cuadrados y contraasientos, contabilización de facturas, cobros, adeudos y nóminas): ver docs/CONTABILIDAD.md
contexts/payments/        # Contexto Pagos (obligaciones de facturas recibidas, nóminas e impuestos; pagos y sus aplicaciones; pagos de las transferencias de Tesorería): ver docs/PAGOS.md
contexts/purchases/       # Contexto Compras (facturas recibidas con IVA soportado y retención, registro numerado, perfil de proveedor): ver docs/COMPRAS.md
contexts/products/        # Contexto Productos (catálogo por empresa, códigos de barras, kits, categorías, unidades, tarifas y cotización): ver docs/PRODUCTOS.md
contexts/inventory/       # Contexto Inventario (almacenes, existencias a coste medio, libro de movimientos, reservas, recuentos y traspasos; reserva y salida del stock de los pedidos): ver docs/INVENTARIO.md
contexts/orders/          # Contexto Pedidos (pedidos de venta valorados, condiciones del cliente, control de crédito, reserva de stock por eventos, albaranes): ver docs/PEDIDOS.md
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
