# Evaluación de la observabilidad (Fw C# → Go)

> **Estado: decisiones aprobadas e implementación en Go de los pasos 1 a 7 (2026-10-06).**
> Javier aprobó las ocho decisiones, D1 a D8, tal como las recomienda este documento
> (2026-10-06). Los pasos 1 a 7 del plan están implementados en Go, cada uno con sus pruebas: ver
> [«Estado de la implementación en Go»](#estado-de-la-implementación-en-go), que también recoge
> en qué se aparta el código del esbozo y qué se ha ejecutado. Los pasos 8 (la traza cruza el
> outbox) y 9 (adaptador de OpenTelemetry) siguen pendientes.
> Lo que aparece bajo «Uso medido», «Karpo frente a Paradigma» y «Defectos» es hecho medido en la
> fase de evaluación, con su fichero y línea (las líneas del Go son las de antes de implementar).
> Lo que no se ha podido comprobar está en «Sin verificar».

Evaluación de `Fw.Infrastructure.Observability` (Serilog + OpenTelemetry), de los middlewares de
petición de `Fw.Distribution.Publisher` y de cómo los registran las publicadoras, antes de
traducirlos. Se usa el mismo método que en [LENGUAJE-UBICUO.md](LENGUAJE-UBICUO.md),
[RASGOS-TRANSVERSALES.md](RASGOS-TRANSVERSALES.md), [AUTORIZACION.md](AUTORIZACION.md) y
[EVENTOS-INTEGRACION.md](EVENTOS-INTEGRACION.md).

Motivo: Theros se construirá sobre este framework y pide como tarea previa (G-74, decisión FG12
de `Karpo_Theros_Fase_G_Servicio.md`) que **cada petición deje registro, métrica y traza**.

Tres palabras que se usan en todo el documento:

- **Registro** (log): una línea por cosa que pasa, con campos (`status=500`, `duration_ms=42`).
- **Métrica**: un número que se acumula (cuántas peticiones, cuánto tardan) y se consulta agregado.
- **Traza**: el recorrido de *una* petición, partido en tramos (*spans*): HTTP → caso de uso → SQL.

Versiones medidas: Karpo `aabaf4e7` (rama `port/paradigma-020back`; los ficheros evaluados no
tienen cambios sin confirmar), Paradigma `origin/main` `92c48eb0` (leído con `git show` y
`git grep`, sin tocar el árbol de trabajo), Karpo.Fw.Go `5b13dae`.

## Método

| Criterio | Peso |
|---|---|
| **U** Utilidad (uso real: procesos que lo usan **y** que el efecto llegue a algún sitio) | 40 % |
| **C** Corrección de la implementación | 25 % |
| **D** Diseño de la firma | 20 % |
| **G** Encaje en Go | 15 % |

Puntuación de 1 a 5 por criterio; nota ponderada sobre 100. **Mantener** ≥ 70 · **Modificar** 45–69 ·
**Retirar** < 45 · **Nuevo** · **Ya retirado**.

Para medir el uso se distingue **registrar** una pieza (aparece en `Program.cs`) de que **produzca
algo que alguien pueda ver** (una línea, un número, una traza en un visor). Aquí la diferencia es
grande.

## Hallazgo principal

**El C# no cumple hoy el mínimo que se le pide al Go.** Medido contra el criterio de Theros:

| «Cada petición deja…» | C# (Karpo) hoy | Go hoy |
|---|---|---|
| **registro** | Solo si tarda más de 1 s o si falla. Una petición normal no deja ninguna línea | Hay un middleware que lo haría (`distribution.RequestLogging`), pero nadie lo usa ni lo prueba |
| **métrica** | Ninguna. 0 apariciones de `Meter`, `Counter`, `Histogram`, `WithMetrics` o Prometheus en todo el back | Ninguna |
| **traza** | Se crea en memoria y se tira: el exportador está apagado en los 15 procesos | Ninguna; sí viaja la correlación (HTTP → outbox → sobre → consumidor) |

Consecuencia: esto no es un port. Del C# se aprovechan **la forma** (un único punto de registro,
correlación por cabecera, «apagado si no hay destino») y **las lecciones** (el fichero de log que
murió en silencio). El contenido —métricas, una línea por petición, spans propios— hay que hacerlo
nuevo.

## Qué hace hoy el C#, pieza a pieza

El proyecto son dos ficheros y 202 líneas (`FwObservabilityExtensions.cs` 179, `FwActivitySource.cs` 23).

| Pieza | Qué hace | Dónde |
|---|---|---|
| `AddFwObservability(builder, serviceName)` | Punto único: configura Serilog y OpenTelemetry | `FwObservabilityExtensions.cs:51` |
| Consola Serilog | Siempre. **Texto** con plantilla `[hora nivel] servicio cid=… user=…/… mensaje` | `:110-112` |
| Fichero Serilog | Solo si `Observability:LogsDirectory` trae valor. JSON compacto (CLEF), rotación diaria, 14 ficheros, 100 MB | `:114-123` |
| Enriquecedores | Máquina, entorno, proceso, hilo y `Service` en cada línea | `:104-109` |
| Aviso de arranque | Una línea que dice si se escribe a fichero o no | `:134-140` |
| Trazas OpenTelemetry | Recurso (`service.name`, versión, `deployment.environment`), instrumentación automática de ASP.NET Core y de `HttpClient`, fuente `Paranoia.Karpo.Fw` | `:150-162` |
| Exportador OTLP de trazas | Solo si `Observability:Otlp:Endpoint` trae valor | `:164-165` |
| Reexportación de logs por OTLP | Ídem | `:168-177` |
| `FwActivitySource` | Nombre, versión y un `ActivitySource` estático para spans manuales | `FwActivitySource.cs:15-22` |
| `FwRequestEnrichmentMiddleware` | Lee o genera `X-Correlation-Id` y lo devuelve; etiqueta el span (`app.correlation_id`, `enduser.id`, `session.id`); mete `CorrelationId`, `UserId`, `UserName`, `SessionId` y `Channel` en el contexto de log; abre el canal de operación | `Middleware/FwRequestEnrichmentMiddleware.cs:44-85` |
| `FwRequestTimingMiddleware` | Mide la petición: `Warning` si pasa de 1 s, `Debug` si no; se salta `/health` | `Middleware/FwRequestTimingMiddleware.cs:23-57` |
| `FwExceptionHandlingMiddleware` (parte de log) | `Error` con la cadena de excepciones en los 5xx, `Warning` en los 4xx | `Middleware/FwExceptionHandlingMiddleware.cs:545-561` |
| Rasgos `ITraceable` / `TraceComponent`, `ILogable` / `IInternalLogger` / `LoggerBase` | Traza y log dentro de la entidad | `Fw.Domain.Contracts/LenguajeUbicuo/Adjetivos/Contratos_y_Componentes/` |

## Uso medido (Karpo)

| Pieza | Uso |
|---|---|
| `AddFwObservability` | 15 llamadas, una por proceso web (15 de 15). El 16.º proceso, `MaccorpKernel.Outbox.Worker`, no la llama |
| `UseFwRequestEnrichment`, `UseFwRequestTiming`, `UseFwExceptionHandling` | Los 15 procesos web |
| `Observability:Otlp:Endpoint` | `""` en los 15 `appsettings.json`; ningún `docker-compose`, script ni `Dockerfile` lo rellena → **exportador activo en 0 de 15** |
| `Observability:LogsDirectory` | `"logs"` en los 15 `appsettings.json`; los dos composes y la imagen lo vacían (`docker-compose.karpo.yml:92`, `docker-compose.prod.yml:57`, `Dockerfile.rebanada:138`) → **en contenedor solo hay consola** |
| `FwActivitySource.Default` (spans manuales) | 0. Tampoco hay ningún `StartActivity` ni `new ActivitySource` en el back |
| `FwActivitySource.Name` / `.Version` | 1 uso cada uno, dentro del propio `AddFwObservability` |
| Métricas (`Meter`, `Counter`, `Histogram`, `WithMetrics`, Prometheus) | 0 |
| `UseSerilogRequestLogging` (una línea por petición) | 0 |
| `ILogger<T>` (el log que sí se usa) | 59 ficheros que no son de test (12 del Fw); unas 139 llamadas `logger.Log*` en todo el back, tests incluidos: 87 `Information`, 30 `Warning`, 16 `Error`, 6 `Debug` |
| `IInternalLogger` (el log propio del Fw) | 10 ficheros, todos del Fw; 0 fuera |
| `ITraceable` / `ITrazable` | 388 ficheros lo declaran; `GetTraceId()` se llama en 3 sitios, los tres dentro del Fw (`LogginService.cs:56,113,151`) |
| `X-Correlation-Id` enviado por el front | 0 apariciones en `010-Front/Angular/src`: la correlación la genera siempre el servidor |
| `X-Session-Id` enviado por el front | 0 |

## Karpo frente a Paradigma

**Código.** Normalizando `Paranoia.Karpo.` ↔ `Exact.`:

| Fichero | Diferencia |
|---|---|
| `…Observability.csproj` | Ninguna (mismos paquetes: Serilog.AspNetCore 9.0.0, OpenTelemetry 1.18.0) |
| `FwActivitySource.cs` | Ninguna (solo cambia el nombre de la fuente) |
| `FwObservabilityExtensions.cs` | **Karpo va por delante.** En Paradigma, si `LogsDirectory` falta o viene vacío se cae a `"logs"`, una ruta relativa, y siempre se escribe fichero. Karpo lo corrigió el 2026-09-06 (`d6a7a18a`): vacío significa «sin fichero», y la primera línea del log lo dice. El último cambio de Paradigma en el proyecto es del 2026-08-29 |
| `FwRequestEnrichmentMiddleware.cs`, `FwRequestTimingMiddleware.cs` | Ninguna |
| `FwAuthorizationContextMiddleware.cs` | Solo existe en Karpo (resuelve la correlación por su cuenta, `:218-233`) |
| Rasgos `Traceable` y `Loggable` (6 ficheros) | Ninguna |

**Uso.**

| | Karpo | Paradigma `origin/main` |
|---|---|---|
| Procesos | 16 (15 web + worker) | 4 (3 web + worker) |
| Llaman a `AddFwObservability` | 15 | 3 |
| Worker del outbox con observabilidad | No | No |
| Ficheros `.cs` que mencionan `AddFwObservability` o `FwActivitySource` | 17 (15 `Program.cs` + 2 propios) | 5 (3 + 2) |
| Ficheros `.cs` que mencionan OpenTelemetry, `ActivitySource` o `AddFwObservability` | 20 | 8 |
| Endpoint OTLP configurado | 0 de 15 | 0 de 3 |
| Spans manuales, métricas | 0 | 0 |

La diferencia de 8 a 17 (o de 8 a 20) son las 12 rebanadas por subdominio que Karpo añadió: la
misma llamada repetida, no un uso distinto. **Paradigma no tiene nada de observabilidad que Karpo
no tenga**; lo único que difiere es el arreglo del fichero de log, y está en Karpo.

## Resultados

| # | Pieza C# | Uso real | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|---|
| 1 | `FwRequestEnrichmentMiddleware` (correlación, contexto de log, etiquetas del span) | 15 procesos | 5 | 3 | 3 | 3 | **76** | Mantener, repartido: la correlación ya existe (`distribution.Correlation`); falta que el log y el span la lean del contexto |
| 2 | Log de errores de `FwExceptionHandlingMiddleware` | 15 procesos | 5 | 3 | 3 | 3 | **76** | Mantener → hoy **falta** en Go: `distribution.WriteError` no registra nada |
| 3 | Consola (stdout) como destino del log | 15 procesos; único destino en contenedor | 5 | 2 | 3 | 4 | **74** | Mantener el destino, cambiar el formato: JSON siempre (`log/vanilla.NewJSON`) |
| 4 | `FwRequestTimingMiddleware` (duración; aviso si > 1 s) | 15 procesos | 5 | 2 | 3 | 4 | **74** | Mantener → `distribution.RequestLogging` (existe) con una línea **por cada** petición, más un histograma |
| 5 | `AddFwObservability` (punto único de registro) | 15 de 16 procesos | 5 | 3 | 2 | 2 | **69** | Modificar → un constructor en el punto de composición que devuelve logger, tracer y medidor; sirve también para procesos sin HTTP |
| 6 | Enriquecedores (máquina, entorno, proceso, hilo, servicio) | 15 procesos | 3 | 4 | 3 | 3 | **65** | Modificar → atributos fijos `service`, `version`, `env`; el hilo no significa nada en Go |
| 7 | Trazas: instrumentación automática de ASP.NET Core y recurso | Registrada en 15; visible en 0 | 2 | 4 | 4 | 3 | **61** | Modificar → contrato `trace.Tracer` + middleware HTTP |
| 8 | Instrumentación automática de `HttpClient` | Registrada en 15; visible en 0 | 2 | 4 | 4 | 3 | **61** | Modificar → envoltorio de `http.RoundTripper`. **Pendiente**: hoy no hay ningún cliente HTTP en el Fw ni en los contextos (0 `http.Client`) |
| 9 | Exportador OTLP de trazas, apagado si no hay endpoint | 0 de 15 | 1 | 3 | 4 | 3 | **48** | Modificar → adaptador en módulo aparte; se conserva la regla «sin destino, apagado» |
| 10 | Reexportación de logs por OTLP | 0 de 15 | 1 | 3 | 3 | 2 | **41** | Retirar: los logs van a stdout y los recoge quien ejecuta el contenedor |
| 11 | `FwActivitySource` (fuente estática para spans manuales) | 0 spans manuales | 1 | 4 | 2 | 1 | **39** | Retirar: en Go el tracer se inyecta; no hay estado global |
| 12 | Fichero CLEF rotatorio | Solo fuera de contenedor | 2 | 2 | 2 | 1 | **37** | Retirar: rotar y enviar es trabajo del motor de contenedores |
| 13 | `ILogable`, `LoggingComponent`, `IInternalLogger`, `LoggerBase` | 0 fuera del Fw | 2 | 3 | 1 | 1 | 38 | Ya retirado en [RASGOS-TRANSVERSALES.md](RASGOS-TRANSVERSALES.md) (#16) → `pkg/log` |
| 14 | `ITraceable` + `TraceComponent` | 388 declaraciones; 0 llamadas fuera del Fw | 1 | 2 | 1 | 1 | 25 | Ya retirado en [RASGOS-TRANSVERSALES.md](RASGOS-TRANSVERSALES.md) (#23) → el span viaja en `context.Context` |
| 15 | Métricas | — | — | — | — | — | — | **Nuevo** → contrato `metrics.Meter` |
| 16 | Observabilidad del outbox y de su worker | — | — | — | — | — | — | **Nuevo** → relay y consumidor instrumentados |
| 17 | Identificador de sesión (`sid`, `X-Session-Id`) | El front no envía la cabecera | — | — | — | — | — | No se porta por ahora (el claim `sid` no se ha comprobado) |

## Defectos encontrados (justifican la columna C)

En el C#:

- **Las trazas nunca han salido del proceso.** El exportador depende de
  `Observability:Otlp:Endpoint` (`FwObservabilityExtensions.cs:145-146,164`) y los 15
  `appsettings.json` lo traen vacío (p. ej. `Parties.Distribution.Publisher/appsettings.json:21`).
  Los spans se crean, se etiquetan y se descartan. El proyecto existe desde el 2026-04-15
  (Paradigma `46a47f3e`); no se ha revisado el historial de configuración para saber si alguna
  vez estuvo encendido.
- **Una petición normal no deja ninguna línea.** `FwRequestTimingMiddleware.cs:45-56` solo escribe
  en `Warning` si pasa de 1000 ms; el resto va a `Debug`, y el nivel mínimo es `Information`
  (`appsettings.json:10-11`). Los logs de petición del propio ASP.NET Core están silenciados
  (`Microsoft.AspNetCore: Warning`, `:13`) y no se usa `UseSerilogRequestLogging`.
- **No hay ninguna métrica.** Ni de peticiones, ni de base de datos, ni del outbox.
- **En contenedor el log no es estructurado.** El JSON solo va al fichero (`:114-123`); la consola
  usa una plantilla de texto (`:110-112`). Como los contenedores vacían `LogsDirectory`, en
  producción lo único que queda es texto.
- **La línea de error pierde la correlación.** `UseFwExceptionHandling` es el middleware más
  externo (`Parties…/Program.cs:143`) y registra en su `catch`
  (`FwExceptionHandlingMiddleware.cs:42-49,552`). Para entonces los `using` que añadían
  `CorrelationId` y `UserId` (`FwRequestEnrichmentMiddleware.cs:76-84`, registrado en
  `Program.cs:152`) ya se han cerrado. *Deducido leyendo el código; no se ha ejecutado.*
- **El punto único no sirve al worker.** `AddFwObservability` recibe un `WebApplicationBuilder`
  (`:51`); el worker del outbox usa `Host.CreateApplicationBuilder`
  (`MaccorpKernel.Outbox.Worker/Program.cs:26`) y se queda sin observabilidad.
- **Tres identificadores para lo mismo.** La correlación (`X-Correlation-Id`), el identificador de
  traza y un `trackingId` de 8 caracteres que el manejador de errores inventa para los errores de
  esquema (`FwExceptionHandlingMiddleware.cs:450,472`). Además la correlación se resuelve en dos
  sitios (`FwRequestEnrichmentMiddleware.cs:87-96` y `FwAuthorizationContextMiddleware.cs:218-233`).
- **`FwActivitySource` no tiene punto de extensión.** Su comentario (`FwActivitySource.cs:8-10`)
  pide a los módulos que registren su fuente «dentro de `AddFwObservability`», es decir, editando
  el Fw.
- **El fichero de log murió en silencio** (documentado en `FwObservabilityExtensions.cs:63-88`):
  contenedor de solo lectura, Serilog se traga el error del sumidero, `200 Healthy` y ni una línea.
  Karpo lo arregló; **Paradigma sigue con el respaldo a `"logs"`**.

En el Go actual (hay que arreglarlos al instrumentar):

- **Un 500 no deja rastro.** `distribution.Problem` convierte el error desconocido en un texto
  genérico (`response.go:33-36`) y `WriteError` lo escribe sin registrar la causa (`:74-79`). Los
  contextos llaman a `WriteError`/`Respond` 211 veces en 20 ficheros.
- **El logger recibe el contexto y no hace nada con él.** `vanilla.WithContext` guarda el `ctx`
  (`vanilla.go:71-76`), pero los manejadores son los estándar de `slog` (`:20-41`), que no leen
  valores del contexto. La correlación solo sale si alguien la pasa a mano, y solo lo hace
  `pipeline.Logging` (`pipeline.go:98`).
- **`RequestLogging` rompe las respuestas en flujo y no lleva correlación.** Su envoltorio de
  `ResponseWriter` (`middleware.go:53-61`) no expone `Flush` ni `Unwrap`: detrás de él no funciona
  el envío progresivo (el canal servidor → navegador de la entrega G5 de Theros). La línea
  (`:74-79`) no incluye correlación ni usa el contexto, y no se escribe si el handler entra en pánico.
- **La correlación del cliente no se valida.** `Correlation()` acepta cualquier cabecera
  (`middleware.go:142-147`) y el valor acaba en columnas `VARCHAR(64)` del outbox y de la
  auditoría (`postgres/postgres.go:63,82`; ídem en MySQL, Oracle y SQL Server). Una cabecera más
  larga haría fallar toda escritura de esa petición. *Deducido; no se ha ejecutado.*
- **El relay calla por defecto.** Solo registra si se le pasa un logger (`outbox.go:115,156`) y
  nadie llama a `WithRelayLogger` (0 usos). Cuando registra, no incluye la correlación del
  mensaje (`:116`).
- **Lo que hay no está enchufado ni probado.** `pipeline.Logging`: 0 usos, 0 tests.
  `distribution.RequestLogging`: 0 usos, 0 tests. `Recovery` solo se prueba con logger nulo
  (`distribution_test.go:146`). `messaging.Lag`, pensada «para métricas y logs»: 0 usos.

## Lo que ya hay en Go y se respeta

| Pieza | Estado |
|---|---|
| `pkg/log` (contrato) + `pkg/log/vanilla` (sobre `log/slog`, biblioteca estándar) | Existe; se usa en 4 puntos del Fw y en ningún contexto |
| Correlación: `distribution.Correlation` → `application.WithCorrelationID` → outbox (`correlation_id`) → `Envelope.CorrelationID` → consumidor | Existe y está probada |
| Causa: `application.WithCausationID`, `causation_id` en el outbox, `Envelope.CausationID` | Existe |
| `distribution.HealthRegistry` (`/healthz`, `/readyz`) | Existe |
| Traza y log **fuera** de la entidad | Decidido en [RASGOS-TRANSVERSALES.md](RASGOS-TRANSVERSALES.md); no se reabre |
| Regla: contrato sin terceros + una implementación por tecnología en su paquete | La vigila `archtest` (`archtest_test.go:31-57`) |
| Dependencias de `go.mod` | Dos directas: `shopspring/decimal` y `modernc.org/sqlite`. Los drivers de base de datos viven en el módulo aparte `integration/` |

## Diseño propuesto en Go

```text
 traceparent, X-Correlation-ID
        │
        ▼
 distribution.Observe ──► span «servidor» + línea de acceso + histograma http
        │   ctx: correlación, span, ficha de la petición
        ▼
 pipeline.Observed ─────► span «caso de uso» + histograma por resultado
        ▼
 orchestration ─ sqlrepo.DB (WithTelemetry) ──► span por transacción y por sentencia + histograma db
        │
        └─ outbox.Recorder: guarda correlación, causa (y, si se aprueba D6, traceparent)
                 ┆  (otro momento, otra goroutine)
 outbox.Relay ──────────► span «entrega» + contadores + antigüedad del más viejo
        ▼
 messaging.Consumer ────► span «consumo» + contador por resultado + retraso
```

### Paquetes

| Paquete | Tipo | Qué contiene |
|---|---|---|
| `pkg/log` | contrato (existe) | Sin cambios de firma |
| `pkg/log/vanilla` | implementación (existe) | Se le añade leer atributos del contexto: correlación, causa, traza, actor |
| `pkg/trace` | **contrato nuevo**, solo biblioteca estándar | `Tracer`, `Span`, `SpanContext`, lectura y escritura de `traceparent`, `Noop` |
| `pkg/metrics` | **contrato nuevo**, solo biblioteca estándar | `Meter`, `Counter`, `Histogram`, `Gauge`, `Noop` |
| `pkg/trace/vanilla` | implementación sin dependencias | Genera identificadores, hereda el padre, muestrea y entrega cada span terminado a un destino: una línea de log, o una lista en memoria para los tests |
| `pkg/metrics/vanilla` | implementación sin dependencias | Registro en memoria (los tests leen de él) y un `http.Handler` que lo escribe en el formato de texto de Prometheus; métricas del runtime con `runtime/metrics` |
| `otel/` (**módulo aparte**, con su propio `go.mod`, como `integration/`) | adaptador | Implementa `trace.Tracer` y `metrics.Meter` sobre el SDK de OpenTelemetry y exporta por OTLP. El `go.mod` del framework no cambia |

`pkg/trace` y `pkg/metrics` siguen el patrón de `pkg/log`, `pkg/cache` y `pkg/time`: contrato en
el paquete, implementaciones en subpaquetes.

### Contratos (esbozo, no código definitivo)

```go
package trace

type SpanContext struct { TraceID [16]byte; SpanID [8]byte; Sampled bool }

func ParseTraceParent(header string) (SpanContext, bool) // W3C: 00-<32 hex>-<16 hex>-<2 hex>
func (sc SpanContext) TraceParent() string

type Tracer interface {
	Start(ctx context.Context, name string, opts ...StartOption) (context.Context, Span)
}
type Span interface {
	Context() SpanContext
	SetAttributes(kv ...any) // pares clave/valor, como en log.Logger
	RecordError(err error)   // marca el tramo como fallido
	End()
}
func WithKind(k Kind) StartOption                // Server, Client, Producer, Consumer, Internal
func WithRemoteParent(sc SpanContext) StartOption // continuar la traza que llega de fuera
func FromContext(ctx context.Context) Span        // nunca nil: Noop si no hay
func LogAttrs(ctx context.Context) []any          // "trace_id", "span_id"
func Noop() Tracer
```

```go
package metrics

type Meter interface {
	Counter(name, unit, help string) Counter
	Histogram(name, unit, help string, buckets ...float64) Histogram
	Gauge(name, unit, help string, read func() float64) // se lee al exportar
}
type Counter interface   { Add(ctx context.Context, n float64, labels ...string) }
type Histogram interface { Record(ctx context.Context, v float64, labels ...string) }
func Noop() Meter
```

Tres reglas del diseño:

1. **Todo es opcional y por defecto no hace nada.** Cada pieza instrumentada recibe el tracer y el
   medidor por opción; si no se pasan, usa `Noop`. Ningún test ni contexto actual cambia de
   comportamiento.
2. **Las etiquetas de las métricas son pocas y cerradas**: método, patrón de ruta, código de
   estado, tipo de petición, resultado, motor, operación, tipo de evento, consumidor. Nunca
   identificadores, organizaciones ni correlaciones (cada valor distinto es una serie nueva).
3. **Nada personal ni secreto en logs, spans o etiquetas**: ni el cuerpo de los comandos, ni los
   argumentos SQL, ni nombres, correos o tokens. Del actor, solo el identificador.

El resultado de una operación se clasifica con la taxonomía de errores que ya existe, la misma que
usa `distribution.Problem` (`response.go:44-67`): `ok`, `validation`, `rule`, `not_found`,
`conflict`, `unauthorized`, `forbidden`, `error`.

### Puntos de instrumentación

| Punto | Dónde se engancha | Qué deja |
|---|---|---|
| **Entrada HTTP** | Middleware nuevo `distribution.Observe`, el más externo después de `Correlation` | Span de servidor (continúa `traceparent` si llega); una línea por petición con método, ruta, estado, duración, `correlation_id`, `trace_id` y actor; histograma de duración; la causa de los 5xx en nivel `Error` |
| **Errores HTTP** | `distribution.WriteError` (`response.go:74`) anota el error en la ficha de la petición; `ProblemDetails` añade `correlationId` | El 500 deja de ser mudo y el cliente recibe con qué buscar |
| **Tubería de casos de uso** | `pipeline.Observed[In, Out]`, que sustituye a `pipeline.Logging` (0 usos) | Span por caso de uso (`%T` del comando); histograma por tipo y resultado; línea solo si falla |
| **SQL** | Opción `sqlrepo.WithTelemetry` en `sqlrepo.New` (`db.go:31`); se aplica en `executor(ctx)` (`db.go:125`, por donde pasan las 20 sentencias de `repository.go`, `outbox.go`, `inbox.go`, `audit.go` y `db.go`) y en `Do` (`db.go:72`) | Span por transacción (confirmada o deshecha) y por sentencia, con motor y operación (`SELECT`, `INSERT`…) y **sin argumentos**; histograma de duración; estado del pool leído de `sql.DB.Stats()` |
| **Outbox (escritura)** | `outbox.Recorder.Record` (`outbox.go:27`) y `messaging.Recorder.message` (`messaging.go:115`) | Nada propio: queda dentro del span del caso de uso. Guarda la correlación (ya lo hace) y, si se aprueba D6, el `traceparent` |
| **Relay** | `Relay.RelayOnce` (`outbox.go:106`), opción `WithRelayTelemetry` | Span por mensaje entregado; contadores de entregados y fallidos por tipo de evento; antigüedad del pendiente más viejo (sale de `msgs[0].OccurredAt`, sin tocar el contrato `OutboxStore`); el log de fallo incluye la correlación y deja de ser opcional |
| **Consumidor** | `Consumer.HandleMessage` (`messaging.go:207`) | Span de consumo; contador por consumidor, tipo y resultado (`ok`, `duplicate`, `error`, `ignored`); histograma del retraso con `messaging.Lag` (`messaging.go:229`) |
| **Salida HTTP** | Envoltorio de `http.RoundTripper` que añade `traceparent` y `X-Correlation-ID` | **Pendiente**: no hay clientes HTTP hoy. Hará falta con las costuras HTTP y con el resolvedor de autorización en modo `Http` |
| **Migraciones** | `migrate.go` usa la conexión directamente (`:210-531`) | Fuera de alcance: se ejecutan al arrancar y ya devuelven su estado |

Un detalle que condiciona el middleware HTTP, comprobado en la biblioteca estándar: el patrón de
ruta (`/parties/{id}` en vez de `/parties/0192…`) lo rellena el `ServeMux` en la petición que
recibe (`net/http/server.go:2901`), y cada middleware intermedio trabaja sobre una copia
(`r.WithContext`). Por eso `Observe` deja en el contexto una **ficha de la petición** mutable que
las capas interiores van rellenando (ruta, actor, error) y que él lee al terminar. Sin esto, la
etiqueta de ruta sería la URL completa y habría una serie por identificador.

### Cómo viaja la traza junto a la correlación

Son dos identificadores con dos trabajos, y se mantienen los dos:

| | Correlación | Traza |
|---|---|---|
| Qué identifica | Un flujo de negocio completo | Una ejecución encadenada |
| Quién lo pone | El cliente, o el servidor si no viene | El servidor |
| Cuánto dura | Días: sobrevive a reintentos y a consumidores | Milisegundos o segundos |
| Dónde se guarda | Outbox, sobre, registro de auditoría, contexto de autorización | Solo en la telemetría |
| Cabecera | `X-Correlation-ID` (existe) | `traceparent` (estándar W3C) |

El C# los mezclaba: `TraceComponent` tomaba la correlación de la raíz de la traza
(`TraceComponet.cs:29-30`). Lo que se conserva es lo que hacía bien el middleware: **la correlación
se anota como atributo en cada span y en cada línea**, de modo que buscando una correlación salen
todas las trazas del flujo.

- **Entrada HTTP**: si llega `traceparent` válido, el span de servidor es hijo suyo; si no, empieza
  una traza. La correlación se lee como hoy, validando longitud (≤ 64) y caracteres; si no vale,
  se genera una nueva.
- **Paso por el outbox, sin cambio de esquema (etapa 1)**: el relay y el consumidor empiezan una
  traza nueva y le ponen `correlation_id`, `causation_id` e identificador del mensaje. El flujo se
  reconstruye por la correlación.
- **Paso por el outbox, con columna (etapa 2, decisión D6)**: `OutboxMessage.TraceParent`, columna
  `trace_parent` de 55 caracteres y admitiendo nulo en las tablas de outbox, y
  `Envelope.TraceParent` (`"traceparent"` en el JSON, el nombre que usa la extensión de trazas de
  CloudEvents). El span del consumidor pasa a ser hijo del que publicó: una sola traza de punta a
  punta. Las filas antiguas, con nulo, siguen funcionando.

### Métricas mínimas

| Métrica | Tipo | Etiquetas | Para qué |
|---|---|---|---|
| `http.server.request.duration` | histograma (s) | método, ruta, estado | Tráfico, errores y latencia de la API |
| `karpo.usecase.duration` | histograma (s) | petición, resultado | Qué caso de uso falla o va lento, y por qué clase de error |
| `db.client.operation.duration` | histograma (s) | motor, operación, resultado | Cuánto del tiempo es base de datos |
| `db.client.connections` | medida instantánea | motor, estado (abiertas, en uso, esperas) | Saturación del pool |
| `karpo.outbox.relayed` | contador | tipo de evento, resultado | Entregas y fallos |
| `karpo.outbox.oldest_pending_age` | medida instantánea (s) | outbox | **La alarma más útil**: si crece, el relay está parado o atascado |
| `karpo.messaging.handled` | contador | consumidor, tipo, resultado | Consumo, duplicados y errores |
| `karpo.messaging.lag` | histograma (s) | consumidor, tipo | Retraso entre el hecho y su efecto |
| Runtime de Go | medidas instantáneas | — | Goroutines, memoria, recolecciones |

Los nombres siguen las convenciones semánticas de OpenTelemetry donde existen (`http.server.*`,
`db.client.*`); el resto lleva prefijo `karpo.`. Queda fuera, porque exige ampliar el contrato
`OutboxStore`: el número exacto de pendientes y de mensajes aparcados por superar los reintentos.

### Guardián de arquitectura

- Añadir `pkg/trace` y `pkg/metrics` a la lista `contracts` (`archtest_test.go:31-39`) y a la regla 2.
- Regla nueva: los árboles `pkg/log`, `pkg/trace` y `pkg/metrics` solo importan la biblioteca
  estándar y el propio módulo. Hoy `pkg/log/vanilla` no la tiene: la regla 2 no es recursiva.
- Regla nueva: nada bajo `pkg/` ni `contexts/` importa `go.opentelemetry.io`.

## Plan de implementación

Pasos pequeños; cada uno deja el repositorio en verde y se puede parar después de cualquiera.

| # | Paso | Hecho cuando |
|---|---|---|
| 1 | **El log lee del contexto.** `log/vanilla` añade a cada línea la correlación, la causa y el actor que haya en el `ctx` | Un test emite una línea con `WithContext(ctx)` y lleva `correlation_id` sin pasarlo a mano; `pipeline.Logging` deja de añadirlo a mano |
| 2 | **Contratos** `pkg/trace` y `pkg/metrics` con `Noop` y `traceparent`; reglas de `archtest` | `archtest` en verde con las reglas nuevas; tests de `traceparent` (válido, versión desconocida, todo ceros, mayúsculas, longitud) |
| 3 | **Implementaciones sin dependencias** `trace/vanilla` y `metrics/vanilla` | Un test lee la salida de `/metrics` y encuentra un contador y un histograma; un test recoge spans padre e hijo en memoria; `git diff go.mod go.sum` vacío |
| 4 | **Entrada HTTP**: `distribution.Observe`, causa de los 5xx, `correlationId` en `ProblemDetails`, validación de la cabecera, envoltorio con `Flush`/`Unwrap` | Test de extremo a extremo en `examples/parties`: una petición deja **una** línea (ruta como patrón, estado, duración, `correlation_id`, `trace_id`), **una** observación en el histograma y **un** span de servidor; con `traceparent` continúa la traza; un 500 forzado deja la causa; una respuesta en flujo atraviesa el middleware; una cabecera de 200 caracteres no rompe la escritura |
| 5 | **Casos de uso**: `pipeline.Observed` | El span del caso de uso es hijo del de HTTP; la etiqueta de resultado sigue la taxonomía (un test por clase de error) |
| 6 | **SQL**: `sqlrepo.WithTelemetry` | Sobre SQLite, un alta deja la cadena HTTP → caso de uso → transacción → `INSERT`; un test comprueba que ningún atributo lleva argumentos; `sqlconformance` y `repotest` siguen en verde |
| 7 | **Outbox, relay y consumidor** | `integration_events_test.go` comprueba los contadores; el consumidor que falla una vez suma un fallo y deja una línea con su correlación; la antigüedad del pendiente más viejo sube si el relay no corre y vuelve a cero cuando entrega |
| 8 | *(si se aprueba D6)* **`traceparent` en el outbox y en el sobre**, con su migración | Una sola traza desde la petición hasta el consumidor; `sqlconformance` del outbox pasa en los cinco motores; las filas sin valor se siguen entregando |
| 9 | *(cuando haya destino)* **Adaptador de OpenTelemetry** en el módulo `otel/` | Con un colector en Docker, la petición del paso 4 se ve como traza en un visor; el `go.mod` raíz no cambia; `archtest` prohíbe el import en `pkg/` |
| 10 | **Documentación**: fila de [INVENTARIO-PATRONES.md](INVENTARIO-PATRONES.md), tablas de [ARQUITECTURA.md](ARQUITECTURA.md) §6 y §7, árbol del README | La fila «Observabilidad» pasa de ❌ a ✅ con lo pendiente anotado |

Los pasos 1 a 7 son el mínimo para producción. El 8 y el 9 son independientes entre sí.

## Estado de la implementación en Go

Rama `feat/observabilidad-minima`, desde `feat/ddd-contracts-agnostic-persistence` (`9491750`).

| # | Paso | Estado | Dónde | Prueba que lo demuestra |
|---|---|---|---|---|
| 1 | El log lee del contexto | ✅ | `pkg/log/vanilla` (manejador que añade `correlation_id`, `causation_id`, `trace_id`, `span_id`, `actor`); `pkg/log/default.go` (`log.Default`, `log.Discard`) | `log/vanilla/context_test.go`; `pipeline.Logging` ya no añade la correlación a mano (`TestLogging_TakesTheCorrelationFromTheContext`) |
| 2 | Contratos `pkg/trace` y `pkg/metrics`; reglas de `archtest` | ✅ | `pkg/trace/trace.go`, `pkg/metrics/metrics.go`; reglas 8 y 9 de `archtest_test.go` | `trace_test.go` (15 casos de `traceparent`); `archtest` en verde |
| 3 | Implementaciones sin dependencias | ✅ | `pkg/trace/vanilla` (`Tracer`, `Recorder`, `LogExporter`), `pkg/metrics/vanilla` (`Registry`, `Handler`, `RegisterRuntime`) | `metrics/vanilla/vanilla_test.go` lee `/metrics`; `trace/vanilla/vanilla_test.go` recoge padre e hijo; `git diff go.mod go.sum` vacío |
| 4 | Entrada HTTP | ✅ | `pkg/distribution/observe.go` (`Observe`, `RecordError`, `ValidCorrelationID`), `response.go`, `middleware.go` | `distribution/observe_test.go`; `examples/parties/observability_test.go`; `e2e/observability_test.go` |
| 5 | Casos de uso | ✅ | `pkg/application/pipeline/observed.go`, `pkg/application/outcome.go` | `pipeline/observed_test.go` (un caso por clase de error) |
| 6 | SQL | ✅ | `pkg/persistence/sqlrepo/telemetry.go`, `db.go` | `sqlrepo/sqlite/telemetry_test.go` (cadena, sin argumentos, conformidad con la telemetría encendida) |
| 7 | Outbox, relay y consumidor | ✅ | `pkg/application/outbox/outbox.go`, `pkg/application/messaging/telemetry.go` | `outbox/relay_log_test.go`; `TestObservability_OutboxRelayAndConsumers` |
| 8 | `traceparent` en el outbox | pendiente (D6) | — | — |
| 9 | Adaptador de OpenTelemetry | pendiente (D3) | — | — |
| 10 | Documentación | parcial | este documento, `INVENTARIO-PATRONES.md`, `README.md`, `BACKLOG.md` | falta `ARQUITECTURA.md` §6 y §7 |

**Prueba de cierre.** `e2e/observability_test.go` compone el contexto Geography real sobre SQLite
detrás de la cadena de un servicio (`Recovery`, `Correlation`, `Observe`, `Authorize`) y comprueba
que una petición HTTP deja **una** línea JSON, **una** observación de
`http.server.request.duration` (visible en `/metrics`, servido en otro puerto) y **un** span de
servidor, los tres con la misma correlación y el mismo `trace_id`; que un 401 deja línea sin
causa; y que un 500 (la base pierde su esquema en caliente) deja su causa (`no such table`) en la
línea y en el span, mientras el cliente solo recibe el `correlationId`.

### Composición

```go
logger := logvanilla.ForService("parties", version, env)   // JSON en stdout
reg := metricsvanilla.NewRegistry()
metricsvanilla.RegisterRuntime(reg)
tracer := tracevanilla.New(tracevanilla.WithExporter(tracevanilla.LogExporter(logger)))

db := postgres.Open(sqlDB, sqlrepo.WithName("parties"), sqlrepo.WithTelemetry(tracer, reg))

api := http.NewServeMux()
module.RegisterRoutes(api)
public := distribution.Chain(api,
	distribution.Recovery(logger),
	distribution.Correlation(),
	distribution.Observe(logger, tracer, reg, distribution.ObserveRoutes(api), distribution.ObserveQuietly("/healthz", "/readyz")),
	distribution.Authorize(authn, resolver),
)
internal := http.NewServeMux()            // puerto interno, nunca el de la API
internal.Handle("GET /metrics", reg.Handler())

relay := messaging.NewRelay(source, integrationOutbox, sender,
	outbox.WithRelayLogger(logger), outbox.WithRelayTelemetry(tracer, reg))
consumer := messaging.NewConsumer("crm", inbox, db).WithTelemetry(logger, tracer, reg)
```

Sin nada de esto, el framework se comporta como antes, con una excepción buscada: el relay
escribe sus fallos en `log.Default()` (el `slog.Default` del proceso) aunque no se le pase logger.

### Nombres (los mismos en C#)

| Uso | Nombre |
|---|---|
| Mensaje de la línea de petición | `http request` |
| Campos de la línea | `method`, `route` (el patrón, nunca la ruta), `status`, `duration_ms`, `correlation_id`, `trace_id`, `span_id`, `actor` (solo el identificador), y en los 5xx `error` y `error_type` |
| Campos fijos del proceso | `service`, `version`, `env` |
| Otras líneas | `causation_id`; `request` y `outcome` (caso de uso); `outbox`, `message_id`, `event_type`, `attempt` (relay); `consumer` (consumidor) |
| Métricas | las de la tabla «Métricas mínimas», con las etiquetas `method`, `route`, `status`, `request`, `outcome`, `engine`, `operation`, `state`, `event_type`, `consumer`, `outbox` |
| Resultados (`outcome`) | `ok`, `validation`, `rule`, `not_found`, `conflict`, `unauthorized`, `forbidden`, `error`; en el consumidor, además, `duplicate` e `ignored` |
| Ruta sin patrón, método no estándar | `unmatched`, `_OTHER` |
| Error devuelto al cliente | `correlationId` en el `ProblemDetails` |

La fuente única en Go son las constantes de `pkg/metrics/metrics.go`, `pkg/distribution/observe.go`
y `pkg/application/outcome.go`.

### En qué se aparta el código del esbozo

- **`metrics.Meter.Gauge` admite etiquetas constantes** (`labels ...string`): sin ellas no se puede
  publicar el pool por estado ni la antigüedad por outbox.
- **`trace.Span` tiene `SetName`**: el nombre del span de servidor (`GET /parties/{id}`) solo se
  conoce al terminar. Y hay `trace.WithNewRoot`, que usan el relay y el consumidor (etapa 1 de D6).
- **La ruta se resuelve por tres vías**, en este orden: `distribution.ObserveRoutes(mux)` (pregunta
  al `ServeMux` antes de servir; es la única que cubre a un handler que responde con `WriteJSON`
  detrás de otro middleware y a los rechazos de `Authorize`), la ficha de la petición (la rellenan
  `WriteError` y `Respond`) y el patrón que deja el propio mux. Si ninguna la da, la etiqueta es
  `unmatched`: nunca la ruta cruda.
- **La línea del caso de uso** va en `Error` solo si el fallo es inesperado (`outcome=error`); un
  rechazo que la taxonomía explica va en `Debug`, porque la línea de la petición ya lo cuenta con
  su estado.
- **Todos los spans van al log en `Debug`** (`tracevanilla.LogExporter`), no solo los de SQL: con
  el destino en el log, el span de servidor duplicaría la línea de petición. Es D8 aplicada a
  todos los tramos.
- **`karpo.outbox.oldest_pending_age` pregunta al almacén** cada vez que se leen las métricas
  (`Pending(1)`), en vez de recordar el último lote: así crece también cuando el relay no corre,
  que es el caso que la alarma debe cubrir. Vale `-1` si el almacén no responde.
- **`db.client.connections`** lleva además la etiqueta `db` (el nombre del backend) y los estados
  `open`, `in_use`, `idle` y `waited` (acumulado de esperas).
- **La correlación válida** es de 1 a 64 caracteres entre letras, dígitos y `-_.:`; el valor que no
  cumple se sustituye por uno nuevo y no se devuelve ni se registra.
- **`Recovery` anota la causa del pánico** y devuelve el `correlationId`; `RequestLogging` se
  conserva con el envoltorio arreglado (`Flush`, `Unwrap`), aunque se recomienda `Observe`.
- **Los contextos no se han tocado** (D7): el span del caso de uso aparece cuando cada contexto
  añada `pipeline.Observed` a su `chain`. El ejemplo `examples/parties` lo hace con
  `Service.Observe`.

### Comprobado al implementar

- **La cabecera de correlación larga** (defecto «deducido»): con la validación, una cabecera de
  200 caracteres ya no llega al outbox (`TestObservability_AnOversizedCorrelationDoesNotBreakTheWrite`,
  sobre SQLite). No se ha reproducido el fallo original en un motor con `VARCHAR(64)`: SQLite no
  limita la longitud.
- **Coste en rendimiento**: `BenchmarkAppend` (una transacción con un `INSERT` sobre SQLite en
  disco) da entre 7 y 31 ms por operación con y sin telemetría, con más dispersión entre
  repeticiones que diferencia entre variantes. La prueba está dominada por la escritura a disco y
  no resuelve el coste de la instrumentación; falta una medida sin E/S.

## Estado de la implementación en C#

Karpo, rama `feat/observabilidad-minima` (parte de `claude/karpo-observabilidad-minima-yoma1a`,
que se había hecho sobre una evaluación reconstruida, y la alinea con los nombres de este
documento). Fw **1.10.0, sin publicar**: la rama se rehízo el 2026-10-07 encima de la línea que
ya tenía 1.7.0, 1.8.0 y 1.9.0 (ADR 0010), porque el 1.7.0 que declaraba al principio ya estaba
ocupado en el feed. El detalle está junto al artefacto, en
`020-Back/020-Source/Paranoia.Karpo.Fw.Infrastructure.Observability/FwObservability.md`.

| Mínimo | C# |
|---|---|
| Una línea JSON por petición en stdout | `FwRequestTelemetryMiddleware` (lo monta `UseFwExceptionHandling`, por fuera) + `FwJsonLineFormatter` (consola JSON por defecto). Mismos campos: `method`, `route`, `status`, `duration_ms`, `correlation_id`, `trace_id`, `span_id`, `actor`, `service`, `env` |
| Causa de todo 5xx con su correlación | `FwRequestTelemetry.RecordError` desde el middleware de excepciones (`error`, `error_type`); la correlación se abre por fuera, así que la línea de error ya no la pierde |
| Duración y resultado por ruta | `http.server.request.duration` (s) con `method`, `route`, `status`, en el medidor `Paranoia.Karpo.Fw`; la métrica homónima de ASP.NET Core no se recoge (otras etiquetas) |
| Métricas básicas | además `karpo.outbox.relayed` (`event_type`, `outcome`) y `karpo.outbox.oldest_pending_age` (`outbox`) |
| Registro que sirva al worker | `AddFwObservability(IHostApplicationBuilder, …)`, usado por `Outbox.Worker` |
| Correlación validada | `FwCorrelationId`: la misma regla, 1 a 64 caracteres |

Lo que sigue siendo distinto, a propósito o pendiente: C# no expone `/metrics` (sus métricas
salen por OTLP cuando hay colector); sus trazas son las de la instrumentación de ASP.NET Core; no
tiene telemetría de casos de uso, de base de datos ni de consumidores; la antigüedad del
pendiente más viejo la calcula el drenador en cada ciclo, no preguntando al almacén; y cada línea
lleva además los campos de proceso de Serilog (`SourceContext`, `MachineName`, `ProcessId`,
`ThreadId`).

## Decisiones

**Aprobadas por Javier el 2026-10-06, las ocho, tal como se recomiendan.**

Cada una con la recomendación y un ejemplo.

**D1. El registro estructurado se queda en `pkg/log`.**
Recomendación: **sí, sin cambiar su firma.** Solo cambia la implementación, para que lea del
contexto. Sustituirlo por `slog` directo obligaría a tocar `archtest` y los cuatro puntos que lo
usan a cambio de nada.
*Ejemplo:* hoy `logger.WithContext(ctx).Info("pedido creado")` escribe solo el mensaje. Después
escribe `{"msg":"pedido creado","correlation_id":"7f3a…","trace_id":"4bf9…","actor":"0192…"}` sin
que quien llama haga nada.

**D2. Contrato propio para trazas y métricas, en vez de usar la API de OpenTelemetry como contrato.**
OpenTelemetry es el estándar abierto para emitir trazas y métricas; tiene una «API» ligera y un
«SDK» pesado. Recomendación: **contrato propio y pequeño** (dos interfaces por paquete). Es la
regla del repositorio —el núcleo no importa terceros— y la que ya se aplicó al log, la caché y los
dialectos SQL.
*Ejemplo:* `sqlrepo` llamará a `tracer.Start(ctx, "db INSERT")`. Si `Tracer` fuera el de
OpenTelemetry, el paquete de persistencia importaría una biblioteca externa y `archtest` (regla 5)
fallaría.

**D3. El SDK de OpenTelemetry no entra en el `go.mod` del framework; va en un módulo aparte y en un paso posterior.**
Recomendación: **módulo `otel/` con su propio `go.mod`**, igual que `integration/` aísla los
drivers de base de datos; y hacerlo cuando exista un sitio al que enviar (paso 9). El dato que lo
respalda: el C# tiene el exportador cableado desde abril y hoy no está encendido en ninguno de
sus 15 procesos. Lo que falta no es el exportador.
*Ejemplo:* un contexto que solo usa el framework sigue compilando con dos dependencias. Theros,
que sí quiere ver trazas en un visor, importa además `karpo-fw-go/otel` en su `main`.
*Antes del paso 9 hay que comprobar la licencia* (ver «Sin verificar»).

**D4. Primer destino: salida estándar en JSON y una página `/metrics` en formato Prometheus.**
Prometheus es el formato de texto que casi cualquier plataforma sabe leer periódicamente; OTLP es
el protocolo de OpenTelemetry para *enviar* a un colector. Recomendación: **stdout + `/metrics`
primero; OTLP después.** No añade dependencias ni exige desplegar nada, y es lo que un servicio
gestionado recoge sin configuración.
*Ejemplo:* `docker logs theros` muestra una línea JSON por petición; `curl` a la página de métricas
devuelve `http_server_request_duration_seconds_count{route="/projects/{id}",status="200"} 1812`.
La página de métricas no se publica en el puerto de la API: va en un puerto interno.

**D5. Qué es «mínimo» para producción.**
Recomendación: **los pasos 1 a 7**, es decir:
(a) una línea JSON por petición, con correlación y traza;
(b) la causa de todo 5xx registrada, y la correlación devuelta al cliente en el error;
(c) duración y resultado por ruta, por caso de uso y por operación de base de datos;
(d) el outbox vigilado: entregas, fallos y antigüedad del pendiente más viejo;
(e) un span por petición, por caso de uso y por sentencia SQL, enlazados;
(f) nada personal ni secreto en ninguno de los tres.
Con la implementación sin dependencias, «la traza» son las líneas que comparten `trace_id`; verla
como un diagrama de tramos en un visor llega con el paso 9. Recomiendo aceptar eso para la entrega
G1 de Theros y exigir el visor antes de G4 (cuando se vende).
*Ejemplo:* un cliente dice «me dio error al guardar». Con el `correlationId` que venía en la
respuesta se busca en los logs y sale la línea del 500, su causa y las sentencias que se ejecutaron.

**D6. La traza cruza el outbox más adelante, no ahora.**
Para que la petición y lo que ocurre después en segundo plano salgan en *una sola* traza hay que
guardar el `traceparent` junto al mensaje: una columna nueva en las tablas de outbox de los
contextos, en los cinco motores. Recomendación: **dejarlo como paso 8** y, mientras, enlazar por
la correlación, que ya viaja.
*Ejemplo:* un alta en Parties publica `party-registered.v1` y el CRM lo consume 200 ms después.
Sin la columna salen dos trazas con la misma `correlation_id`; con ella, una traza con los dos
tramos.

**D7. Se instrumenta el framework; los contextos, no.**
Recomendación: **solo el framework.** Los 16 contextos usan `sqlrepo` y `distribution`, y 15 usan
el orquestador: reciben HTTP, SQL, outbox y consumo sin tocar una línea. Lo único que no llega
solo es el span del caso de uso, porque cada contexto arma su tubería en una función local
(`chain`, p. ej. `contexts/parties/application/service.go:467`; 18 llamadas a `Chain` en 17
ficheros). Es una línea por contexto, y propongo hacerla en una tanda aparte, cuando no haya otras
sesiones trabajando sobre `contexts/`.
*Ejemplo:* tras el paso 6, una petición a Facturación ya muestra HTTP → transacción → `INSERT` sin
cambiar Facturación. El tramo «`CreateInvoice`» en medio aparece cuando su `chain` añada
`pipeline.Observed`.

**D8. Se registra el 100 % de las trazas, con un interruptor.**
Muestrear es guardar solo una parte de las trazas para ahorrar. Recomendación: **todo al
principio**, con una opción de porcentaje, y los spans de SQL en nivel `Debug` mientras el destino
sea el log (para no multiplicar las líneas).
*Ejemplo:* con 20 peticiones por segundo y 6 sentencias por petición, el nivel `Info` produce 20
líneas por segundo; con SQL en `Info` serían 140.

## Sin verificar

- **Dependencias que arrastra el SDK de OpenTelemetry para Go.** No se han medido: en esta máquina
  no hay copia de sus módulos y no se ha descargado nada. Hay que medirlo al empezar el paso 9
  (`go mod graph` en el módulo `otel/`).
- **Licencia.** El framework es GPL-2.0 (`LICENSE`, README) y no he encontrado ninguna mención
  «or later» (búsqueda en `*.go`, `*.md` y `LICENSE`). OpenTelemetry se distribuye, hasta donde
  sé, bajo Apache-2.0, que la FSF considera compatible con la GPL versión 3 pero no con la
  versión 2. No lo he contrastado en esta sesión ni me corresponde resolverlo: hay que aclararlo
  antes del paso 9, y enlaza con la decisión FG13 de Theros. Un módulo aparte aísla la dependencia,
  pero no decide por sí solo qué pasa con el binario que los junta.
- **Los dos defectos marcados como «deducido»** (la línea de error sin correlación en C#; la
  cabecera de correlación larga en Go) salen de leer el código. No se han reproducido. El de Go
  queda cerrado por la validación (ver «Comprobado al implementar»).
- **Rama paralela.** Existe en la remota `claude/karpo-observabilidad-minima-yoma1a`, hecha en una
  sesión en la nube sobre una evaluación reconstruida (paquete único `pkg/observability`, otros
  nombres de métrica, correlación de hasta 128 caracteres, sin SQL ni consumidor). No se ha
  fusionado: esta implementación sigue el diseño aprobado aquí.
- **Que Serilog añada el identificador de traza a cada línea** del fichero CLEF: es lo que dice su
  documentación para estas versiones; no se ha ejecutado.
- **El claim `sid`** de los tokens: no se ha comprobado si el login lo emite.
- **Paradigma**: solo `origin/main`. Las ~1.400 entradas sin confirmar de su árbol de trabajo no se
  han mirado más allá de lo ya comprobado (que el proyecto de observabilidad coincide con la remota).
- **Coste en rendimiento** de la instrumentación: no medido. El paso 6 debería incluir una
  comparación con y sin telemetría sobre SQLite.
