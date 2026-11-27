## Análisis: revision-claude-md (vuelta 4)

Esta vuelta corrige los dos hallazgos del juez de la vuelta anterior: faltaba Trench y faltaba CLAUDE.md:168. Además recoge lo que señaló el asesor.

### 1. Situación de partida (hechos comprobados, raíz `C:/Git/Paranoia/karpo-fw-go-roles`)

**Duplicado entre `CLAUDE.md` y §5**
- La sección «Forma de trabajo» de `CLAUDE.md` (líneas 54-81) repite la lista de roles y resume §5 de `docs/FORMA-DE-TRABAJO.md`.
- El resumen ya no coincide con §5: dice «80 % en lo tocado», y §5 dice «en los paquetes tocados».

**§7: falta `carpeta`**
- El JSON de §7 (líneas 133-135) no lleva `carpeta`.
- `cinco-roles.js:16-17` lanza un error si falta, y la línea 4 la nombra. Si se sigue el ejemplo tal cual, el workflow falla.

**§8: no hay comando de mutación**
- §8 dice «`go run …@versión`», sin comando.
- `juez-construccion.md:13`, `constructor.md:16` y `planificador.md:28` hablan de mutación, pero ninguno da el comando.

**`CLAUDE.md:18` no menciona el front React**
- Sólo describe `front/` como Angular.
- Existen `front/react/` (con `vitest.config.ts`), su jurado anterior en `front/react/docs/jurado/ronda-1..3.md` y `PLAN.md` («Papel: planificador»).
- `front/react/README.md:404` sigue remitiendo a ese jurado.

**Integración: dos reglas que no encajan**
- `CLAUDE.md:168`, apartado «Integración», ya distingue dos casos:
  - Para fusionar es «obligatoria antes de fusionar, D-139».
  - Para un carril, sólo «si toca persistencia, esquema o el anfitrión».
- §5, líneas 88-89, dice que el juez de construcción la exige «si cambian el esquema o las consultas». Se parece a la regla del carril, pero es más estrecha: deja fuera el anfitrión y la persistencia en general.
- `CLAUDE.md:100-104` dice que el juez de construcción es la revisión independiente de D-139, que exige la integración siempre.
- La contradicción real: un mismo juez cumple dos papeles, la revisión de fusión y la de un carril, y §5 sólo le da una regla.

**La tarea pequeña no tiene camino en el workflow**
- §1 y `CLAUDE.md` dicen que la tarea pequeña va sólo con constructor y juez.
- `cinco-roles.js` empieza siempre por el análisis.

**El workflow no redacta lo que se entrega a Javier**
- `cinco-roles.js` devuelve `desviacionesParaJavier` y `parada`.
- Las consultas al asesor y el resumen del desacuerdo los redacta el orquestador, y §7 no lo dice.

**Lo que está bien**
- Los `CLAUDE.md` de `pkg/`, `contexts/`, `docs/` e `integration/` no hablan de roles, jueces, cobertura, mutación ni D-139. La única coincidencia del `grep` es `contexts/CLAUDE.md:9-10`, sobre los eventos de integración entre contextos, que es otro asunto. No hay duplicados ni contradicciones con ellos.
- El asesor está declarado con `model: fable` y esfuerzo medio en `asesor.md` y en `cinco-roles.js:54`. Según el asesor, el workflow no lee el frontmatter de los agentes: pasa modelo y esfuerzo en las opciones de `agent()`.

### 2. Proyectos hermanos y decisiones comunes

**Karpo C#.** `.claude/` sólo tiene `worktrees`. No hay nada equivalente.

**Trench, comprobado en esta vuelta.** He leído `.claude/commands/drive-orchestration.md` y `doc/_orchestration/_templates/harness-sdlc.workflow.template.js`.

*Lo que coincide con lo nuestro:*
- Un orquestador dirige y no implementa.
- Hay roles separados con jueces que devuelven el trabajo.
- Hay un tope de rebotes; al agotarlo, decide la persona (`MAX_REBOTES` frente a nuestras tres vueltas).
- Un worktree por agente que modifica código.
- No se hace push ni se fusiona dentro del workflow.
- La memoria de trabajo se guarda en disco: `sessions/<fecha>-<slug>/STATUS.md` frente a nuestro `docs/trabajo/<tarea>/`.

*Lo que difiere:*
- **(a) Fusión.** Trench tiene una puerta G2: antes de fusionar pide confirmación explícita a la persona, y la fusión final a `main` la hace la persona. Nosotros seguimos D-139: el orquestador fusiona sus PR con la batería local verde y la revisión independiente, y los carriles nunca fusionan. Es una diferencia deliberada entre proyectos, no un error nuestro.
- **(b) Diseño.** Trench tiene otra puerta, G1, sólo para cambios de dominio pesados (su Carril C). Nosotros no tenemos puerta de diseño: el juez del plan aprueba y las decisiones de producto suben a Javier como mucho de tres en tres.
- **(c) Por complejidad.** Trench trabaja en tres carriles:
  - El A se salta el planificador.
  - El B pasa por planificador y arquitecto.
  - El C, además, por un panel de diseño y un revisor adversarial.

  Nosotros sólo distinguimos «tarea pequeña» de «mediana o grande», y la pequeña no tiene modo en el script.
- **(d) A quién se devuelve el trabajo.** En Trench el juez puede devolver a un rol anterior (`routeBackTo`: developer, planner, architect, infra o human). Nosotros devolvemos sólo al propio rol.
- **(e) Prueba de humo.** Trench hace una por ola, tras consolidar las ramas, y además exige no esperar en segundo plano (`NO_BACKGROUND_WAIT`). Nosotros no tenemos consolidación de ramas en el script.

*Qué hacer con cada diferencia:*
- **(a)** Debe quedar escrita en `docs/DECISIONES-COMUNES.md` como diferencia aceptada, junto a D-139. Así nadie la «corrige» copiando a Trench. El registro lo lleva el orquestador; esta tarea sólo lo anota.
- **(d)** Coincide con la propuesta del asesor de un campo `origen`. Es un cambio de §3, así que va a Javier.
- **(c)** Se resuelve con la corrección 6 de abajo.
- **(b) y (e)** No se adoptan en esta tarea.
- No se escribe el nombre real de Trench en el repositorio.

### 3. Fuentes
- **DRY**, de Hunt y Thomas en *The Pragmatic Programmer*: los criterios deben vivir sólo en §5.
- **«Manage Claude's memory»**, en la documentación de Claude Code: `CLAUDE.md` debe ser breve y llevar el detalle en ficheros referenciados.
- **La referencia de Go Modules sobre `go run pkg@version`**: permite ejecutar una herramienta sin tocar `go.mod`.
- **gremlins v0.6.0 (`unleash [path]`)**: lo citó el asesor; yo no lo he leído.
- **El workflow de Trench**: es el precedente interno del tope de rebotes y de las puertas humanas.

### 4. Correcciones propuestas
1. **`CLAUDE.md`, sección «Forma de trabajo».** Sustituirla por un puntero breve que diga:
   - Las tareas medianas y grandes van con `cinco-roles`; la pequeña, con constructor y juez de construcción.
   - Como mucho tres vueltas; después decide Javier.
   - El juez de construcción es la revisión de D-139.
   - El detalle está en `.claude/agents/` y en `docs/FORMA-DE-TRABAJO.md`, y §5 no se cambia sin Javier.
2. **§7.** Añadir al JSON `"carpeta": "<ruta absoluta del worktree>"` y una frase: el orquestador redacta para Javier las desviaciones, la parada y las consultas.
3. **§8, comando de mutación.** Escribirlo una sola vez, en §8: `go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --workers 4 --timeout-coefficient 10 ./contexts/<nombre>/domain`.
   - Se lanza desde la raíz del módulo y nunca sobre `./...`.
   - `--diff origin/main` es opcional.
   - Los mutantes «TIMED OUT» y «NOT COVERED» se repiten con un coeficiente mayor.

   Los tres agentes que hablan de mutación remiten a §8.
4. **Front.** Añadir a `CLAUDE.md` una fila para `front/react/`: front React con vitest y StrykerJS; su jurado anterior queda sustituido por `docs/FORMA-DE-TRABAJO.md`. Añadir también una nota en `front/react/README.md:404` que diga que ese jurado es histórico.
5. **Integración en §5.** No se toca §5. La contradicción sube a Javier (decisión 1).
   - Mientras tanto, `juez-construccion.md` cita `CLAUDE.md:168` tal como está: es una regla fija vigente, no un criterio nuevo.
   - Si el juez lo considera un cambio de criterio, se queda fuera y espera a Javier.
6. **Tarea pequeña.** En §7 se dice que se lanza a mano con la herramienta Agent: constructor y después `juez-construccion`. No se cambia el script.
7. **Modelo del asesor.** Se deja `model: fable`. La comprobación es lanzar `asesor` con la herramienta Agent desde una sesión que no sea Fable. Si falla, se cambia a `claude-fable-5-2` en `asesor.md` y en `cinco-roles.js:54`. Hasta entonces, §8 lo anota como «no comprobado».
8. **Comprobar gremlins antes de dejar §8.** Lanzar `go run …/gremlins@v0.6.0 unleash --dry-run ./examples/parties` si hay permiso para descargar código de terceros. Si no lo hay, se anota como «no ejecutado».

### 5. Hechos, opiniones y lo no comprobado
- **Hechos:** lo de los §1 y §2, comprobado leyendo los ficheros y con `grep`, incluidos los dos ficheros de Trench y `CLAUDE.md:160-175`.
- **Opiniones:**
  - Prefiero un puntero a un resumen en `CLAUDE.md`.
  - Prefiero que la tarea pequeña se lance a mano y no con un modo nuevo del script.
  - Propongo una forma de tratar «TIMED OUT» y «NOT COVERED».
  - Creo que la diferencia de fusión con Trench va al registro de decisiones comunes.
- **Sin comprobar:**
  - Que v0.6.0 sea la última versión de gremlins.
  - Que gremlins funcione con Go 1.27 en Windows.
  - Que el alias `fable` valga en el frontmatter.
  - Que el `MAX_REBOTES` de Trench valga 3: no he leído el número.

### 6. Decisiones para Javier
1. **¿Qué exige el juez de construcción sobre la integración?** Hoy sirve para dos cosas:
   - **Cuando juzga para fusionar** (revisión de D-139): la integración completa siempre, como dice `CLAUDE.md:168`.
   - **Cuando juzga un carril:** la integración sólo si toca persistencia, esquema o el anfitrión, como dice `CLAUDE.md:168`. No «esquema o consultas», como dice §5.

   Recomiendo que §5 diga las dos cosas con esas mismas palabras. Cambia §5, por eso es tuya.
2. **¿Puede un juez devolver el trabajo a un rol anterior?** Por ejemplo, el juez de construcción al planificador, como hace el `routeBackTo` de Trench, con un campo `origen` en el veredicto. Cambia el «para a la tercera» de §3. Recomiendo no hacerlo por ahora: si el plan resulta estar mal, se para y decides tú.
3. **Diferencia de fusión con Trench.** Allí la fusión la confirma y la hace la persona; aquí la hace el orquestador (D-139). Propongo anotarla como diferencia aceptada en `docs/DECISIONES-COMUNES.md`.
