# Karpo.Fw.Go

[![License: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/jhermoso/karpo-fw-go)](https://goreportcard.com/report/github.com/jhermoso/karpo-fw-go)

Framework base en Go para la plataforma **Karpo**.

Diseñado bajo principios de **Domain-Driven Design (DDD)**, **Clean Architecture**, **Inversión de Dependencias (DIP)** y **bajo consumo de recursos** (< 30 MB por servicio), optimizado para ser la base de microservicios de dominio (slices/publishers) e interactuar con herramientas de generación y Low-Code asistidas por LLMs.

---

## 🏛️ Jerarquía Estricta de Capas

El framework organiza el código respetando la regla de dependencias unidireccional de Clean Architecture:

$$\text{Distribution (Transporte / API)} \longrightarrow \text{Application (Casos de Uso)} \longrightarrow \text{Domain (Reglas Puras)}$$

```
Karpo.Fw.Go/
├── pkg/
│   ├── distribution/     # 1. CAPA DE DISTRIBUCIÓN (Transporte HTTP, API, Middleware)
│   │   ├── server.go          # Servidor HTTP con Graceful Shutdown
│   │   ├── endpoint.go        # EndpointModule (registro modular de rutas)
│   │   ├── middleware.go      # Recovery, RequestLogging, TenantActorContext, CORS
│   │   ├── response.go        # WriteResult (serialización de Result[T] a ProblemDetails RFC 7807)
│   │   ├── context.go         # Extracción y propagación de TenantID, ActorID, OrgID
│   │   └── health.go          # Chequeos de salud liveness (/healthz) y readiness (/readyz)
│   │
│   ├── application/      # 2. CAPA DE APLICACIÓN (Casos de Uso y Orquestación)
│   │   ├── cqrs.go            # Command[R], Query[R], CommandHandler, QueryHandler
│   │   ├── mediator.go        # Mediador tipado en memoria (equivalente a MediatR)
│   │   ├── behavior.go        # PipelineBehavior (logging, validación, métricas)
│   │   ├── orchestrator.go    # Orchestrator[ID, T] (coordina repo + mutación + eventos)
│   │   └── dto.go             # DTO, ReadDTO, CommandDTO
│   │
│   ├── domain/           # 3. CAPA DE DOMINIO (Patrones Tácticos y Contratos DIP - Núcleo)
│   │   ├── entity.go          # Entity[ID], BaseEntity[ID] (igualdad por Id)
│   │   ├── value_object.go    # ValueObject[T] (igualdad estructural)
│   │   ├── aggregate.go       # AggregateRoot[ID], BaseAggregateRoot[ID]
│   │   ├── service.go         # DomainService (lógica sin estado que abarca múltiples entidades)
│   │   ├── specification.go   # Specification, PagedSpecification, PageRequest, PagedResult
│   │   ├── factory.go         # Factory[T] (creación y reconstitución compleja)
│   │   └── repository.go      # ReadRepository, WriteRepository, Repository, UnitOfWork
│   │
│   ├── persistence/      # CAPA DE INFRAESTRUCTURA (Adaptadores de Persistencia)
│   │   ├── memory/            # Repositorio en memoria para tests y desarrollo
│   │   └── ent/               # Adaptador ORM de grafos con consultas tipo LINQ
│   │
│   └── testing/          # CAPA DE TESTING (Unitario, Integración y Arquitectura)
│       ├── archtest/          # Guardián de reglas de arquitectura (AST parser)
│       └── testkit/           # Arnés de pruebas unitarias e integración (fakes, db harness)
```

---

## 🛡️ Guardián de Arquitectura (`archtest`)

Al igual que en Karpo C#, el framework cuenta con una suite de pruebas de arquitectura automáticas ([`pkg/testing/archtest`](pkg/testing/archtest)) que se ejecutan en cada `go test`:
1. **Regla de Pureza de Dominio**: El Dominio no puede importar Aplicación, Distribución ni Infraestructura.
2. **Regla de Frontera de Aplicación**: La Aplicación no puede importar Distribución ni adaptadores de base de datos concretos.
3. **Regla de Aislamiento de Distribución**: La Distribución no puede importar adaptadores concretos de base de datos (opera exclusivamente contra la capa de Aplicación).

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

Este proyecto está licenciado bajo la **GNU General Public License v2.0** (GPL-2.0). Consulta el archivo [LICENSE](LICENSE) para más detalles.
