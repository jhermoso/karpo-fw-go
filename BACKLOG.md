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
- ✅ Ejemplo de referencia en `examples/parties` (agregado con hijos, VOs, eventos,
  especificaciones de colección y custom, CQRS, outbox, HTTP, cambio en caliente).
- Pendiente: portar el modelo real de `ErpKernel.Parties` / `ErpDetail.Parties` (PartyRole,
  jerarquía de roles, vigencias) sobre el mismo patrón y compartir tablas con el C# en
  SQL Server/Oracle (`oracle.WithDotNetGUIDs`, `UNIQUEIDENTIFIER`).
- ~~Persistencia con `ent`~~: descartado; `ent` no soporta Oracle ni SQL Server. Sustituido por
  `pkg/persistence/sqlrepo` (agnóstico, un dialecto por motor).

### 2b. Framework: siguientes pasos
- **Migraciones de esquema** por dialecto (equivalente a `IDatabaseSchemaManager`): hoy el DDL
  vive en cada contexto (`infrastructure.Schema`).
- **Componentes transversales de `BusinessEntity`** (auditable, activable, autorizable,
  trazable) como piezas componibles, no como clase base.
- **Lenguaje ubicuo común** (`Name`, `Email`, `ValidPeriod`, `Percentage`, `Actor`,
  `UTCDateTime`...) en un paquete `pkg/domain/ubiquitous`.
- **Outbox multi-instancia**: `SELECT ... FOR UPDATE SKIP LOCKED` (PostgreSQL/Oracle/MySQL) y
  `READPAST` (SQL Server) para varios relays en paralelo.
- **Idempotencia persistente** (`sqlrepo` store) para despliegues con varias réplicas.
- **Herramienta de backfill/dual-write** para acompañar a `hotswap.Swap` cuando haya que mover
  datos entre motores.
- **Filtros HTTP → especificaciones** (lista blanca de campos) para búsquedas genéricas.
- **CI en Linux** con `go test -race` (en Windows no hay compilador C) y `integration/run.ps1`.

### 3. PoC Frontend: Evaluación de Stack Ligero (Svelte 5 / SolidJS)
- Prototipar la interfaz de listado y ficha de `Parties`.
- Comparar tamaño de bundle (< 100 KB objetivo vs ~9.4 MB de Angular 20) y velocidad de carga/hidratación.

### 4. Convivencia y Enrutamiento en Gateway
- Configurar Nginx (`060-Deploy/gateway`) para derivar las rutas `/api/parties` al nuevo servicio en Go manteniendo el resto del ERP en .NET.

### 5. Metamodelo & DSL Textual en Grafo
- Diseñar la gramática / formato de grafo (nodos y aristas) para modelar entidades, agregados y flujos de negocio para que un LLM pueda generar código completo de backend y frontend de forma determinista.
