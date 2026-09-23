# Karpo.Fw.Go

[![License: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/jhermoso/karpo-fw-go)](https://goreportcard.com/report/github.com/jhermoso/karpo-fw-go)

Framework base en Go para la plataforma **Karpo**.

Diseñado bajo principios de **Domain-Driven Design (DDD)**, **Clean Architecture**, **Inversión de Dependencias (DIP)** y **bajo consumo de recursos** (< 30 MB por servicio), optimizado para ser la base de microservicios de dominio (slices/publishers) e interactuar con herramientas de generación y Low-Code asistidas por LLMs.

---

## 🏛️ Filosofía y Arquitectura

1. **Aislamiento por Capas y Paquetes**:
   A diferencia del ecosistema tradicional .NET (donde cada capa y adaptador requería un `.csproj` independiente), en Go el compilador prohíbe ciclos de importación y compila únicamente los paquetes efectivamente utilizados.
   - **`pkg/domain` (Capa de Dominio - Patrones Tácticos)**: Sin dependencias externas. Contiene `Entity`, `ValueObject`, `AggregateRoot` y `Specification`.
   - **`pkg/application` (Capa de Aplicación - Patrones Estratégicos/CQRS)**: Orquesta casos de uso mediante `Command`, `Query`, `CommandHandler`, `QueryHandler` y un `Mediator` con *Pipeline Behaviors* (equivalente a MediatR de C#).
2. **Result Pattern en lugar de excepciones**:
   Toda operación con posibilidad de fallo se modela de forma determinista mediante `result.Result[T]`, eliminando costes de desenrollado de pila (*stack unwinding*) y garantizando seguridad en tiempo de compilación.
3. **Persistencia orientada a Grafos con consultas tipo LINQ (`ent`)**:
   Soporte integrado para **ent** (entgo.io). Las entidades se modelan como nodos y aristas de un grafo, permitiendo consultas fluidas 100% tipadas en tiempo de compilación, carga perezosa/ansiosa de relaciones (`WithContacts()`) y filtrado reverso sin escribir SQL manual ni sufrir sobrecarga de reflexión.
4. **Determinismo y Testabilidad**:
   Contratos puros para tiempo (`time.Clock`), registro (`log.Logger`), caché (`cache.Cache`) y eventos (`events.Dispatcher`), permitiendo pruebas unitarias e integración 100% deterministas (relojes congelables, buses en memoria sin dependencias externas).

---

## 📦 Catálogo de Verticales del Framework

| Capa | Paquete | Patrones / Contratos | Implementaciones / Adaptadores |
| :--- | :--- | :--- | :--- |
| **Dominio (Táctico)** | **`pkg/domain`** | `Entity[ID]`, `ValueObject[T]`, `AggregateRoot[ID]`, `Specification[T]` | `BaseEntity[ID]`, `BaseAggregateRoot[ID]`, especificaciones compuestas (`And`, `Or`, `Not`) |
| **Aplicación (Estratégico)** | **`pkg/application`** | `Command[R]`, `Query[R]`, `CommandHandler`, `QueryHandler`, `Mediator`, `PipelineBehavior` | Despachador en memoria tipado, cadena de interceptores/middleware |
| **Sustrato Funcional** | **`pkg/result`** | `Result[T]` | `Ok[T]`, `Fail[T]`, combinadores funcionales `Map`, `FlatMap` |
| **Plataforma** | **`pkg/time`** | `Clock` | `real` (reloj de sistema), `fake` (reloj congelable/desplazable para tests) |
| **Plataforma** | **`pkg/log`** | `Logger` | `vanilla` (envoltorio estructurado sobre `log/slog` nativo de Go) |
| **Plataforma** | **`pkg/cache`** | `Cache[K, V]` | `memory` (caché thread-safe con soporte de expiración TTL) |
| **Plataforma** | **`pkg/events`** | `Event`, `Dispatcher` | `inprocess` (despachador en memoria con soporte de comodines y cancelación) |
| **Persistencia** | **`pkg/persistence`** | `Repository[ID, T]`, `UnitOfWork` | `memory` (repositorio genérico en memoria), `ent` (ORM de grafos y consultas tipadas) |

---

## 🚀 Requisitos y Uso

### Requisitos
- **Go 1.23+** (probado con Go 1.27.0).

### Compilar y Probar
```powershell
# Ejecutar todas las pruebas unitarias
go test ./... -v

# Verificar cobertura de código
go test ./... -cover

# Regenerar esquemas de ent (si se modifican los modelos en pkg/persistence/ent/schema)
go generate ./...
```

### Ejemplo: Despacho CQRS con Mediador y Pipeline
```go
m := application.NewMediator()

// Middleware de Logging / Auditoría
m.Use(func(ctx context.Context, req any, next application.NextFunc) (any, error) {
    log.Printf("Executing request: %T", req)
    res, err := next(ctx)
    log.Printf("Finished request: %T", req)
    return res, err
})

// Registrar Command Handler
application.RegisterCommandHandler(m, application.CommandHandlerFunc[CreatePartyCommand, string](
    func(ctx context.Context, cmd CreatePartyCommand) result.Result[string] {
        return result.Ok("Party created successfully")
    },
))

// Ejecutar comando
res := application.Send[string](ctx, m, CreatePartyCommand{TaxID: "B12345678"})
```

---

## 📄 Licencia

Este proyecto está licenciado bajo la **GNU General Public License v2.0** (GPL-2.0), la misma licencia que el kernel de Linux. Consulta el archivo [LICENSE](LICENSE) para más detalles.
