# Aprobaciones

Decisión D-156 ([DECISIONES-COMUNES.md](DECISIONES-COMUNES.md)): cualquier acción configurada puede
exigir aprobación; qué acciones la exigen se configura por empresa; aprueba quien tenga el permiso de
aprobar esa acción y no sea quien la pidió. Primer uso: bloquear y desbloquear proveedores y clientes.

Este documento cubre el **paso 1**, el motor en el framework. Los pasos 2 y 3 están pendientes (abajo).

## Conceptos

| Concepto | Dónde | Qué es |
|---|---|---|
| Acción | `pkg/domain/approval.Action` | Nombre estable en tres segmentos en minúsculas, `{contexto}.{recurso}.{verbo}`: `purchases.supplier.block`. Se persiste: no se renombra |
| Solicitud | `approval.Request` (agregado `approval.request`) | Empresa, acción, objetivo (`Target{Type, ID}`), carga útil JSON con lo pedido, motivo, solicitante, estado, quién resolvió, cuándo, comentario y versión |
| Estado | `approval.Status` | `pending` → `approved`, `rejected` o `cancelled`. Los tres últimos son finales |
| Eventos | `approval.Requested`, `Approved`, `Rejected`, `Cancelled` | `approval.requested`, `approval.approved`, ... Los de resolución llevan acción, objetivo y carga útil, para aplicar la acción sin releer la solicitud |
| Política | `pkg/application/approval.Policy` | «¿Esta acción exige aprobación en esta empresa?». Hay `Never` (por defecto) y `Fixed` (lista fija, para pruebas) |
| Permiso de aprobar | `approval.ApprovePermission` | Se deriva de la acción con la forma de los permisos de Karpo: `purchases.supplier.block` → `Purchases.Supplier.ApproveBlock`; `sales.credit_limit.raise` → `Sales.CreditLimit.ApproveRaise`. El mismo permiso aprueba y rechaza |
| Servicio | `approval.Service` | `Request`, `Approve`, `Reject`, `Cancel`. El actor sale del contexto de autorización (`authz.FromContext`) |
| Persistencia | `pkg/persistence/approvals` | `Mapping(tabla)` para `sqlrepo` y `DDL(tabla)` con los marcadores de dialecto. En memoria, el repositorio genérico `memory.NewRepository` |

Reglas, en el dominio: una solicitud se resuelve una sola vez (`approval.already_resolved`); quien la
pidió no la aprueba ni la rechaza (`approval.self_resolution`), aunque tenga el permiso; sólo quien la
pidió la cancela (`approval.not_requester`); rechazar exige comentario (`approval.comment_required`).
Las cuatro son `domain.Violation` (422).

**Se resuelve sólo a través de `Service`**: los métodos del agregado no comprueban permisos.
`Service.Request` tampoco comprueba ninguno: lo comprueba el caso de uso que crea la solicitud, con el
permiso de la acción original (p. ej. el de bloquear el proveedor). En aplicación: aprobar o rechazar sin el permiso es
`ErrForbidden` (403); sin contexto de autorización, `ErrUnauthorized` (401).

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
- **La tabla es de cada contexto**, como su outbox, con el nombre `<prefijo-del-contexto>_approvals` (p. ej. `pur_approvals`; como mucho 23 caracteres, para que el índice quepa en los 30 de Oracle), y no un
  esquema `workflow` compartido: cada contexto guarda sus solicitudes en la misma transacción que su
  acción, sin depender de otro módulo.

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

## Pendiente

- **Paso 2**: la política configurable por empresa (qué acciones exigen aprobación en cada una), con su
  tabla y su pantalla. Sustituye a `Never` en la composición.
- **Paso 3**: el primer uso, bloquear y desbloquear proveedores en Compras y clientes en Ventas.
- Que el aprobador tenga además acceso de escritura a la empresa (`authz.RequireWrite`) se decidirá en el
  paso 3, cuando se vea cómo se nombra la empresa en esos contextos.
