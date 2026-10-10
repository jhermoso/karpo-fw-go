# El anfitrión

`host` es el punto de composición de Karpo: el único sitio que conoce todos los contextos. No
tiene negocio; monta los veintiséis contextos sobre un mismo almacén, da a cada uno los puertos
que pide de los demás, lleva los mensajes de unos a otros, declara los permisos a Seguridad, sirve
las rutas tras la autenticación y hace lo que los contextos dejan para un planificador.

Hay dos programas, que solo se distinguen en el controlador y en la línea que abre la base:

- **`cmd/karpo-postgres`**: el de producción, sobre **PostgreSQL o Supabase**.
- **`cmd/karpo`**: sobre un fichero SQLite, para desarrollar.

Lo que tienen en común está en `host/serve`.

Hasta ahora cada contexto se probaba solo o con sus vecinos. Sin esto, nada de lo portado se podía
arrancar junto.

## Qué hace

- **`host.Compose(sw, opciones)`** monta los contextos en orden de dependencia y los cablea con
  los adaptadores que ya existían en cada uno:

  | Quien pide | Qué pide | De quién |
  |---|---|---|
  | Instalaciones, Parties | comprobar direcciones | Geografía |
  | Parties | directorio de instalaciones | Instalaciones |
  | Seguridad | personas y empresas | Parties |
  | Fiscal, Facturación, Tesorería | identidad fiscal | Parties |
  | RRHH | organizaciones e instalaciones | Parties, Instalaciones |
  | Nóminas | empleados | RRHH |
  | Inventario, Pedidos | catálogo y precios | Productos |
  | Pedidos | riesgo del cliente | Cobros |
  | Facturación, Compras | motor de impuestos | Fiscal |
  | Pagos | reparto del neto de nómina | Nóminas |
  | Tesorería | lo cobrable y lo pagable | Cobros, Pagos |
  | Cobros | calendario del vendedor (festivos y fines de semana) | Geografía y Parties (escrito aquí) |
  | Cambio | de quién es un código de promoción | Parties (escrito aquí) |
  | Importación | cargadores de empresas, departamentos y personas | Parties (escritos aquí) |
| Importación | cargador de centros de trabajo | Instalaciones (escrito aquí) |
| Importación | cargador de empleos | RRHH y Parties (escrito aquí) |
| Importación | cargadores del puesto y del centro de cada persona | RRHH, Parties (escritos aquí) |
| Importación | cargadores de clientes y proveedores | Parties (escritos aquí) |
| Importación | cargadores de tipos de IVA, plan de cuentas y cuentas propias | Fiscal, Contabilidad, Tesorería (escritos aquí) |
| Importación | cargador de cuentas de clientes | Sectorial financiero (escrito aquí) |
| Importación | cargador del calendario de festivos | Geografía (escrito aquí) |
  | Sectorial financiero | qué empresas son entidad financiera | Parties (escrito aquí) |
| Módulos | qué capacidades se derivan de lo que es la empresa (`financial`) | Parties (escrito aquí) |
| Exportación | listado de cuentas de clientes | Sectorial financiero (escrito aquí) |
| Exportación | listados de participantes, roles y relaciones | Parties (escritos aquí) |
| Exportación | listado de empleados | RRHH y Parties (escrito aquí) |
| Exportación | listados de facturas, pedidos, vencimientos y facturas de proveedor | Facturación, Pedidos, Cobros, Compras y Parties (escritos aquí) |
| Exportación | libro diario, vencimientos de pago e inmovilizado | Contabilidad, Pagos, Activos y Parties (escritos aquí) |

- **Mensajes:** un transporte en memoria. Los veintiún contextos que publican tienen su relé y
  los diez que escuchan (Contabilidad, Facturación, Documentos, Fiscal, Inventario, Pedidos,
  Parties, Pagos, Cobros y Envíos) están suscritos. `Deliver` lleva lo publicado hasta que no
  queda nada.
- **`Start`** (tras migrar; se puede llamar en cada arranque): pasa a Seguridad los 151 permisos
  de todos los contextos, completa el catálogo de módulos y, si nadie administra la instalación,
  crea el administrador que nombre el entorno.
- **`Handler`**: las rutas de sesión son públicas; todo lo demás exige sesión y pasa por los
  permisos y el ámbito de quien llama.
- **`RunChores`**, lo que estaba pendiente de programar en los documentos de cada contexto:
  - cierra las importaciones cuyo proceso murió;
  - escribe las exportaciones en espera (veinte por ronda) y borra los ficheros caducados;
  - empresa por empresa, caduca las reservas de divisa y los presupuestos vencidos;
  - asienta lo que Contabilidad tenía aparcado y ya puede asentar;
  - vuelve a entregar lo que algún oyente no pudo coger (ver «Un buzón por oyente»).
  Una tarea que falla no detiene a las demás.
- **`Run`**: entrega y tareas en un temporizador hasta que el proceso se para.
- **Historial:** todos los tipos de agregado con historial quedan registrados en un solo sitio.

## Los programas

```
karpo-postgres migrate   aplica el esquema de todos los contextos
karpo-postgres serve     comprueba el esquema y sirve (por defecto)
```

`serve` no arranca sobre un esquema atrasado ni adelantado: lo dice y sale. `karpo` tiene los
mismos dos mandatos.

| Variable | Qué es | Por defecto |
|---|---|---|
| `KARPO_DATABASE_URL` | cadena de conexión de PostgreSQL (`karpo-postgres`) | obligatoria |
| `KARPO_DB_MAX_CONNS` | conexiones que mantiene (`karpo-postgres`) | `10` |
| `KARPO_SQLITE` | ruta del fichero de base de datos (`karpo`) | obligatoria |
| `KARPO_JWT_SECRET` | secreto que firma las sesiones, 32 caracteres como mínimo | obligatoria |
| `KARPO_ADDR` | dónde escucha | `:8080` |
| `KARPO_EXPORTS_DIR` | dónde esperan los ficheros exportados | `exports` |
| `KARPO_ALERT_WEBHOOK` | dirección a la que se avisa cuando un mensaje se da por imposible | — (solo el registro) |
| `KARPO_APISCORE_ENTITY` | nombre de la entidad financiera de la que son los ficheros de Apiscore | — (sin ella no se importa Apiscore) |
| `KARPO_DELIVER_EVERY` | cada cuánto se llevan los mensajes | `2s` |
| `KARPO_CHORES_EVERY` | cada cuánto se hacen las tareas | `1m` |
| `KARPO_BOOTSTRAP_ADMIN_USER` / `…_PASSWORD` | primer administrador, solo si no hay ninguno | — |

Sirven además `/healthz` y `/readyz` (este comprueba la base de datos).

**En Supabase**, conectar por la conexión directa o por el *pooler* de sesión (puerto 5432). El
*pooler* de transacción (puerto 6543) da a cada sentencia una conexión distinta, y tanto el
bloqueo que impide dos migraciones a la vez como las transacciones de los casos de uso necesitan
una que se mantenga. Esto sale de cómo funciona el programa y de la documentación de Supabase:
**no lo he probado contra un proyecto de Supabase real**, solo contra PostgreSQL.

`karpo-postgres` es un módulo aparte (`cmd/karpo-postgres/go.mod`) para que el controlador de
PostgreSQL no entre en el módulo raíz.

## Decisiones (aprobadas por Javier el 2026-10-08)

Con dos precisiones suyas: la base de datos de producción es **PostgreSQL o Supabase** (decisión
3), y más adelante habrá **una versión repartida en varios contextos o subdominios, como
macroservicios** (decisión 1).

1. **Un solo proceso con todos los contextos** y los mensajes en memoria, **por ahora**. Cuando
   se pueda se hará una versión repartida en macroservicios: varios contextos o subdominios por
   proceso. Es cambiar el transporte y decidir qué contextos van juntos, no los contextos.
2. **Una sola base de datos**, con el historial de migraciones de cada contexto. Migrar es un
   paso aparte de servir, y servir se niega sobre un esquema que no es el suyo. Sugerencia: sí.
3. **El motor de producción es PostgreSQL (o Supabase, que lo es).** Su programa es
   `cmd/karpo-postgres`, en un módulo aparte para no meter el controlador en el módulo raíz;
   `cmd/karpo` sobre SQLite queda para desarrollar. SQL Server, Oracle y MySQL siguen cubiertos
   por la integración, sin programa propio.
4. **Entrega y tareas en un temporizador dentro del proceso, con una sola instancia.** El relé
   aún no es seguro con varias a la vez (está anotado en el backlog). Sugerencia: sí por ahora.
5. **Las tareas las hace el propio anfitrión como administrador global de sistema** («karpo-host»
   en la auditoría). Sugerencia: sí; no hay un usuario detrás de una caducidad.
6. **El historial de cualquier tipo lo lee, de momento, solo un administrador global.** Dejar que
   lo lea quien puede ver el agregado necesita una guarda por tipo, que se añade una a una.
   Sugerencia: sí como punto de partida; es el lado seguro.
7. **Los puertos sin adaptador quedan vacíos:** Cobros no salta festivos al calcular
   vencimientos y Cambio de divisas no valida códigos de promoción. Sugerencia: sí, hasta que
   existan el calendario y la consulta en Parties. (Los dos existen desde el 2026-10-09.)
8. **Un cargador y un listado reales como muestra** (empresas de una importación; cuentas de
   clientes como exportación). El resto sigue pendiente. Sugerencia: sí; prueban que el diseño de
   Importación y Exportación funciona de punta a punta.
9. **Configuración por variables de entorno**, sin fichero. Sugerencia: sí.

## El oyente que rechaza un mensaje: un buzón por oyente

- **El problema:** el transporte en memoria entrega a todos los oyentes a la vez y, si uno
  falla, el relé del emisor reintenta el mensaje entero y, a los diez intentos, lo da por
  perdido para todos. Un oyente con un problema retenía los mensajes de los demás.
- **Primera salida (2026-10-08), solo para Contabilidad:** Contabilidad aparca lo que una regla
  suya no deja asentar. Sigue en pie: ver [CONTABILIDAD.md](CONTABILIDAD.md), «Hechos aparcados».
- **Salida general (2026-10-09): cada oyente tiene un buzón**, en `host/mailbox`. El anfitrión
  ya no suscribe al oyente, sino a su buzón:
  - Si el oyente coge el mensaje, no queda rastro.
  - Si lo rechaza, **el mensaje se guarda para ese oyente** con el motivo, y al emisor se le dice
    que está entregado. Los demás oyentes no se enteran.
  - Lo guardado se reintenta en cada ronda de tareas, con espera creciente: 1, 2, 4… minutos,
    hasta una hora. A los **diez intentos se da por imposible** y espera a una persona.
  - **Orden:** mientras un mensaje espera, los que llegan después sobre lo mismo (el mismo
    `subject`: la misma factura, el mismo pedido) se ponen detrás sin intentarse. Lo que trata de
    otra cosa pasa con normalidad.
  - Un mensaje reenviado que ya está guardado no se guarda dos veces.
- **Para quien administra** (solo administrador global, como el historial):
  - `GET /api/deliveries?consumer=&status=` — lo guardado, lo más antiguo primero.
  - `POST /api/deliveries/retry` — vuelve a intentar ya todo lo que espera, y lo dado por
    imposible (uno, con `{"id":…}`, o todo).
  - `POST /api/deliveries/{id}/discard` con `{"note":…}` — ese oyente no lo recibirá; lo que
    esperaba detrás sigue su camino.
- **Tabla:** `host_deliveries`, migración propia del anfitrión (contexto `host`). No toca
  `pkg/`: con un transporte real (NATS, Kafka) cada consumidor tiene su cola y el buzón sobra.

### Aviso cuando un mensaje se da por imposible

Añadido el 2026-10-10. Era lo que quedaba abierto en la decisión 7: un mensaje dado por imposible
esperaba en una lista que nadie tenía por qué mirar.

- **Cuándo:** una sola vez por mensaje, en el momento en que agota sus diez intentos. Es el único
  instante en que hace falta una persona: desde ahí ya no pasa nada solo.
- **Dónde:**
  - **siempre en el registro**, como error, con el oyente, el tipo de mensaje, los intentos y el
    motivo;
  - **en un webhook**, si se configura `KARPO_ALERT_WEBHOOK`: un `POST` con JSON que lleva un
    `text` legible (lo que aceptan los webhooks de entrada de las herramientas de chat
    habituales) y los datos sueltos (`kind`, `consumer`, `delivery`, `eventType`, `subject`,
    `attempts`, `reason`) para quien quiera procesarlo.
- **Qué no se envía:** el contenido del mensaje. Solo de quién era, de qué tipo y por qué se
  rechazó.
- **Si el aviso falla** (el webhook no responde o responde error), se anota en el registro y la
  ronda sigue: lo dado por imposible continúa en la lista.
- **Resumen para una pantalla:** `GET /api/deliveries/summary` (administrador global) devuelve,
  por oyente, cuántos mensajes esperan y cuántos están dados por imposibles, con estos primero.

Decisiones (aprobadas por Javier el 2026-10-10):

1. **Se avisa solo al dar por imposible**, no en cada rechazo. Sugerencia: sí; los rechazos
   intermedios se arreglan solos casi siempre y avisarían de más.
2. **El canal es un webhook genérico**, no correo. Karpo no tiene servidor de correo configurado
   y un webhook sirve para Slack, Teams, un correo vía pasarela o una herramienta de guardias.
   Sugerencia: sí.
3. **Una sola dirección para toda la instalación**, por variable de entorno. Sugerencia: sí; es
   operación del servidor, no de una empresa.
4. **El texto del aviso va en castellano y no incluye el contenido del mensaje.** Sugerencia: sí;
   el webhook sale de la instalación y el mensaje puede llevar importes o nombres.
5. **El motivo del rechazo sí se envía.** Suele ser un texto técnico («la empresa no tiene
   libro»), pero podría nombrar algo. Sugerencia: sí; sin el motivo el aviso no sirve. Dime si
   prefieres quitarlo.
6. **No se repite el aviso** si nadie hace nada. Sugerencia: sí por ahora; un recordatorio diario
   sería el siguiente paso si se quedan sin atender.
7. **Los hechos aparcados de Contabilidad no avisan**: se resuelven solos al abrir el libro o
   completar el perfil, y los ve quien lleva la contabilidad. Sugerencia: sí.

### Decisiones (aprobadas por Javier el 2026-10-09)

1. **Al emisor siempre se le dice «entregado»** cuando el mensaje queda guardado en el buzón.
   Solo se le devuelve error si ni siquiera se pudo guardar. Sugerencia: sí; es lo que desacopla
   a los oyentes.
2. **Se guarda cualquier fallo**, no solo los de una regla: también una base de datos caída o un
   error de programación. Sugerencia: sí; así ningún fallo de un oyente toca a los demás.
3. **Diez intentos y espera hasta una hora** antes de darlo por imposible (unas cuatro horas
   en total). Sugerencia: sí; son constantes fáciles de cambiar.
4. **Un mensaje dado por imposible retiene a los que vienen detrás sobre lo mismo** hasta que
   alguien lo reintenta o lo descarta. Sugerencia: sí; entregar una anulación antes que la
   factura que anula es peor que esperar.
5. **Lo entregado se borra del buzón**; lo descartado se conserva con su nota. Sugerencia: sí.
6. **Solo un administrador global ve y resuelve los buzones**, sin permiso propio en el catálogo.
   Sugerencia: sí por ahora; es operación de la instalación, no de una empresa.
7. **Nadie recibe aviso** cuando un mensaje se da por imposible: hay que mirar la lista.
   Sugerencia: aceptarlo hasta que haya notificaciones u observabilidad; es el siguiente paso
   natural.
8. **El aparcamiento de Contabilidad se queda**, delante de su buzón: sabe de empresas (ordena
   por empresa, lo resuelve quien lleva la contabilidad con su permiso). Sugerencia: sí.

## Validación

- **`host`** (en memoria y en SQLite migrada con los 25 esquemas): arranque sin secreto
  rechazado; permisos, módulos y primer administrador, y un segundo arranque que no cambia nada;
  sin sesión 401, token inválido 401, contraseña errónea 401, cambio de contraseña obligatorio
  (403 antes); los 151 permisos en el catálogo de Seguridad; importación por HTTP que crea dos
  empresas en Parties y omite con aviso lo que nadie carga; repetirla con el nombre escrito de
  otra forma no crea nada; cuenta de cliente, exportación pedida por HTTP, escrita por las tareas
  y descargada; segunda ronda de tareas sin nada que hacer; mensajes entregados y nada que
  entregar después; tipos con historial y el historial de la cuenta.
- **`cmd/karpo`** arrancado de verdad: `serve` sin migrar sale con error; `migrate` aplica 76
  migraciones; `serve` responde en `/readyz`, 401 sin sesión, y la sesión del administrador.
- **`cmd/karpo-postgres`** contra un PostgreSQL real: sin configuración no arranca y dice qué
  falta; mandato desconocido; migrar dos veces; servir, `/readyz`, 401 sin sesión, varias rondas
  de entrega y tareas, y parada ordenada.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: los 25 esquemas migrados en una
  misma base (ninguna tabla ni índice repetido entre contextos), verificación, arranque,
  importación, cuenta, exportación, tareas y entrega.

- **`host/mailbox`** (en memoria, SQLite y los cuatro motores): un oyente rechaza y el otro
  recibe; el emisor no se entera; reenvío sin duplicar; lo que llega detrás sobre lo mismo
  espera y lo demás pasa; reintento en orden; diez intentos y se da por imposible; descartar
  con nota libera lo que esperaba; reintentar uno o todos; solo administrador global; el aviso
  sale una vez por mensaje y el resumen cuenta por oyente.
- **`host/serve`**: el aviso sin webhook solo va al registro; con webhook lleva oyente, tipo,
  intentos y motivo y nunca el contenido; un webhook que falla o no existe se anota y no detiene
  nada; se envía aunque la ronda esté parando; la dirección mal escrita impide arrancar.

## Pendiente

- **Versión en macroservicios** (decisión 1): qué contextos van en cada proceso, transporte real
  entre ellos (NATS o Kafka) y relé seguro con varias instancias.
- Probar `karpo-postgres` contra un proyecto de Supabase.
- ~~Resolver el riesgo del oyente que rechaza~~: hecho, ver «Un buzón por oyente», con su aviso
  cuando un mensaje se da por imposible.
- Más listados de Exportación (nóminas, movimientos de almacén, pagos, cobros…).
- ~~Calendario de festivos para Cobros~~: hecho, ver [GEOGRAFIA.md](GEOGRAFIA.md).
- ~~Códigos de promoción para Cambio~~: hecho, ver [CAMBIO.md](CAMBIO.md).
- Guardas del historial por tipo, para que no sea solo de administradores.
- Observabilidad: la rama de trazas y métricas aún no está fusionada; el anfitrión es donde se
  conecta.
- Principales de servicio desde la configuración (el anfitrión ya los acepta en `Options`).
- Purga de sesiones caducadas y de las bandejas de entrada por antigüedad.
