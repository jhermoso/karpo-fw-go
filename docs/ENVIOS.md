# Contexto Envíos

Port del subdominio `Shipments` de C# (`ErpKernel.Domain/Subdominios/Shipments`) al contexto
`contexts/shipments`.

La fase 1 es la **expedición de lo que se vende**: cada albarán de Pedidos genera un envío, que se
prepara (transportista, bultos, destino, seguimiento), sale, y se entrega o vuelve. El albarán
dice *qué* se entregó; el envío dice *cómo* viajó y *cuándo* llegó.

## Método

| Criterio | Peso |
|---|---|
| **U** Utilidad (uso real en contextos, sin Fw ni tests) | 40 % |
| **C** Corrección de la implementación | 25 % |
| **D** Diseño de la firma | 20 % |
| **G** Encaje en Go | 15 % |

Puntuación de 1 a 5 por criterio; nota ponderada sobre 100. **Mantener** ≥ 70 · **Modificar** 45–69 ·
**Retirar** < 45 · **Sustituido**.

## Estado del C#

- **Un envío sin dirección ni transportista.** `Shipment` tiene fechas estimadas, método, estado,
  remitente y destinatario, pero no tiene sentido (entrada o salida), ni transportista, ni número
  de seguimiento, ni fechas reales de salida y llegada.
- **Columnas que nadie escribe:** costes estimado y real, fecha límite de cancelación y los
  cuatro mecanismos de contacto (origen, destino…) están en la tabla, pero ninguna petición ni
  servicio los toca: siempre son `NULL`.
- **Estado libre.** El estado lo fija el cliente en el alta y en la modificación; cambiarlo no
  escribe historial, y crear una fila de historial no cambia el estado.
- **Ocho subtipos vacíos** (`CustomerShipment`, `PurchaseShipment`, `Transfer`, `DropShipment`,
  devoluciones…): clases de diez líneas sin discriminador, que nada instancia.
- **Sin tabla:** bultos y su contenido, tramos de ruta, métodos por transportista, portes, y toda
  la jerarquía de documentos de transporte. `Carrier` es un rol de Parties cuya tabla solo tiene
  el identificador, y ningún envío lo referencia.
- **Preparación y salida sin efecto.**
  - `Picklist` no enlaza con ningún envío ni pedido.
  - `ItemIssuance` no comprueba cantidades ni mueve existencias: un POST marca una línea como
    «entregada» y, por tanto, facturable.
- **Recepción mal formada.** `ShipmentReceipt` no enlaza con el envío ni con su línea, exige un
  motivo de rechazo aunque se acepte todo y apunta a una tabla de bultos que no existe. No valida
  cantidades ni da entrada en el almacén.
- **`DeliveryNote`** es una cabecera sin líneas, sin enlace con el pedido ni con la factura; su
  único flujo es numerarse a través de Documents.
- **Lecturas con tope:** 1 000 envíos y 5 000 líneas sin orden, filtrados en memoria.
- **Sin permisos:** 47 operaciones solo con autenticación.
- **Sin datos:** 18 filas de catálogos; ningún envío, albarán ni recepción.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `Shipment` + `ShipmentItem` (sin sentido, transportista ni seguimiento) | 4 | 2 | 2 | 3 | **59** | Modificar → agregado `Shipment` de **salida**: empresa, cliente, documento que expide, líneas (lo que viaja) y plan: método, transportista, seguimiento, destinatario, destino, bultos, peso, porte, instrucciones y fechas estimadas; fechas reales de salida y cierre, y quién lo recibió |
| 2 | `ShipmentStatusType` + `ShipmentStatus` (estado libre, historial desligado) | 3 | 1 | 2 | 3 | **46** | Modificar → estados `scheduled`, `in-transit`, `delivered`, `returned`, `cancelled` que **solo cambian por sus reglas**, con historial en el agregado |
| 3 | `ShipmentMethodType` (5 sembrados) | 3 | 3 | 2 | 3 | **56** | Modificar → lista cerrada `truck`, `courier`, `rail`, `air`, `ocean`, más `pickup` (lo recoge el cliente: sin transportista) |
| 4 | `Carrier` (rol de Parties con tabla vacía) + `CarrierShipmentMethod` | 3 | 1 | 2 | 3 | **46** | Modificar → agregado `Carrier` por empresa: código, nombre, tercero de Parties (opcional), **enlace de seguimiento** con `{tracking}` y bloqueo |
| 5 | `DeliveryNote` (cabecera sin líneas) | 3 | 2 | 1 | 3 | **47** | Sustituido → el albarán de **Pedidos** (con líneas, numerado, facturable). Envíos lo consume |
| 6 | `Picklist` + `ItemIssuance` (sin enlace ni efecto en existencias) | 2 | 1 | 1 | 2 | **31** | Sustituido → la reserva y la salida de stock ya las hacen Pedidos e Inventario por eventos |
| 7 | `ShipmentReceipt` + `RejectionReason` | 2 | 1 | 1 | 2 | **31** | Aplazado → albarán de entrada, en la fase 2 de **Compras** (decisión 1 de [COMPRAS.md](COMPRAS.md)) |
| 8 | `ShipmentPackage`, `PackagingContent`, `ShipmentRouteSegment` (sin tabla) | 2 | 1 | 2 | 2 | **35** | Retirar → número de bultos y peso total en el envío. Bultos detallados y tramos, en la fase 2 |
| 9 | Ocho subtipos de envío y documentos de transporte (sin tabla) | 1 | 1 | 1 | 2 | **23** | Retirar |
| 10 | Costes y mecanismos de contacto que nadie escribe | 2 | 1 | 2 | 3 | **38** | Retirar → se conserva solo `cost` (el porte que cobra el transportista) y la dirección de destino como texto de etiqueta |
| 11 | Recursos `Shipments.*` declarados y no comprobados | — | — | — | — | — | Sustituido → `Shipments.Shipment.Read/Update/Dispatch` y `Shipments.Carrier.Read/Update` (**preparar el envío y moverlo están separados**) |

## Diseño

```
contexts/shipments/
├── domain/          # Shipment (+Plan, Line, Step, Source), Carrier, reglas del estado
├── contracts/       # lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito; suscripción al albarán
├── infrastructure/  # esquema shp_* de 5 motores, mapeos, bandeja de entrada
└── module.go        # composición y rutas /api/shipments/... y /api/carriers/...
```

- **Del albarán al envío**, por eventos: `orders.delivery-issued.v1` planifica un envío con las
  líneas **almacenables** del albarán, en su fecha y sin nada decidido todavía. Los servicios no
  se envían: un albarán sin mercancía no genera envío. Un envío por albarán, aunque el mensaje
  llegue repetido.
- **El estado avanza por su regla**, en una fecha no anterior a la del cambio previo:

  | Desde | Puede pasar a | Requisitos |
  |---|---|---|
  | `scheduled` | `in-transit` | método y bultos; salvo recogida, transportista (no bloqueado) y destino |
  | `scheduled` | `cancelled` | motivo |
  | `in-transit` | `delivered` | quién lo recibe es opcional |
  | `in-transit` | `returned` | motivo: no se pudo entregar y ha vuelto |

  `delivered`, `returned` y `cancelled` son finales.
- **Qué se puede cambiar y cuándo.** Antes de salir, todo el plan. En tránsito, solo el
  seguimiento, la llegada estimada y el porte: transportista, destino y bultos ya no cambian. Un
  envío cerrado no se toca.
- **Transportistas.** Código único por empresa. Un transportista bloqueado no se asigna a envíos
  nuevos ni puede sacar uno, pero sigue en los que ya lo tenían. El enlace de seguimiento se
  construye con el número del envío, codificado para URL.
- **Envíos a mano**, sin albarán: muestras, material prestado, devoluciones al proveedor.
- **Lenguaje publicado** (sin consumidores todavía): `shipments.shipment-dispatched.v1`,
  `shipments.shipment-delivered.v1` y `shipments.shipment-returned.v1`, con el albarán de origen.
- **Tablas:** `shp_shipments` (+ `shp_shipment_lines`, `shp_shipment_history`), `shp_carriers`,
  las bandejas de salida, la auditoría y `shipments_inbox`.

## Decisiones (aprobadas por Javier el 2026-10-07)

1. **La fase 1 de Envíos es la expedición de salida** (lo que se entrega a clientes). Las
   entradas de proveedor van con la fase 2 de Compras, y los traspasos entre almacenes ya son de
   Inventario. Sugerencia: sí; así no hay dos contextos dando entrada a la misma mercancía.
2. **Cada albarán de Pedidos genera automáticamente un envío**, con sus líneas almacenables. Un
   envío por albarán; agrupar varios albaranes en una expedición es fase 2. Sugerencia: sí, igual
   que el borrador de factura por albarán.
3. **El albarán sigue siendo de Pedidos y la salida de stock de Inventario**: Envíos no numera
   albaranes ni mueve existencias. Se retiran `DeliveryNote`, `Picklist` e `ItemIssuance` de C#.
   Sugerencia: sí.
4. **Estados cerrados con reglas** (`scheduled → in-transit → delivered | returned`, y
   `cancelled` solo antes de salir). El estado «Picked» de C# no se porta: la preparación es del
   almacén. Sugerencia: sí.
5. **Un envío devuelto solo se registra**: no repone existencias ni anula el albarán ni la
   factura. Publica `shipment-returned` para que, en la fase 2, Inventario y Pedidos reaccionen.
   Sugerencia: sí en la fase 1; la devolución completa (reentrada de stock y rectificativa) es un
   circuito propio.
6. **El porte se guarda como dato del envío** (lo que cobra el transportista), sin repercutirlo
   al cliente ni contabilizarlo: la factura del transportista entra por Compras con la categoría
   `transport`. Sugerencia: sí; repercutir portes en la factura de venta es fase 2.
7. **Bultos y peso como totales, y el destino como texto de etiqueta.** Bultos detallados con su
   contenido, tramos de ruta y la dirección estructurada de Parties van a la fase 2. Sugerencia:
   sí.
8. **Transportistas propios de Envíos**, por empresa, con enlace opcional al tercero de Parties.
   Sugerencia: sí; el rol «Carrier» de Parties no aportaba ningún dato.

## Validación

- **Dominio:**
  - plan (método, recogida sin transportista, bultos, peso con tres decimales, porte en céntimos,
    llegada no anterior a la salida), líneas numeradas con cantidad positiva;
  - salida incompleta, entrega antes de salir, fechas que no retroceden;
  - cambios en tránsito (el destino no; el seguimiento y el porte sí), cancelar en tránsito,
    devolver sin motivo, envío cerrado inmutable;
  - transportista: código, enlace `https` con `{tracking}` una sola vez, enlace codificado.
- **Extremo a extremo** (Envíos sobre el backend, en memoria y en SQLite migrada; la prueba hace
  de Pedidos enviando el albarán al broker):
  - transportistas: solo lectura 403, ajeno 404, enlace inválido 400, código repetido 422,
    bloqueo, listado por código;
  - albarán con dos líneas de mercancía y un servicio: un solo envío aunque llegue tres veces, con
    sus dos líneas; albarán solo de servicios: ninguno;
  - preparar no es expedir (403), estado inválido 400, salida sin nada decidido 422;
  - plan: expedir no es preparar (403), transportista bloqueado 422, recogida con transportista
    400; plan con nombre del transportista y enlace de seguimiento;
  - salida antes de la fecha planificada 422; salida; destino en tránsito 422; seguimiento en
    tránsito; cancelar en tránsito 422; entrega antes de salir 422; entrega; una sola vez; cerrado
    422;
  - envíos a mano: sin líneas 400; recogida, devuelta con motivo (sin motivo 422); otro cancelado;
  - cuatro eventos publicados; búsquedas por estado, seguimiento, albarán, transportista y
    cliente; ajeno 404 y lista vacía.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, alimentado con **el tipo de contrato
  de Pedidos**:
  - un envío por albarán a través de `shipments_inbox`;
  - ida y vuelta del plan con sus columnas opcionales, líneas e historial en orden;
  - transportistas (código único, bloqueo, versión 2);
  - salida, seguimiento, entrega; envío a mano devuelto;
  - búsquedas (texto sin distinguir mayúsculas), orden por fecha planificada y bandeja de salida.

## Pendiente

- Fase 2:
  - devolución completa: reentrada en Inventario y factura rectificativa;
  - Pedidos marca el albarán como entregado al oír `shipment-delivered`;
  - varios albaranes en una expedición y entregas parciales de un albarán;
  - bultos detallados, etiquetas y tramos de ruta; dirección estructurada de Parties;
  - portes repercutidos al cliente y conciliación con la factura del transportista;
  - integración con las API de los transportistas (alta del envío y estados automáticos);
  - registrar el envío en Documentos (carta de porte, CMR).
- Compras, fase 2: albarán de entrada con cantidades aceptadas y rechazadas.
- Importar de C#: nada (ningún envío ni transportista con datos).

## El contrato de mantenimiento (front; añadido el 2026-11-13)

Los transportistas se mantienen también por el **contrato de mantenimiento**, el que lee el motor de
Angular de Karpo C# (el contrato `crud` del lenguaje de Theros), bajo `/api/v1/carriers`. Es otra
puerta a los mismos casos de uso; la configuración de su pantalla está en
`front/angular/config/entities/shipments/carrier.config.json` (ver front/README.md). Es el mismo
contrato que el de Productos, Instalaciones y Activos (docs/PRODUCTOS.md, docs/INSTALACIONES.md,
docs/ACTIVOS.md).

| Operación | Ruta | Qué hace |
|---|---|---|
| Lista | `GET /api/v1/carriers?page&pageSize&sortBy&sortOrder&search&name&company&isActive&ids`, o `POST /api/v1/carriers/search` con `{filter, pagination: {pageNumber, pageSize, orderBy, ascending}}` | la página del motor: `{items, total, totalCount, pageNumber, pageSize, totalPages}`. Orden por `code` (por omisión) o `name`, con la identidad para deshacer empates; otro, 400. `search` y `name` buscan en el nombre; `isActive=true` deja los que no están bloqueados. `company` sólo estrecha el ámbito de quien llama: la de otra organización da una lista vacía. Un filtro que no es de la lista se ignora. `ids` pide varias identidades a la vez, para resolver etiquetas (ver [el filtro ids](CONTRATO-MANTENIMIENTO.md)) |
| Lectura | `GET /api/v1/carriers/{id}` | el transportista |
| Alta | `POST /api/v1/carriers` | 201 con `Location`; lo que sobra en el cuerpo se ignora. Un transportista nace sin bloquear: `blocked: true` en el alta es 400 |
| Cambio | `PUT /api/v1/carriers/{id}` con lo leído y su `version` | 200; lo que el cuerpo no trae, o trae como null, se queda como está. Sin `version`, 400; con una que ya no es la suya, 409. Se traduce a `ChangeCarrier` (`name`, `party`, `trackingUrl`), sólo si algo cambia; un `blocked` distinto del actual es 422 `shipments.block_change_requires_command` (D-156: el bloqueo tiene sus órdenes); aun sin cambios exige el permiso y el ámbito de escritura. Todo el cambio es una sola unidad de trabajo. `company` y `code` no los cambia ningún caso de uso: cambiarlos es 400 `immutable` (se comparan por su valor: la identidad y el código sin distinguir mayúsculas ni los espacios de los extremos) |
| Baja | `DELETE /api/v1/carriers/{id}` | un transportista no se borra (los envíos lo nombran): DELETE lo **bloquea**, y uno bloqueado no se puede elegir en un envío nuevo. El motivo es obligatorio, en el cuerpo `{"reason": "..."}` o en `?reason=` (sin él, 422 `shipments.block_reason_required`). 204; sobre uno ya bloqueado, 204 otra vez y sin versión nueva; si la empresa exige aprobación, 202 con `{"pendingApproval": id}`. Siempre exige el permiso y el ámbito de escritura (`Shipments.Carrier.Update`; 403 sin él) |
| Bloquear | `POST /api/v1/carriers/{id}/block` con `{"reason", "version"}` | D-156: motivo obligatorio (422 `shipments.block_reason_required`) y la versión leída (409 si ya no es la suya; sin ella no se comprueba). 200 con el transportista; si ya está bloqueado, 200 sin cambios. Con aprobación, 202 con `pendingApproval` y el transportista sin tocar |
| Desbloquear | `POST /api/v1/carriers/{id}/unblock` con `{"reason", "version"}` | igual, con 422 `shipments.unblock_reason_required` |
| Historial | `GET /api/v1/carriers/{id}/block-history` | los bloqueos y desbloqueos efectivos, del más antiguo al más reciente: `blocked`, `reason`, `at`, `by`, `byName` |

- **El objeto:** `id`, `company`, `code`, `name`, `party`, `trackingUrl`, `blocked`, `blockReason`
  (el del bloqueo vigente), `unblockReason` (el del último desbloqueo), `version` y, en un 202,
  `pendingApproval`. Los dos motivos son de sólo lectura.
- **La versión:** `ChangeCarrier` acepta la versión leída (`Version`, fuera del JSON de
  `/api/carriers`) y da conflicto si el transportista ya está en otra; con ella el conflicto es
  definitivo y no se reintenta dentro de la transacción. Sin ella, se reintenta como siempre.
- **El error:** RFC 9457 con `errors` como diccionario (campo → mensajes) y `errorCodes`, en el
  mismo orden; en un 409 o un 422, `code`. Un cuerpo de más de 1 MiB es 413. El resto de la API de
  Envíos sigue con la lista del framework.
- **Permisos y ámbito:** los de siempre (`Shipments.Carrier.Read` y `…Update`), más
  `Shipments.Carrier.ApproveBlock` y `…ApproveUnblock` para aprobar (D-156).
- **Migración:** el contrato no añadió columnas; el motivo del bloqueo sí (abajo, «El bloqueo»).
- **Prueba:** `TestShipments_CarrierMaintenanceContract_MemoryThenSQLite`.

## El bloqueo: motivo, historial y aprobación (D-156, añadido el 2026-11-15)

Un transportista es **de una empresa** (`shp_carriers.company`), no un catálogo global: la empresa de
la política de aprobaciones es la suya.

- **Órdenes propias.** Bloquear y desbloquear son `BlockCarrier` y `UnblockCarrier`
  (`SetCarrierBlock{ID, Reason, Version}`), con motivo obligatorio (422
  `shipments.block_reason_required` / `shipments.unblock_reason_required`; 200 caracteres como
  mucho) y la versión leída. `ChangeCarrier` ya no bloquea: su `blocked` es opcional y, si pide
  otro estado, 422 `shipments.block_change_requires_command`. Rutas: `POST /api/v1/carriers/{id}/block`,
  `…/unblock`, `GET …/block-history` y `DELETE` con motivo (arriba). `PUT /api/carriers/{id}` sigue,
  sin bloquear.
- **Motivos vigentes.** `block_reason` (se borra al desbloquear) y `unblock_reason` (se borra al
  bloquear), en la ficha y en la auditoría.
- **Historial.** `shp_carrier_block_events` (`carrier_id`, `line_no`, `blocked`, `reason`,
  `changed_at`, `changed_by_id`, `changed_by_name`): una fila por bloqueo o desbloqueo efectivo, hija
  del agregado, así que se escribe en la misma transacción. Al aprobar una solicitud, quien figura es
  el aprobador; el solicitante queda en la solicitud.
- **Aprobación.** Acciones `shipments.carrier.block` y `shipments.carrier.unblock`; permisos de
  aprobar `Shipments.Carrier.ApproveBlock` y `Shipments.Carrier.ApproveUnblock`. La política es la de
  Parties (`shipments.WithApprovalPolicy(h.Parties.ApprovalPolicy)` en `host/host.go`; sin ella,
  ninguna). Con la acción en la lista de la empresa, la orden deja la solicitud en `shp_approvals` y
  responde 202 con `pendingApproval` sin guardar el transportista; otra pendiente sobre el mismo
  transportista, sea bloquear o desbloquear, es 409 `shipments.approval_pending` (en SQL, además, el
  índice único de `pending_key`; docs/APROBACIONES.md). Bandeja: `GET /api/shipments/approvals?company&status` y `POST
  /api/shipments/approvals/{id}/approve|reject|cancel`, como en Compras (docs/APROBACIONES.md), y
  las mismas bajo `/api/v1/shipments/approvals`. Al
  aprobar, el bloqueo se aplica en la misma transacción, de forma idempotente, y escribe el historial.
- **Migraciones:** 4 (`block_reason`, `unblock_reason` y `shp_carrier_block_events`), 5
  (`shp_approvals`), 6 (`pending_key`, rellenada en las solicitudes que ya hubiera) y 7 (su índice
  único `ux_shp_approvals_pending`).
- **Pruebas:** `TestShipments_CarrierMaintenanceContract_MemoryThenSQLite` (motivo, versión,
  historial, DELETE con motivo, PUT que bloquea 422) y `TestShipments_CarrierApprovals_MemoryThenSQLite`.
