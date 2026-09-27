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
- ~~Componentes transversales de `BusinessEntity`~~ ✅ `pkg/domain/traits`.
- **Almacén de auditoría a prueba de manipulación** (cadena de hashes verificable) como
  implementación de `application.AuditLog`.
- **Atributos extendidos declarativos** (campos personalizados por cliente, persistidos como
  JSON con esquema) en lugar del `IExtensible` basado en delegados.
- **Cifrado de campos** en el mapeo de persistencia (sustituto de `EncryptedAttribute`).
- ~~Lenguaje ubicuo común~~ ✅ `pkg/domain/vocab`.
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

### 6. Impuestos indirectos (IVA y equivalentes): cálculo por país y por sector

Decisión de Javier (2026-09-28): el cálculo se estructura **por país** (cada jurisdicción tiene
sus impuestos, reglas, modelos y sistemas de envío) y dentro de cada país **por régimen o
sector**. Hoy (fase 1 de Fiscal, ver [docs/FISCAL.md](docs/FISCAL.md)) solo existen el catálogo
de tipos con vigencia, los tratamientos y la consulta `Rates.RateOn`; **nada calcula el IVA**
(tampoco en C#).

**Estructura propuesta**
- **Núcleo de Fiscal, independiente del país:**
  - contribuyente, catálogo de tipos, presentaciones y numeración;
  - un puerto `TaxEngine.Calculate(documento imponible) → desglose` que usan Facturación y
    Compras;
  - el registro de jurisdicciones.
- **Una jurisdicción por país** (`contexts/fiscal/jurisdictions/<iso>`, o un contexto propio si
  crece). Cada una aporta:
  - sus impuestos y territorios;
  - las reglas de cálculo y redondeo;
  - los regímenes;
  - la validación del identificador fiscal;
  - los requisitos de la factura;
  - sus modelos y sus sistemas de envío o de factura electrónica.

  Se añade un país sin tocar el núcleo.
- **Regímenes y sectores dentro de cada país:** son estrategias que se eligen por el
  contribuyente, la contraparte, el producto y la operación. Cada régimen cambia la base, el
  tipo, quién declara el impuesto o qué se deduce.
- **Categoría fiscal del producto:** se asigna por país (por ejemplo, en España, alimentos
  básicos al tipo superreducido, libros, medicamentos, hostelería al tipo reducido…). Sustituye
  al `VatGroupId` de `ProductCommercialProfile`, que en C# no tenía clave foránea.

**España (primera jurisdicción): lo que queda**
- Cálculo básico:
  - base por tipo;
  - cuota por tipo, redondeada al céntimo sobre la base agregada de la factura (no por línea);
  - descuentos en factura y pronto pago;
  - suplidos fuera de la base (art. 78);
  - anticipos;
  - portes;
  - envases y embalajes.
- Territorios:
  - IVA común;
  - IGIC de Canarias (tipos propios y AIEM);
  - IPSI de Ceuta y Melilla;
  - territorios forales (País Vasco y Navarra: normativa y modelos propios, TicketBAI).
- Recargo de equivalencia (comercio minorista): se aplica según el régimen del cliente, con
  sus tipos ligados al tipo de IVA.
- Inversión del sujeto pasivo:
  - construcción y rehabilitación;
  - chatarra y residuos;
  - oro;
  - teléfonos, tabletas y portátiles;
  - derechos de emisión;
  - operaciones inmobiliarias con renuncia a la exención.
- Regímenes especiales por sector:
  - REAGP (agricultura, ganadería y pesca: compensación en lugar de IVA);
  - REBU (bienes usados, objetos de arte y antigüedades: margen de beneficio);
  - REAV (agencias de viajes: margen);
  - oro de inversión;
  - criterio de caja (RECC: devengo al cobro y al pago);
  - grupo de entidades;
  - régimen simplificado (módulos).
- Exenciones con su causa:
  - arts. 20, 21, 22 y 25 (E1–E6 del SII);
  - operaciones no sujetas y localización (arts. 69–70).
- Operaciones con el exterior:
  - entregas intracomunitarias exentas, con validación del NIF-IVA en ROI/VIES;
  - adquisiciones intracomunitarias con autoliquidación;
  - operaciones triangulares;
  - exportaciones e importaciones (DUA, IVA diferido);
  - ventas a distancia B2C (OSS/IOSS, modelo 369);
  - servicios B2B y B2C según las reglas de localización.
- Deducciones:
  - prorrata general y especial;
  - sectores diferenciados;
  - regularización de bienes de inversión (5 y 10 años);
  - no deducibles y afectación parcial (vehículos al 50 %);
  - IVA soportado de importación.
- Rectificativas: por diferencias o por sustitución, causa R1–R5, y modificación de la base por
  impago o concurso (art. 80).
- Retenciones en factura:
  - IRPF de profesionales (15 % y 7 %);
  - arrendamientos (19 %; modelos 115 y 180);
  - retención al cobro (`AppliesWithholdingOnCollection` de C#).
- Libros registro: facturas expedidas, facturas recibidas, bienes de inversión y operaciones
  intracomunitarias.
- Envíos y factura electrónica:
  - SII (4 días hábiles);
  - Verifactu (RRSIF: encadenamiento de huellas, QR y remisión);
  - TicketBAI;
  - FacturaE y la B2B obligatoria (Ley Crea y Crece).
- Modelos: 303, 390, 349, 347, 309, 369, además de 115 y 180. Ficheros oficiales y
  validación contra los casos de la AEAT.
- Datos legales: tablas anuales con vigencia (tipos, recargos, compensaciones del REAGP),
  mantenidas por el permiso `Fiscal.Catalog.Update`, más la importación desde Sage **con el
  territorio**.
- Contabilidad: asientos de IVA repercutido y soportado (477, 472), recargo, inversión del sujeto
  pasivo (doble apunte), liquidación (4750 y 4700) y retenciones (4751 y 473).

**Otros países (cuando haya clientes):** Portugal (SAF-T, ATCUD, QR), Francia (Factur-X, Chorus
Pro, e-invoicing 2026), Italia (SDI, FatturaPA), Alemania (XRechnung, ZUGFeRD) y el resto de la UE
mediante la estructura común de la Directiva 2006/112/CE.
