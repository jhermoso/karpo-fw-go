# Aprobaciones

Decisión D-156 ([DECISIONES-COMUNES.md](DECISIONES-COMUNES.md)): cualquier acción configurada puede
exigir aprobación; qué acciones la exigen se configura por empresa; aprueba quien tenga el permiso de
aprobar esa acción y no sea quien la pidió. Primer uso: bloquear y desbloquear proveedores y clientes.

Este documento cubre el **paso 1**, el motor en el framework, y el **paso 2**, la configuración por
empresa en Parties, y el **paso 3** en Compras: bloquear y desbloquear proveedores (abajo).

## Conceptos

| Concepto | Dónde | Qué es |
|---|---|---|
| Acción | `pkg/domain/approval.Action` | Nombre estable en tres segmentos en minúsculas, `{contexto}.{recurso}.{verbo}`: `purchases.supplier.block`. Se persiste: no se renombra |
| Solicitud | `approval.Request` (agregado `approval.request`) | Empresa, acción, objetivo (`Target{Type, ID}`), carga útil JSON con lo pedido, motivo, solicitante, estado, quién resolvió, cuándo, comentario y versión |
| Estado | `approval.Status` | `pending` → `approved`, `rejected` o `cancelled`. Los tres últimos son finales |
| Eventos | `approval.Requested`, `Approved`, `Rejected`, `Cancelled` | `approval.requested`, `approval.approved`, ... Los de resolución llevan acción, objetivo y carga útil, para aplicar la acción sin releer la solicitud |
| Política | `pkg/application/approval.Policy` | «¿Esta acción exige aprobación en esta empresa?». Hay `Never` (por defecto) y `Fixed` (lista fija, para pruebas); la configurable por empresa es la de Parties (paso 2, abajo) |
| Permiso de aprobar | `approval.ApprovePermission` | Se deriva de la acción con la forma de los permisos de Karpo: `purchases.supplier.block` → `Purchases.Supplier.ApproveBlock`; `sales.credit_limit.raise` → `Sales.CreditLimit.ApproveRaise`. El mismo permiso aprueba y rechaza |
| Servicio | `approval.Service` | `Request`, `Approve`, `Reject`, `Cancel`. El actor sale del contexto de autorización (`authz.FromContext`) |
| Persistencia | `pkg/persistence/approvals` | `Mapping(tabla)` para `sqlrepo`, `DDL(tabla)` y `PendingKeyDDL(tabla)` con los marcadores de dialecto. En memoria, el repositorio genérico `memory.NewRepository` |
| Grupo y clave | `approval.InGroup`, `RequestInput.Group`, `Request.PendingKey` | Una sola solicitud pendiente por grupo y objetivo (abajo, «Una sola pendiente») |

Reglas, en el dominio: una solicitud se resuelve una sola vez (`approval.already_resolved`); quien la
pidió no la aprueba ni la rechaza (`approval.self_resolution`), aunque tenga el permiso; sólo quien la
pidió la cancela (`approval.not_requester`); rechazar exige comentario (`approval.comment_required`).
Las cuatro son `domain.Violation` (422).

**Se resuelve sólo a través de `Service`**: los métodos del agregado no comprueban permisos.
`Service.Request` tampoco comprueba ninguno: lo comprueba el caso de uso que crea la solicitud, con el
permiso de la acción original (p. ej. el de bloquear el proveedor). En aplicación: aprobar o rechazar sin el permiso es
`ErrForbidden` (403); sin contexto de autorización, `ErrUnauthorized` (401).

## Una sola pendiente (la clave común)

Sobre un mismo objetivo no puede haber dos solicitudes pendientes del mismo **grupo**. El grupo lo
decide quien crea la solicitud (`RequestInput.Group`, o `approval.InGroup` en `approval.New`); sin
él, el grupo es la propia acción (una pendiente por acción y objetivo). Para bloqueos se usa
`approval.GroupBlock` (`"block"`), de modo que bloquear y desbloquear el mismo objetivo no conviven.

- **La clave** (`Request.PendingKey`) vale `grupo:tipo:id` del objetivo mientras la solicitud está
  pendiente (`block:purchases.supplier_profile:<id>`) y su propio id al aprobarse, rechazarse o
  cancelarse. Nunca es NULL, así que un índice único normal vale en los cinco motores (SQL Server no
  admite dos NULL en uno).
- **En la base**, la columna `pending_key` `{str:200}` con el índice único `ux_<tabla>_pending`
  (por eso la tabla mide como mucho 19 caracteres, `approvals.MaxTableLen`). El grupo no tiene
  columna: se lee del principio de la clave mientras está pendiente.
- **El conflicto** es `approval.ErrPending` (envuelve `domain.ErrConflict`, 409): lo devuelve
  `Service.Request` si ya hay una pendiente (también en memoria, con `approval.PendingKeyField`) y
  `Save` si choca con el índice. Cada contexto lo traduce a su código con `errors.Is`
  (`purchases.approval_pending`, ...).
- **Migraciones.** Tabla nueva: `DDL(t)` y `PendingKeyDDL(t)` en la misma migración. Tabla que ya
  existía sin la columna: `PendingKeyColumnDDL(t)` con `Run: FillPendingKeys(t, grupo)` y, en la
  siguiente, `PendingKeyIndexDDL(t)`. Con la columna en otro formato: sólo `Run: FillPendingKeys`.
  `FillPendingKeys` recalcula todas las filas; si dos pendientes chocan, la más antigua se queda la
  clave y las demás su id (siguen pendientes y se pueden resolver).

| Contexto | Tabla | Migraciones de la clave común |
|---|---|---|
| Compras | `pur_approvals` | 10 (recalcula; columna e índice de las 8 y 9) |
| Envíos | `shp_approvals` | 6 (columna y relleno) y 7 (índice) |
| Cuentas | `fin_approvals` | 6 (columna y relleno) y 7 (índice) |
| Cobros | `rec_approvals` | 6 (recalcula; columna e índice de la 5) |
| Pedidos | `ord_approvals` | 12 (recalcula; columna e índice de la 11) |

## Las rutas bajo `/api/v1`

Los cinco contextos publican su bandeja en `/api/<contexto>/approvals` (`GET`, y `POST
/{id}/approve|reject|cancel`) y, con las mismas rutas y respuestas, en
`/api/v1/<contexto>/approvals`, porque el motor de Angular de Karpo C# sólo llama a rutas con `/v1`
(front/README.md). Las constantes son `ApprovalsBase` y `ApprovalsBaseV1` de cada módulo.

## Diferencias con Karpo C# y por qué

Karpo C# tiene un motor de flujos general (`Paranoia.Karpo.Fw.Workflow`): definiciones versionadas
en JSON con su hash (`WorkflowDefinition`, `WorkflowDefinitionVersion`), instancias con estado actual,
variables, reintentos y fecha del siguiente reintento (`WorkflowInstance`), historial de pasos y de
transiciones (`WorkflowStepExecution`, `WorkflowTransition`), tres catálogos de estados y tipos de paso,
y un contrato de motor (`IWorkflowEngine`: `Start`, `Advance`, `SignalExternal`, `Cancel`) con
manejadores de paso registrados. El motor en sí está sin implementar («Fase 2»).

Aquí sólo hace falta un flujo: un paso humano que aprueba o rechaza. Por eso:

- **Sin definiciones ni versiones**: el flujo es fijo y está en el código del agregado. Si algún día
  hace falta más de un nivel de aprobación, se añade al agregado, no un intérprete de JSON.
- **Estados como constantes, no catálogo**: cuatro valores fijos, como el resto de estados del framework.
- **Sin pasos, transiciones, variables ni reintentos**: la carga útil JSON hace de variables; el
  historial lo da el registro de auditoría y los eventos; los reintentos de aplicar la acción los da el
  outbox, que reentrega el evento `approval.approved`.
- **Sin `SignalExternal`**: aprobar, rechazar y cancelar son las únicas señales.
- **La tabla es de cada contexto**, como su outbox, con el nombre `<prefijo-del-contexto>_approvals` (p. ej. `pur_approvals`; como mucho 19 caracteres, para que el índice `ux_<tabla>_pending` quepa en los 30 de Oracle), y no un
  esquema `workflow` compartido: cada contexto guarda sus solicitudes en la misma transacción que su
  acción, sin depender de otro módulo.

## La configuración por empresa (paso 2)

Qué acciones exigen aprobación es un ajuste de cada empresa interna, en su perfil de Parties
(docs/PARTIES.md §25):

- **Tabla** `internal_org_approval_action` (migración 37 de Parties): `internal_organization_id`
  (clave ajena a `parties`) y `action` `{str:120}`, con clave compuesta. Una fila por empresa y
  acción; sin filas, ninguna acción exige aprobación (lo de siempre).
- **API:** `GET` y `PUT /api/profiles/internal-organizations/{id}/approval-actions`, con
  `{"actions": ["purchases.supplier.block"]}`; la lista reemplaza a la anterior, vacía o nula la
  borra. La respuesta lleva `internalOrganizationId` y `actions`, ordenadas y sin repetidas. Se lee
  con el permiso de leer parties y se cambia con el de modificar la empresa interna, como el resto
  del perfil. Una acción mal formada (`approval.ParseAction`) da 422 `Company.InvalidApprovalAction`.
  Cada cambio queda en la auditoría del perfil como `approvalActions`.
- **La política:** `parties.Module.ApprovalPolicy` es una `approval.Policy` que lee esa lista. Un
  contexto la recibe en su composición en lugar de `Never`, igual que recibe `CompanyProfiles` para
  el modo de servir los servicios: p. ej. `purchases.WithApprovalPolicy(h.Parties.ApprovalPolicy)`
  en `host/host.go`. No va en `contracts/` de Parties porque ese paquete sólo importa la biblioteca
  estándar; el tipo es el del framework.

## Cómo lo usará un contexto (paso 3)

1. **Al pedir la acción**, el manejador comprueba el permiso de ejecutarla, como siempre, y pregunta a
   la política: `policy.Requires(ctx, empresa, "purchases.supplier.block")`.
2. **Si no la exige**, la ejecuta como hasta ahora.
3. **Si la exige**, llama a `Service.Request` con la empresa, la acción, el objetivo
   (`Target{Type: "purchases.supplier", ID: id}`), la carga útil (lo pedido, p. ej. el motivo del
   bloqueo) y el motivo, y responde «pendiente de aprobación» con el id de la solicitud.
4. **Para resolver**, expone `Approve`, `Reject` y `Cancel` del servicio (sus propios endpoints, con la
   tabla del contexto).
5. **Al aprobarse**, un suscriptor del evento `approval.approved` con su acción la aplica: decodifica la
   carga útil y llama al mismo caso de uso que el paso 2, sin volver a preguntar a la política. Debe ser
   idempotente (el outbox puede reentregar).

El contexto declara en su catálogo de permisos el permiso de aprobar cada acción que admita aprobación
(`ApprovePermission(acción)`), para que Seguridad pueda concederlo.

## Paso 3 en Compras (hecho)

Como arriba, con estas concreciones (docs/COMPRAS.md, «Aprobación del bloqueo y el desbloqueo»):

- **Al aprobar, la acción se aplica en la misma transacción** que la resolución, no en un suscriptor
  de `approval.approved`: es lo más sencillo y no deja una solicitud aprobada sin aplicar. Es
  idempotente: si el objetivo ya está en el estado pedido, no se toca. Los eventos `approval.*` quedan
  en la auditoría y no salen al outbox.
- **El aprobador necesita también el ámbito de escritura** de la empresa de la solicitud (404 fuera
  de él, 403 de sólo lectura), como cualquier otro cambio del contexto.
- **Sólo se engancha a las órdenes de bloquear y desbloquear** (D-156 ampliada, 2026-11-15): el
  cambio general de la ficha ya no toca el bloqueo (422 `purchases.block_change_requires_command`).
  Con política, la orden responde 202 con `pendingApproval` y **no guarda el perfil** (ni sube su
  versión). Al aprobar, el bloqueo deja su fila en el historial (`pur_supplier_block_events`) con el
  id de la solicitud.
- **Una pendiente por objetivo, sea bloquear o desbloquear, también en la base:** la clave común
  del motor (arriba, «Una sola pendiente»), con el grupo `block`. Empezó en Compras con un mapeo
  propio y la clave `acción:objetivo`; desde 2026-11-15 es la del motor en los cinco contextos.

## Paso 3 en Envíos y en Cuentas (hecho, 2026-11-15)

D-156 ampliada: lo mismo que en Compras para el **transportista** (docs/ENVIOS.md, «El bloqueo») y
la **cuenta financiera** (docs/CUENTAS-CLIENTES.md, «El bloqueo»), con dos diferencias: bloquear y
desbloquear son órdenes propias (`POST …/{id}/block` y `…/unblock`, con motivo y versión), así que
la orden con aprobación no aplica nada (202 con `pendingApproval` y el agregado sin guardar); y cada
bloqueo y desbloqueo efectivo queda en una tabla de historial propia, en la misma transacción.

| Contexto | Acciones | Solicitudes | Historial | Bandeja |
|---|---|---|---|---|
| Envíos | `shipments.carrier.block`, `.unblock` | `shp_approvals` | `shp_carrier_block_events` | `/api/shipments/approvals` |
| Cuentas | `financial.account.block`, `.unblock` | `fin_approvals` | `fin_account_block_events` | `/api/financial/approvals` |

La empresa de la política es la del transportista (cada transportista es de una empresa) y la
entidad de la cuenta.

## Paso 3 en Ventas: los clientes (hecho, 2026-11-15)

Lo mismo para los dos bloqueos del cliente: el **crédito** de Cobros (docs/COBROS.md, «El bloqueo»)
y las **condiciones de venta** de Pedidos, que bloquean pedidos y entregas con un único motivo
(docs/PEDIDOS.md, «El bloqueo»). En Pedidos hay dos órdenes, bloquear y desbloquear, con los ámbitos
(`scopes`: `orders`, `delivery`), como Trench: una sola acción por sentido. Las dos tablas de
solicitudes llevan la clave común con índice único, como Compras.

| Contexto | Acciones | Solicitudes | Historial | Bandeja |
|---|---|---|---|---|
| Cobros | `receivables.credit.block`, `.unblock` | `rec_approvals` | `rec_credit_block_events` | `/api/receivables/approvals` |
| Pedidos | `orders.customer_terms.block`, `.unblock` | `ord_approvals` | `ord_terms_block_events` | `/api/orders/approvals` |

La empresa de la política es la del vendedor (Cobros) y la de las condiciones (Pedidos).

## Pendiente

- El front: enseñar que un cambio quedó pendiente y una bandeja de solicitudes.
