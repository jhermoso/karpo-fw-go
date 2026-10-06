# Observabilidad mínima: registro estructurado, métricas y trazas

> **Reconstrucción (2026-10-06).** La evaluación original estaba sin confirmar en el checkout de
> Windows. Esta rama se trabajó en un contenedor en la nube y ese fichero no estaba en ninguna rama
> remota. Javier pidió rehacerla a partir del código, así que este documento es esa reconstrucción.
> El plan de diez pasos y las decisiones D1 a D8 son **las recomendaciones de esta reconstrucción**,
> no una copia de las originales. Javier aprobó «D1 a D8 tal como las recomienda el documento», pero
> sobre el original. **Hay que cotejarlas con él** y anotar aquí cualquier diferencia.

El objetivo es el mismo mínimo en Go y en C#, con los mismos nombres, para que los dos lenguajes se
lean igual:

- una línea JSON por petición en la salida estándar;
- la causa de todo 5xx, con su correlación;
- duración y resultado por ruta;
- las métricas básicas;
- un registro que sirva también fuera de la web (el relay del outbox, el worker).

## 1. Lo medido en el C# (Karpo, `020-Back/020-Source`)

Todo lo de esta sección está comprobado en el código el 2026-10-06; las rutas son de esa fecha.

| Hecho | Dónde |
|---|---|
| Una petición normal no deja línea: `FwRequestTimingMiddleware` registra en `LogDebug` (filtrado, el mínimo es Information) y sólo avisa por encima de 1 s. Además lee el status en el `finally`, antes de que el middleware de excepciones (el más externo) ponga el 500. | `Paranoia.Karpo.Fw.Distribution.Publisher/Middleware/FwRequestTimingMiddleware.cs:42-56` |
| No hay ninguna métrica: ni `Meter`, ni `WithMetrics`, ni exportador. | búsqueda de `System.Diagnostics.Metrics` en todo el árbol: 0 resultados |
| Las trazas se crean y se descartan: `AddOtlpExporter` sólo se registra si `Observability:Otlp:Endpoint` trae valor, y está vacío en todos los `appsettings.json`. Nadie usa `FwActivitySource.Default`. | `FwObservabilityExtensions.cs:145-165`; los 15 `appsettings.json` |
| En contenedor el registro es texto: la consola usa una plantilla de texto. El JSON (CLEF) sólo va a fichero, y el fichero está apagado en compose y en `Dockerfile.rebanada` (`Observability__LogsDirectory=""`). | `FwObservabilityExtensions.cs:110-112`; `060-Deploy/docker/Dockerfile.rebanada:138` |
| La línea de error pierde la correlación: `UseFwExceptionHandling` es el primer middleware y `UseFwRequestEnrichment` va después. Cuando la excepción sube, el `using (LogContext.PushProperty("CorrelationId", …))` ya se ha liberado. | `Parties.Distribution.Publisher/Program.cs:143-154`; `FwRequestEnrichmentMiddleware.cs:77-84` |
| La correlación del cliente se acepta sin validar: cualquier valor que no sea solo espacios, sin límite de longitud ni de caracteres. | `FwRequestEnrichmentMiddleware.cs:87-96` |
| El worker del outbox no puede usar la observabilidad: `AddFwObservability` exige `WebApplicationBuilder` y el worker usa `Host.CreateApplicationBuilder`. | `FwObservabilityExtensions.cs:51`; `Paranoia.Karpo.MaccorpKernel.Outbox.Worker/Program.cs:26` |
| Posible pérdida de los logs OTLP: `UseSerilog` sustituye el `ILoggerFactory` (`writeToProviders` es false por defecto), así que el proveedor OTel de `builder.Logging` probablemente no recibe nada. | `FwObservabilityExtensions.cs:99,170` (**inferido, no probado**) |

## 2. Defectos del Go antes de esta rama

| # | Defecto | Dónde estaba |
|---|---|---|
| G1 | Un 5xx no dejaba causa. `WriteError` y `Respond` mapean un error desconocido a 500 con un detalle genérico (bien para el cliente) y el error se perdía: nadie lo registraba. | `pkg/distribution/response.go` |
| G2 | El envoltorio de `RequestLogging` rompía las respuestas en flujo. `responseRecorder` no implementaba `http.Flusher` ni `Unwrap`, así que un `w.(http.Flusher)` fallaba y `http.ResponseController` no llegaba al escritor real. | `pkg/distribution/middleware.go` |
| G3 | La correlación del cliente se usaba tal cual. `X-Correlation-ID` entraba en el contexto, en el outbox y en la respuesta sin validar longitud ni caracteres: se podían falsear líneas de registro o meter datos personales. | `pkg/distribution/middleware.go` (`Correlation`) |
| G4 | El relay callaba por defecto. Sin `WithRelayLogger`, un fallo de entrega se marcaba y no dejaba rastro. | `pkg/application/outbox/outbox.go` |
| G5 | El registro ignoraba el contexto. `VanillaLogger.WithContext` guardaba el contexto, pero el handler de slog no lee sus valores: ninguna línea llevaba `correlation_id` salvo que se pasara a mano. | `pkg/log/vanilla/vanilla.go` |
| G6 | `RequestLogging` registraba la ruta cruda (`r.URL.Path`): cardinalidad sin límite y posibles identificadores personales en el registro. | `pkg/distribution/middleware.go` |
| G7 | No había trazas ni métricas: ni contrato, ni implementación (el inventario lo marcaba ❌). | — |

## 3. Diseño

**Contrato en el núcleo, una implementación por tecnología en su paquete.** Es la regla del
repositorio, y la vigila `pkg/testing/archtest` (regla 8).

```
pkg/observability/            CONTRATO (sólo biblioteca estándar)
├── observability.go          Telemetry{Tracer, Meter}, Meter, Counter, Histogram, Noop
├── trace.go                  Tracer, Span, SpanContext, W3C traceparent, contexto
└── names.go                  nombres compartidos con C# (campos, atributos, métricas)
pkg/observability/inprocess/  ids W3C en proceso + últimos spans + registro de métricas
pkg/observability/prometheus/ GET /metrics en formato de texto Prometheus (sin dependencias)
pkg/log/default.go            log.Default(): slog.Default del proceso, para quien no tenga logger
pkg/log/vanilla/              Enrich (correlation_id, causation_id, trace_id, span_id) + FromEnv
pkg/distribution/observe.go   Observe (span + métrica + línea por petición), RecordError
pkg/application/pipeline/     Telemetry (span + duración por caso de uso)
pkg/application/outbox/       relay con log.Default por defecto y WithRelayTelemetry
```

Un adaptador de OpenTelemetry (paso 9) implementaría `observability.Tracer` y `observability.Meter`
en **un módulo aparte**, con su propio `go.mod`, como ya hace `integration/`. Así el `go.mod` raíz
no crece.

**Sin implementación configurada, todo es no-op.** El valor cero de `observability.Telemetry` no
crea spans ni métricas y cuesta una llamada vacía. Lo único que el framework hace distinto por
defecto es lo que corrige defectos:

- el relay escribe sus fallos;
- la correlación inválida se sustituye;
- `Observe`, si se monta, escribe su línea aunque no haya trazas ni métricas.

### 3.1 Nombres compartidos con C#

Los nombres son los de las convenciones semánticas de OpenTelemetry para HTTP, que es lo que ASP.NET
Core emite de forma nativa. Lo propio del framework lleva el prefijo `karpo.`. La fuente única en Go
es `pkg/observability/names.go`; en C# es `FwTelemetryNames`. **Un cambio se hace en los dos o en
ninguno.**

| Uso | Nombre |
|---|---|
| Correlación del flujo | `correlation_id` |
| Mensaje causante | `causation_id` |
| Traza y span | `trace_id`, `span_id` (hex W3C) |
| Causa de un 5xx | `error` (mensaje), `error.type` (tipo, o el status si no hay error) |
| Duración en la línea | `duration_ms` (milisegundos con tres decimales) |
| Servicio | `service.name` |
| Método, ruta, status | `http.request.method`, `http.route` (plantilla, nunca la ruta cruda), `http.response.status_code` |
| Caso de uso y resultado | `karpo.use_case`, `karpo.outcome` (`ok`, `error`) |
| Tipo de evento | `karpo.event_type` |
| Métrica HTTP | `http.server.request.duration` (s, histograma; su recuento es el de peticiones) |
| Métrica de casos de uso | `karpo.use_case.duration` (s) |
| Métricas del relay | `karpo.outbox.delivered`, `karpo.outbox.failed` |

Una línea de petición en Go (la de la prueba de extremo a extremo):

```json
{"time":"2026-10-06T06:54:31.909668118Z","level":"ERROR","msg":"http request","service.name":"geography",
 "http.request.method":"GET","http.route":"/api/reference/countries/{alpha2}","http.response.status_code":500,
 "duration_ms":1.415,"error":"sqlrepo: query domain.Country: SQL logic error: no such table: geo_countries (1)",
 "error.type":"*sqlite.Error","correlation_id":"e2e-500-1","trace_id":"87728b286ed4b3322efa010308f923b8",
 "span_id":"4d07e8526d0e6e6d"}
```

### 3.2 Composición recomendada (Go)

```go
logger := vanilla.FromEnv("parties")                 // JSON en stdout; KARPO_LOG_FORMAT=text, KARPO_LOG_LEVEL
slog.SetDefault(logger.Slog())                        // lo que use log.Default escribe igual
tel := observability.Telemetry{Tracer: inprocess.NewTracer(0), Meter: inprocess.NewRegistry()}

root := http.NewServeMux()
root.Handle("GET /metrics", prometheus.Handler(tel.Meter.(*inprocess.Registry)))
root.Handle("/", distribution.Chain(api,
	distribution.Recovery(logger),
	distribution.Correlation(),          // antes de Observe: la línea lleva la correlación
	distribution.Observe(logger, tel),   // lo más cerca posible del mux
	distribution.Authorize(jwt, resolver),
))
relay := module.Relay(sender, outbox.WithRelayLogger(logger), outbox.WithRelayTelemetry(tel))
```

## 4. Plan de diez pasos

| Paso | Qué | Hecho cuando | Estado |
|---|---|---|---|
| 1 | Contrato `pkg/observability` (trazas, métricas, no-op, nombres compartidos) y regla del guardián | el contrato sólo importa la biblioteca estándar; archtest en verde; pruebas de `traceparent` y no-op | ✅ |
| 2 | Implementación en proceso (ids W3C, últimos spans, registro de métricas) y `/metrics` en formato Prometheus | spans hijos heredan la traza; un `traceparent` válido se continúa; el texto expuesto es el esperado | ✅ |
| 3 | Registro enriquecido por contexto y JSON en stdout configurable (G5) | una línea con contexto lleva `correlation_id`, `causation_id`, `trace_id`, `span_id` sin claves repetidas; `FromEnv` da JSON por defecto | ✅ |
| 4 | Correlación del cliente validada y acotada (G3) | un valor con espacios, comillas, saltos de línea, no ASCII o de más de 128 caracteres se sustituye por un UUID nuevo y no se devuelve | ✅ |
| 5 | Telemetría por petición: `Observe` (G6, G7) con un envoltorio que no rompe el flujo (G2) | una línea, un span y una serie de `http.server.request.duration` por petición, con la ruta como plantilla; SSE sigue funcionando a través de `Observe` y de `RequestLogging` | ✅ |
| 6 | Causa de todo 5xx con su correlación (G1) | `WriteError`, `Recovery` y `RecordError` dejan `error` y `error.type` en la línea y en el span; el cliente sigue recibiendo un detalle genérico | ✅ |
| 7 | Relay que no calla (G4), métricas de entrega y telemetría de casos de uso | sin logger, el fallo sale por `slog.Default`; `karpo.outbox.delivered/failed` por tipo; `pipeline.Telemetry` mide por caso de uso y resultado | ✅ |
| 8 | Que la traza cruce el outbox (`traceparent` en `OutboxMessage`, columna nueva por dialecto) | un evento entregado por el relay continúa la traza de la petición que lo grabó | fuera de alcance |
| 9 | Adaptador de OpenTelemetry en un módulo aparte, con el colector por configuración | con `OTEL_EXPORTER_OTLP_ENDPOINT` las trazas y métricas llegan a un colector; sin él, nada cambia | fuera de alcance |
| 10 | El mismo mínimo en C# (Karpo), con los mismos nombres | ver la sección 7 | Parte 2 |

**Prueba de cierre (Go):** `e2e/observability_test.go` compone el contexto Geography real detrás de
la cadena estándar (Recovery, Correlation, Observe, Authorize) y comprueba tres cosas:

1. Una petición deja una única línea JSON con su `correlation_id` y su `trace_id`, un span de
   servidor con esa traza y esa correlación, y una serie de la métrica por ruta y status.
2. Al cambiar en caliente a una base sin esquema, el 500 deja su causa (`no such table`) en la
   línea, y el cliente no la ve.
3. `/metrics` expone las dos series.

## 5. Decisiones

| # | Decisión | Recomendación (aplicada) | Por qué |
|---|---|---|---|
| D1 | ¿API de OpenTelemetry en el núcleo o contrato propio? | **Contrato propio mínimo**; OpenTelemetry como adaptador en un módulo aparte | El núcleo no admite dependencias de terceros y el `go.mod` raíz no debe crecer. El contrato cabe en tres ficheros y se adapta 1:1 a OTel. |
| D2 | Nombres | **Convenciones semánticas de OTel para HTTP**, prefijo `karpo.` para lo propio, `snake_case` para los campos de correlación; la misma tabla en Go y en C# | ASP.NET Core ya emite `http.server.request.duration` con esos atributos: en C# la métrica sale gratis y con el mismo nombre. |
| D3 | Formato por defecto | **JSON en stdout** en el arranque de servicio (`vanilla.FromEnv`, `AddFwObservability`); texto sólo si se pide (`KARPO_LOG_FORMAT=text`, `Observability:Console:Format=text`). `vanilla.Default()` no cambia | Lo que lee un contenedor es stdout, línea a línea. |
| D4 | Correlación del cliente | **Aceptar sólo `[A-Za-z0-9._:-]{1,128}`**. Si no cumple, se genera un UUID nuevo; el valor del cliente no se registra ni se devuelve | Cubre UUID, ULID, ids de traza y de gateway; corta la inyección de líneas y los datos personales. |
| D5 | Etiqueta de ruta | **La plantilla** (`/api/x/{id}`, la del `ServeMux` o la del endpoint de ASP.NET); `unknown` si no hay. Nunca la ruta cruda | Cardinalidad acotada y sin identificadores en las métricas. |
| D6 | 5xx | Línea en **ERROR** con `error` y `error.type`; span en error; **el cliente recibe el detalle genérico de siempre**. Los 4xx van en INFO, sin causa | La causa es para quien opera, no para quien llama. |
| D7 | Relay sin logger | **`log.Default()` (`slog.Default` del proceso)**; para callarlo hay que pasarlo a propósito | Un fallo de entrega nunca debe pasar en silencio por omisión. |
| D8 | Trazas sin colector | **No-op por defecto**. Con `inprocess` hay ids W3C en los registros sin exportar nada; se respeta el `sampled` del `traceparent` entrante y una raíz local va muestreada | «Sin colector, el framework se comporta como hoy»; quien quiera `trace_id` en las líneas lo tiene sin colector. |

## 6. Qué no se pudo comprobar

- **Las decisiones originales.** Ver el aviso inicial.
- **En C#, que los logs OTLP no llegan con Serilog**: es una inferencia del funcionamiento de
  `UseSerilog`, sin colector para probarlo. No se cambia: se anota.
- **Ningún colector real.** Ni en Go ni en C# se ha probado una exportación. En C# el exportador OTLP
  sigue siendo opcional y por configuración, como antes.

## 7. C# (Parte 2)

Hecho en Karpo, en la rama `claude/karpo-observabilidad-minima-yoma1a`, con Fw 1.7.0. El detalle
está junto al artefacto, en
`020-Back/020-Source/Paranoia.Karpo.Fw.Infrastructure.Observability/FwObservability.md`.

| Mínimo | C# |
|---|---|
| Una línea JSON por petición en stdout | `FwRequestTelemetryMiddleware` (lo monta `UseFwExceptionHandling`) + `FwJsonLineFormatter` (consola JSON por defecto) |
| Causa de todo 5xx con su correlación | `FwRequestTelemetry.RecordError` desde el middleware de excepciones; la correlación se abre por fuera, así que también la línea de error la lleva |
| Duración y resultado por ruta | `http.route`, `http.response.status_code`, `duration_ms` en la línea; `http.server.request.duration` de ASP.NET Core |
| Métricas básicas | `WithMetrics`: ASP.NET Core, HttpClient, `Paranoia.Karpo.Fw` (`karpo.outbox.delivered/failed`) |
| Registro que sirva al worker | `AddFwObservability(IHostApplicationBuilder, …)`, usado por `Outbox.Worker` |
| Correlación validada | `FwCorrelationId`, la misma regla que `distribution.ValidCorrelationID` |

Diferencias que quedan, a propósito o pendientes:

- C# no expone `/metrics`: sus métricas salen por OTLP cuando hay colector.
- `http.route` en C# es la plantilla de ASP.NET tal cual, con restricciones (`{id:guid}`) y a
  veces barra final.
- C# no tiene telemetría de casos de uso.

**Falta publicar Fw 1.7.0** (`publicar-fw.ps1`, con el feed local). Hasta entonces las rebanadas no
la ven.
