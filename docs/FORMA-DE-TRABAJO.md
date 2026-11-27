# Forma de trabajo: roles, jueces y asesor

Cómo se lleva una tarea mediana o grande, en backend o en front, de la forma más autónoma
posible. La pidió Javier el 2026-11-27. Sustituye al «bucle de cuatro agentes» que usaba el front
React (analista, planificador, constructor y un jurado que sólo juzgaba la construcción) y lo
amplía: **cada rol tiene su propio juez** y hay un **asesor** al que todos pueden consultar.

El arnés que lo ejecuta es el workflow `.claude/workflows/cinco-roles.js`. Los roles son agentes
de `.claude/agents/`.

## 1. Cuándo se usa

| Tarea | Cómo se lleva |
|---|---|
| **Mediana o grande:** toca más de un contexto, cambia el esquema, abre una pantalla nueva o una pieza de `pkg/`, o su diseño tiene más de una salida razonable | Los cinco roles completos, con el arnés |
| **Pequeña:** un filtro, una fila de documentación, un arreglo localizado | Sólo constructor y juez de construcción (la revisión independiente de D-139), lanzados como dice §7 |

Si hay duda, se cuenta como mediana.

## 2. Los roles

| Rol | Agente | Qué entrega | Su juez |
|---|---|---|---|
| **Analista** | `analista` | La situación de partida (qué hay hoy en el código y en los documentos, qué dicen Karpo C# y Trench) y las fuentes, patrones y ejemplos más aceptados o de prestigio reconocido para el objetivo, cada uno con su referencia | `juez-analisis` |
| **Planificador** | `planificador` | Un plan escrito que prevé las consecuencias con **al menos tres niveles de profundidad**: (1) los contextos delimitados afectados; (2) los artefactos necesarios y los patrones de obligado cumplimiento; (3) las librerías de terceros más adecuadas por coste, complejidad y funcionalidad. Incluye el reparto en tareas, el modelo y el esfuerzo de cada una, y el diseño detallado | `juez-plan` |
| **Constructor** | `constructor` | El plan ejecutado: divide el trabajo, lo delega en subagentes con el modelo que diga el plan y deja la rama lista para fusionar | `juez-construccion` (y `juez-ux` si hay front) |
| **Juez** | `juez-*` | Un veredicto por vuelta: aprobado o no, con hallazgos concretos y lo que falta para aprobar | — |
| **Asesor** | `asesor` (modelo `fable`, esfuerzo medio) | Respuestas a las consultas de cualquier otro rol, jueces incluidos | — |

## 3. El bucle de cada rol

Cada rol trabaja en su propio bucle con su juez:

1. El rol entrega su trabajo y, si las tiene, una lista de **consultas al asesor**.
2. Si hay consultas, las contesta el asesor y el rol rehace su entrega con las respuestas.
3. El juez lee la entrega **sin las conclusiones del autor sobre su propia calidad** y da un
   veredicto. El juez también puede consultar al asesor.
4. Si no aprueba, el rol rehace la entrega con los hallazgos. **Como mucho tres vueltas**.
   - **Desde el segundo suspenso**, antes de rehacer, el asesor lee los hallazgos y da
     directrices al rol.
   - **Si a la tercera vuelta sigue sin aprobar, arbitra el orquestador.** Lo que se pueda
     comprobar en el repositorio lo resuelve él con lo que diga la realidad. Si un punto le
     ofrece dudas, consulta antes al asesor. Corrige la entrega y le da al juez **una vuelta
     más**.
   - Si tampoco aprueba en esa vuelta, o si la discrepancia es de producto o irreversible,
     decide Javier, que recibe el desacuerdo resumido.
5. **Devolver a un rol anterior.** Un juez puede indicar en su veredicto (campo `origen`) que el
   fallo nace en un rol anterior, por ejemplo un plan que no se puede construir. Entonces el
   asesor da directrices para corregirlo, y el bucle de ese rol se repite con los hallazgos y las
   directrices. Después vuelven a ejecutarse los roles que lo siguen. No se devuelve sólo el
   trabajo, sino también las directrices. Como mucho, una devolución por tarea; la segunda para y
   decide Javier.

El orden es analista → planificador → constructor. Cada rol empieza con la entrega aprobada del
anterior.

## 4. Memoria de trabajo

Cada tarea escribe en `docs/trabajo/<tarea>/`:

- `01-analisis.md`, `02-plan.md` y `03-construccion.md`: la entrega aprobada de cada rol;
- `veredictos.md`: cada veredicto de cada vuelta;
- `consultas.md`: cada consulta al asesor y su respuesta.

El plan aprobado **es** la memoria de diseño: se escribe para que otra sesión pueda ejecutarlo
sin esta conversación. El repositorio es público: en estas notas rigen las mismas reglas de
nombres que en el resto (Trench, FinancialDetail).

## 5. Criterios de aprobación de cada juez

Los fijó Javier el 2026-11-27.

### Juez de análisis

- La situación de partida está comprobada en el código, no supuesta: rutas de fichero, rutas
  HTTP y tablas citadas que existen.
- Cada patrón o fuente lleva su referencia (libro, documento o proyecto reconocido) y dice por
  qué encaja aquí.
- Se ha mirado lo que hacen Karpo C# y Trench para lo mismo, y las decisiones comunes que aplican
  (`docs/DECISIONES-COMUNES.md`).
- Separa lo que es un hecho de lo que es una opinión, y nombra lo que no ha podido comprobar.

### Juez de plan

- Los tres niveles están completos: contextos afectados, artefactos y patrones obligatorios, y
  librerías con su comparación de coste, complejidad y funcionalidad (y su licencia).
- **El plan usa los contratos de DDD del framework:** agregados, identidades, eventos,
  especificaciones, vocabulario y rasgos de `pkg/domain`, y CQRS, permisos, idempotencia y
  transacción de `pkg/application`, o sus equivalentes en Karpo C#. Si se aparta de ellos, lo
  justifica, y la única justificación válida es que seguirlos añade complejidad innecesaria.
- Cada tarea del reparto dice qué modelo y qué esfuerzo usa, y por qué. El plan más barato que
  cumple es el mejor.
- Ninguna dependencia nueva en un `go.mod` sin el visto bueno de Javier: si el plan la necesita,
  la deja como decisión para él.
- El diseño basta para construir sin preguntar.

### Juez de construcción (backend y front)

- **Cada cambio de comportamiento tiene su prueba**, también el caso negativo.
- La batería de «Comprobar» de `CLAUDE.md` está en verde. La integración en los motores se
  exige como dice `CLAUDE.md`, en «Comprobar», apartado «Integración».
- **Cobertura mínima del 80 %** en los paquetes tocados (`go test -cover`; en el front, la de
  vitest). Si un paquete tocado ya estaba por debajo antes de la tarea, basta con que el código
  nuevo esté cubierto y la cifra del paquete no baje; el déficit anterior se anota en `BACKLOG.md`
  (Javier, 2026-11-27).
- **Pruebas de mutación** sobre el dominio tocado. Una prueba de mutación cambia el código a
  propósito, por ejemplo un `<` por un `<=`, y comprueba que alguna prueba falla. Se miden con
  gremlins en Go y con StrykerJS en el front, con los comandos de §8. Un mutante que ninguna
  prueba alcanza («NOT COVERED») también cuenta como superviviente. Un mutante vivo en código anterior a la
  tarea, del que el cambio no depende, cuenta como explicado si se lista en `BACKLOG.md` con su
  fichero, su línea y su tipo, y se dice con qué herramienta se midió (Javier, 2026-11-27). Un mutante superviviente sin explicar impide
  aprobar.
- **La implementación usa los contratos y patrones de DDD del framework**, como en el juez de
  plan. Cada desviación se justifica por complejidad innecesaria y se informa a Javier, para que
  decida si es un caso excepcional o un patrón nuevo que generalizar.
- La revisión de corrección no deja hallazgos abiertos.
- La documentación del contexto y, si toca, `BACKLOG.md` cambian en la misma rama.

### Juez de UX (front)

- **Primero la funcionalidad, la sencillez de uso y la velocidad de respuesta.** Ninguna otra
  consideración pasa por delante.
- Después, **WCAG 2.2 AAA donde sea posible** (WCAG son las pautas de accesibilidad del W3C; AAA
  es su nivel más alto, por ejemplo un contraste de 7:1). Cuando AAA estorba a la sencillez o a la
  velocidad, basta AA, y se dice dónde.
- Lo comprueba con axe y con el teclado. Respeta el presupuesto de tamaño, se ve bien en claro,
  oscuro y RTL, y está completo en español y en inglés.

### Criterios por tipo de tarea

| Tipo | Qué añade el juez de construcción |
|---|---|
| Implementación | Lo de arriba |
| Testing | Las pruebas nuevas matan mutantes que antes sobrevivían; no se aceptan pruebas que nada puede hacer fallar |
| Diseño de front o UX | El juez de UX, con capturas revisadas |
| DevOps | El cambio se puede deshacer, no deja secretos en código ni en configuración, y se ha probado sobre bases desechables |
| Documentación | Ningún enlace relativo roto; responde a quien la va a leer |

## 6. El asesor

- Lo ejecuta el modelo más avanzado disponible: hoy **Fable 5.2, con esfuerzo medio**.
- Está a disposición de todos los roles, jueces incluidos. Responde consultas concretas, no hace
  el trabajo de otro rol.
- Cada respuesta separa lo que sabe con seguridad de lo que es su opinión, y da referencias.

## 7. Cómo se lanza

Desde la sesión de orquestación o desde un carril, se ejecuta el workflow `cinco-roles` con:

```json
{"tarea": "<slug>", "objetivo": "<qué hay que conseguir>", "tipo": "implementacion|testing|front|devops|documentacion", "carpeta": "<ruta absoluta del worktree>", "rama": "<rama de la tarea>"}
```

- **`carpeta`** es el worktree de la tarea, creado antes según `CLAUDE.md`, «Empezar una
  tarea». Los roles leen allí sus definiciones (`.claude/agents/`), así que el arnés funciona
  aunque la sesión que lo lanza no tenga los agentes registrados.
- **Cómo se pasa el script:** si la sesión no puede leer el fichero del script, se pasa entero en
  `script`. Si puede, se pasa la ruta en `scriptPath`.
- **Tarea pequeña:** se lanza a mano con la herramienta Agent: primero el constructor y después el
  juez de construcción. Cada uno recibe el mismo encargo que en el script: «lee primero
  `<carpeta>/.claude/agents/<rol>.md` y sigue sus instrucciones».
- **El arbitraje de §3 lo hace la sesión que lanzó el arnés.** El arnés devuelve `parada` con los
  hallazgos. La sesión arbitra y relanza desde la fase parada con `desde`
  (`"plan"` o `"construccion"`), `previo` (la entrega aprobada del rol anterior) y `arbitrada` (la
  entrega corregida, que el juez juzga primero).
- Para Javier, la sesión que lanzó el arnés redacta las desviaciones, las paradas y las consultas
  que le tocan.

Javier se entera de las paradas (tres vueltas sin aprobar, decisiones de producto o
irreversibles, desviaciones de patrón) y del resultado. Las decisiones técnicas reversibles las
toman los roles y quedan en la memoria de trabajo.

## 8. Decisiones técnicas tomadas al montarlo

- Modelos: el asesor usa `fable` con esfuerzo medio: el script lo pasa en las opciones de cada
  llamada, y `asesor.md` lo declara con el alias `fable` (sin comprobar que el frontmatter lo
  acepte; el ID completo es `claude-fable-5-2`). Los jueces, el modelo de la sesión con
  esfuerzo alto. El analista, el planificador y el constructor, el de la sesión. Los subagentes
  del constructor usan el modelo que diga el plan.
- Herramientas de mutación:
  - **Go:** gremlins (licencia Apache-2.0). No entra en ningún `go.mod`. Se ejecuta desde la raíz
    del módulo y nunca sobre `./...`:
    `go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --workers 4 --timeout-coefficient 10 ./contexts/<nombre>/domain`.
    Los nombres de las opciones se confirman con `unleash --help`. Un mutante «TIMED OUT» se
    repite con un coeficiente mayor y `--workers 1`. Si sigue sin acabar, no impide aprobar, pero
    se enumera. Si gremlins no arranca, la mutación figura como «no ejecutada», nunca como
    aprobada.
  - **Front:** StrykerJS (Apache-2.0). Todavía no está instalado. Entra como dependencia de
    desarrollo de `front/react` en la primera tarea de front que mida la mutación. Hasta
    entonces, la mutación del front figura como «no ejecutada», que no es lo mismo que aprobada.
- Una devolución a un rol anterior por tarea; la segunda la decide Javier.
- El juez lee la entrega sin la valoración que hace el autor de su propio trabajo. Así se cumple
  la revisión independiente de D-139.
