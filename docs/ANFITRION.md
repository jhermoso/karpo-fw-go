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
  | Importación | cargadores de empresas, departamentos y personas | Parties (escritos aquí) |
| Importación | cargador de centros de trabajo | Instalaciones (escrito aquí) |
| Importación | cargador de empleos | RRHH y Parties (escrito aquí) |
| Importación | cargadores del puesto y del centro de cada persona | RRHH, Parties (escritos aquí) |
| Importación | cargadores de clientes y proveedores | Parties (escritos aquí) |
| Importación | cargadores de tipos de IVA, plan de cuentas y cuentas propias | Fiscal, Contabilidad, Tesorería (escritos aquí) |
| Importación | cargador de cuentas de clientes | Sectorial financiero (escrito aquí) |
  | Sectorial financiero | qué empresas son entidad financiera | Parties (escrito aquí) |
| Módulos | qué capacidades se derivan de lo que es la empresa (`financial`) | Parties (escrito aquí) |
| Exportación | listado de cuentas de clientes | Sectorial financiero (escrito aquí) |
| Exportación | listados de participantes, roles y relaciones | Parties (escritos aquí) |
| Exportación | listado de empleados | RRHH y Parties (escrito aquí) |

- **Mensajes:** un transporte en memoria. Los veintiún contextos que publican tienen su relé y
  los diez que escuchan (Contabilidad, Facturación, Documentos, Fiscal, Inventario, Pedidos,
  Parties, Pagos, Cobros y Envíos) están suscritos. `Deliver` lleva lo publicado hasta que no
  queda nada.
- **`Start`** (tras migrar; se puede llamar en cada arranque): pasa a Seguridad los 149 permisos
  de todos los contextos, completa el catálogo de módulos y, si nadie administra la instalación,
  crea el administrador que nombre el entorno.
- **`Handler`**: las rutas de sesión son públicas; todo lo demás exige sesión y pasa por los
  permisos y el ámbito de quien llama.
- **`RunChores`**, lo que estaba pendiente de programar en los documentos de cada contexto:
  - cierra las importaciones cuyo proceso murió;
  - escribe las exportaciones en espera (veinte por ronda) y borra los ficheros caducados;
  - empresa por empresa, caduca las reservas de divisa y los presupuestos vencidos;
  - asienta lo que Contabilidad tenía aparcado y ya puede asentar.
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
   existan el calendario y la consulta en Parties.
8. **Un cargador y un listado reales como muestra** (empresas de una importación; cuentas de
   clientes como exportación). El resto sigue pendiente. Sugerencia: sí; prueban que el diseño de
   Importación y Exportación funciona de punta a punta.
9. **Configuración por variables de entorno**, sin fichero. Sugerencia: sí.

## El oyente que rechaza un mensaje (resuelto para Contabilidad)

- **El problema:** el transporte en memoria entrega a todos los oyentes a la vez y, si uno falla,
  el relé del emisor reintenta el mensaje entero. Contabilidad rechazaba los mensajes de una
  empresa que aún no tiene libro o cuentas de contrapartida, así que una empresa sin plan
  contable dejaba reintentándose mensajes de Facturación, Cobros, Pagos, Nóminas, Compras y
  Activos.
- **La salida (aprobada por Javier el 2026-10-08):** Contabilidad **aparca** lo que no puede
  asentar y lo asienta cuando puede. El anfitrión suscribe ese aparcamiento en lugar del
  consumidor directo. Ver [CONTABILIDAD.md](CONTABILIDAD.md), «Hechos aparcados».
- **Lo que queda:** es una solución de Contabilidad, no del transporte. Cualquier otro oyente que
  rechace un mensaje por una regla tendría el mismo efecto; hoy ninguno de los otros nueve lo
  hace por un motivo que dependa de la configuración de una empresa, pero una cola por oyente en
  el transporte sigue siendo la salida general.

## Validación

- **`host`** (en memoria y en SQLite migrada con los 25 esquemas): arranque sin secreto
  rechazado; permisos, módulos y primer administrador, y un segundo arranque que no cambia nada;
  sin sesión 401, token inválido 401, contraseña errónea 401, cambio de contraseña obligatorio
  (403 antes); los 149 permisos en el catálogo de Seguridad; importación por HTTP que crea dos
  empresas en Parties y omite con aviso lo que nadie carga; repetirla con el nombre escrito de
  otra forma no crea nada; cuenta de cliente, exportación pedida por HTTP, escrita por las tareas
  y descargada; segunda ronda de tareas sin nada que hacer; mensajes entregados y nada que
  entregar después; tipos con historial y el historial de la cuenta.
- **`cmd/karpo`** arrancado de verdad: `serve` sin migrar sale con error; `migrate` aplica 73
  migraciones; `serve` responde en `/readyz`, 401 sin sesión, y la sesión del administrador.
- **`cmd/karpo-postgres`** contra un PostgreSQL real: sin configuración no arranca y dice qué
  falta; mandato desconocido; migrar dos veces; servir, `/readyz`, 401 sin sesión, varias rondas
  de entrega y tareas, y parada ordenada.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: los 25 esquemas migrados en una
  misma base (ninguna tabla ni índice repetido entre contextos), verificación, arranque,
  importación, cuenta, exportación, tareas y entrega.

## Pendiente

- **Versión en macroservicios** (decisión 1): qué contextos van en cada proceso, transporte real
  entre ellos (NATS o Kafka) y relé seguro con varias instancias.
- Probar `karpo-postgres` contra un proyecto de Supabase.
- Resolver el riesgo del oyente que rechaza.
- Listados de Exportación de facturas, pedidos…
- Adaptadores que faltan: calendario de festivos para Cobros y códigos de promoción para Cambio.
- Guardas del historial por tipo, para que no sea solo de administradores.
- Observabilidad: la rama de trazas y métricas aún no está fusionada; el anfitrión es donde se
  conecta.
- Principales de servicio desde la configuración (el anfitrión ya los acepta en `Options`).
- Purga de sesiones caducadas y de las bandejas de entrada por antigüedad.
