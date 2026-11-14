# Integración del 2026-10-11 — bitácora de Karpo Go

Ejecución de las etapas **G-0 a G-5** del plan de integración (está en el repositorio de
Karpo C#: `000-Docs/planning/Integracion_Plan_2026-10-11.md`). La rama es `integra/2026-10-11`,
en el worktree `.claude/worktrees/integra-2026-10-11`. **Nada se ha subido ni fusionado en `main`.**

## Resultado

| Etapa | Hora | Resultado |
|---|---|---|
| G-0 Foto | 01:25 | Hecha. Los números del plan coinciden |
| G-1 NATS y `CLAUDE.md` a `wip/` | 01:27 | Hecha. `wip/2026-10-11-broker-nats` (`66613f9`) y `wip/2026-10-11-docs` (`87808db`) |
| G-2 Rama de integración | 01:28 | Hecha. Cuatro fusiones `--no-ff`, **sin conflictos** |
| G-3 Compilar y tres pasadas | 01:28–02:40 | Hecha. Raíz: 3 de 3. Cuatro motores reales: 3 de 3, ningún salto |
| G-4 Base de datos | 01:31 | Hecha. `migrate` dos veces sobre SQLite nuevo: 0 y 0 |
| G-5 Publicadora de humo | 01:32–02:40 | Hecha. Tres commits; sus pruebas, 3 de 3 |
| G-6 Revisión | — | Hecha por otra sesión. Sus arreglos, en «Arreglos tras la revisión»: seis commits, tres pasadas en verde |
| G-7 Humo por HTTP | — | **No ejecutada** como etapa. Hay un recorrido con `curl`, anotado abajo |
| G-8 Subida, G-9 Limpieza | — | **No ejecutadas**. No se ha borrado ninguna rama ni worktree |

## G-0 · Foto

- Checkout principal: `feat/ddd-contracts-agnostic-persistence`, 5 commits por detrás de su remoto, y `?? CLAUDE.md`.
- Worktree `angry-diffie-9a1681`: 33 entradas sin confirmar, todas del 6 de octubre.
- `origin/main...origin/feat/ddd-contracts-agnostic-persistence`: `13 18`.

## G-1 · Lo sin confirmar, a ramas `wip/`

- **Broker NATS**: confirmado tal cual en `wip/2026-10-11-broker-nats`. Son **38 ficheros**
  (25 modificados y 13 nuevos); el plan decía 33 porque contaba entradas de `git status`, y dos
  de ellas son carpetas. No se ha arreglado ni completado nada.
- **`CLAUDE.md`**: confirmado en `wip/2026-10-11-docs`, tras avanzar la rama de trabajo local
  hasta su remoto (avance directo, 5 commits).
- **Examen de secretos (R5)**: el patrón señala `.github/workflows/ci.yml` e
  `integration/docker-compose.yml` por la contraseña del SQL Server **de los contenedores de
  prueba**, que ya estaba confirmada en `main` desde antes. Ninguna línea **nueva** de los dos
  commits tiene forma de secreto. No se apartó ningún fichero.
- **Choque del broker con la integración** (para decidir cuándo retomarlo; no se fusiona):
  seis ficheros en conflicto — `.github/workflows/ci.yml`, `BACKLOG.md`, `README.md`,
  `integration/run.ps1`, `pkg/application/outbox/outbox.go`, `pkg/persistence/sqlrepo/sqlite/sqlite_test.go`.

## G-2 · Fusiones

En este orden, todas limpias: geografía (PR #7) → `main` (observabilidad, PR #4 y #5) → gofmt
(PR #6) → `CLAUDE.md`.

**`gofmt -l` lista 603 ficheros en este worktree, y no es un defecto.** Git está configurado
aquí con `core.autocrlf=true`: al crear el worktree escribe los ficheros con finales de línea
de Windows, y `gofmt` los da por mal formateados. Sobre el contenido confirmado (sacado con
`git -c core.autocrlf=false archive HEAD`), `gofmt -l pkg examples integration host cmd contexts`
**no lista nada**. No se hizo el commit `style: gofmt tras la integración`: no habría cambiado
ni un byte. El CI corre en Linux y ve el contenido confirmado.

## G-3 · Tres pasadas

| Batería | Pasada 1 | Pasada 2 | Pasada 3 | Fallos | Saltos |
|---|---|---|---|---|---|
| Raíz (`go build`, `go vet`, `go test -count=1 ./...`), tras las fusiones | 92 s | 56 s | 54 s | 0 | 0 |
| `integration/` contra PostgreSQL, SQL Server, Oracle y MySQL | 1 311 s | 1 289 s | 1 301 s | 0 | 0 |
| Raíz, repetida con la publicadora de humo ya confirmada (`33e2753`) | 63 s | 61 s | 73 s | 0 | 0 |
| Publicadora de humo (`go vet -tags smoke ./...` y sus pruebas con la etiqueta) | 15 s | 15 s | 16 s | 0 | 0 |

- Raíz: **81 paquetes** en verde en cada pasada (el plan pedía 77 o más).
- Integración: 40 pruebas y 476 subpruebas por pasada; subpruebas por motor: postgres 102,
  sqlserver 102, mysql 102, oracle 170. **Los cuatro motores se probaron de verdad**, en los
  contenedores del proyecto `integration-s3` (puertos +30), creados y eliminados por esta ronda.
- `go vet` de `cmd/karpo-postgres`: 0.
- Ninguna prueba en cuarentena.
- **Turno de pruebas (R7)**: al empezar la primera de las pasadas finales de la raíz había un
  `go` de otra sesión en marcha; pasó igual. Durante las pasadas de integración esta misma
  sesión compiló y probó la publicadora de humo (cargas cortas); las tres pasaron.
- **No ejecutado**: `go test -race` (pide compilador de C en Windows; queda para el CI) y las
  pruebas de `cmd/karpo-postgres` contra un PostgreSQL real (el plan sólo pide su `go vet`; el
  CI sí las ejecuta).
- Las tres pasadas de integración corrieron sobre el árbol **anterior** a la publicadora de
  humo. La publicadora sólo añade ficheros con etiqueta y una prueba de `archtest`; el módulo
  `integration/` no compila ninguno de ellos.

## G-4 · Base de datos

`go run ./cmd/karpo migrate` sobre un SQLite nuevo: 26 contextos, **80 migraciones**, salida 0.
Otra vez: 0 migraciones, salida 0. El fichero y su carpeta temporal se borraron.

## G-5 · Publicadora de humo

Está descrita en `docs/PUBLICADORA-DE-HUMO.md`. Tres commits:

| Commit | Qué |
|---|---|
| `d853847` | `host/smoke/` y `cmd/karpo-smoke/`, con sus pruebas, y el documento |
| `ecfaacd` | Reglas 10 y 11 de `pkg/testing/archtest` (sin etiqueta) |
| `33e2753` | CI: paso de la publicadora y `gofmt` también sobre `contexts` |

Decisiones tomadas al construirla (todas reversibles):

| Decisión | Por qué |
|---|---|
| **No hizo falta ninguna costura en `host/serve`** (G-5.1) | La publicadora usa `host.Compose`, `Start` y `Handler`, que ya son públicos. No se ha tocado ni una línea del anfitrión real |
| La obligación de cambiar la contraseña se quita **por las rutas del propio anfitrión** | La publicadora crea `smoke-admin` con el arranque de Security, abre sesión y cambia la contraseña ella misma, dentro del proceso. No se añade nada a Security |
| **Sólo arranca sobre una base sin administrador** | La contraseña no se guarda en ningún sitio: en un segundo arranque nadie la conoce. Además impide abrir la puerta sobre una base con datos de alguien |
| Se niega si `KARPO_BOOTSTRAP_ADMIN_USER` o `…_PASSWORD` están puestas | «No pide, ni lee, ni guarda ninguna contraseña» |
| Un solo perfil, `admin` | Es lo que fija el plan. Un perfil de sólo lectura exigiría crear un rol y un usuario; queda para quien lo necesite |
| Puerto por defecto **8092** | El 8081 es de `karpo-gateway` y el 8091 es el que usa la de Theros |
| Para un usuario que no es un perfil, `POST /api/auth/login` es el del anfitrión | No se debilita nada más que lo imprescindible |
| Comentarios del código en inglés | Es como está todo el código del repositorio y el modelo de Theros. El `CLAUDE.md` nuevo pide comentarios en español: **queda para Javier** decidir cuál manda |

Comprobaciones de las barreras:

| Barrera | Orden | Salida |
|---|---|---|
| Compilación | `go build ./cmd/karpo-smoke` (sin etiqueta) | Falla: «build constraints exclude all Go files»; no hay binario |
| Compilación | `go list ./...` sin etiqueta | Ningún paquete con `smoke`; `cmd/karpo` no depende de `host/smoke` |
| Arranque | El binario sin `KARPO_ENV`, y con `KARPO_ENV=production` | Sale con 1: «KARPO_ENV is "…" and must be exactly "development"» |
| Arranque | En marcha, `Get-NetTCPConnection -LocalPort 8092` | Escucha sólo en `127.0.0.1` |
| Pipeline | Un fichero sin etiqueta que importa `host/smoke`, puesto a mano en `host/serve` | `TestSmoke_IsNotImportedByThePrograms` y `TestSmoke_StaysBehindItsTag` fallan; al quitarlo, verde. Se deshizo |

### Recorrido con `curl` (anotación para G-7; no sustituye a la etapa)

Contra `cmd/karpo-smoke -port 8092 -seed`, sobre una base temporal que se borró al acabar:

| # del guion | Petición | Resultado |
|---|---|---|
| 1 | `GET /api/parties` sin token | 401 |
| 2 | `GET /api/smoke/token?as=admin` | 200, `success`, `smoke-admin`, sin obligación de cambiar contraseña |
| 3 | `POST /api/organizations` | 201, `version` 1, `createdBy` `smoke-admin` |
| 4 | Leerla y `PUT /api/parties/{id}/name` | 200 y 200, `version` 2 |
| 5 | Modificar con una versión vieja | **No se puede hacer por HTTP**: las rutas de Parties no reciben la versión (ni cabecera ni cuerpo) |
| 6 | `GET /api/geography/boundaries?q=Coru` | 200; «Coruña» y «Coruña, A», bien escritas |
| 7 | Métricas | **404**: el anfitrión no sirve `/metrics` (tampoco `cmd/karpo`) |
| 8 | `GET /api/exports/datasets` | 200. No se pidió ninguna exportación |
| — | `/readyz`, `/api/auth/context`, `/api/smoke/enter?as=admin` | 200 los tres |
| — | Login de `admin` con cualquier contraseña / de otro usuario | 200 / 401 |

## Arreglos tras la revisión

La revisión independiente (G-6) dedujo sus hallazgos leyendo el código, sin ejecutarlo. Antes de
tocar nada se confirmó cada uno leyendo el código, y cada prueba nueva se ejecutó además **sin**
su arreglo para ver que fallaba. Los cuatro eran ciertos.

| Arreglo | Qué pasaba | Qué se hizo | Prueba | Commit |
|---|---|---|---|---|
| **H-1** | Con `-db` sobre un fichero existente, la publicadora de humo lo migraba y arrancaba el anfitrión antes de mirar si había administrador: sobre una base real sin administrador dejaba `smoke-admin` como administrador global | `cmd/karpo-smoke/main.go`: se niega con `os.Stat` antes de abrir nada. Sólo trabaja sobre una base que crea ella | `TestRun_RefusesADatabaseThatAlreadyExists`: con un fichero vacío (que SQLite sí abre) y con otro contenido, sale con error, el fichero queda byte a byte igual y no aparece nada a su lado | `956c7d4` |
| **H-2** | En `Parking.retry`, un error que no era de regla (una validación) devolvía y cortaba el bucle: bloqueaba a los hechos posteriores de todas las empresas | `contexts/accounting/application/parking.go`: se anota en el hecho con el motivo `accounting.failed`, se detiene sólo a su empresa, se sigue con las demás y los errores se devuelven juntos al final | `TestParking_AFailureThatIsNoRuleStopsItsCompanyAlone`: dos empresas; el hecho de la primera falla con una validación y el de la segunda se asienta | `1862f5c` |
| **H-3** | El anfitrión reintenta cada minuto y cada rechazo escribía el hecho y su auditoría: unas 1.440 escrituras al día por empresa sin libro | Si el código y el motivo son los del intento anterior, no se escribe (`ParkedFact.SameRefusal`). Sin cambio de esquema | `TestParking_TheSameRefusalIsNotWrittenAgain`: 25 rondas por el mismo motivo, una sola escritura (la que lo aparcó); al cambiar el motivo, una más | `c22a9f6` |
| **Menores de la publicadora** | `CheckAddress` aceptaba el nombre `localhost`; no se miraba la cabecera `Host`; la regla de arquitectura comparaba texto | Sólo direcciones IP de bucle local; 403 si `Host` no es `127.0.0.1`, `[::1]` o `localhost` (con cualquier puerto); la regla lee la etiqueta con `go/parser` y `go/build/constraint` y mira todos los `.go` de fuera de la publicadora, pruebas y todos los `cmd/*` incluidos | `TestListen_RefusesWhatIsNotThisMachine`, `TestHost_OnlyThisMachine`, y `TestSmoke_TheRuleBites`, que pasa de 6 a 13 casos | `8ab4583` |
| **Backlog** | Hallazgos menores que no se tocan | Siete entradas en `BACKLOG.md`, §2d, con fichero y línea | — | `ebe2411` |

Decisiones tomadas (técnicas y reversibles):

| Decisión | Por qué |
|---|---|
| **Cambia el significado de `attempts`** de un hecho aparcado: ya no cuenta todos los intentos, cuenta el primero, cada rechazo por un motivo distinto y el intento que lo asentó | Es la consecuencia de no escribir cuando nada cambió. Está documentado en el dominio y en `docs/CONTABILIDAD.md` |
| **No se aplicó la espera creciente con tope** de `host/mailbox` (`NextAttempt`, `MaxAttempts`) | Exige una columna nueva en `acc_parked_facts`, es decir, una migración. Queda en el backlog. El hecho se sigue intentando cada minuto, pero ya no escribe |
| Motivo nuevo `accounting.failed` para lo que no es una regla | El hecho necesita un código que diga que no lo rechazó una regla; el texto del error va en el motivo |
| Mientras un hecho siga fallando con algo que no es una regla, el anfitrión lo anota en el registro **en cada ronda** | Es lo que pide «devolver el error al final». No se escribe nada en la base (H-3) |
| **La regla 10 es más estricta**: nada de fuera de la publicadora la importa, tampoco una prueba con la etiqueta `smoke` | Un programa real compilado con la etiqueta llevaría la puerta. No había ningún fichero así |
| La comprobación de `Host` cubre **todas** las rutas de la publicadora, no sólo las de la puerta | Con el reenlace de DNS, la página de fuera pediría el token y después cualquier ruta |
| El proxy de desarrollo del front de Karpo C# (`010-Front/Angular/proxy.conf.js`, sólo leído) usa `changeOrigin: true` | Envía el `Host` de su destino (`localhost:<puerto>`), que pasa. Sin `changeOrigin` enviaría `localhost:4200`, que también pasa |

Tres pasadas tras el último arreglo, sin reintentos:

| Batería | Pasada 1 | Pasada 2 | Pasada 3 | Fallos |
|---|---|---|---|---|
| Raíz (`go build ./... && go vet ./... && go test -count=1 ./...`) | 62 s | 63 s | 79 s | 0 |
| Publicadora de humo (`go vet -tags smoke ./...` y las pruebas con la etiqueta de `host/smoke`, `cmd/karpo-smoke` y `pkg/testing/archtest`) | 17 s | 18 s | 37 s | 0 |

- Raíz: **81 paquetes** en verde en cada pasada.
- **Turno de pruebas**: antes de cada batería se esperó a que no hubiera `go` ni pruebas de otra
  sesión (30 s antes de la primera, 150 s antes de la última). Los `dotnet` que había eran nodos
  de MSBuild en reposo.
- **No ejecutado**: la batería contra motores reales (`integration/`). Los arreglos **no tocan
  SQL ni el esquema**; del módulo `integration/` sólo se pasó `go vet`, que compila sus pruebas.
  Tampoco `go test -race` (queda para el CI).
- Sin dependencias nuevas: `go.mod` y `go.sum` no cambian.

## Lo que el plan decía y no era así

| El plan | La realidad |
|---|---|
| El commit del broker lista 33 ficheros | Son 38 (33 entradas de `git status`, dos de ellas carpetas) |
| «Bien si `gofmt -l` no lista nada» | En este worktree lista 603 por los finales de línea de Windows; el contenido confirmado está limpio |
| G-7, paso 5: una versión vieja da 409 | Las rutas HTTP de Parties no llevan versión |
| G-7, paso 7: métricas, 200 | El anfitrión no monta `/metrics` |

## Estado en que queda el disco

- **Checkout principal**: en `feat/ddd-contracts-agnostic-persistence`, al día con su remoto y
  limpio. **Ya no tiene `CLAUDE.md` en disco**: el fichero vive en `wip/2026-10-11-docs` y en
  `integra/2026-10-11`. Vuelve al fusionar la integración.
- **Worktree `angry-diffie-9a1681`**: en `wip/2026-10-11-broker-nats`, limpio.
- **Worktree `integra-2026-10-11`**: en `integra/2026-10-11`, limpio. Los `*.log` de las
  pasadas están ahí, ignorados por git.
- **Docker**: el proyecto `integration-s3` se eliminó (`down -v`). No se tocó ningún otro contenedor.

## Lo hace Javier

```powershell
cd C:\Git\Paranoia\Karpo.Fw.Go
git push -u origin integra/2026-10-11 wip/2026-10-11-broker-nats
```

Debería verse: dos ramas nuevas en el remoto, y el CI de la primera en marcha (ahí corre
`-race` por primera vez sobre esta integración). El resto de la subida está en el plan, §8.2.

Antes de subir falta G-7 (humo por HTTP). G-6 está hecha y sus arreglos aplicados.

Para comprobar los arreglos:

```powershell
cd C:\Git\Paranoia\Karpo.Fw.Go\.claude\worktrees\integra-2026-10-11
go test -count=1 ./contexts/accounting/... ./pkg/testing/archtest/...
go test -tags smoke -count=1 ./host/smoke/... ./cmd/karpo-smoke/...
```

Debería verse `ok` en cuatro paquetes (y dos líneas «no test files» de Contabilidad).
