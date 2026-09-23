# Karpo.Fw.Go

[![License: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/jhermoso/karpo-fw-go)](https://goreportcard.com/report/github.com/jhermoso/karpo-fw-go)

Framework base en Go para la plataforma **Karpo**.

Diseñado bajo principios de **Clean Architecture**, **Inversión de Dependencias (DIP)** y **bajo consumo de recursos** (< 30 MB por servicio), optimizado para ser la base de microservicios de dominio (slices/publishers) e interactuar con herramientas de generación y Low-Code asistidas por LLMs.

---

## 🏛️ Filosofía y Arquitectura

1. **Aislamiento por Paquete (sin sobrecarga de proyectos)**:
   A diferencia del ecosistema tradicional .NET (donde cada adaptador requería un `.csproj` independiente), en Go el compilador prohíbe ciclos de importación y compila únicamente los paquetes efectivamente utilizados. Cada vertical del framework expone sus contratos e implementaciones en paquetes aislados dentro de un único módulo.
2. **Result Pattern en lugar de excepciones**:
   Toda operación con posibilidad de fallo se modela de forma determinista mediante `result.Result[T]`, eliminando costes de desenrollado de pila (*stack unwinding*) y garantizando seguridad en tiempo de compilación.
3. **Persistencia orientada a Grafos con consultas tipo LINQ (`ent`)**:
   Soporte integrado para **ent** (entgo.io). Las entidades se modelan como nodos y aristas de un grafo, permitiendo consultas fluidas 100% tipadas en tiempo de compilación, carga perezosa/ansiosa de relaciones (`WithContacts()`) y filtrado reverso sin escribir SQL manual ni sufrir sobrecarga de reflexión.
4. **Determinismo y Testabilidad**:
   Contratos puros para tiempo (`time.Clock`), registro (`log.Logger`), caché (`cache.Cache`) y eventos (`events.Dispatcher`), permitiendo pruebas unitarias e integración 100% deterministas (relojes congelables, buses en memoria sin dependencias externas).

---

## 📦 Catálogo de Verticales del Framework

| Vertical | Contrato Principal | Implementaciones / Adaptadores |
| :--- | :--- | :--- |
| **`pkg/result`** | `Result[T]` | `Ok[T]`, `Fail[T]`, combinadores funcionales `Map`, `FlatMap` |
| **`pkg/time`** | `Clock` | `real` (reloj de sistema), `fake` (reloj congelable/desplazable para tests) |
| **`pkg/log`** | `Logger` | `vanilla` (envoltorio estructurado sobre `log/slog` nativo de Go) |
| **`pkg/cache`** | `Cache[K, V]` | `memory` (caché thread-safe con soporte de expiración TTL) |
| **`pkg/events`** | `Event`, `Dispatcher` | `inprocess` (despachador en memoria con soporte de comodines y cancelación) |
| **`pkg/persistence`** | `Repository[ID, T]`, `UnitOfWork` | `memory` (repositorio genérico en memoria), `ent` (ORM de grafos y consultas tipadas) |

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

### Ejemplo de Consulta Tipo LINQ con `ent`
```go
// Equivalente a: db.Parties.Where(p => p.TaxId == taxId && p.IsActive).Include(p => p.Contacts).First()
foundParty, err := client.Party.Query().
    Where(
        party.TaxID("B12345678"),
        party.IsActive(true),
    ).
    WithContacts().
    Only(ctx)
```

---

## 📄 Licencia

Este proyecto está licenciado bajo la **GNU General Public License v2.0** (GPL-2.0), la misma licencia que el kernel de Linux. Consulta el archivo [LICENSE](LICENSE) para más detalles.
