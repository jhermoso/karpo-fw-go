# Karpo.Fw.Go

[![License: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/jhermoso/karpo-fw-go)](https://goreportcard.com/report/github.com/jhermoso/karpo-fw-go)

Framework base en Go para la plataforma **Karpo**.

Diseñado bajo principios de **Domain-Driven Design (DDD)**, **Clean Architecture**, **Inversión de Dependencias (DIP)** y **bajo consumo de recursos** (< 30 MB por servicio), optimizado para ser la base de microservicios de dominio (slices/publishers) e interactuar con herramientas de generación y Low-Code asistidas por LLMs.

---

## 🏛️ Filosofía y Arquitectura por Capas

El framework estructura los patrones en paquetes independientes que garantizan la pureza del modelo y la regla de dependencias:

```
Karpo.Fw.Go/
├── pkg/
│   ├── domain/           # CAPA DE DOMINIO (Patrones Tácticos y Contratos DIP)
│   │   ├── entity.go          # Entity[ID], BaseEntity[ID] (igualdad por Id)
│   │   ├── value_object.go    # ValueObject[T] (igualdad estructural)
│   │   ├── aggregate.go       # AggregateRoot[ID], BaseAggregateRoot[ID]
│   │   ├── service.go         # DomainService (lógica que abarca múltiples entidades)
│   │   ├── specification.go   # Specification[T], PagedSpecification[T], PageRequest
│   │   ├── factory.go         # Factory[T] (creación y reconstitución compleja)
│   │   └── repository.go      # ReadRepository, WriteRepository, Repository, UnitOfWork
│   │
│   ├── application/      # CAPA DE APLICACIÓN (Patrones Estratégicos y Orquestación)
│   │   ├── cqrs.go            # Command[R], Query[R], CommandHandler, QueryHandler
│   │   ├── mediator.go        # Mediador tipado en memoria (equivalente a MediatR)
│   │   ├── behavior.go        # PipelineBehavior (logging, validación, transacciones)
│   │   ├── orchestrator.go    # Orchestrator[ID, T] (coordina repo + mutación + outbox/eventos)
│   │   └── dto.go             # DTO, ReadDTO, CommandDTO
│   │
│   ├── persistence/      # CAPA DE INFRAESTRUCTURA (Adaptadores de Persistencia)
│   │   ├── memory/            # Repositorio en memoria para tests y desarrollo
│   │   └── ent/               # Adaptador ORM de grafos con consultas tipo LINQ
│   │
│   └── testing/          # CAPA DE TESTING (Unitario, Integración y Arquitectura)
│       ├── archtest/          # Guardián de reglas de arquitectura (Domain Purity)
│       └── testkit/           # Arnés de pruebas unitarias e integración
```

---

## 🛡️ Guardián de Arquitectura (`archtest`)

Al igual que en Karpo C#, el framework cuenta con una suite de pruebas de arquitectura automáticas ([`pkg/testing/archtest`](pkg/testing/archtest)) que se ejecutan en cada `go test`:
1. **Regla de Pureza de Dominio**: Analiza el AST de Go para certificar que ningún archivo de `pkg/domain` importa `application`, `persistence`, `net/http` ni librerías de infraestructura.
2. **Regla de Frontera de Aplicación**: Certifica que `pkg/application` no importa implementaciones de base de datos ni adaptadores de persistencia.

---

## 📦 Catálogo de Verticales del Framework

| Capa | Paquete | Patrones / Contratos | Implementaciones / Adaptadores |
| :--- | :--- | :--- | :--- |
| **Dominio (Táctico)** | **`pkg/domain`** | `Entity[ID]`, `ValueObject[T]`, `AggregateRoot[ID]`, `DomainService`, `Specification[T]`, `Factory[T]`, `ReadRepository[ID, T]`, `WriteRepository[ID, T]`, `Repository[ID, T]`, `UnitOfWork` | `BaseEntity[ID]`, `BaseAggregateRoot[ID]`, `BaseDomainService`, especificaciones compuestas y paginadas |
| **Aplicación (Estratégico)** | **`pkg/application`** | `Command[R]`, `Query[R]`, `CommandHandler`, `QueryHandler`, `Mediator`, `PipelineBehavior`, `Orchestrator[ID, T]`, `DTO` | Despachador en memoria tipado, cadena de interceptores/middleware, orquestador de ciclo de vida del agregado |
| **Testing** | **`pkg/testing`** | Reglas de Arquitectura (*ArchTest*), Harness de pruebas | Verificador AST de fronteras, arnés de fakes y repositorios mock |
| **Sustrato Funcional** | **`pkg/result`** | `Result[T]` | `Ok[T]`, `Fail[T]`, combinadores funcionales `Map`, `FlatMap` |
| **Plataforma** | **`pkg/time`** | `Clock` | `real` (reloj de sistema), `fake` (reloj congelable/desplazable para tests) |
| **Plataforma** | **`pkg/log`** | `Logger` | `vanilla` (envoltorio estructurado sobre `log/slog` nativo de Go) |
| **Plataforma** | **`pkg/cache`** | `Cache[K, V]` | `memory` (caché thread-safe con soporte de expiración TTL) |
| **Plataforma** | **`pkg/events`** | `Event`, `Dispatcher` | `inprocess` (despachador en memoria con soporte de comodines y cancelación) |
| **Persistencia** | **`pkg/persistence`** | Adaptadores de `domain.Repository` | `memory` (repositorio genérico en memoria), `ent` (ORM de grafos y consultas tipadas) |

---

## 🚀 Requisitos y Uso

### Requisitos
- **Go 1.23+** (probado con Go 1.27.0).

### Compilar y Probar
```powershell
# Ejecutar todas las pruebas unitarias y de arquitectura
go test ./... -v

# Verificar cobertura de código
go test ./... -cover

# Regenerar esquemas de ent (si se modifican los modelos en pkg/persistence/ent/schema)
go generate ./...
```

---

## 📄 Licencia

Este proyecto está licenciado bajo la **GNU General Public License v2.0** (GPL-2.0), la misma licencia que el kernel de Linux. Consulta el archivo [LICENSE](LICENSE) para más detalles.
