# El anfitrión

`host` es el punto de composición de Karpo: el único sitio que conoce todos los contextos. No
tiene negocio; monta los veintiséis contextos sobre un mismo almacén, da a cada uno los puertos
que pide de los demás, lleva los mensajes de unos a otros, declara los permisos a Seguridad, sirve
las rutas tras la autenticación y hace lo que los contextos dejan para un planificador.

`cmd/karpo` es ese anfitrión hecho programa, sobre un fichero SQLite.

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
  | Importación | cargador de empresas | Parties (escrito aquí) |
  | Sectorial financiero | qué empresas son entidad financiera | Parties (escrito aquí) |
| Módulos | qué capacidades se derivan de lo que es la empresa (`financial`) | Parties (escrito aquí) |
| Exportación | listado de cuentas de clientes | Sectorial financiero (escrito aquí) |

- **Mensajes:** un transporte en memoria. Los veintiún contextos que publican tienen su relé y
  los diez que escuchan (Contabilidad, Facturación, Documentos, Fiscal, Inventario, Pedidos,
  Parties, Pagos, Cobros y Envíos) están suscritos. `Deliver` lleva lo publicado hasta que no
  queda nada.
- **`Start`** (tras migrar; se puede llamar en cada arranque): pasa a Seguridad los 147 permisos
  de todos los contextos, completa el catálogo de módulos y, si nadie administra la instalación,
  crea el administrador que nombre el entorno.
- **`Handler`**: las rutas de sesión son públicas; todo lo demás exige sesión y pasa por los
  permisos y el ámbito de quien llama.
- **`RunChores`**, lo que estaba pendiente de programar en los documentos de cada contexto:
  - cierra las importaciones cuyo proceso murió;
  - escribe las exportaciones en espera (veinte por ronda) y borra los ficheros caducados;
  - empresa por empresa, caduca las reservas de divisa y los presupuestos vencidos.
  Una tarea que falla no detiene a las demás.
- **`Run`**: entrega y tareas en un temporizador hasta que el proceso se para.
- **Historial:** todos los tipos de agregado con historial quedan registrados en un solo sitio.

## El programa

```
karpo migrate   aplica el esquema de todos los contextos
karpo serve     comprueba el esquema y sirve (por defecto)
```

`serve` no arranca sobre un esquema atrasado ni adelantado: lo dice y sale.

| Variable | Qué es | Por defecto |
|---|---|---|
| `KARPO_SQLITE` | ruta del fichero de base de datos | obligatoria |
| `KARPO_JWT_SECRET` | secreto que firma las sesiones, 32 caracteres como mínimo | obligatoria |
| `KARPO_ADDR` | dónde escucha | `:8080` |
| `KARPO_EXPORTS_DIR` | dónde esperan los ficheros exportados | `exports`, junto a la base |
| `KARPO_DELIVER_EVERY` | cada cuánto se llevan los mensajes | `2s` |
| `KARPO_CHORES_EVERY` | cada cuánto se hacen las tareas | `1m` |
| `KARPO_BOOTSTRAP_ADMIN_USER` / `…_PASSWORD` | primer administrador, solo si no hay ninguno | — |

Sirve además `/healthz` y `/readyz` (este comprueba la base de datos).

## Decisiones propuestas (pendientes de confirmar)

1. **Un solo proceso con todos los contextos** y los mensajes en memoria. Sugerencia: sí para
   empezar; repartir en varios procesos es cambiar el transporte, no los contextos.
2. **Una sola base de datos**, con el historial de migraciones de cada contexto. Migrar es un
   paso aparte de servir, y servir se niega sobre un esquema que no es el suyo. Sugerencia: sí.
3. **El programa del repositorio solo abre SQLite**, porque el módulo raíz no lleva más
   controladores. El de PostgreSQL, SQL Server, Oracle o MySQL es este mismo `host` con otra
   línea para abrir la base, en un módulo aparte que sí los lleve. Sugerencia: sí, y dime **cuál
   es el motor de producción** para escribir ese programa; no quiero meter cuatro controladores
   en el módulo raíz sin que lo decidas.
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

## Riesgo conocido

- **Un oyente que rechaza un mensaje frena al emisor.** El transporte en memoria entrega a todos
  los oyentes a la vez y, si uno falla, el relé del emisor reintenta el mensaje entero. Los demás
  oyentes lo descartan por repetido, así que no se duplica nada, pero ese mensaje no avanza.
- **El caso real:** Contabilidad rechaza los mensajes de una empresa que aún no tiene libro o
  cuentas de contrapartida. Con Contabilidad suscrita a todo, una empresa sin plan contable deja
  mensajes de Facturación, Cobros, Pagos, Nóminas, Compras y Activos reintentándose.
- Lo sé por la lectura del código y porque dos pruebas de integración ya restringen la
  suscripción de Contabilidad por este motivo; no lo he reproducido en el anfitrión.
- **No lo he resuelto**: la salida correcta (que Contabilidad aparque lo que no puede asentar, o
  una cola por oyente) es una decisión de diseño que prefiero no tomar de paso.

## Validación

- **`host`** (en memoria y en SQLite migrada con los 25 esquemas): arranque sin secreto
  rechazado; permisos, módulos y primer administrador, y un segundo arranque que no cambia nada;
  sin sesión 401, token inválido 401, contraseña errónea 401, cambio de contraseña obligatorio
  (403 antes); los 147 permisos en el catálogo de Seguridad; importación por HTTP que crea dos
  empresas en Parties y omite con aviso lo que nadie carga; repetirla con el nombre escrito de
  otra forma no crea nada; cuenta de cliente, exportación pedida por HTTP, escrita por las tareas
  y descargada; segunda ronda de tareas sin nada que hacer; mensajes entregados y nada que
  entregar después; tipos con historial y el historial de la cuenta.
- **`cmd/karpo`** arrancado de verdad: `serve` sin migrar sale con error; `migrate` aplica 73
  migraciones; `serve` responde en `/readyz`, 401 sin sesión, y la sesión del administrador.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: los 25 esquemas migrados en una
  misma base (ninguna tabla ni índice repetido entre contextos), verificación, arranque,
  importación, cuenta, exportación, tareas y entrega.

## Pendiente

- El programa para el motor de producción (decisión 3).
- Resolver el riesgo del oyente que rechaza.
- Cargadores de Importación que faltan (departamentos, centros, personas, empleos; Sage y
  Apiscore) y listados de Exportación (Parties, facturas, pedidos…).
- Adaptadores que faltan: calendario de festivos para Cobros y códigos de promoción para Cambio.
- Guardas del historial por tipo, para que no sea solo de administradores.
- Observabilidad: la rama de trazas y métricas aún no está fusionada; el anfitrión es donde se
  conecta.
- Principales de servicio desde la configuración (el anfitrión ya los acepta en `Options`).
- Purga de sesiones caducadas y de las bandejas de entrada por antigüedad.
- Reparto en varios procesos y transporte real (NATS o Kafka), con el relé seguro para varias
  instancias.
