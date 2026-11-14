# CLAUDE.md — Karpo Go

## Qué es

Karpo en Go (`jhermoso/karpo-fw-go`, público): el **framework** (`pkg/`), los **contextos de
negocio** (`contexts/`), el **anfitrión** que los monta en un proceso (`host/`) y sus
**programas** (`cmd/`). Es la traducción idiomática de Karpo C#, sobre DDD, arquitectura limpia y
CQRS. Para entender el diseño, empezar por `docs/ARQUITECTURA.md`.

| Carpeta | Qué |
|---|---|
| `pkg/` | El framework (reglas propias en `pkg/CLAUDE.md`) |
| `contexts/<nombre>/` | Los 25 contextos de negocio (`contexts/CLAUDE.md`) |
| `host/`, `cmd/` | Composición y programas: `cmd/karpo` (SQLite, desarrollo: `go run ./cmd/karpo`), `cmd/karpo-postgres` (producción), `cmd/karpo-smoke*` (publicadoras de humo) |
| `examples/parties/` | Ejemplo del framework y banco de pruebas de Theros |
| `integration/` | Pruebas contra servidores reales (`integration/CLAUDE.md`) |
| `e2e/`, `contracts/billing/` | Pruebas de extremo a extremo; casos de cálculo compartidos con Karpo C# |
| `front/` | Configuración del motor de Angular de Karpo C# para los contextos de Go |
| `docs/` | Un documento por contexto y por tema (`docs/CLAUDE.md`); índice en `README.md` |

El port de los contextos de Karpo C# está terminado. Lo que se hace ahora:

- **converger** con Karpo C# y Trench en el modelo común, las reglas y el contrato HTTP, según el
  registro de decisiones comunes (`docs/DECISIONES-COMUNES.md`; lo que Go aún no cumple está en
  su §4);
- las **tareas del plan Sage** (T1 a T10) que lleguen como propuesta (D-127);
- lo pendiente de `BACKLOG.md`;
- **separar el framework y el ERP** en dos repositorios (`docs/plan/SEPARACION-FW-ERP.md`). La
  ejecuta la sesión de orquestación; los pasos irreversibles esperan a las decisiones de Javier y
  a la demo de Theros. Hasta entonces, este fichero describe el repositorio tal como está.

**Una tarea está terminada** cuando pasa la batería de «Comprobar», el documento de su contexto
(`docs/<CONTEXTO>.md`) y, si toca, `BACKLOG.md` dicen lo que ha cambiado, y su PR está abierta (o
fusionada, si es de la sesión de orquestación).

## Sesiones y proyectos hermanos

| Proyecto | Carpeta | Qué es |
|---|---|---|
| Trench | — | El ERP original en C#; sigue evolucionando |
| Karpo C# | `C:\Git\Paranoia\Karpo` | Karpo en C#; su motor de Angular es también el front de Go |
| Karpo Go | este repositorio | — |
| Theros | `C:\Git\Paranoia\Theros` | Generador de código; compila lo que genera contra este framework |

- Cada proyecto tiene **una sola sesión de orquestación**; la de éste es «Orquestador Karpo Go».
  Un **carril** es una sesión lanzada para una sola tarea, en su propia rama y worktree: informa
  a la sesión que la lanzó (no a Javier, salvo que él le hable directamente), no fusiona y no
  anota en el registro.
- Los otros repositorios **sólo se leen**. Si hace falta un cambio allí, se anota y se pide por
  mensaje a su sesión de orquestación; un aviso que deba quedar escrito va además en la PR.
- Un cambio en la API pública de `pkg/` puede romper lo que genera Theros: se avisa a
  «Orquestador Theros».

## Reglas fijas

### Git

- **La historia se reescribió el 2026-10-16.** El checkout `C:\Git\Paranoia\Karpo.Fw.Go` (y sus
  worktrees) conserva la historia antigua en su `main` local y en referencias `origin/*` viejas, y
  su `pushurl` de `origin` es inválido a propósito: no se arregla. Las ramas salen siempre de
  `origin/main`, nunca de la `main` local, y nunca se empuja un commit de la historia antigua (el
  hook lo comprueba). Desde aquí se empuja con la URL:
  `git push https://github.com/jhermoso/karpo-fw-go.git <rama>`. El clon `C:\Git\Paranoia\karpo-fw-go`
  tiene la historia nueva y un `pushurl` normal.
- **Empezar una tarea:** `git fetch origin`, y `git worktree add <ruta> -b <rama> origin/main` (o
  `git switch -c <rama> origin/main`). Ramas `feat/…`, `fix/…`, `docs/…`; los carriles,
  `carril/…`. Commits como `feat(shipments): …` o `docs: D-155: …`.
- **Una rama y una PR por tarea**, con un commit al abrirla; los arreglos posteriores (los de la
  revisión, por ejemplo) van en commits nuevos, sin reescribir lo empujado. La PR se abre con
  `gh pr create --head <rama>`. A `main` no se empuja nunca.
- **Fusionar** es cosa de la sesión de orquestación y sólo de sus propias PR (D-139; Javier,
  2026-11-06): con la batería en verde, la integración incluida, y una revisión independiente sin
  bloqueos (un subagente nuevo que lee el diff sin las conclusiones del autor), fusiona con
  `gh pr merge N --merge`, sin borrar la rama, y se lo cuenta a Javier.
- Nunca se saltan los hooks (`--no-verify`). `--force` sólo si Javier lo pide.

### Nombres

El repositorio es público. El ERP original se llama **Trench**, y su vertical financiera,
**FinancialDetail**. Sus nombres reales y los de terceros que se quitaron de la historia el
2026-10-16 no se escriben en ningún fichero, comentario, commit ni PR. En el chat y en los mensajes
entre sesiones sí.

### Datos

- **Seis motores:** SQLite (pruebas unitarias y `cmd/karpo`), PostgreSQL, Supabase, SQL Server,
  Oracle y MySQL (`integration/`). En producción, PostgreSQL o Supabase (`cmd/karpo-postgres`): si
  hay que elegir, se elige su comportamiento.
- **Los esquemas de Go y de C# son distintos a propósito** (D-1, D-127): lo común es el modelo,
  las reglas y el contrato HTTP; en esquema sólo convergen Trench y Karpo C#. Si lo que hace Trench
  es un error, se corrige aquí y se pide a su sesión que lo traslade (D-36).
- **Sólo bases desechables:** SQLite temporal en las pruebas, los contenedores de `integration/` en
  un slot propio y los que levanta `docs/SMOKE-MANUAL.md`. Nunca una base viva (la de desarrollo de
  Karpo C# en el puerto 55432, un proyecto real de Supabase…): ni se lee, ni se borra, ni se recrea.
- Las migraciones: ver `contexts/CLAUDE.md`.

### Código

- **Dependencias:** ninguna nueva en un `go.mod` sin el visto bueno de Javier. Los drivers de base
  de datos sólo entran en los módulos aparte (`integration/`, `cmd/karpo-postgres/`,
  `cmd/karpo-smoke-postgres/`).
- **El dominio es puro.** `pkg/domain` sólo usa la biblioteca estándar (y `shopspring/decimal`
  fuera de `pkg/domain` y `pkg/domain/spec`): lo comprueba `pkg/testing/archtest`, regla 1. El
  `domain/` de cada contexto, sólo la biblioteca estándar, `pkg/domain` y su propio dominio: eso no
  lo comprueba ninguna prueba, sólo el hook al escribir.
- **Secretos:** ninguno en código ni en configuración.
- **Sencillez:** la implementación más sencilla que cumpla; las dudas de diseño se resuelven antes
  de escribir código.
- **Idioma:** documentación, comentarios, commits y PR en español; identificadores y nombres de
  prueba en inglés.

### Documentación y decisiones

- El documento de cada contexto cambia en la misma rama que su código (`contexts/CLAUDE.md`).
- **Registro de decisiones comunes** (`docs/DECISIONES-COMUNES.md`): lo mantiene la sesión de
  orquestación (ver `docs/CLAUDE.md`). Un carril que reciba una decisión de Javier, o que cambie
  lo que Go cumple (§4), escribe en su PR el texto de la fila y la anota el orquestador.
- Lo pendiente va a `BACKLOG.md`.

## Comprobar

La CI de GitHub no corre desde el 2026-10-21 (cuenta bloqueada): **la puerta es local**. No se
espera a `gh pr checks`. **La batería** es la línea base más los apartados que apliquen:

```
go build ./... && go vet ./... && go test -count=1 ./...
```

- **Formato** de los `.go` cambiados y nuevos (el checkout tiene CRLF y `gofmt -l` los da todos
  por mal). En Bash, no debe imprimir nada:
  `b=$(git merge-base origin/main HEAD); { git diff --name-only --diff-filter=d $b -- '*.go'; git ls-files -o --exclude-standard -- '*.go'; } | while read f; do tr -d '\r' < "$f" | gofmt -l | sed "s|<standard input>|$f|"; done`
  `gofmt -w` sólo sobre ficheros sueltos, nunca sobre una carpeta.
- **Publicadoras de humo** (`host/smoke`, etiqueta `smoke`), si se toca `host/` o un contexto:
  `go vet -tags smoke ./... && go test -tags smoke -count=1 ./host/smoke/... ./cmd/karpo-smoke/...`
- **Módulos aparte:** `go vet ./...` dentro de `integration/` y de `cmd/karpo-postgres/`;
  `go vet -tags smoke ./... && go test -tags smoke -count=1 ./...` dentro de
  `cmd/karpo-smoke-postgres/` (sin la etiqueta no tiene paquetes).
- **Integración** (obligatoria antes de fusionar, D-139; para un carril, si toca persistencia,
  esquema o el anfitrión): la suite de `integration/` en un **slot** propio (un juego de
  contenedores con puertos propios, ver `integration/CLAUDE.md`), y con su PostgreSQL, `go test
  -count=1 ./...` dentro de `cmd/karpo-postgres/` con `KARPO_PG_DSN` apuntando a él (sin la
  variable, `TestProgram` se salta). Tarda entre 40 y 60 minutos: en segundo plano.
- **Un recorrido de punta a punta** con las publicadoras de humo (D-140, «en lo posible»):
  `docs/PUBLICADORA-DE-HUMO.md` y `docs/SMOKE-MANUAL.md`. La prueba `TestSeed_LoadsWhatTheWalkNeeds`
  (`host/smoke`) ya recorre el mismo camino en la batería.
- **Sólo documentación:** basta con que ningún enlace relativo apunte a un fichero que no existe.
- En local no hay `-race` (Windows sin compilador C); la CI lo usaba.

Prueba que falla a veces sin relación con el cambio: `TestParties_EndToEnd_MemoryThenSQLite`
(`BACKLOG.md` §2a y §2b-bis). Se repite antes de investigar.

**El hook** `.claude/hooks/guard.ps1` comprueba antes de ejecutar las reglas que se pueden
comprobar; las demás valen igual aunque no salte. Si se cambia, sus casos tienen que seguir
pasando: `pwsh -NoProfile -File .claude/hooks/guard.tests.ps1`.

Trampas de esta máquina (Windows): un comando de PowerShell con `//` al principio de una línea se
bloquea, así que el código Go se escribe con las herramientas de edición, no con here-strings.

## Cómo informar a Javier

- **Siempre en español**, también al resumir lo que un subagente o una herramienta dijo en inglés.
- Informes cortos. Lo primero, el resultado; después, lo que no se ha ejecutado o comprobado.
- Cada informe que cierra una tarea o una parte de ella termina con **un comando que él pueda
  ejecutar** y lo que debería ver.
- Como mucho tres decisiones por mensaje, y sólo las de producto o las irreversibles. Las técnicas
  y reversibles se toman, se anotan y se le cuentan.
- Un término de una herramienta ajena se explica en una frase, con un ejemplo.
