# Backlog & Roadmap - Karpo

Registro de tareas pendientes, ideas de tooling y mejoras para el ecosistema Karpo y su framework en Go.

---

## 🛠️ Herramientas y Tooling para Desarrolladores

### 1. Extensión de VS Code: Custom Folder Ordering / Architecture Explorer
- **Objetivo**: Permitir visualizar y ordenar las carpetas de un proyecto según el orden arquitectónico deseado (`Distribution` $\rightarrow$ `Application` $\rightarrow$ `Domain` $\rightarrow$ `Persistence` $\rightarrow$ `Testing`) en el explorador de VS Code **sin necesidad de alterar los nombres físicos de las carpetas con prefijos numéricos** (`010_...`, `020_...`).
- **Problema que resuelve**: Los exploradores de archivos estándar ordenan alfabéticamente (`application`, `cache`, `distribution`, `domain`), lo que invierte visualmente la jerarquía real de dependencias. Nombrar las carpetas con números (`010_distribution`) causa fricción con las convenciones de Go y sus linters.
- **Enfoque de implementación**:
  - Opción 1: Vista de árbol personalizada (`Custom TreeView` en la barra lateral de VS Code o integrada en el panel del Explorador) que lee un archivo de configuración opcional (`.vscode/architecture.json` o `karpo.arch.json`).
  - Opción 2: Investigar la API de decoración y ordenación del explorador nativo si VS Code expone ganchos para `sortOrder` personalizado.
  - Características deseadas:
    - Agrupación por capas (Presentation/Distribution, Use Cases/Application, Domain Kernel, Infrastructure/Adapters).
    - Insignias visuales de pureza arquitectónica e indicadores de dirección de dependencias.
    - Soporte multi-lenguaje (Go, C#, TypeScript, Rust).

---

## 🚀 Migración y Servicios (Roadmap)

### 2. Bounded Context Piloto: `Karpo.Parties` en Go
- Implementar el primer servicio de negocio real migrado desde C#:
  - Agregado `Party` y Value Objects (`TaxId`, `PartyName`, `Address`, etc.).
  - Contratos de repositorio (`domain.ReadRepository`, `domain.WriteRepository`).
  - Casos de uso / CQRS (`CreatePartyCommand`, `GetPartyByIdQuery`).
  - Persistencia con `ent` y SQLite/PostgreSQL.
  - Endpoints HTTP usando `pkg/distribution`.

### 3. PoC Frontend: Evaluación de Stack Ligero (Svelte 5 / SolidJS)
- Prototipar la interfaz de listado y ficha de `Parties`.
- Comparar tamaño de bundle (< 100 KB objetivo vs ~9.4 MB de Angular 20) y velocidad de carga/hidratación.

### 4. Convivencia y Enrutamiento en Gateway
- Configurar Nginx (`060-Deploy/gateway`) para derivar las rutas `/api/parties` al nuevo servicio en Go manteniendo el resto del ERP en .NET.

### 5. Metamodelo & DSL Textual en Grafo
- Diseñar la gramática / formato de grafo (nodos y aristas) para modelar entidades, agregados y flujos de negocio para que un LLM pueda generar código completo de backend y frontend de forma determinista.
