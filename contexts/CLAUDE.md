# CLAUDE.md — `contexts/` (los contextos de negocio)

Un contexto delimitado por carpeta. Forma habitual: `domain/`, `application/` (casos de uso; sus
permisos, en la función `Permissions()`), `infrastructure/` (persistencia y `Migrations()`, en
`persistence.go` o, en Parties y Security, en `schema.go`), `contracts/` (lo que pueden usar los
demás), a veces `distribution/`, y `module.go` en la raíz, que lo monta.

- **Entre contextos sólo por contratos** (`archtest`, regla 7): un contexto importa de otro sólo su
  paquete `contracts`, y nunca desde su `domain/`. Lo demás va por eventos de integración
  versionados (`<contexto>.<hecho>.v1`, `docs/EVENTOS-INTEGRACION.md`) o por un puerto que cablea
  `host/`. Nada de llamadas directas que haya que deshacer cuando haya varios procesos.
- **El dominio** (`domain/`) sólo usa la biblioteca estándar, `pkg/domain` y su propio dominio.
- **Esquema:** cada contexto lleva su propia serie de migraciones. **Una migración aplicada no
  cambia nunca**: se añade la siguiente, con las sentencias de cada motor que lo necesite
  (`pkg/persistence/sqlrepo`, tipo `Migration`; `docs/ESQUEMA-MIGRACIONES.md`).
- **Permisos** (`Contexto.Recurso.Acción`): llegan al catálogo de Security a través del anfitrión.
  Uno nuevo o renombrado se anota en el documento del contexto.
- **Un contexto nuevo** se monta en `host/host.go` (mensajes, permisos, rutas y tareas), tiene su
  documento en `docs/` y su fila en el índice de `README.md`.
- **El documento del contexto** (`docs/<CONTEXTO>.md`, índice en `README.md`) se cambia en la misma
  rama que el código: rutas, permisos, migraciones, eventos y decisiones.
- **Decisiones:** las de un contexto las aprueba Javier y quedan en su documento; si valen también
  para Karpo C# o Trench, van además al registro (`docs/DECISIONES-COMUNES.md`). Antes de cambiar
  una regla de Parties, Facturación o Pedidos, se mira si el registro la fija.
- **El contrato de mantenimiento** (`maintenance.go` en assets, facilities, products y shipments;
  rutas `/api/v1/...`) es el que lee el motor de Angular de Karpo C#: sus rutas, formas y errores
  no cambian sin cambiar también `front/angular/config` (`front/README.md`).
- **Sectoriales:** `financial` sólo existe para una empresa con el rol de entidad financiera en
  Parties (`docs/CUENTAS-CLIENTES.md`). Que una pieza portada de C# parezca vacía no es motivo
  para proponer retirarla: antes se pregunta si es de un sectorial.
