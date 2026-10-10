# Transporte real de eventos de integración (broker)

> **Estado: decisiones aprobadas por Javier el 2026-10-06 e implementado.** Los §1–§9 son la
> evaluación previa, puesta al día donde la implementación la corrigió. Lo que se hizo y cómo se
> comprobó está en el [§10](#10-lo-implementado-y-cómo-se-comprobó). Continúa lo que dejó
> pendiente [EVENTOS-INTEGRACION.md](EVENTOS-INTEGRACION.md).

Hoy los eventos de integración sólo viajan dentro de un proceso (`pkg/messaging/inprocess`). Las
arquitecturas `macroservices` y `microservices` del perfil de Theros necesitan que viajen **entre
procesos**. Este documento evalúa con qué, y qué más hace falta para que funcione con varias
instancias de cada servicio.

## 1. Lo que cambia al salir del proceso

En memoria, `Send` llama al consumidor y **devuelve su error**. Con un broker, `Send` termina
cuando el broker ha guardado el mensaje, y el consumidor lo recibe después, en otro proceso. La
cadena se parte en dos mitades que fallan y reintentan por separado:

| | En memoria (hoy) | Con broker |
|---|---|---|
| `Send` devuelve éxito cuando… | todos los consumidores han terminado bien | el broker ha guardado el mensaje |
| Si el consumidor falla, reintenta… | el relay (el mensaje sigue pendiente en el outbox) | el broker (reentrega) |
| El mensaje que falla siempre se aparta en… | el outbox del publicador (`attempts ≥ 10`) | el lado del consumidor |
| Un consumidor caído… | hace fallar al publicador | no afecta al publicador: el broker le guarda los mensajes |

Leyendo el código con esa idea salen cinco cosas que hay que resolver, y que no estaban en la
lista de pendientes:

1. **No hay puerto para suscribirse.** `application.MessageSender` existe, pero suscribirse es un
   método del tipo concreto `inprocess.Broker`. La composición que genere Theros no puede
   depender de un tipo concreto.
2. **El relay no conserva el orden cuando un envío falla.** `Relay.RelayOnce`
   (`pkg/application/outbox/outbox.go:113-121`) marca el fallo y sigue con el siguiente mensaje,
   aunque sea del mismo agregado.
3. **Hay consumidores que ya dependen del orden.** Contabilidad, al recibir
   `payroll.payslip-cancelled.v1`, busca el asiento de la nómina aprobada y, si no lo encuentra,
   termina sin hacer nada (`contexts/accounting/application/subscriptions.go:452-455`). Si la
   anulación llegara antes que la aprobación, la anulación se perdería. El orden por sujeto es,
   por tanto, un requisito y no una opción.
4. **Una caída del broker gastaría los intentos.** Cada pasada fallida del relay suma un intento,
   y a los diez el mensaje queda apartado para siempre. Con el transporte en memoria eso mide un
   consumidor que falla. Con un broker en red, un corte de diez pasadas (segundos) dejaría
   mensajes sin enviar. Un corte del transporte no debe contar como intento.
5. **No hay ningún ejecutable en el repositorio** (ningún `package main`). Los dos procesos de la
   demostración hay que crearlos.

## 2. Requisitos que impone lo que ya existe

| # | Requisito | De dónde sale |
|---|---|---|
| R1 | **Al menos una vez.** El broker guarda el mensaje en disco antes de confirmar el envío | El relay marca `processed_at` al volver `Send` |
| R2 | **Confirmación explícita después del éxito.** El mensaje sólo se da por entregado cuando `HandleMessage` devuelve `nil`; si devuelve error o el proceso muere, se reentrega | `messaging.Consumer` ejecuta inbox y manejador en la misma unidad de trabajo |
| R3 | **Duplicados inofensivos.** El identificador del sobre viaja intacto: es la clave del inbox | `InboxStore.Claim(consumer, messageID)` |
| R4 | **Orden por sujeto.** Los hechos de un mismo agregado (`Envelope.Subject`) llegan a cada consumidor en el orden en que ocurrieron. Entre sujetos distintos no se promete orden | Hallazgo 3 |
| R5 | **Grupos de consumidores.** Un consumidor lógico (`parties`, `accounting`…) con varias réplicas: cada mensaje lo procesa una. Dos consumidores lógicos distintos reciben los dos el mensaje | `messaging.NewConsumer(name, …)`: el nombre es la partición del inbox |
| R6 | **Tipos versionados.** `{contexto}.{hecho}.v{n}` se usa para filtrar: cada consumidor recibe sólo los tipos que maneja, y `v1` y `v2` conviven | `messaging.ValidType`, `Consumer.Types()` |
| R7 | **El sobre no se pierde por el camino.** Identificador, tipo, origen, sujeto, instante, correlación, causa y datos llegan iguales | `application.Envelope` |
| R8 | **Durabilidad con el consumidor parado.** Lo publicado mientras un servicio está caído le llega al volver | Es lo que justifica un broker |

## 3. Opciones

El equipo es una persona más agentes, así que la **superficie de operación** pesa tanto como el
encaje técnico.

| Criterio | NATS JetStream | Kafka | PostgreSQL como cola |
|---|---|---|---|
| Orden por sujeto (R4) | Sí, con un mensaje en vuelo por consumidor lógico (las réplicas se relevan, no trabajan en paralelo). El reparto en paralelo conservando el orden pide particionar los sujetos a mano | **El mejor**: la clave de partición es el sujeto, y cada partición la lee una sola réplica. Orden y paralelismo a la vez | Sí, lo implementamos nosotros |
| Reentrega y confirmación (R2) | **Nativo**: confirmación por mensaje, reentrega con espera creciente y tope de entregas | No existe por mensaje: se confirma una posición. Reintentar o apartar un mensaje es código del cliente (temas de reintento y de descarte) | Lo implementamos nosotros |
| Grupos de consumidores (R5) | Consumidor duradero compartido por las réplicas | Grupo de consumidores, con rebalanceo de particiones | Lo implementamos nosotros (`SKIP LOCKED`) |
| Filtrado por tipo (R6) | Nativo: el tipo es el asunto del mensaje (`karpo.evt.parties.party-registered.v1`) y el consumidor filtra por asuntos | Un tema por tipo (y el orden entre tipos del mismo sujeto se pierde) o un tema por contexto filtrando en el cliente | Una columna |
| Superficie de operación | **Un binario** de unos 20 MB, sin dependencias, un fichero de configuración. Alta disponibilidad opcional con 3 nodos | Clúster sobre la JVM (ya sin ZooKeeper desde la 4.0). Hay que dimensionar particiones, réplicas y retención por tema, y vigilar los rebalanceos | **Ninguna nueva** si la base ya es PostgreSQL. Pero el broker lo escribimos y lo mantenemos nosotros |
| Servicio gestionado | Synadia Cloud (un proveedor) | **Muchos**: Confluent, Amazon MSK, Azure Event Hubs (protocolo Kafka), Aiven, Redpanda | Cualquier PostgreSQL gestionado |
| Dependencias Go | `nats.go` (Apache-2.0, Go puro) y media docena de módulos transitivos | `franz-go` (Go puro). El cliente oficial de Confluent usa cgo y en Windows no compila sin compilador de C | Ninguna |
| Prueba en integración continua | Contenedor de pocos MB que arranca en menos de un segundo. Además **el servidor se puede arrancar dentro del propio test** (es una librería Go): la conformidad corre sin Docker | Contenedor con JVM: decenas de segundos y del orden de 1 GB de memoria | Ya está en la suite |
| Encaje con el resto | Vale con cualquiera de los cinco motores | Igual | Sólo PostgreSQL, y **obliga a que todos los servicios compartan una base**, justo lo que `microservices` evita |

Otras que se han mirado y se descartan sin tabla: **RabbitMQ** (maduro y con oferta gestionada,
pero el orden con varias réplicas pide configuración aparte y el servidor es más pesado que NATS
sin dar nada que aquí falte) y **Redpanda** (habla el protocolo de Kafka en un solo binario: es
una forma de operar Kafka, no otro adaptador).

**Lectura:** Kafka gana en orden con paralelismo y en oferta gestionada; pierde en todo lo que
toca operar y en que la reentrega por mensaje (R2) hay que construirla. NATS JetStream cubre R1–R8
de serie, con la salvedad de que el orden se consigue sin paralelismo dentro de un consumidor
lógico, lo cual sobra para el volumen de un ERP. PostgreSQL como cola es tentador para despliegues
pequeños, pero es escribir un broker y sólo vale para un motor de los cinco.

## 4. Cómo se mantiene el patrón del framework

Contrato en `pkg/application`, una implementación por tecnología, y el núcleo sin conocer al
cliente del broker. Igual que los dialectos de `sqlrepo`, con una diferencia: un dialecto no
importa su driver, pero un adaptador de broker **tiene** que importar su cliente. Por eso va en un
**módulo Go aparte**, como ya lo son `integration/` y el `otel/` propuesto en
[OBSERVABILIDAD.md](OBSERVABILIDAD.md):

```text
pkg/application/integration.go      contrato: + MessageSubscriber (nuevo, pequeño)
pkg/messaging/inprocess/            implementación en memoria (existe; gana el método del contrato)
pkg/testing/transporttest/          batería de conformidad del transporte (nueva)
nats/                 go.mod propio  adaptador JetStream: Sender + Subscriber + mensajes muertos
                                     (sus tests arrancan un servidor dentro del propio test)
integration/          go.mod propio  conformidad y extremo a extremo contra el servidor real
```

- El `go.mod` del framework **no cambia**. Un monolito generado no descarga ni audita `nats.go`.
- Añadir Kafka más adelante es añadir `kafka/` y pasar `transporttest.Run`. Nada del núcleo se toca.
- **Guardián de arquitectura.** La regla 6 dice hoy que `pkg/messaging` sólo depende de contratos,
  pero no entra en subcarpetas, así que en realidad no revisa `inprocess`. Se corrige para que sí
  lo haga. Se añade una regla para `nats/`: sólo contratos del framework, `nats.go` y biblioteca
  estándar; nunca persistencia ni contextos.

El contrato nuevo. El método se llama `Attach` porque `inprocess.Broker` ya tiene un `Subscribe`
con otra firma, que no se toca:

```go
// MessageSubscriber attaches consumers to a transport. Attachments that share the consumer name
// are replicas of it, and each message takes effect once among them. A message is settled only
// when h returns nil; otherwise it is delivered again.
type MessageSubscriber interface {
	Attach(ctx context.Context, consumer string, h MessageHandler, types ...string) (detach func() error, err error)
}
```

La composición de un contexto queda igual con cualquier transporte:

```go
detach, err := messaging.Attach(ctx, transport, mod.Consumer)   // inprocess.Broker o nats.Transport
relay := mod.Relay(transport, outbox.WithClaims(sw))            // un relay por réplica
```

Correspondencia del sobre con JetStream:

| Karpo | JetStream |
|---|---|
| `Envelope` completo | cuerpo del mensaje, en JSON (el contrato en la red no depende del broker) |
| `Type` | asunto: `{prefijo}.evt.{type}`, por defecto `karpo.evt.parties.party-registered.v1` |
| `ID` | cabecera `Nats-Msg-Id`: el broker descarta el reenvío de un mismo mensaje dentro de su ventana |
| consumidor lógico | consumidor duradero con ese nombre, filtrado por los tipos de `Consumer.Types()`, con un solo mensaje en vuelo |
| `HandleMessage` devuelve `nil` / error | confirmación / reintento tras una espera creciente |
| correlación, causa, origen y sujeto | van en el sobre; se copian además a cabeceras (`Karpo-Correlation-Id`…) para verlas sin abrir el cuerpo |
| mensaje muerto | asunto `{prefijo}.dead.{consumidor}`, en un segundo stream (`KARPO_DEAD`) que no caduca, con el error, el número de entregas y la fecha en cabeceras |

Los streams se crean si faltan; si existen no se tocan (sus límites son de quien opera el broker).
La dirección del servidor y las credenciales llegan al punto de composición por configuración del
entorno (`nats.Config.URL`, `Config.Options`). Nada de eso se versiona.

## 5. Relay con varias instancias

En `microservices` con dos réplicas de un servicio hay dos relays sobre la misma tabla de outbox.
Hoy los dos leerían los mismos pendientes y los enviarían dos veces.

**Diseño.** Sin columnas nuevas ni tabla de bloqueos:

- El almacén gana una capacidad opcional, `application.OutboxClaimer`. `Claim` hace tres cosas
  dentro de la unidad de trabajo del relay:
  1. Lee, **sin bloquear**, los identificadores de los pendientes más antiguos, los tenga quien
     los tenga.
  2. Los **bloquea por clave primaria, saltándose los que tenga otro relay**: `FOR UPDATE SKIP
     LOCKED` en PostgreSQL, Oracle y MySQL; `WITH (UPDLOCK, READPAST, ROWLOCK)` en SQL Server.
  3. Devuelve sólo **el tramo inicial sin huecos**: los mensajes que continúan el outbox sin
     saltarse ninguno que tenga otro relay.
- El relay, con `outbox.WithClaims(uow)`, hace en una unidad de trabajo: reclamar, enviar, marcar
  y confirmar. Si el proceso muere, la base suelta los bloqueos sola: no hay arrendamientos que
  caduquen ni relojes que ajustar.
- Varias instancias no se esperan ni repiten envíos, y el orden del outbox se conserva. El relay
  que tiene el mensaje más antiguo siempre puede enviarlo, así que siempre hay avance. El precio:
  sobre un mismo outbox drena, en la práctica, un relay cada vez (los outbox de contextos
  distintos sí van en paralelo).
- **Fallo de un envío:** el relay deja de enviar los mensajes posteriores **del mismo sujeto** en
  esa pasada (hallazgo 2).
- **Corte del transporte:** el adaptador lo señala con `application.ErrTransportUnavailable` y el
  relay corta la pasada **sin sumar intentos** (hallazgo 4).

Sin `WithClaims`, el relay lee con `Pending` como antes. Las dos reglas de fallo valen siempre.

**Dos cosas que sólo salieron al probar en los servidores**, y que cambiaron el diseño inicial
(un único `SELECT … FOR UPDATE SKIP LOCKED` con límite):

- **Dos relays que reclaman a la vez se reparten las filas intercaladas** (uno se queda la 11, la
  13 y la 14; el otro la 12). Comprobar sólo «¿hay algo anterior a mi primera fila?» dejaba pasar
  la 19 antes que la 12, del mismo sujeto. De ahí el paso 3.
- **En MySQL, el bloqueo recorriendo por rango también bloquea los huecos**, y los publicadores
  se quedaban esperando al relay hasta agotar el tiempo de espera. De ahí bloquear por clave
  primaria (paso 2), forzando ese índice en MySQL.

**Para qué no sirve:** las entregas se hacen fuera de la unidad de trabajo que retiene los
mensajes. Con el transporte en memoria y un almacén de un solo escritor (memoria, SQLite), el
consumidor necesitaría ese mismo almacén mientras el relay lo retiene. `WithClaims` es para
servidores de base de datos con un broker; el monolito sigue con un relay sin reclamos.

Límite conocido: el outbox se ordena por instante y por identificador. Con dos instancias del
mismo contexto, el orden entre dos cambios seguidos de un mismo agregado depende de que sus
relojes difieran menos que el tiempo entre esos dos cambios (con NTP, milisegundos).

## 6. Batería de conformidad del transporte

`pkg/testing/transporttest`, al estilo de `repotest`. El adaptador entrega un arnés (un
`MessageSender`, un `MessageSubscriber` y lo que declara soportar) y la batería monta encima la
cadena real: outbox en memoria, `messaging.NewRelay`, `messaging.Consumer` e inbox.

| # | Lo que debe demostrar | En memoria | Broker |
|---|---|---|---|
| C1 | **Entrega.** Todo lo enviado llega a cada consumidor suscrito, con el sobre intacto (R7) | ✔ | ✔ |
| C2 | **Filtro por tipo.** Un consumidor no recibe tipos que no pidió; `v1` y `v2` se separan | ✔ | ✔ |
| C3 | **Dos consumidores lógicos** reciben los dos cada mensaje, y el fallo de uno no repite el efecto en el otro | ✔ | ✔ |
| C4 | **Reentrega.** Si el manejador falla, el mensaje vuelve hasta que termina bien | ✔ (reintenta el relay) | ✔ (reintenta el broker) |
| C5 | **Duplicados.** El mismo sobre enviado dos veces produce el efecto una vez (inbox) | ✔ | ✔ |
| C6 | **Réplicas.** Dos suscripciones del mismo consumidor: cada efecto ocurre una vez en total | ✔ | ✔ |
| C7 | **Orden por sujeto**, también cuando un mensaje intermedio falla y se reintenta | ✔ | ✔ |
| C8 | **Reparto real** entre réplicas: un mensaje confirmado por una no llega a la otra | — | ✔ |
| C9 | **Durabilidad:** lo publicado con el consumidor parado le llega al volver (R8) | — | ✔ |
| C10 | **Mensaje que falla siempre:** se aparta tras el tope y los siguientes continúan | ✔ (apartado en el outbox) | ✔ (mensajes muertos) |

C8 y C9 dependen de capacidades que el transporte en memoria no tiene ni necesita (réplicas y
persistencia dentro de un proceso no tienen sentido). El arnés las declara y la batería las exige
sólo a quien las declara. Nunca se saltan en silencio: cada caso no ejecutado sale como omitido
con su motivo.

## 7. Purga del inbox

El inbox crece una fila por mensaje y consumidor. Se añade una capacidad opcional al almacén,
`application.InboxPurger` (`Purge(ctx, anterioresA)`), en memoria, SQL (cinco motores) e
intercambiable en caliente, y un caso nuevo en `sqlconformance.RunInbox`. Para usarla,
`messaging.PurgeInbox(ctx, inbox, retención)` y `messaging.RunInboxPurge` (ahora y cada cierto
tiempo, junto al relay).

**Regla de seguridad:** el inbox debe guardar un mensaje **más tiempo del que el broker puede
tardar en reentregarlo**. Si el broker conserva siete días y el inbox purga a los tres, una
reentrega del cuarto día se aplicaría dos veces.

No se añade un índice por fecha: obligaría a una migración en cada contexto (también en Security,
que está fuera de esta tarea). Una purga diaria recorriendo la tabla es suficiente a este volumen.
Queda anotado para cuando no lo sea.

## 8. Costuras HTTP

Los puertos síncronos entre contextos (`AddressChecker`, `FacilityDirectory`, `Organizations`…)
son la otra mitad del transporte entre procesos. **Recomendación: no entran en esta tarea.**

- Hoy no hay ningún adaptador HTTP de costura en Go, ni cliente HTTP en `pkg/distribution`. No es
  completar algo: es empezarlo.
- Es trabajo **por puerto** (rutas del proveedor más adaptador del consumidor), es decir, código
  que debe emitir el pack de Theros. Al framework le toca una pieza pequeña y común: un cliente
  que propague la correlación, convierta los errores RFC 9457 de vuelta a los centinelas del
  dominio («fallar ≠ no encontrar») y presente la identidad del servicio que llama.
- Esa identidad depende del «resolutor en modo `Http`» y de los principales de servicio de
  Security, que están pendientes en el BACKLOG y en manos de otra sesión.
- No comparte nada con el broker: ni código, ni pruebas, ni riesgos.

Con esta tarea, `macroservices` y `microservices` quedan generables para contextos que se hablen
por eventos. Los que además se llamen por un puerto síncrono necesitan la tarea siguiente.

## 9. Decisiones

Las siete se aprobaron tal como se recomendaban (Javier, 2026-10-06).

**D1. Qué broker primero → NATS JetStream.**
Cubre los ocho requisitos de serie y es lo más barato de operar: un binario. Ejemplo: para probar
el cambio en local basta `docker run nats -js`; en un cliente pequeño es un servicio más junto a
la base de datos. Kafka se queda como segundo adaptador para el cliente que ya lo tenga o que
necesite paralelismo con orden; la batería de conformidad le marca el camino.
*Alternativa:* Kafka primero, si pesa más la oferta gestionada que la operación propia.

**D2. Dónde vive el adaptador → módulo Go aparte (`nats/`).**
El `go.mod` del framework no gana dependencias. Ejemplo: un monolito generado no descarga
`nats.go`; un servicio repartido añade una línea a su `go.mod`.
*Alternativa:* `pkg/messaging/nats` dentro del módulo principal. Más cómodo de importar, pero todo
usuario del framework arrastra el cliente en su `go.sum`.

**D3. Puerto de suscripción nuevo en `pkg/application` → sí (`MessageSubscriber`).**
Es lo que permite que la composición generada no cambie al cambiar de transporte. Ejemplo: en
`monolith` se le pasa el broker en memoria y en `microservices` el de NATS; la línea que suscribe
a Parties es la misma. Es aditivo: el `Subscribe` actual del transporte en memoria no se toca.

**D4. Orden → se promete por sujeto, y se garantiza.**
Los hechos de un mismo agregado llegan a cada consumidor en el orden en que ocurrieron. Entre
agregados distintos no se promete nada. Ejemplo: la anulación de una nómina nunca llega antes que
su aprobación; la nómina de Ana puede llegar antes o después que la de Luis.
El precio: cada consumidor lógico procesa **un mensaje cada vez**, y cada outbox lo drena **un
relay cada vez**. Las réplicas dan relevo si una cae, no más velocidad. Para un ERP sobra; si un
día no, la salida es particionar por sujeto (o Kafka), sin tocar los contextos.
*Alternativa:* no prometer orden y repartir en paralelo. Obligaría a revisar los consumidores que
hoy lo dan por hecho (Contabilidad, Pagos, Fiscal) y a que Theros genere consumidores que lo
toleren.

**D5. Mensajes que fallan siempre → se reintentan con espera creciente, y al llegar al tope se
apartan y se sigue.**
Propuesta: 8 intentos repartidos en unos 10 minutos. Mientras dura, ese consumidor espera (es lo
que conserva el orden). Al agotarlos, el mensaje va a un asunto de **mensajes muertos**
(`karpo.dead.{consumidor}`) con el error y el número de intentos, se registra como error, y el
consumidor continúa. Reinyectarlo es seguro: el inbox impide el doble efecto.
Ejemplo: llega una factura de una empresa que aún no tiene plan contable. Contabilidad la
reintenta diez minutos, la aparta, y sigue contabilizando las demás. Cuando alguien crea el plan,
se reinyecta la factura.
Hay que saberlo: apartar un mensaje **rompe el orden de ese sujeto** (si después llega su
anulación, no encontrará nada que anular). Quien reinyecta debe mirar qué más llegó de ese sujeto.
Una herramienta para ver y reinyectar los mensajes muertos queda para el BACKLOG.
*Alternativa:* no apartar nunca y parar el consumidor hasta que alguien lo arregle. Conserva el
orden siempre, pero una sola factura mala para toda la contabilidad.

**D6. Costuras HTTP → fuera de esta tarea**, como tarea aparte detrás de Security ([§8](#8-costuras-http)).

**D7. Retención → el broker conserva 7 días y el inbox 30.**
Cumple la regla del [§7](#7-purga-del-inbox) con margen. Ejemplo: un servicio parado un fin de
semana largo recupera todo al volver; un mensaje de hace 31 días ya no puede reentregarse, así
que su fila del inbox no hace falta. Los dos valores son configurables.

## 10. Lo implementado y cómo se comprobó

| Paso | Qué | Dónde |
|---|---|---|
| 1 | Puerto `MessageSubscriber` y centinela `ErrTransportUnavailable`; batería de conformidad; el transporte en memoria la pasa | `pkg/application/integration.go`, `pkg/testing/transporttest`, `pkg/messaging/inprocess` |
| 2 | Relay: orden por sujeto ante un fallo, corte del transporte sin intentos, y `WithClaims` para varias instancias. `Claim` en memoria, SQL e intercambiable en caliente | `pkg/application/outbox`, `pkg/persistence/{memory,sqlrepo,hotswap}`, `sqlconformance.RunOutbox` |
| 3 | Adaptador de NATS JetStream con mensajes muertos | módulo `nats/` |
| 4 | Purga del inbox | `InboxPurger`, `messaging.PurgeInbox`, `sqlconformance.RunInbox` |
| 5 | NATS en `docker-compose.yml`, en `run.ps1` (puerto por *slot*) y en el flujo de CI; conformidad contra el contenedor; dos servicios en procesos separados | `integration/broker_test.go`, `integration/outbox_test.go`, `.github/workflows/ci.yml` |

**La demostración de extremo a extremo** (`TestTwoProcesses_HRToParties`). Cuatro procesos: dos
réplicas de RR. HH. y dos de Parties, **cada servicio con su propia base de datos** en PostgreSQL
y NATS en medio. RR. HH. se compone sin puertos síncronos, porque Parties está en otro proceso.

1. Cada réplica de RR. HH. contrata y da de baja a dos personas. Los dos relays comparten el
   outbox de RR. HH. (`WithClaims`).
2. Parties, que sólo pidió `hr.employee-terminated.v1`, termina las cuatro relaciones de empleo.
3. Se mata una réplica de cada servicio y la réplica de RR. HH. que queda da de baja a dos más.
   El relay y el consumidor supervivientes siguen solos.
4. Resultado: 12 eventos publicados por RR. HH., 6 manejados por Parties (una vez cada uno: 6
   filas en su inbox), ningún pendiente en el outbox y ningún mensaje muerto.

**Ejecutado en esta máquina (Windows, Docker, 2026-10-06):**

- `go test ./...` en el módulo principal: 59 paquetes en verde y **un fallo que ya existía y es
  ajeno a esta tarea** (`contexts/payments`: la prueba liquida en una fecha fija, el 5 de
  octubre, y falla desde el 6). La misma causa hace fallar `TestPaymentsContext` en la suite de
  integración.
- Módulo `nats/`: conformidad completa (las diez pruebas, ninguna omitida) contra un servidor
  arrancado en el propio test, más formato en la red, mensajes muertos, manejador lento y caída
  y vuelta del broker. Repetido varias veces; la de caída del broker, 80.
- `integration/run.ps1 -Slot 3`, contra PostgreSQL 17, SQL Server 2022, Oracle 23 y MySQL 8.4 y
  contra NATS: todas las pruebas de la suite, en verde salvo `TestPaymentsContext` (el fallo de
  fecha de arriba). Lo de esta tarea pasa todo: el outbox con tres relays a la vez en cada
  motor, la purga del inbox, la conformidad del transporte y la demostración de dos procesos.
  Se ejecutó en dos tandas: la suite entera tarda aquí unos quince minutos y `go test` la
  cortaba a los diez con seis contextos sin ejecutar, así que `run.ps1` y el flujo de CI
  amplían ahora ese límite a 45 minutos.
- Las pruebas se comprobaron también al revés: quitando la regla de orden del relay, o
  permitiendo más de un mensaje en vuelo en NATS, la batería falla.

**No ejecutado:** el flujo de CI en GitHub Actions (se ha modificado, pero no se ha lanzado) y
`go test -race` (en esta máquina no hay compilador de C; el flujo de CI sí lo ejecuta). Tampoco
se ha probado NATS en clúster de tres nodos ni con TLS o credenciales.

**Lo que se aprendió por el camino** y no estaba en la evaluación:

- Las dos correcciones al diseño del reclamo del outbox ([§5](#5-relay-con-varias-instancias)).
- Tras reiniciar el broker, el cliente de NATS tarda en darse cuenta de que su petición del
  siguiente mensaje se perdió: medio minuto con sus valores por defecto. El adaptador pide
  latidos cada dos segundos y lo detecta en unos pocos.
- MySQL en contenedor tarda unos 55 ms por confirmación de transacción: las pruebas del outbox
  duran allí veinte veces más que en los otros motores. No son esperas por bloqueos.
- Un test de `examples/parties` contaba con que el renombrado de una empresa se entregara en la
  misma pasada en que falló su alta. Con el orden por sujeto ya no es así y se actualizó la
  expectativa: es el único test existente que cambió.

## 11. Riesgos y coincidencias con otras sesiones

- **`pkg/application/outbox/outbox.go`**: la propuesta de observabilidad
  (rama `claude/karpo-observabilidad-minima-…`) instrumenta `Relay.RelayOnce`, el mismo método que
  toca el paso 2. Habrá que fusionar a mano quien llegue segundo.
- **`README.md` e `INVENTARIO-PATRONES.md`**: los tocan también Security y Parties-UDM. Los cambios
  de esta tarea se limitan a las filas de mensajería y outbox.
- No se toca `contexts/security` ni el dominio de `contexts/parties`. La demostración usa la
  suscripción de Parties que ya existe, sin modificarla.
- **Semántica fina de JetStream** (qué hace el servidor con un mensaje rechazado cuando sólo se
  permite uno en vuelo): no se dio por supuesta. C7 la comprobó contra el servidor real: el
  mensaje rechazado sigue ocupando el único hueco hasta que se reentrega, y los siguientes
  esperan. Con más de uno en vuelo, C7 falla (se probó a propósito).
- **Tamaño máximo de mensaje** en NATS: 1 MB por defecto. Los eventos actuales están muy por
  debajo; el adaptador rechazará con un error claro, que sí cuenta como intento.
