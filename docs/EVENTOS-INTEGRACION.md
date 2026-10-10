# Evaluación de los eventos de integración (Fw C# → Go)

Evaluación de cómo se comunican hoy los bounded contexts de Karpo: el outbox de
`Fw.Infrastructure.EF`, el worker `FinancialDetail.Outbox.Worker`, el publicador de eventos de
dominio y los puertos síncronos entre contextos («costuras»). Se usa el mismo método que en
[LENGUAJE-UBICUO.md](LENGUAJE-UBICUO.md), [RASGOS-TRANSVERSALES.md](RASGOS-TRANSVERSALES.md) y
[AUTORIZACION.md](AUTORIZACION.md).

## Método

| Criterio | Peso |
|---|---|
| **U** Utilidad (uso real en contextos, sin Fw ni tests) | 40 % |
| **C** Corrección de la implementación | 25 % |
| **D** Diseño de la firma | 20 % |
| **G** Encaje en Go | 15 % |

Puntuación de 1 a 5 por criterio; nota ponderada sobre 100. **Mantener** ≥ 70 · **Modificar** 45–69 ·
**Retirar** < 45 · **Sustituido**.

## Hallazgo principal

**En el C# no existen los eventos de integración.** No hay ningún tipo, interfaz ni broker con ese
papel: ninguna aparición de `IntegrationEvent`, `Inbox`, `EventBus`, MassTransit, RabbitMQ, Kafka
ni Service Bus.

- **Lo asíncrono** es un outbox que guarda los **eventos de dominio** tal cual (el tipo C#
  serializado) y un worker que los vuelve a publicar **en proceso**. El propio README del worker
  reconoce que **no hay ningún `IDomainEventHandler<T>` concreto en producción**: hoy se publica
  sin consumidores.
- **La integración real entre contextos es síncrona**, mediante 12 puertos llamados «costuras»
  (`IPartyDirectory`, `IOrganizationHierarchy`, `IOrderBillableLines`, `IPaymentSettlement`...).
  Cada uno tiene un adaptador EF (en el mismo proceso) y otro HTTP (en la pila por rebanadas).

## Uso medido

| Pieza | Uso |
|---|---|
| `OutboxEvent` + `EfUnitOfWork.FlushDomainEventsToOutbox` | todos los `DbContext` que mapean `outbox_event` (migraciones en 20 proyectos) |
| `OutboxBackgroundService` + `OutboxEventTypeRegistry` | 1 host (`FinancialDetail.Outbox.Worker`) |
| `IDomainEventHandler<T>` | 0 handlers concretos en producción (solo bases del Fw y tests) |
| `DomainEventLog` (`domain_event_log`) | visor de eventos (`DomainEventLogEndpoints`, `DomainEventLogApplicationService`) |
| Costuras (puertos síncronos) | 12 contratos en `ErpKernel.Application.Contracts/Costuras`, adaptadores EF y HTTP en 10 publishers |

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | Costuras: puertos síncronos por contexto con adaptador EF/HTTP, por lotes, «fallar ≠ no encontrar» | 5 | 4 | 4 | 4 | **86** | Mantener como patrón (Open Host Service + ACL). No es código del Fw: cada contexto declara su puerto en sus contratos de aplicación |
| 2 | Outbox transaccional (`OutboxEvent` escrito en la misma transacción) | 4 | 3 | 3 | 4 | **70** | Mantener → ya existe (`application.OutboxStore`, `outbox.Recorder`) |
| 3 | `OutboxBackgroundService` (lotes, reintentos, dead-letter, un único drenador) | 3 | 2 | 3 | 3 | **56** | Modificar → `outbox.Relay` / `messaging.NewRelay` (el fallo del consumidor sí cuenta) |
| 4 | `OutboxEventTypeRegistry` (nombre simple → tipo, por reflexión) | 3 | 2 | 2 | 1 | **49** | Sustituido → `events.Registry` (registro explícito) para dominio y tipo versionado para integración |
| 5 | `DomainEventPublisher` + `IDomainEventHandler<T>` (resolución por contenedor) | 2 | 1 | 3 | 2 | **38** | Sustituido → `application.Dispatcher` + `events/inprocess` |
| 6 | `OutboxHandlerComposition` (escaneo de ensamblados) | 1 | 3 | 2 | 1 | **30** | Retirar: en Go se registra explícitamente en el punto de composición |
| 7 | `DomainEventLog` (registro permanente de eventos de dominio) | 3 | 3 | 3 | 3 | **60** | Modificar → cubierto por `application.AuditLog` (lleva los eventos lanzados). Si hace falta un visor propio, será una proyección |
| 8 | Eventos de integración / lenguaje publicado | — | — | — | — | — | **Nuevo** → `application.IntegrationEvent`, `Envelope`, `Translator`, `MessageSender`, `MessageHandler`, `InboxStore` |

## Defectos encontrados (justifican la columna C)

- **El outbox marca como procesado lo que ha fallado.** `DomainEventPublisher.PublishAsync`
  captura la excepción del handler, la registra en el log y **no la relanza**. Así que
  `OutboxBackgroundService` pone `ProcessedAtUtc` también cuando el consumidor falló: la entrega
  «al menos una vez» y los reintentos solo cubren errores de deserialización. En Go, el error del
  consumidor vuelve al relay y el mensaje queda pendiente.
- **Los consumidores no pueden descartar duplicados.** `OutboxEvent.Id` es un `Guid.NewGuid()`
  nuevo, no el id del evento, y no hay inbox. Si un día hay consumidores, cada reentrega se
  procesará otra vez.
- **El tipo se guarda por nombre simple.** `event_type = GetType().Name`: dos eventos con el mismo
  nombre en ensamblados distintos colisionan (`AmbiguousEventTypes`) y se resuelve el primero
  que aparece.
- **El modelo interno se filtra.** Se serializa el evento de dominio tal cual. Renombrar una
  propiedad rompe las filas pendientes y a cualquier consumidor externo, y no hay versión.
- **Se pierden la correlación y la causalidad.** La fila no guarda correlación, causa, versión
  del agregado ni el id del evento.
- **Se pierden eventos en silencio.** Si el `DbContext` no mapea ni `outbox_event` ni
  `domain_event_log`, `FlushDomainEventsToOutbox` limpia los eventos sin publicarlos.
- **Las colas se procesan en orden de `CreatedAtUtc = DateTime.UtcNow`**, con la resolución del
  reloj: los eventos de una misma transacción pueden empatar. En Go se usan UUID v7 monótonos.

## Diseño en Go

```text
 Parties (upstream)                                          CRM / Billing (downstream)
 ───────────────────                                          ──────────────────────────
 agregado ─► evento de dominio ─► orchestration (misma UoW)
                                   ├─ outbox.Recorder ─► outbox_messages        (dentro de Parties)
                                   └─ messaging.Recorder ─ Translator ─► parties_integration_outbox
                                                                         │
                          messaging.NewRelay ─► application.MessageSender ┤ (inprocess.Broker, nats.Transport...)
                                                                         ▼
                                                     messaging.Consumer ─ UoW { inbox.Claim + handler }
```

| Pieza | Paquete | Papel |
|---|---|---|
| `IntegrationEvent` | `application` (contrato) | Hecho publicado, con el tipo versionado `{contexto}.{hecho}.v{n}` |
| `Envelope` | `application` | Mensaje en la red: id, tipo, origen, sujeto, instante, correlación, causa (id del evento de dominio) y datos JSON |
| `Translator` | `application` | Traduce eventos de dominio a eventos de integración: la ACL de salida |
| `MessageSender` / `MessageHandler` | `application` | Puerto del transporte y del consumidor |
| `MessageSubscriber` | `application` | Puerto para suscribir un consumidor lógico a un transporte: la composición no depende de un transporte concreto |
| `InboxStore`, `InboxPurger` | `application` | `Claim(consumer, messageID)` dentro de la UoW del consumidor; `Purge` olvida por antigüedad |
| `OutboxClaimer` | `application` | Capacidad del outbox para que lo drenen varios relays a la vez |
| `messaging.Recorder`, `messaging.On` | `application/messaging` | Aplica las traducciones y escribe en el outbox de integración |
| `messaging.NewRelay` | `application/messaging` | Reenvía el outbox de integración al transporte (al menos una vez); con `outbox.WithClaims`, un relay por réplica |
| `messaging.Attach`, `messaging.PurgeInbox` | `application/messaging` | Suscribir un consumidor a un transporte; purga del inbox (30 días por defecto) |
| `messaging.Consumer`, `messaging.Handle` | `application/messaging` | Lector tolerante tipado; inbox y handler en la misma UoW |
| `outbox.Recorders`, `outbox.NewForwarder` | `application/outbox` | Combinar el outbox de dominio con el de integración; relay con entrega propia |
| `inprocess.Broker` | `messaging/inprocess` | Transporte en memoria (monolito modular y tests) |
| `nats.Transport` | módulo `nats/` | Transporte entre procesos sobre NATS JetStream (ver [BROKER.md](BROKER.md)) |
| `transporttest.Run` | `testing/transporttest` | Batería de conformidad que todo transporte debe pasar |
| `memory.Inbox`, `sqlrepo.Inbox` (+ `InboxDDL` ×5), `hotswap.Inbox` | persistencia | Almacenes del inbox |

**Reglas:**

- Los eventos de dominio no salen del contexto. Lo que se publica es su **lenguaje publicado**
  (`examples/parties/contracts`), que solo cambia añadiendo campos o publicando una `v2` junto a
  la `v1`.
- El consumidor tiene **su propia copia** del contrato, solo con los campos que usa (lector
  tolerante).
- **«Exactamente una vez» en el efecto:** la entrega es al menos una vez, y el inbox registra el
  mensaje en la misma transacción que los cambios del consumidor. Si el handler falla, el registro
  se deshace y la reentrega se procesa.
- En SQL, `Claim` consulta antes de insertar (en PostgreSQL una sentencia fallida aborta la
  transacción). Si dos entregas simultáneas chocan en la clave primaria, la segunda recibe
  `ErrConflict` y se reintenta.
- **Orden por sujeto:** los hechos de un mismo agregado llegan a cada consumidor en el orden en
  que ocurrieron. Si la entrega de uno falla, los posteriores del mismo agregado esperan. Entre
  agregados distintos no se promete orden.
- **Un corte del transporte no es un fallo del mensaje:** el transporte lo señala con
  `application.ErrTransportUnavailable` y el relay corta la pasada sin sumar intentos.
- **El inbox recuerda más tiempo del que el transporte puede reentregar** (30 días frente a los
  7 que conserva el broker): un mensaje purgado que volviera se aplicaría otra vez.

## Validación

- `messaging`: nombres de tipo, traducción en la UoW (causa = id del evento de dominio,
  correlación y sujeto), errores de traducción que deshacen la UoW, inbox que libera el mensaje
  al fallar y descarta duplicados, tipos desconocidos que se confirman sin efecto, y reparto a
  varios consumidores con reintento.
- `sqlconformance.RunInbox`: primer registro, duplicado, un inbox por consumidor y rollback. Pasa
  en SQLite, PostgreSQL, SQL Server, Oracle y MySQL. El esquema de Parties, con su outbox de
  integración, también se crea en los cinco motores.
- Extremo a extremo (`examples/parties/integration_events_test.go`): Parties publica
  `party-registered.v1` y `party-renamed.v1` (`ContactAdded` no se publica). El CRM, sobre SQLite
  con inbox SQL, y Billing, en memoria, los consumen. Billing falla una vez, así que el mensaje se
  reentrega a los dos. Se comprobó que **quitando el inbox el test falla** (la reentrega viola la
  clave primaria del CRM).

## Transporte entre procesos (2026-10-06)

Evaluado, decidido e implementado en [BROKER.md](BROKER.md):

- **Adaptador de NATS JetStream** (módulo `nats/`) sobre `MessageSender` y `MessageSubscriber`,
  con grupos de consumidores, reentrega con espera creciente y mensajes muertos.
- **Batería de conformidad del transporte** (`transporttest`): la pasan el transporte en memoria
  y el de NATS.
- **Relay con varias instancias** (`outbox.WithClaims`): `FOR UPDATE SKIP LOCKED` en PostgreSQL,
  Oracle y MySQL, y `READPAST` en SQL Server, conservando el orden del outbox.
- **Purga del inbox** por antigüedad, en memoria, SQL (cinco motores) e intercambiable en
  caliente.
- **Dos servicios en procesos separados** (`integration/broker_test.go`): RR. HH. publica
  `hr.employee-terminated.v1` y Parties, con su propia base de datos, termina la relación de
  empleo.

Un cambio de comportamiento respecto a lo descrito arriba: en el test de extremo a extremo de
`examples/parties`, el renombrado de Acme ya no se entrega en la misma pasada en que falló su
alta. Espera detrás de ella (orden por sujeto) y se entrega en la siguiente.

## Pendiente

- Adaptadores HTTP de las costuras (el patrón se mantiene: puerto en los contratos del
  consumidor y adaptador por despliegue). Es la otra mitad del transporte entre procesos; se
  dejó fuera de la tarea del broker (decisión 6 de [BROKER.md](BROKER.md)).
- Lo demás, en el BACKLOG («Transporte entre procesos: siguientes pasos»).
