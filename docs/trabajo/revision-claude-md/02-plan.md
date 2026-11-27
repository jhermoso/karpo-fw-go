## Plan: revision-claude-md (vuelta 6: resuelve los dos hallazgos del juez del plan)

**Cambios respecto a la vuelta 5:**
- Las referencias a «Ramas y PR» se cambian por la sección real: «Reglas fijas», «Git», «Fusionar» (`CLAUDE.md:100-104`).
- C5 pasa a ser sólo un puntero.
- §7 incluye una comprobación de las secciones citadas.

**Encabezados reales de `CLAUDE.md`, comprobados con grep:** Qué es · Sesiones y proyectos hermanos · Forma de trabajo · Reglas fijas (Git, Nombres, Datos, Código, Documentación y decisiones) · Comprobar · Cómo informar a Javier. Dentro de «Git» hay una viñeta en negrita, **Fusionar** (l. 100). Dentro de «Comprobar» hay otra, **Integración** (l. 168).

**Dónde se trabaja:** carpeta `C:/Git/Paranoia/karpo-fw-go-roles`, rama `feat/forma-de-trabajo`.

**Límites:**
- No se hace commit ni push.
- No cambian §5 de `docs/FORMA-DE-TRABAJO.md` ni las reglas fijas.
- El nombre real de Trench no aparece en el repositorio.

### 1. Contextos afectados
No se toca ningún contexto: sólo documentación y configuración de agentes.

**Ficheros que se tocan:**
- `CLAUDE.md`
- `README.md` (el índice de la l. 24)
- `docs/FORMA-DE-TRABAJO.md`: §1, §7 y §8. §5 nunca.
- `.claude/agents/juez-construccion.md`, `constructor.md`, `planificador.md` y `asesor.md`
- `front/react/README.md`

**Ficheros que no se tocan:**
- `cinco-roles.js`
- `pkg/CLAUDE.md`, `contexts/CLAUDE.md`, `docs/CLAUDE.md` e `integration/CLAUDE.md`

### 2. Artefactos y patrones
No hay contratos, migraciones ni rutas.

**Patrón obligatorio: una sola fuente (DRY).** Cada cosa se escribe en un solo sitio y los demás ficheros sólo remiten a él, sin repetir la regla:

| Qué | Dónde vive |
|---|---|
| Criterios | §5 |
| Comando de mutación | §8 |
| Regla de fusión (D-139) | `CLAUDE.md`, «Reglas fijas», «Git», «Fusionar» |
| Regla de integración | `CLAUDE.md`, «Comprobar», «Integración» |

- Ningún fichero dice que prevalece sobre otro.
- `CLAUDE.md` es breve y está en español.

### 3. Librerías
- **gremlins v0.6.0**: es la última etiqueta y tiene licencia Apache-2.0. Se lanza con `go run …@v0.6.0`, así que `go.mod` no cambia.
- **go-mutesting**: se descarta porque no tiene mantenimiento.
- **StrykerJS**: no está instalado en `front/react` (comprobado: ni en `package.json` ni con `git ls-files`). Según §8, entra como dependencia de desarrollo en la primera tarea de front que mida la mutación; hasta entonces la mutación del front figura como «no ejecutada», no como aprobada.

### 4. Consecuencias en tres niveles
**C1. La sección de `CLAUDE.md` pasa a ser un puntero**
- Nivel 1: desaparece el resumen desfasado.
- Nivel 2: para ver los criterios hay que abrir `FORMA-DE-TRABAJO.md`.
- Nivel 3: las dos versiones de los criterios ya no pueden separarse.

**C2. Se añade `carpeta` al JSON de §7**
- Nivel 1: el ejemplo funciona tal cual.
- Nivel 2: el worktree tiene que existir antes de lanzar el workflow.
- Nivel 3: no se pisa el checkout principal.

**C3. El comando de mutación va en §8**
- Nivel 1: los jueces pueden ejecutarlo.
- Nivel 2: no está probado en Windows con Go 1.27. Si falla, la mutación queda «no ejecutada».
- Nivel 3: la primera tarea de implementación lo prueba con `--help` y `--dry-run`.

**C5. La integración en el juez es un puntero puro**
- Nivel 1: no queda ninguna copia de la regla.
- Nivel 2: si cambia `CLAUDE.md:168`, el juez aplica el texto nuevo sin tocar nada más.
- Nivel 3: si Javier aprueba la decisión 1, sólo cambia §5.

**C6. La tarea pequeña se lanza a mano**
- Nivel 1: el script no cambia.
- Nivel 2: el orquestador encadena los dos agentes.
- Nivel 3: si se repite mucho, se añade un modo al script.

**C8. Enlace en el índice de `README.md`**
- Nivel 1: el documento se encuentra desde el índice.
- Nivel 2: hay un enlace relativo más que comprobar.

### 5. Diseño detallado: el texto que se escribe

**C1. `CLAUDE.md`, sección «Forma de trabajo» (l. 54-81)**

Se sustituye entera por esto:
```
## Forma de trabajo

- Tareas medianas y grandes: workflow `cinco-roles` (analista, planificador, constructor,
  cada uno con su juez, y el asesor). Tarea pequeña: `constructor` y después
  `juez-construccion`, lanzados a mano.
- Cada rol tiene como mucho tres vueltas con su juez; después decide Javier.
- El juez de construcción es la revisión independiente de D-139 (ver «Reglas fijas», «Git»,
  «Fusionar»).
- Roles en `.claude/agents/`; detalle, criterios (§5) y cómo se lanza en
  `[docs/FORMA-DE-TRABAJO.md](docs/FORMA-DE-TRABAJO.md)`. Los criterios de §5 sólo los cambia Javier.
```
Las l. 100-104 no se tocan, porque son la fuente de la regla de D-139.

**C2. §7 de `FORMA-DE-TRABAJO.md`**

Se añade `"carpeta": "<ruta absoluta del worktree>"` al JSON, y debajo este texto:
> `carpeta` es un worktree ya creado según `CLAUDE.md`, «Reglas fijas», «Git», «Empezar una tarea»; el workflow no lo crea ni comprueba que exista. El workflow devuelve `desviacionesParaJavier` y `parada`; el orquestador redacta para Javier esas desviaciones, la parada y las consultas al asesor.

**C3. §8 de `FORMA-DE-TRABAJO.md`**

Se sustituye «`go run …@versión`» por este texto:
> Mutación del dominio, desde la raíz del módulo y nunca sobre `./...`:
> `go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --workers 4 --timeout-coefficient 10 ./contexts/<nombre>/domain` (opcional `--diff origin/main`; los nombres de las opciones se confirman con `unleash --help`).
> - LIVED y NOT COVERED del dominio tocado cuentan como supervivientes (§5): se explican uno a uno o impiden aprobar.
> - TIMED OUT: se repite con un coeficiente mayor y `--workers 1`; si persiste, no impide aprobar, pero se enumera en la entrega, un mutante por línea.
> - Si v0.6.0 no arranca, se anota y la mutación queda «no ejecutada», nunca «aprobada».

**C3b. Los tres agentes que hablan de mutación**

Afecta a `juez-construccion.md:13`, `constructor.md:16` y `planificador.md:28`:
- Cualquier detalle sobre cómo lanzar la mutación se sustituye por «según §8 de `docs/FORMA-DE-TRABAJO.md`».
- Se quita cualquier porcentaje de mutación, porque §5 no lo fija.

**C5. `juez-construccion.md`**

Se añade una línea que sólo remite, sin repetir la regla:
> Integración: la exige `CLAUDE.md`, «Comprobar», «Integración»; aplícala tal como dice allí.

**C6. §1 de `FORMA-DE-TRABAJO.md`**

Se añade esta frase:
> Tarea pequeña: se lanza a mano con Agent, primero `constructor` y después `juez-construccion`, con el mismo tope de tres vueltas.

**C7. Modelo del asesor**
- En `asesor.md` se pone `model: claude-fable-5-2`. Es un ID confirmado.
- `cinco-roles.js` sigue con el alias `fable`.
- En §8 se anota: «El asesor usa `claude-fable-5-2` en el frontmatter y `fable` en el script.»

**C4. Front React**
- En la tabla de `CLAUDE.md`, junto a la l. 18, se añade esta fila:
  > `front/react/` | Front React (vitest con cobertura). Su jurado anterior (`front/react/docs/jurado/`) queda sustituido por `docs/FORMA-DE-TRABAJO.md`
- En `front/react/README.md:404` se añade esta nota:
  > Nota: este jurado es histórico; hoy rige `[docs/FORMA-DE-TRABAJO.md](../../docs/FORMA-DE-TRABAJO.md)`.

**C8. `README.md:24`**

Tras «Decisiones comunes…» se añade:
  > `` · Forma de trabajo: [docs/FORMA-DE-TRABAJO.md](docs/FORMA-DE-TRABAJO.md)``

### 6. Reparto
- El constructor corre con el modelo de la sesión.
- Delega en **un solo subagente Sonnet, esfuerzo medio**, que trabaja en secuencia. Son cambios de texto ya redactados y los ficheros se cruzan, así que no hace falta paralelo.
- Orden de trabajo:
  0. Guardar la copia de §5.
  1. C2, C3, C6 y la nota de C7 en `FORMA-DE-TRABAJO.md`.
  2. C3b y C5 en los agentes, y C7 en `asesor.md`.
  3. C1 y C4 en `CLAUDE.md`.
  4. C8 en `README.md`.
  5. C4 en `front/react/README.md`.
  6. Las comprobaciones.
- El juez de construcción trabaja con esfuerzo alto.

### 7. Comprobaciones

**Antes de editar.** Guardar §5, desde `## 5.` hasta antes de `## 6.`, en `<scratchpad>/s5-antes.txt`. El fichero está sin seguimiento, así que `git diff` no serviría.

**Después de editar**
1. Extraer §5 de nuevo con los mismos encabezados y compararla con `diff` contra la copia. Tiene que salir vacío.
2. Con grep sobre los ficheros tocados, comprobar que no quedan «@versión», «prevalece» ni «Ramas y PR», y que «80 % de cobertura en los paquetes tocados» está escrito así. Que no aparece el nombre real de Trench lo comprueba el hook al escribir (`pwsh -NoProfile -File .claude/hooks/guard.tests.ps1`).
3. Con grep, comprobar que `carpeta` aparece en §7 y en `cinco-roles.js`, y que «§8» aparece en los tres agentes.
4. **Secciones citadas.** Con grep, sacar todo lo citado entre comillas angulares en los ficheros tocados: `grep -o '«[^»]*»'`. Cada nombre que se refiera a `CLAUDE.md` tiene que existir allí como encabezado (`^#+ `) o como viñeta en negrita (`- \*\*<nombre>`). Hay que revisar por lo menos:
   - Reglas fijas
   - Git
   - Fusionar
   - Empezar una tarea
   - Comprobar
   - Integración
   - Forma de trabajo

   Las citas que no son nombres de sección se descartan a mano, por ejemplo los estados de gremlins o «no ejecutada».
5. Comprobar con `ls` que existen todas las rutas citadas, incluidos los enlaces relativos de los dos README.
6. Ejecutar `node --check .claude/workflows/cinco-roles.js`.

**Gremlins.** Si hay permiso para descargar código de terceros, ejecutar `go run …/gremlins@v0.6.0 unleash --dry-run ./examples/parties`. Si no lo hay, queda «no ejecutado».

Como no se toca código, no hace falta mutación ni `go build`.

### 8. Decisiones para Javier

**1. ¿Qué exige el juez de construcción sobre la integración?**
- Recomiendo que §5 diga lo mismo que `CLAUDE.md`, «Comprobar», «Integración»:
  - siempre, al juzgar para fusionar (D-139);
  - en un carril, sólo si toca persistencia, esquema o el anfitrión.
- Hoy §5 dice «esquema o consultas». Cambiarlo es cambiar §5, por eso la decisión es tuya.

**2. ¿Puede un juez devolver el trabajo a un rol anterior?**
- Sería con un campo `origen` en el veredicto, como el `routeBackTo` de Trench.
- Recomiendo que no, por ahora.

**3. Diferencia de fusión con Trench**
- Allí la fusión la hace la persona; aquí la hace el orquestador, según D-139.
- Propongo anotarla como diferencia aceptada en `docs/DECISIONES-COMUNES.md`. La anota el orquestador, no esta tarea.

### 9. Lo no comprobado
- Que gremlins v0.6.0 funcione con Go 1.27 en Windows.
- Los nombres exactos de `--workers` y `--timeout-coefficient`.
- El valor de `MAX_REBOTES` en Trench.


### 10. Correcciones del árbitro (orquestador, tras la tercera vuelta)

Hallazgos de la tercera vuelta del juez-plan, comprobados en el repositorio y resueltos así:

- **Nombre real de Trench en §7:** quitado. La comprobación se apoya en el hook.
- **StrykerJS:** no está instalado; corregidos §3 y la fila de C4.
- **`docs/trabajo/` en `docs/CLAUDE.md`:** el orquestador ya ha añadido el puntero. Las otras
  tres `CLAUDE.md` (`pkg/`, `contexts/` e `integration/`) se han revisado: no hablan de roles ni
  de jueces, así que no se contradicen.
- **Cadena de §7:** corregida a la real.
- **Tarea pequeña (C6):** `docs/FORMA-DE-TRABAJO.md` §7 ya dice que se usa el mismo encargo que el
  script («lee primero `<carpeta>/.claude/agents/<rol>.md`»).

Javier ha decidido las tres preguntas de §8:

1. **Integración:** §5 remite a `CLAUDE.md`, «Comprobar», «Integración». Ya está escrito.
2. **Devolver a un rol anterior:** sí, siempre con directrices del asesor. Ya están escritos §3,
   punto 5, el campo `origen` y el script.
3. **La diferencia de fusión con Trench:** la anota el orquestador en el registro.

Además, el orquestador ya ha aplicado, siguiendo decisiones de Javier, cambios que este plan
proponía:

- en §7: `carpeta`, cómo se pasa el script y el arbitraje;
- en §8: el comando de gremlins;
- el enlace de `README.md`;
- en `CLAUDE.md`: el arbitraje, la devolución y la cadena del 80 %.

El constructor **comprueba** que esos cambios están, sin duplicarlos, y aplica el resto de C1 a C7
sobre el estado actual de los ficheros.
