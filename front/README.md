# Front de Karpo Go

Karpo Go no tiene un front propio: usa el **motor de Angular de Karpo C#**
(`010-Front/Angular` del repositorio de Karpo), que se dirige por configuración. Aquí está la
configuración de las pantallas de los contextos de Go que hablan el **contrato de
mantenimiento** (las rutas, las formas y los errores que el motor lee; el contrato `crud` del
lenguaje de Theros).

| Contexto | Pantalla | Configuración | API |
|---|---|---|---|
| Productos | catálogo de productos: rejilla, alta, detalle, retirada | `angular/config/entities/products/product.config.json` | `/api/v1/products` (docs/PRODUCTOS.md, «El contrato de mantenimiento») |
| Instalaciones | instalaciones de las organizaciones internas: rejilla, alta, detalle, desactivación | `angular/config/entities/facilities/facility.config.json` | `/api/v1/facilities` (docs/INSTALACIONES.md, «El contrato de mantenimiento») |
| Activos | registro de inmovilizado: rejilla, alta, detalle, cambio y baja (por el cambio) | `angular/config/entities/assets/asset.config.json` | `/api/v1/assets` (docs/ACTIVOS.md, «El contrato de mantenimiento») |
| Envíos | transportistas: rejilla, alta, detalle, cambio e historial de bloqueos | `angular/config/entities/shipments/carrier.config.json` | `/api/v1/carriers` (docs/ENVIOS.md, «El contrato de mantenimiento») |
| Compras | proveedores (perfil de compra): rejilla, alta, detalle, cambio e historial de bloqueos | `angular/config/entities/purchases/supplier.config.json` | `/api/v1/suppliers` (docs/COMPRAS.md, «El contrato de mantenimiento») |

## Cómo se ve

1. Arrancar el anfitrión de Go (`go run ./cmd/karpo`, sobre SQLite; ver docs/ANFITRION.md).
2. En una copia del motor de Angular, copiar `angular/config/entities/products/product.config.json`
   sobre `public/config/entities/products/product.config.json` y apuntar el proxy de `/api` del
   servidor de desarrollo al puerto del anfitrión de Go.
3. Entrar con un usuario de Go con `Products.Product.Read` (y `…Update` para cambiar).

El motor antepone `/api/v1` al `apiEndpoint` de la configuración (`/products`).

## Lo que falta

- Probarlo en un navegador: la prueba de Go recorre el contrato por HTTP
  (`TestProducts_MaintenanceContract_MemoryThenSQLite`), no el motor.
- Las partes del producto (códigos de barras, componentes, proveedores) no viajan por el
  contrato: se mantienen por `/api/products/products`, y un cambio por el contrato las conserva.
- Los demás contextos.

## Bloqueos y aprobaciones: lo que el motor no admite todavía

Tras D-156, bloquear y desbloquear proveedores y transportistas son órdenes
(`POST /api/v1/<entidad>/{id}/block|unblock` con `{version, reason}`), el `DELETE` bloquea pero
exige motivo, y con aprobación activa las órdenes responden 202 con `pendingApproval`.

Configurado hoy (2026-11-15), con lo que el motor admite:

- **Sin acción «Bloquear» en la rejilla.** Era la acción `delete`, y el motor manda el `DELETE`
  sin cuerpo ni `?reason=`: siempre acababa en 422. Se quita hasta que el motor pueda pedir motivo.
- **Pestaña «Bloqueos»** en el detalle: rejilla hija (`detail.childGrids`) sobre
  `GET /api/v1/<entidad>/{id}/block-history` (el motor sustituye `{id}` y acepta la lista plana).
  Añade `?<entidad>Id=…` y `page`/`pageSize`, que Go ignora.

Lo que falta en el motor de Karpo C# (`010-Front/Angular`), para pedírselo a su sesión:

1. **Órdenes declarativas por fila y en la cabecera del detalle.** Hoy `rowActions[].action` es
   el id de un comando registrado en código (`view`, `edit`, `delete`) y `detail.headerActions`
   está declarado pero no se pinta. Propuesta:

   ```json
   "commands": [
     {
       "id": "block", "label": "Bloquear", "icon": "lock", "color": "danger",
       "surfaces": ["row", "detail-header"],
       "visibleWhen": { "field": "blocked", "equals": false },
       "method": "POST", "endpoint": "/suppliers/{id}/block",
       "prompt": { "field": "reason", "label": "Motivo", "required": true, "maxLength": 200 },
       "sendVersion": true,
       "successMessage": "Proveedor bloqueado"
     }
   ]
   ```

   `prompt` abre un diálogo con un texto (obligatorio o no) que viaja en el cuerpo con el nombre
   de `field`; `sendVersion` añade la `version` del registro; `{id}` (y `{campo}`) se sustituyen
   con la fila. Al terminar, se recarga la fila o la ficha. «Desbloquear» igual, con
   `blocked = true` y `/unblock`. `visibleWhen` necesita una cláusula sobre un campo de la fila.
2. **Motivo en el borrado.** `delete` con `"prompt"` como arriba, enviado en el cuerpo del
   `DELETE` (`{"reason": …}`); hoy `GenericEntityService.delete` no manda cuerpo.
3. **El 202 no es un éxito callado.** Si la respuesta es 202 (o trae `pendingApproval`), avisar
   «Solicitud enviada; queda pendiente de aprobación» y no dar el cambio por hecho. Propuesta:
   `"acceptedMessage": "Queda pendiente de aprobación"` en la orden y en la entidad (para el
   guardado y el borrado).
4. **Endpoint fuera de la versión.** `buildUrl` antepone siempre `/{apiVersion}`; las bandejas
   están en `/api/purchases/approvals`, `/api/shipments/approvals` y `/api/financial/approvals`,
   sin `v1`. Propuesta: `"apiVersioned": false` en la entidad (o un `apiEndpoint` que empiece por
   `~/` para ir tal cual bajo `apiUrl`).
5. **Entidad de sólo lectura con órdenes: la bandeja.** Con 1, 3 y 4 bastaría una entidad
   `"editable": false` sin alta, por ejemplo `purchases/approval.config.json`:

   ```json
   {
     "id": "purchase-approval", "apiEndpoint": "/purchases/approvals", "apiVersioned": false,
     "editable": false,
     "filters": [{ "field": "status", "serverField": "status", "type": "select",
                   "options": ["pending", "approved", "rejected", "cancelled", "all"] }],
     "commands": [
       { "id": "approve", "label": "Aprobar", "method": "POST",
         "endpoint": "/purchases/approvals/{id}/approve",
         "prompt": { "field": "comment", "required": false },
         "visibleWhen": { "field": "status", "equals": "pending" } },
       { "id": "reject", "label": "Rechazar", "method": "POST",
         "endpoint": "/purchases/approvals/{id}/reject",
         "prompt": { "field": "comment", "required": true },
         "visibleWhen": { "field": "status", "equals": "pending" } },
       { "id": "cancel", "label": "Cancelar", "method": "POST",
         "endpoint": "/purchases/approvals/{id}/cancel",
         "prompt": { "field": "comment", "required": false },
         "visibleWhen": { "field": "status", "equals": "pending" } }
     ]
   }
   ```

   Los errores 422 (`approval.self_resolution`, `approval.already_resolved`,
   `approval.comment_required`, `approval.not_requester`) se enseñan con su mensaje, como los
   del guardado.

Cuando el motor lo tenga, se configuran aquí las órdenes de proveedores y transportistas y las
bandejas de Compras, Envíos y Finanzas.
