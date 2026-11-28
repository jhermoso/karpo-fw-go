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

### 2a. Parties y UDM 1: lo que queda

Hecho (ver [docs/PARTIES-UDM.md](docs/PARTIES-UDM.md), decisiones aprobadas el 2026-10-04): datos
propios por tipo de relación (código estable en el catálogo, relación de cliente potencial con su
tiempo de prueba, participación en el capital) y edición de persona.

- **Detalles por tipo de rol** en `PartyRole`, con el mismo mecanismo: cuando haya un dato que
  sea del rol y no de la pareja ni de otra área. Los roles heredan, así que los detalles también.
- **Número de cliente** como detalle de la relación de cliente, único por empresa: con la
  importación de Sage o con Ventas.
- **Convertir un cliente potencial en cliente** en una sola operación.
- **Participación:** importes, participación indirecta y tope del 100 % por sociedad.
- **Estado y prioridad de la relación:** decidido no hacerlos; la prioridad, en su caso, en el
  contexto de interacciones.
- **Contexto `subscriptions`** (Theros): consumirá `parties.prospect-trial-changed.v1` y el
  puerto `Trials`.
- **Incidencias de calidad de datos (D-92, [docs/PARTIES.md](docs/PARTIES.md) §21):** el IBAN
  sigue rechazándose (cuentas de Nóminas, Compras, Tesorería, Pagos y cuentas financieras, que
  alimentan remesas SEPA): pendiente de que Javier diga si pasa a aviso. La marca de documento
  rechazado (`parties.doc_issue`, §10) podría pasar a la tabla genérica
  `party_data_quality_issues` como `INVALID_DOCUMENT` con severidad de error: migración de datos y
  cambio del filtro `documentIssue`, aparte.
- **Prueba intermitente:** `TestParties_EndToEnd_MemoryThenSQLite` falla a veces en Windows al
  terminar un contacto en el mismo tic de reloj en que se creó.
- ~~**Cascada cliente > empresa de los ajustes de facturación (D-144):**~~ ✅ D-150: el cliente
  fija su redondeo y sus decimales en sus condiciones de Pedidos (`ord_terms`, migración 9);
  docs/FACTURACION.md «Los ajustes de la empresa». Falta que Karpo C# y Trench lo adopten.

### 2b. Framework: siguientes pasos
- ~~**Migraciones de esquema** por dialecto~~ ✅ `sqlrepo.Migrator`: cada contexto lleva su serie
  en `infrastructure` (`Migrations()`), con historial, checksum y bloqueo
  ([docs/ESQUEMA-MIGRACIONES.md](docs/ESQUEMA-MIGRACIONES.md)).
- ~~Componentes transversales de `BusinessEntity`~~ ✅ `pkg/domain/traits`.
- **Almacén de auditoría a prueba de manipulación** (cadena de hashes verificable) como
  implementación de `application.AuditLog`.
- **Atributos extendidos declarativos** (campos personalizados por cliente, persistidos como
  JSON con esquema) en lugar del `IExtensible` basado en delegados.
- **Cifrado de campos** en el mapeo de persistencia (sustituto de `EncryptedAttribute`).
- ~~Lenguaje ubicuo común~~ ✅ `pkg/domain/vocab`.
- **Outbox multi-instancia**: `SELECT ... FOR UPDATE SKIP LOCKED` (PostgreSQL/Oracle/MySQL) y
  `READPAST` (SQL Server) para varios relays en paralelo.
- **Idempotencia del resto de órdenes.** Las altas de Parties ya la tienen en su transacción, con
  `party_idempotency_keys` (D-125, [PARTIES.md](docs/PARTIES.md) §24). `pipeline.Idempotent` y su
  almacén de memoria guardan después de confirmar y sólo los usa el ejemplo `examples/parties`.
- **Herramienta de backfill/dual-write** para acompañar a `hotswap.Swap` cuando haya que mover
  datos entre motores.
- **Filtros HTTP → especificaciones** (lista blanca de campos) para búsquedas genéricas.
- **errorCode en el límite de lote de los directorios** (parties, facilities, hr, geography,
  security). Prioridad baja. El filtro `ids` del contrato de mantenimiento da 400 `ids/format` con
  un id mal formado; los directorios entre contextos lo descartan en silencio
  ([CONTRATO-MANTENIMIENTO.md](docs/CONTRATO-MANTENIMIENTO.md)). Alinearlos pide una D-n y avisar a C#.
- **Testing: cobertura de `hr/application` y `products/application` por debajo del 80 %.** Medido
  con `-coverpkg` sobre los paquetes tocados por B-15, `origin/main` frente a `feat/filtro-ids`:
  `contexts/hr/application` 72,9 → 73,0 %; `contexts/products/application` 77,3 → 77,6 %. No bajan
  («trinquete», [docs/trabajo/b15-filtro-ids](docs/trabajo/b15-filtro-ids/consultas.md)); lo que
  falta es código anterior. Subirlos al 80 %.
- **Testing: mutantes vivos en los dominios tocados por B-15.** Obtenidos el 2026-11-27 con un
  mutador propio (operadores binarios al estilo de gremlins: límite, negación, lógico,
  aritmética; cada mutante con `go test -count=1 -overlay` sobre el paquete del dominio, sin tocar
  el árbol), porque gremlins v0.5.0 en Windows da todos los mutantes por tiempo agotado. Sólo
  cuentan las pruebas del propio paquete `domain`: muchas reglas se prueban desde la aplicación o
  la raíz del contexto, por eso hay paquetes con muchos vivos (shipments). Todos están en código
  anterior: las líneas de B-15 (`…FieldID`, `…WithIDs`) no tienen operadores, y el filtro `ids`
  no depende de ninguna de estas condiciones. Formato: fichero, línea, tipo y cambio.
  - **assets/domain** (16 vivos, 73 muertos):
    - `asset.go`: 63 aritmética `-`→`+`; 87 límite `>=`→`>`; 87 límite `<=`→`<`; 141 lógico `||`→`&&`; 141 límite `>`→`>=`; 145 límite `>=`→`>`; 145 límite `<=`→`<`; 145 límite `<=`→`<`; 154 límite `<=`→`<`; 155 límite `<=`→`<`; 171 límite `<=`→`<`; 174 lógico `&&`→`||`; 179 límite `>=`→`>`; 179 límite `<=`→`<`; 309 lógico `||`→`&&`; 342 límite `>`→`>=`
  - **facilities/domain** (13 vivos, 31 muertos):
    - `facility.go`: 75 lógico `&&`→`||`; 80 negación `==`→`!=`; 101 lógico `||`→`&&`; 106 límite `<=`→`<`; 149 lógico `&&`→`||`; 149 límite `<=`→`<`; 150 límite `<=`→`<`; 151 negación `!=`→`==`; 254 lógico `||`→`&&`; 254 negación `==`→`!=`; 254 límite `>`→`>=`; 254 negación `>`→`<=`; 259 negación `==`→`!=`
  - **geography/domain** (30 vivos, 4 muertos):
    - `country.go`: 81 negación `!=`→`==`; 92 negación `!=`→`==`; 98 negación `!=`→`==`; 99 negación `!=`→`==`; 104 negación `!=`→`==`; 129 negación `==`→`!=`; 133 negación `!=`→`==`; 144 negación `==`→`!=`; 145 lógico `||`→`&&`; 145 negación `==`→`!=`; 145 negación `==`→`!=`; 146 negación `!=`→`==`
    - `geography.go`: 152 negación `!=`→`==`; 156 negación `!=`→`==`; 160 negación `==`→`!=`; 163 negación `!=`→`==`; 165 límite `<=`→`<`; 165 negación `<=`→`>`; 169 negación `!=`→`==`; 197 negación `==`→`!=`; 208 negación `==`→`!=`; 266 negación `!=`→`==`; 270 lógico `||`→`&&`; 270 negación `==`→`!=`
    - `holiday.go`: 53 lógico `&&`→`||`; 53 límite `<=`→`<`; 72 lógico `||`→`&&`; 72 negación `==`→`!=`; 72 límite `>`→`>=`; 72 negación `>`→`<=`
  - **hr/domain** (23 vivos, 66 muertos):
    - `catalogs.go`: 125 lógico `&&`→`||`; 125 límite `<=`→`<`; 126 lógico `&&`→`||`; 126 límite `<=`→`<`; 127 lógico `&&`→`||`; 127 límite `<=`→`<`; 137 lógico `&&`→`||`; 137 lógico `&&`→`||`; 137 lógico `||`→`&&`
    - `employment.go`: 35 lógico `&&`→`||`; 72 límite `<=`→`<`; 161 negación `!=`→`==`; 192 lógico `&&`→`||`; 262 lógico `&&`→`||`
    - `position.go`: 73 lógico `&&`→`||`; 167 negación `==`→`!=`; 170 negación `!=`→`==`; 235 lógico `&&`→`||`; 247 lógico `||`→`&&`; 273 lógico `&&`→`||`
    - `workcenter.go`: 51 lógico `&&`→`||`; 51 límite `<=`→`<`; 98 lógico `&&`→`||`
  - **products/domain** (44 vivos, 129 muertos):
    - `catalog.go`: 53 lógico `||`→`&&`; 53 lógico `||`→`&&`; 59 lógico `&&`→`||`; 59 negación `==`→`!=`; 59 negación `==`→`!=`; 62 lógico `&&`→`||`; 62 negación `==`→`!=`; 62 negación `==`→`!=`; 97 lógico `&&`→`||`; 97 límite `<=`→`<`; 98 límite `<=`→`<`; 157 lógico `&&`→`||`; 165 límite `<=`→`<`; 184 límite `>=`→`>`; 195 límite `<`→`<=`; 244 lógico `&&`→`||`; 244 límite `<=`→`<`; 245 lógico `&&`→`||`; 245 límite `<=`→`<`
    - `product.go`: 28 aritmética `+`→`-`; 40 negación `==`→`!=`; 65 negación `==`→`!=`; 90 lógico `||`→`&&`; 106 negación `!=`→`==`; 106 límite `<=`→`<`; 106 negación `<=`→`>`; 203 lógico `||`→`&&`; 203 límite `>`→`>=`; 207 límite `<=`→`<`; 207 límite `>=`→`>`; 207 límite `<=`→`<`; 218 límite `<=`→`<`; 219 límite `<=`→`<`; 225 límite `<=`→`<`; 226 límite `<=`→`<`; 234 negación `==`→`!=`; 234 negación `==`→`!=`; 242 límite `<=`→`<`; 243 límite `<=`→`<`; 247 lógico `&&`→`||`; 248 negación `==`→`!=`; 253 límite `<=`→`<`; 253 límite `<=`→`<`; 259 límite `<=`→`<`
  - **purchases/domain** (27 vivos, 59 muertos):
    - `blockevent.go`: 40 negación `!=`→`==`; 44 lógico `&&`→`||`; 44 lógico `&&`→`||`; 46 negación `!=`→`==`
    - `invoice.go`: 142 lógico `&&`→`||`; 144 límite `<=`→`<`; 147 lógico `&&`→`||`; 147 límite `>`→`>=`; 147 límite `<=`→`<`; 151 límite `<=`→`<`; 153 lógico `&&`→`||`; 160 lógico `&&`→`||`; 160 lógico `&&`→`||`; 172 lógico `&&`→`||`; 172 límite `>=`→`>`; 257 límite `<`→`<=`; 270 límite `<`→`<=`; 270 aritmética `-`→`+`; 286 límite `>`→`>=`; 325 límite `<`→`<=`; 325 límite `<`→`<=`
    - `supplier.go`: 66 lógico `&&`→`||`; 77 límite `<=`→`<`; 80 límite `<=`→`<`; 124 límite `<=`→`<`; 152 negación `!=`→`==`; 155 negación `!=`→`==`
  - **shipments/domain** (153 vivos, 12 muertos):
    - `carrier.go`: 64 lógico `&&`→`||`; 64 límite `<=`→`<`; 65 lógico `&&`→`||`; 65 lógico `&&`→`||`; 65 límite `<=`→`<`; 65 negación `<=`→`>`; 66 negación `==`→`!=`; 78 lógico `&&`→`||`; 78 límite `<=`→`<`; 80 lógico `&&`→`||`; 80 lógico `||`→`&&`; 80 lógico `&&`→`||`; 80 límite `>=`→`>`; 80 límite `<=`→`<`; 80 lógico `&&`→`||`; 80 límite `>=`→`>`; 80 negación `>=`→`<`; 80 límite `<=`→`<`; 80 negación `<=`→`>`; 80 negación `==`→`!=`; 80 negación `==`→`!=`; 102 límite `<=`→`<`; 113 negación `!=`→`==`; 125 lógico `&&`→`||`; 125 negación `==`→`!=`; 127 negación `==`→`!=`; 139 negación `!=`→`==`; 143 negación `==`→`!=`; 159 lógico `||`→`&&`; 159 negación `==`→`!=`; 159 negación `==`→`!=`; 170 límite `<`→`<=`; 170 negación `<`→`>=`; 172 lógico `||`→`&&`; 172 lógico `||`→`&&`; 172 lógico `||`→`&&`; 172 lógico `&&`→`||`; 172 límite `>=`→`>`; 172 negación `>=`→`<`; 172 límite `<=`→`<`; 172 negación `<=`→`>`; 172 lógico `&&`→`||`; 172 límite `>=`→`>`; 172 negación `>=`→`<`; 172 límite `<=`→`<`; 172 negación `<=`→`>`; 172 lógico `&&`→`||`; 172 límite `>=`→`>`; 172 negación `>=`→`<`; 172 límite `<=`→`<`; 172 negación `<=`→`>`; 172 negación `==`→`!=`; 172 negación `==`→`!=`; 172 negación `==`→`!=`; 172 negación `==`→`!=`; 186 negación `!=`→`==`; 189 negación `!=`→`==`
    - `shipment.go`: 134 lógico `||`→`&&`; 134 negación `==`→`!=`; 135 lógico `||`→`&&`; 135 negación `!=`→`==`; 136 límite `<=`→`<`; 136 negación `<=`→`>`; 137 límite `<=`→`<`; 137 negación `<=`→`>`; 138 límite `<=`→`<`; 138 negación `<=`→`>`; 139 límite `<=`→`<`; 139 negación `<=`→`>`; 140 lógico `&&`→`||`; 140 límite `>=`→`>`; 140 negación `>=`→`<`; 140 límite `<=`→`<`; 140 negación `<=`→`>`; 141 lógico `&&`→`||`; 142 lógico `&&`→`||`; 143 lógico `||`→`&&`; 143 lógico `||`→`&&`; 150 negación `!=`→`==`; 156 lógico `&&`→`||`; 156 lógico `&&`→`||`; 156 lógico `&&`→`||`; 156 negación `==`→`!=`; 156 negación `==`→`!=`; 156 negación `==`→`!=`; 156 límite `<=`→`<`; 156 negación `<=`→`>`; 156 límite `<=`→`<`; 156 negación `<=`→`>`; 156 límite `<=`→`<`; 156 negación `<=`→`>`; 159 límite `>=`→`>`; 159 negación `>=`→`<`; 161 lógico `&&`→`||`; 161 límite `>=`→`>`; 161 negación `>=`→`<`; 161 límite `<=`→`<`; 161 negación `<=`→`>`; 165 negación `==`→`!=`; 165 aritmética `+`→`-`; 166 lógico `&&`→`||`; 166 lógico `&&`→`||`; 166 lógico `&&`→`||`; 166 negación `!=`→`==`; 166 límite `<=`→`<`; 166 negación `<=`→`>`; 166 límite `<=`→`<`; 166 negación `<=`→`>`; 166 límite `<=`→`<`; 166 negación `<=`→`>`; 168 lógico `&&`→`||`; 170 negación `!=`→`==`; 184 aritmética `+`→`-`; 188 negación `!=`→`==`; 207 negación `!=`→`==`; 217 lógico `||`→`&&`; 216 lógico `||`→`&&`; 216 lógico `||`→`&&`; 216 lógico `||`→`&&`; 216 lógico `||`→`&&`; 216 negación `!=`→`==`; 216 negación `!=`→`==`; 216 negación `!=`→`==`; 216 negación `!=`→`==`; 217 negación `!=`→`==`; 228 negación `!=`→`==`; 234 lógico `&&`→`||`; 234 límite `>`→`>=`; 234 negación `>`→`<=`; 234 aritmética `-`→`+`; 244 lógico `||`→`&&`; 244 negación `==`→`!=`; 244 límite `>`→`>=`; 244 negación `>`→`<=`; 254 lógico `&&`→`||`; 254 negación `==`→`!=`; 254 lógico `||`→`&&`; 254 lógico `||`→`&&`; 254 negación `==`→`!=`; 254 límite `<`→`<=`; 254 negación `<`→`>=`; 254 lógico `&&`→`||`; 254 negación `!=`→`==`; 254 lógico `||`→`&&`; 254 negación `==`→`!=`; 257 negación `!=`→`==`; 270 límite `>`→`>=`; 270 negación `>`→`<=`; 273 negación `!=`→`==`; 285 negación `!=`→`==`; 288 negación `!=`→`==`; 300 negación `!=`→`==`; 303 negación `!=`→`==`
- ~~**CI en Linux** con `go test -race`~~ ✅ `.github/workflows/ci.yml` (unitarias con `-race`,
  publicadoras de humo e integración en cinco motores, Supabase incluido desde el 2026-11-14). **No corre desde el 2026-10-21** (cuenta
  de GitHub bloqueada): la puerta es local.
- **Listados sin fichas fusionadas (D-100) con muchas fusionadas.** Cada listado añade «party NOT
  IN (ids fusionados)», un parámetro por ficha (`pkg/application/merged.go`). SQL Server admite 2100
  parámetros por petición, no por lista, así que trocear no basta: con unas 2000 fusionadas los
  listados fallarían allí. Salida: que el NOT IN sea una subconsulta o semi-join contra las parties
  marcadas como duplicado (dentro de un proceso con una base) o contra una tabla de fusionadas que
  cada contexto mantenga con el evento de Parties (procesos separados). Mientras tanto,
  `MergedParties` (`contexts/parties/application/ports.go`) reconstruye las parties enteras sólo
  para sacar sus ids: el repositorio genérico no tiene una lectura de identidades (`FindIDs`), que
  habría que añadir a `sqlrepo` y a `memory`.

### 2b-bis. Observabilidad: siguientes pasos
Hecho (ver [docs/OBSERVABILIDAD.md](docs/OBSERVABILIDAD.md), decisiones D1 a D8 aprobadas el
2026-10-06, pasos 1 a 7): contratos `pkg/trace` y `pkg/metrics` con implementaciones sin
dependencias y `/metrics`; registro que lee la correlación, la traza y el actor del contexto;
correlación del cliente validada; `distribution.Observe` (una línea JSON, un span y una métrica
por petición, y la causa de todo 5xx); `pipeline.Observed`; `sqlrepo.WithTelemetry`; relay que
no calla por defecto, con contadores y antigüedad del pendiente más viejo; consumidor medido.

Pendiente:
- **`pipeline.Observed` en la `chain` de cada contexto** (D7): una línea por contexto, en una
  tanda aparte cuando no haya otras sesiones sobre `contexts/`. Hasta entonces los contextos
  dejan HTTP y SQL, pero no el tramo del caso de uso.
- **Paso 8: la traza cruza el outbox** (D6): `traceparent` en `OutboxMessage` y en el sobre,
  columna nueva en los cinco motores; hoy el relay y el consumidor abren traza propia y el flujo
  se sigue por la correlación.
- **Paso 9: adaptador de OpenTelemetry** en un módulo `otel/` con su propio `go.mod` (D3).
  Antes hay que aclarar la licencia (GPL-2.0 del framework frente a Apache-2.0) y medir las
  dependencias que arrastra.
- **Ejecutable de servicio** que monte la composición (logger de servicio, puerto interno de
  `/metrics`, relay con telemetría). Los programas ya existen (`cmd/karpo`, `cmd/karpo-postgres`,
  sobre `host/serve`), pero todavía no montan esa telemetría.
- **Envoltorio de `http.RoundTripper`** que propague `traceparent` y `X-Correlation-ID`, cuando
  haya clientes HTTP (costuras HTTP, resolvedor de autorización en modo `Http`).
- **Número de pendientes y de aparcados del outbox** como métricas: exige ampliar `OutboxStore`.
- **Medir el coste de la instrumentación sin E/S**: `BenchmarkAppend` (SQLite en disco) no lo
  resuelve.
- **`ARQUITECTURA.md` §6 y §7** (paso 10) sin actualizar.
- **Prueba intermitente ajena:** `TestParties_EndToEnd_MemoryThenSQLite/Security_directory`
  (`contexts/parties`) falló una vez con «end e-mail: status 422» al correr toda la suite y pasó
  en 12 repeticiones, también en el checkout sin estos cambios. Sin investigar.

### 2c. Security: siguientes pasos
Hecho (ver [docs/SEGURIDAD.md](docs/SEGURIDAD.md), decisiones aprobadas el 2026-10-04): contexto
`contexts/security` con usuarios, roles, catálogo de permisos declarado por cada contexto, acceso
por organización, sesiones con rotación, identidades externas y el `authz.Directory` real.

Pendiente:
- **Verificador OIDC** (`contracts.TokenVerifier` con clave pública) cuando la prueba G-43 elija
  el proveedor de identidad; hoy solo existe el puerto y un verificador de prueba.
- **Resolutor en modo `Http`** sobre `GET /api/auth/context`, para servicios que no alojen Security.
- **Límite de intentos por origen** al iniciar sesión, y purga de las sesiones caducadas.
- **Front de Angular**: leer permisos y accesos del contexto, no del token (decisión 2).
- **Importar los usuarios de C#** (`infrastructure.FromCSharp` ya convierte sus hashes).
- Segundo factor, recuperación de contraseña y alta por invitación (o delegarlos en el proveedor).
- Desactivar al usuario al recibir `hr.employee-terminated.v1`.
- Principales de servicio en base de datos, el día que haya que administrarlos sin desplegar.
- Vigencia de las asignaciones de rol e `IncludeSubsidiaries` (P2).
- **Sincronizar el catálogo con varios procesos** (macroservicios): hoy cada proceso declara los
  permisos de los contextos que aloja y desactiva los que no declara. Con dos procesos que alojan
  contextos distintos, cada arranque desactivaría los permisos del otro, y con D-98 (se conceden
  una sola vez) cada vaivén los quitaría y volvería a dar a los roles del sistema. Antes de partir
  el monolito: que cada proceso sólo gestione los permisos de sus contextos (por espacio de
  nombres), o un catálogo declarado por despliegue. Lo señaló la revisión de D-98.
- **Roles por organización:** decidir si un rol debe valer sólo en una organización del usuario.
  Hoy vale en todas: quien recibe «Currency desk» de un `OrganizationAdmin` cotiza también en sus
  otras organizaciones (D-141, ver [docs/SEGURIDAD.md](docs/SEGURIDAD.md), sección 11).

### 2d. Revisión de la integración del 2026-10-11: lo que queda
Hallazgos menores de la revisión independiente (G-6) que no se arreglaron en la rama
`integra/2026-10-11`, más lo que dejaron pendiente los arreglos que sí se hicieron (ver
[docs/archivo/integracion-2026-10-11.md](docs/archivo/integracion-2026-10-11.md), «Arreglos tras la revisión»).

- **Prefijo reservado para los códigos de catálogo que añade un administrador** (decisión de
  producto, pendiente): la unicidad del código ya la asegura un índice único (migración 29,
  `docs/PARTIES.md` §22), pero si una versión futura siembra un tipo con un código que un
  administrador ya usó, la migración de esa siembra se detiene y dice cuál (`codeTaken`). Reservar
  un prefijo para los añadidos (por ejemplo `x-`) lo evitaría; cambia la API (`code` de
  `POST /api/catalogs/party-role-types` y `.../party-relationship-types`) y los tipos ya
  añadidos.
- **Historial de números de cliente** — `contexts/parties/application/details.go`: un número que
  se cambia o se retira a un cliente vigente queda libre para otro (`docs/PARTIES.md` §9).
- ~~**Comentario de `cmd/karpo-postgres`**~~ ✅ (2026-11-14). El informe de la revisión no se
  conserva. Entonces la cabecera estaba en inglés, contra la regla nueva de comentar en español
  (la bitácora la deja «para Javier»), y D-84 la tradujo el 2026-10-17. Revisada otra vez contra
  el código, faltaba el mínimo de `KARPO_DB_MAX_CONNS` (2), y ya está.

### 2e. Varios albaranes en una factura (D-146): lo que queda
Ver [docs/FACTURACION.md](docs/FACTURACION.md), «Varios albaranes en una factura».

- ~~**Cierre del periodo programado**~~ ✅ `CloseDuePeriods` en las tareas del anfitrión: cada
  empresa cierra el mes anterior en su zona horaria (docs/FACTURACION.md).
- ~~**Documentos enlaza sólo el primer albarán**~~ ✅ enlaza todos los de `sources` (docs/DOCUMENTOS.md).

### 2f. Nombres traducibles de delimitaciones y catálogos (B-24 del front)

Hoy cada delimitación (los países también) y cada valor de catálogo tienen un solo nombre, en el
idioma de la semilla: `geo_boundaries.name`, los tipos de delimitación y de instalación, las
monedas, los tipos de vía y los títulos de puesto. Con la interfaz en español salen textos en
inglés («Building», «Warehouse»), y hoy el front lo apaña en sus datos simulados (B-24 de
`front/react/docs/PETICIONES-BACKEND.md`).

- **Opciones:**
  1. Una clave estable y un diccionario en el front. Sirve para los catálogos cerrados, no para
     los miles de nombres geográficos.
  2. Una tabla de traducciones por entidad, con el nombre de la semilla como base, elegida por
     `Accept-Language`.
  3. Además de lo anterior, el nombre oficial y el nombre en las lenguas cooficiales de cada país.
- **Antes de decidir:** qué hacen Karpo C# y Trench. Las fuentes del mapa (Q-8: Natural Earth,
  OSM e IGN) traen los nombres en varios idiomas.
- **Cómo se lleva:** como tarea mediana con la forma de trabajo de `docs/FORMA-DE-TRABAJO.md`. Es
  una decisión de modelo común a los tres proyectos: el plan llega a Javier con la decisión.
- Javier lo pidió al backlog el 2026-11-27.

### 2g. Mapa de delimitaciones (B-2 y B-26): lo que queda

La tesela `karpo.pmtiles` y la API con `code` y `parentCode` están hechas (rama
`feat/mapas-pmtiles`, ver `docs/GEOGRAFIA.md`). Queda:

- **Navarra:** la comunidad sale `ES-NA` y la ISO 3166-2 es `ES-NC`. Se corrige en la semilla con
  una migración de datos; cambia también la ruta pública de festivos. El dato es común con Trench
  y Karpo C#: se propone como decisión común. Tarea aparte.
- **OSM (D-a):** el mundo sale de Natural Earth y España del IGN. Las regiones de otros países
  (Geofabrik y osmium) llegan cuando haya clientes fuera de España.
- **Islas (D-c, provisional):** la clave de la isla es su NUTS 3 (`ES532`), como excepción. La
  alternativa, las 52 provincias INE sin islas, exige cambiar la semilla.
- **`shortName`:** no se emite; pasa a §2f (B-24).
- **Países sin geometría propia en Natural Earth 10m** (BQ, BV, CC, CX, GF, GP, MQ, RE, SJ, TK y
  YT): si hacen falta, salen de OSM.
- **Las migas del front desde la URL con el `.pmtiles` real** son trabajo del front (su sesión).
- **Instantánea de la BDDAE (opcional):** el WFS cambia cada trimestre, así que regenerar idéntico
  exige guardar las 9 páginas descargadas (752 MB, CC-BY 4.0 lo permite) fuera del repositorio.
  Hoy lo que se conserva es el artefacto y su procedencia (`karpo.provenance.txt`).
- **Zoom máximo:** la tesela llega a z8 porque el front fija `maxzoom: 8`. Si el front sube su
  zoom (callejero), el siguiente escalón es z10: `MAXZOOM=10 tools/geo/build.sh`.
- **Supervivientes de mutación** de esta rama: ver la entrega de `b2-mapas-pmtiles`.

### 3. PoC Frontend: Evaluación de Stack Ligero (Svelte 5 / SolidJS)
- Prototipar la interfaz de listado y ficha de `Parties`.
- Comparar tamaño de bundle (< 100 KB objetivo vs ~9.4 MB de Angular 20) y velocidad de carga/hidratación.
- Ampliado el 2026-11-14 (Javier), con §7: evaluar qué frameworks de front pueden ofrecerse para
  las aplicaciones generadas, como hace Supabase con sus plantillas.

Los apartados 4 (enrutar `/api/parties` a Go con Nginx) y 5 (metamodelo y DSL en grafo, que hoy
es Theros) salieron de aquí el 2026-11-14: están en
[docs/archivo/backlog-retirado-2026-11-14.md](docs/archivo/backlog-retirado-2026-11-14.md).

### 6. Impuestos indirectos (IVA y equivalentes): cálculo por país y por sector

Decisión de Javier (2026-09-28): el cálculo se estructura **por país** (cada jurisdicción tiene
sus impuestos, reglas, modelos y sistemas de envío) y dentro de cada país **por régimen o
sector**. En C# nada calculaba el IVA.

**Hecho** (ver [docs/FISCAL.md](docs/FISCAL.md) y [docs/FACTURACION.md](docs/FACTURACION.md)):
- Fiscal, independiente del país:
  - catálogo de tipos con vigencia y tratamientos;
  - `Rates.RateOn`;
  - el puerto `TaxEngine`, que resuelve la jurisdicción según el país del vendedor;
  - la interfaz `Jurisdiction` con `Assessment`, `Breakdown` y `RateBook`.
- Jurisdicción **España** (`contexts/fiscal/jurisdictions/es`), solo el **régimen general**:
  - base por tipo agregada en el documento y cuota por tipo al céntimo;
  - exentas y no sujetas por tratamiento;
  - recargo de equivalencia a petición del documento;
  - IGIC en Canarias e IPSI en Ceuta y Melilla con sus tipos del catálogo;
  - bases negativas (rectificativas por diferencias).
- Facturación congela el desglose al emitir y lo publica en `billing.invoice-issued.v1`.


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
- Cálculo básico (lo hecho está arriba):
  - descuentos globales en factura y pronto pago (el descuento por línea ya existe);
  - suplidos fuera de la base (art. 78);
  - anticipos;
  - portes;
  - envases y embalajes.
- Territorios:
  - IVA común;
  - IGIC de Canarias (tipos propios y AIEM);
  - IPSI de Ceuta y Melilla;
  - territorios forales (País Vasco y Navarra: normativa y modelos propios, TicketBAI).
- Recargo de equivalencia (comercio minorista): el cálculo ya existe. Falta que el régimen
  venga del perfil comercial del cliente en lugar del documento.
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

### 7. Estudio: integraciones de terceros, frameworks de front y destinos de despliegue (con Theros)

Pedido por Javier el 2026-11-14. Es un estudio, no una implementación. Lo que salga de él para el
generador se pide a «Orquestador Theros».

- **Integraciones de terceros:** qué herramientas externas ya hechas pueden integrarse con Theros
  y con las aplicaciones que genera, al estilo del catálogo de integraciones de Supabase.
- **Frameworks de front:** cuántos pueden ofrecerse para las aplicaciones generadas, como en el
  diálogo «Connect to your project» de Supabase. Allí aparecen Next.js, React Router, React, Nuxt,
  Vue.js, SvelteKit, Solid.js, Astro, Refine y TanStack Start. El mismo diálogo ofrece también
  otras formas de conectarse: biblioteca cliente, API de servidor, cadena de conexión directa, ORM
  y MCP. Recoge la PoC de §3.
- **Destinos de ejecución:** en qué plataformas pueden desplegarse las aplicaciones generadas,
  según el lenguaje: AWS, Azure y otras.
