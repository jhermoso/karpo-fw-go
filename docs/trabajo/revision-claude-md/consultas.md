## analista

- ¿Es 'effort' una clave admitida en el frontmatter de los agentes de .claude/agents/ en Claude Code, y 'fable' un alias de modelo válido tanto ahí como en la opción model de agent() del runtime de workflows?
- Para el juez de construcción: ¿qué comando y versión estable de gremlins (go-gremlins/gremlins) recomiendas ejecutar con 'go run …@versión' sin añadirlo a ningún go.mod, y funciona en Windows sin compilador C?
- En un CLAUDE.md raíz que enlaza a un documento de procedimiento, ¿es mejor práctica dejar sólo el puntero y el criterio de cuándo aplicarlo (eliminando el resumen de criterios de aprobación) o mantener un resumen corto? Recomendación concreta para una sesión nueva.

ASESOR — tarea «revision-claude-md». Respuestas a las tres consultas del analista.

AVISO PREVIO: no pude ejecutar gremlins. La orden `go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 --version` fue denegada por el clasificador de permisos de la sesión y no la reintenté por otra vía. Lo que digo de gremlins en la consulta 2 es, salvo la lista de versiones, conocimiento previo sin comprobar hoy.

## 1. `effort` en el frontmatter y `fable` como alias

Recomendación: dejar `model: fable` y `effort: medium|high` como están, y que el constructor lo compruebe una vez con `/agents` antes de darlo por bueno.

Comprobado:
- `agent()` del runtime de workflows admite `model: 'fable'` y `effort: 'medium'`: esta misma ejecución la lanzó `.claude/workflows/cinco-roles.js:54` con esas opciones y el modelo que responde es Fable 5.2 (`claude-fable-5-2`).

Sin comprobar (lo sé de memoria, no lo he contrastado con la documentación hoy):
- `effort` es clave admitida en el frontmatter de subagentes de Claude Code (valores `low`, `medium`, `high`, y `max` según modelo); si falta, hereda el de la sesión.
- `model` en el frontmatter admite un alias, un identificador completo o `inherit`. Los alias que recuerdo documentados son `sonnet`, `opus` y `haiku`; que `fable` lo sea también en el frontmatter es probable porque lo es en el runtime, pero no lo he visto escrito.

Si `/agents` no muestra el asesor con Fable, la alternativa segura es el identificador completo `claude-fable-5-2` en `asesor.md` y en `cinco-roles.js:54`. Coste: deja de seguir «el modelo más avanzado disponible» (§6 de FORMA-DE-TRABAJO.md) y habría que cambiarlo a mano con cada modelo nuevo; por eso prefiero el alias si funciona.

## 2. Gremlins para el juez de construcción

Recomendación: fijar la versión v0.6.0 y lanzarlo desde la raíz del módulo, sólo sobre el dominio tocado:

```
go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash ./contexts/<nombre>/domain
```

Comprobado:
- v0.6.0 es la última versión publicada (`go list -m -versions`: v0.1.0 … v0.5.1, v0.6.0).
- `go run paquete@versión` no toca ningún `go.mod`: es el comportamiento del propio Go, y coincide con §8.
- La máquina tiene Go 1.27.0 en windows/amd64.

Sin comprobar:
- Que funcione en Windows sin compilador C. Mi conocimiento es que gremlins es Go puro (cobra, viper, `go/ast`), sin cgo, y que publica binarios para Windows; no lo he ejecutado aquí.
- Que v0.6.0 compile con Go 1.27.
- Las opciones: recuerdo `--dry-run`, `--tags`, `--workers`, `--timeout-coefficient`, `--threshold-efficacy` y `--output` (JSON). Que el constructor las confirme con `… gremlins@v0.6.0 unleash --help` antes de escribirlas en un documento.

Cosas a tener en cuenta (conocimiento previo):
- Es una versión 0.x: «estable» significa aquí «versión fija», no API estable. Hay que escribir la versión exacta en §8 y en `juez-construccion.md`, no `@latest`.
- Sólo muta el código que cubren las pruebas y lanza las pruebas una vez por mutante: es lento, sobre todo en Windows. Limitarlo al paquete de dominio tocado, nunca `./...`.
- Trabaja sobre un módulo: si el repositorio tiene varios `go.mod`, se lanza en la raíz de cada uno.
- Los mutantes «NOT COVERED» no son supervivientes; el criterio de §5 («un mutante superviviente sin explicar impide aprobar») se aplica a los «LIVED».

Hallazgo para la revisión: hoy ningún fichero dice el comando ni la versión. §8 dice «`go run …@versión`» con puntos suspensivos, y `juez-construccion.md` y `constructor.md` sólo dicen «pruebas de mutación». Una sesión nueva no puede ejecutarlo sin adivinar. Propongo poner el comando completo en §8 (un solo sitio) y que los agentes remitan a él. Esto no cambia el criterio de §5.

## 3. Puntero o resumen en el CLAUDE.md raíz

Recomendación: puntero y criterio de cuándo aplicarlo, sin el resumen de criterios ni la lista de roles.

Razones:
- `CLAUDE.md` se carga en todas las sesiones; cada línea cuesta en todas. La lista de roles y los cinco criterios ocupan unas veinte líneas (`CLAUDE.md` 60-76) que ya están en §2 y §5 de `docs/FORMA-DE-TRABAJO.md`.
- Los criterios de §5 los fijó Javier. Una copia resumida es una segunda fuente que puede desviarse: la tarea pide «sin duplicados» y prohíbe cambiar §5, y un resumen es justo donde se cambia sin querer (ya hay matiz perdido: el resumen dice «80 % en lo tocado» y §5 dice «en los paquetes tocados»).
- Quien aplica los criterios son los jueces, y sus ficheros ya mandan leer §5. La sesión nueva sólo necesita saber cuándo arrancar el arnés y dónde está.

Qué debe quedar en `CLAUDE.md` (lo que cambia la conducta de una sesión que no abra el documento):
1. Cuándo una tarea es mediana o grande, y que entonces va con `.claude/workflows/cinco-roles.js`.
2. Que la pequeña va sólo con constructor y juez de construcción.
3. Tres vueltas como mucho; si no aprueba, decide Javier.
4. Que el juez de construcción es la revisión independiente de D-139.
5. Dónde está lo demás, y que los criterios de §5 no se cambian sin Javier.

Texto propuesto (es opinión; el constructor ajusta la redacción):

```
## Forma de trabajo: roles, jueces y asesor

Una tarea **mediana o grande** se lleva, en backend y en front, con el arnés
`.claude/workflows/cinco-roles.js`: analista, planificador y constructor, cada uno con su juez,
y un asesor al que consultan todos. Es mediana o grande si toca más de un contexto, cambia el
esquema, abre una pantalla o una pieza de `pkg/`, o su diseño admite más de una salida. Una
tarea **pequeña** va sólo con constructor y juez de construcción.

- Como mucho **tres vueltas** por juez; si no aprueba, decide Javier.
- El juez de construcción es la revisión independiente de D-139.
- Los roles están en `.claude/agents/`; el procedimiento y los criterios de aprobación (§5, los
  fijó Javier el 2026-11-27 y no se cambian sin él), en `docs/FORMA-DE-TRABAJO.md`.
```

Decisión que anotaría para Javier: él pidió «documenta esta forma de trabajar en el CLAUDE.md». Quitar el resumen de criterios cumple eso con un puntero, pero es un cambio visible en lo que él ve al abrir el fichero. Es reversible; yo lo aplicaría y se lo contaría en el informe.

Ficheros citados:
- C:/Git/Paranoia/karpo-fw-go-roles/CLAUDE.md (líneas 54-80)
- C:/Git/Paranoia/karpo-fw-go-roles/docs/FORMA-DE-TRABAJO.md (§5 líneas 86-98, §8 líneas 141-150)
- C:/Git/Paranoia/karpo-fw-go-roles/.claude/agents/asesor.md, juez-construccion.md, constructor.md
- C:/Git/Paranoia/karpo-fw-go-roles/.claude/workflows/cinco-roles.js (líneas 54, 61, 65, 125)

## analista

- ¿Hay alguna opción de `gremlins unleash` v0.6.0 que limite el trabajo a un paquete y que conviene fijar en §8 (por ejemplo, tiempo de espera o número de procesos) para Windows?

ASESOR — tarea «revision-claude-md», consulta 1 del analista (gremlins v0.6.0 en §8)

RECOMENDACIÓN
Sí. Conviene fijar en §8 de `C:/Git/Paranoia/karpo-fw-go-roles/docs/FORMA-DE-TRABAJO.md` el comando completo, no sólo «`go run …@versión`». Propuesta de texto:

  go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --workers 4 --timeout-coefficient 10 ./contexts/<nombre>/domain

- El límite a un paquete lo da el argumento posicional `[path]` (uno como máximo), no una opción.
- `--workers` y `--timeout-coefficient` son las dos opciones que merece la pena fijar para Windows.
- Los valores 4 y 10 son mi punto de partida, no un dato medido: el constructor debe ejecutarlo una vez sobre un paquete de dominio real y ajustar antes de dejarlos escritos (o escribirlos como «valores iniciales»).

LO QUE SÉ CON SEGURIDAD (comprobado hoy)
Fuentes: `cmd/unleash.go` de la etiqueta v0.6.0 (raw.githubusercontent.com/go-gremlins/gremlins/v0.6.0/cmd/unleash.go) y la página de la orden en gremlins.dev.
- Uso: `unleash [path]`, con `cobra.MaximumNArgs(1)`. Sin argumento, usa el directorio actual. No existe una opción `--package` ni similar.
- Opciones que existen en v0.6.0: `--workers` (int, 0 = por defecto), `--test-cpu` (int, 0 = sin fijar), `--timeout-coefficient` (int, 0 = por defecto), `--integration/-i`, `--tags/-t`, `--coverpkg`, `--diff/-D`, `--exclude-files/-E`, `--dry-run/-d`, `--output/-o`, `--output-statuses/-S`, `--threshold-efficacy`, `--threshold-mcover`, más una por tipo de mutante.
- Por defecto usa todos los núcleos como procesos de trabajo (la mitad en modo integración). El tiempo de espera de cada mutante es el tiempo de la pasada de cobertura multiplicado por el coeficiente.
- La documentación no menciona Windows: no hay opción específica.
- En esta máquina gremlins no está instalado ni descargado (en la caché de módulos sólo están `v0.6.0.info` y `.mod`). No lo he ejecutado: descargar y ejecutar código no es tarea del asesor. Por tanto nada de lo anterior está probado aquí.

MI OPINIÓN (sin verificar en este repositorio)
- Con `[path]` apuntando a una carpeta, creo que recorre esa carpeta y sus subcarpetas (`./...` desde ahí), no un único paquete estricto. Para el dominio de un contexto eso es lo deseado; si se quisiera excluir subcarpetas, `--exclude-files` con una expresión regular.
- Por qué fijar las dos opciones en Windows: gremlins copia el módulo a una carpeta temporal por cada proceso de trabajo y compila en cada mutante. En Windows (antivirus, disco) eso es lento y variable; con todos los núcleos y el coeficiente por defecto, un paquete de dominio cuya pasada de cobertura dura poco tiende a dar mutantes «TIMED OUT», que no son ni muertos ni supervivientes y ensucian el veredicto del juez.
- `--diff origin/main` es útil para acotar a lo tocado en la rama y encaja con «sobre el dominio tocado» de §5; lo dejaría como opción mencionada, no fijada, porque depende de la rama base de cada tarea.
- No fijaría `--threshold-efficacy`: el criterio de §5 es «ningún superviviente sin explicar», no un porcentaje; poner un umbral sería cambiar un criterio de Javier.
- No usaría `--integration`: el dominio es puro y sus pruebas viven en su paquete.
- `--test-cpu` no hace falta si se limita `--workers`.

DOS OBSERVACIONES PARA EL PLANIFICADOR
1. §8 escribe hoy `go run …@versión` con puntos suspensivos. La regla de la tarea es «comandos que existen»: debe figurar la ruta real `github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`.
2. Conviene una frase en §8 (no en §5) que diga cómo trata el juez un mutante «TIMED OUT» o «NOT COVERED». Propongo: se repite con más coeficiente y, si persiste, cuenta como superviviente sin explicar. Es una interpretación del criterio de §5: si el planificador ve que lo modifica en vez de aclararlo, va como decisión para Javier.

COMPROBACIÓN PARA EL CONSTRUCTOR (antes de dejar el texto)
  go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --dry-run ./examples/parties

Debería listar mutantes sólo de esa carpeta, sin ejecutar pruebas. Confirma la ruta del módulo, la versión y el alcance del argumento. Descarga código de terceros: si la sesión no tiene ese permiso, se anota como «no ejecutado».

## analista

- ¿Admite el runtime de subagentes el alias de modelo 'fable' y el campo 'effort' en el frontmatter de .claude/agents/*.md, o hay que usar 'claude-fable-5-2'?
- ¿Conviene que el juez de construcción pueda devolver el trabajo al planificador o al analista (como routeBackTo en Trench), contando esa vuelta en el tope de tres, o eso rompe el bucle por rol que pidió Javier?

## Respuestas del asesor (tarea «revision-claude-md»)

### 1. ¿Alias `fable` y campo `effort` en el frontmatter de `.claude/agents/*.md`?

**Recomendación:** dejar `effort` como está y dejar `model: fable` sólo en `asesor.md`, sin cambiarlo en esta tarea. Antes de darlo por bueno, comprobar el alias una vez en una sesión real; si falla, usar `claude-fable-5-2`.

**Comprobado:**
- `effort` en el frontmatter se usa en los agentes de los plugins instalados en esta máquina (`~/.claude/plugins`: `effort: low|medium|high|xhigh`, 20 apariciones). Claude Code instalado: 2.1.332.
- El arnés **no lee el frontmatter**. `cinco-roles.js` pasa el fichero del rol como texto (`rolDe`) y fija modelo y esfuerzo en las opciones de `agent()`: `{model:'fable', effort:'medium'}` para el asesor y `effort:'high'` para los jueces. El frontmatter sólo cuenta si alguien lanza el rol con la herramienta Agent.
- Esta consulta la lanzó el arnés con `model: 'fable'` y la contesta `claude-fable-5-2`. Eso apunta a que el alias vale en `agent()`, pero no lo prueba: no distingo si se resolvió el alias o se heredó el modelo de la sesión.

**Sin comprobar:** que `model: fable` valga como alias en el frontmatter. En los agentes instalados sólo aparecen `sonnet`, `opus`, `haiku` e `inherit`. El identificador completo `claude-fable-5-2` vale seguro.

**Opinión:** el alias encaja mejor con «el modelo más avanzado disponible» de §6, porque no hay que tocarlo al cambiar de versión. El identificador completo es más seguro pero fija la versión. Comprobación de un minuto: lanzar el agente `asesor` con la herramienta Agent desde una sesión que no sea Fable y preguntarle su modelo. Si la tarea no puede hacerla, que quede anotada como «no comprobado» en §8 y no como hecho.

### 2. ¿Puede el juez de construcción devolver el trabajo al planificador o al analista?

**Recomendación:** no, no dentro del arnés. Cada juez devuelve sólo a su rol. Y en esta tarea no se añade nada: es una revisión de documentación.

**Por qué (opinión razonada):**
- Javier pidió «un juez experto específico para cada rol, en su propio loop». Una vuelta atrás entre fases convierte tres bucles en un grafo, y el tope de tres vueltas deja de estar claro: no se sabe de qué bucle cuenta ni si el plan rehecho vuelve a pasar por `juez-plan`.
- El plan ya lo aprobó su juez. Si la construcción descubre que el plan está mal, es un fallo del juez de plan o un hecho nuevo, y eso es justo lo que §3 y §7 mandan a Javier como parada.
- Regla fija de sencillez: el script actual es lineal y se lee de un tirón.

**Si se quiere algo más adelante (decisión para Javier, no para esta tarea):** un campo opcional en el veredicto, por ejemplo `origen: construccion|plan|analisis`. Si el juez atribuye el fallo al plan, el arnés para en esa vuelta, sin gastar las tres, e informa. El orquestador relanza desde la fase que toque. Eso cambia §3 («para a la tercera»), así que lo decide él.

### Incoherencias vistas al leer, para el analista

No las he verificado más allá de la lectura.

- **`carpeta` falta en §7.** El JSON de ejemplo de `docs/FORMA-DE-TRABAJO.md` §7 no la lleva, y el script la exige (`throw` si falta) y la nombra en su `whenToUse`.
- **La tarea pequeña no tiene camino en el arnés.** §1 y `CLAUDE.md` dicen «sólo constructor y juez de construcción», pero `cinco-roles.js` siempre empieza por el análisis. Hay que decir que se lanza a mano con la herramienta Agent, o darle un modo al script.
- **El script no entrega a Javier las consultas ni el resumen del desacuerdo.** Guarda `desviacionesParaJavier` y `parada`, y nada más; lo redacta el orquestador. Conviene decirlo en §7.
- **Dos números de motores en el contexto de sesión.** El `CLAUDE.md` del worktree `musing-ramanujan` dice «tres motores»; el de `karpo-fw-go-roles` dice seis. Rige el de la carpeta de la tarea. No es un defecto de esta rama.

Ficheros leídos:
- `C:/Git/Paranoia/karpo-fw-go-roles/CLAUDE.md`
- `C:/Git/Paranoia/karpo-fw-go-roles/docs/FORMA-DE-TRABAJO.md`
- `C:/Git/Paranoia/karpo-fw-go-roles/.claude/agents/*.md`
- `C:/Git/Paranoia/karpo-fw-go-roles/.claude/workflows/cinco-roles.js`

## planificador

- ¿El alias 'fable' en el frontmatter model de .claude/agents/asesor.md lo resuelve Claude Code al lanzar el agente con la herramienta Agent, o hay que poner el identificador completo (claude-fable-5-2)?
- ¿Es v0.6.0 la versión estable vigente de gremlins y funciona con Go 1.27 en Windows? Si no, ¿qué versión fijar en §8?
- Para mutantes TIMED OUT, ¿es razonable repetir con un --timeout-coefficient mayor, o es mejor excluirlos del porcentaje que exige el juez?

Respuestas del asesor (tarea «revision-claude-md»). Aviso previo: no he podido ejecutar gremlins (el clasificador de permisos denegó `go run …gremlins@v0.6.0`), así que lo que depende de ejecutarlo va marcado como no comprobado.

## 1. Alias `fable` en `.claude/agents/asesor.md`

Recomendación: dejar `model: fable`; no hace falta el identificador completo.

- Comprobado: esta misma consulta la lanzó `.claude/workflows/cinco-roles.js` (línea 54, `model: 'fable', effort: 'medium'`) y la sesión resultante es `claude-fable-5-2`. El alias se resuelve en el camino del workflow.
- No comprobado: que el frontmatter `model:` de un agente lanzado con la herramienta Agent resuelva el mismo alias. Lo documentado para ese campo son alias (`sonnet`, `opus`, `haiku`), `inherit` o un identificador completo; `fable` debería seguir la misma tabla de alias, pero no lo he visto ejecutarse por esa vía.
- Opinión: el alias es mejor que `claude-fable-5-2` porque §5/§6 dice «el modelo más avanzado disponible, hoy Fable 5.2»; un identificador fijo caduca. Si el constructor quiere cerrarlo, basta una prueba de un minuto: lanzar `asesor` con Agent y preguntarle su model ID. Si fallara, el identificador exacto es `claude-fable-5-2`.
- Coherencia: `asesor.md`, FORMA-DE-TRABAJO.md (línea 28 y §8) y `cinco-roles.js` dicen lo mismo (`fable`, medio). No hay nada que corregir aquí.

## 2. Versión de gremlins

Recomendación: fijar `v0.6.0` en §8, con el comando completo: `go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash ./ruta/del/paquete`.

- Comprobado: `go list -m -versions github.com/go-gremlins/gremlins` devuelve `… v0.5.0 v0.5.1 v0.6.0`; v0.6.0 es la última etiqueta publicada. El repositorio usa `go 1.27.0` (go.mod) y la máquina tiene `go1.27.0 windows/amd64`.
- No comprobado: que v0.6.0 funcione con Go 1.27 en Windows. Sigue siendo 0.x (sin garantía de estabilidad de API ni de flags). Gremlins publica binarios para Windows y no depende de internos del compilador (muta el AST y lanza `go test`), así que es probable que funcione, pero es opinión, no prueba.
- Sugerencia para §8 (decisión técnica, no toca §5): escribir hoy `…@versión` es ambiguo para una sesión nueva; conviene poner la versión y el comando literal, y añadir una frase: «si v0.6.0 no arranca con el Go del repositorio, el juez lo anota y la mutación de esa tarea queda como no ejecutada, no como aprobada». La primera tarea de tipo implementación debe confirmar la versión con `… --version` y un `unleash --dry-run` sobre un paquete pequeño de `pkg/domain`.

## 3. Mutantes TIMED OUT

Recomendación: las dos cosas, en este orden. Primero repetir; lo que siga agotando el tiempo no cuenta como superviviente, pero se lista.

1. Repetir sólo los paquetes afectados con `--timeout-coefficient` mayor (por ejemplo el doble o el triple del valor por defecto) y menos paralelismo (`--workers 1` o 2). En Windows el arranque de procesos es lento y muchos TIMED OUT son ruido de carga, no del mutante.
2. Los que persistan suelen ser bucles que el mutante hace infinitos (condición de salida alterada): la prueba no termina, luego el cambio sí se detecta. No son LIVED. Gremlins tampoco los cuenta en su «efficacy» (muertos / (muertos + vivos)).
3. El juez los trata así: TIMED OUT persistente no impide aprobar, pero la entrega los enumera con una línea de explicación cada uno. LIVED sin explicar sí impide aprobar (es lo que dice §5, línea 94-95). NOT COVERED no es mutación sino cobertura: lo cubre el criterio del 80 %.

Seguro: los nombres de estado (KILLED, LIVED, NOT COVERED, TIMED OUT, NOT VIABLE) y que existe `--timeout-coefficient` en `unleash`. No comprobado en v0.6.0: el valor por defecto del coeficiente y el nombre exacto de `--workers` (verificar con `unleash --help`).

Nota sobre el alcance: §5 no exige un porcentaje de mutación, exige «ningún superviviente sin explicar». La consulta habla de «porcentaje que exige el juez»; si algún fichero revisado (agente `juez-construccion.md` o el workflow) introduce un umbral porcentual, contradice §5 y debe quitarse. La regla de los TIMED OUT es una aclaración técnica que cabe en §8 sin cambiar el criterio de Javier; si el planificador prefiere que conste en §5, eso es decisión para Javier.

Ficheros consultados: C:/Git/Paranoia/karpo-fw-go-roles/.claude/agents/asesor.md, C:/Git/Paranoia/karpo-fw-go-roles/docs/FORMA-DE-TRABAJO.md (§5 líneas 88-100, §8 líneas 141-150), C:/Git/Paranoia/karpo-fw-go-roles/.claude/workflows/cinco-roles.js (líneas 54-65, 125), C:/Git/Paranoia/karpo-fw-go-roles/go.mod.
