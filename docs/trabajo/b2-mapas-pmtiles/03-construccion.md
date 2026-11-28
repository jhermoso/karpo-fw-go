## Entrega del constructor: b2-mapas-pmtiles (vuelta con los hallazgos del juez y las respuestas del asesor)

## Resultado
Los cuatro hallazgos del juez están resueltos, y las condiciones del asesor también. La integración en el slot 60 y `TestProgram` contra su PostgreSQL pasan. No se ha tocado nada bajo `front/`.

La rama `feat/mapas-pmtiles` está en `C:/Git/Paranoia/karpo-fw-go-pmtiles`. Estos son sus commits nuevos sobre `a3b52d5`:
- `2e212e0` resuelve los hallazgos 2, 3 y 4:
  - `join.run(args, stderr)` tiene prueba con ficheros mínimos en `t.TempDir()`: municipio insular, duplicado, unidad esperada sin enlazar y otra inesperada, clave española sin geometría, y `nationalLevelName` como objeto o como texto;
  - `geokeys.run` escribe en un `io.Writer`, y su salida casa con `MapKeyHeader` y con `MapKeys`;
  - BACKLOG lista los supervivientes de mutación con fichero, línea, tipo y herramienta, y los paquetes que ya estaban por debajo del 80 %;
  - el tiempo de `MAXZOOM=8` en `build.sh` queda igual que en `SOURCES.md`: 16 min;
  - la repetición de `GetBoundary` con `mapParents` queda anotada en BACKLOG.
- `83df92a` aplica las condiciones del asesor para los límites inactivos:
  - `MapKeys` da error si un `parentCode` no está entre las claves que emite (un padre inactivo con hijos activos);
  - prueba en el dominio: `MapCode()` da lo mismo con `active=false`;
  - pruebas en la infraestructura: `MapKeys` deja fuera un municipio inactivo, y falla con una provincia inactiva que tiene municipios activos;
  - `docs/GEOGRAFIA.md` explica cómo distingue el cliente un inactivo (por `active`), que su `code` no tiene geometría en la tesela, que en la API `parentCode` puede apuntar a un inactivo y que la clave es única entre los activos.
- `191b3cf`: cobertura medida de nuevo en BACKLOG.

**Decisión sobre el hallazgo 4b:** la API sigue dando `code` también a un límite inactivo, y se documenta la diferencia con la tesela en vez de filtrar. Es la recomendación del asesor: `code` es la identidad del límite, y los datos antiguos siguen apuntando a límites inactivos. Hoy la semilla no trae ninguno.

## Comandos y resultados reales
- **Batería** `go build ./... && go vet ./... && go test -count=1 ./...`: verde, 93 paquetes `ok`.
- **Integración** con `./integration/run.ps1 -Slot 60`:
  - antes de lanzarla, `docker ps` no mostraba ninguna otra suite de bases, y `netsh interface ipv4 show excludedportrange protocol=tcp` no reservaba ninguno de los puertos del slot 60 (56033, 55033, 52033, 52121, 53906);
  - salida literal: `ok  github.com/jhermoso/karpo-fw-go/integration 3007.311s`, `exit=0`;
  - después se quitaron los contenedores con `docker compose -p integration-s60 down -v`;
  - registro completo: `C:/Git/Paranoia/geo-downloads/integration-s60.log`.
- **`TestProgram`** en `cmd/karpo-postgres`, con `KARPO_PG_DSN` apuntando al PostgreSQL del slot 60:
  - el primer intento falló: `relation "pur_invoices" already exists`. La suite estaba migrando esa misma base a la vez;
  - se repitió sobre una base nueva y desechable en el mismo servidor (`CREATE DATABASE karpo_program`). Salida literal: `--- PASS: TestProgram (15.08s)`, `ok github.com/jhermoso/karpo-fw-go/cmd/karpo-postgres 15.503s`.
- **Cobertura:**

| Paquete | Cobertura | Nota |
|---|---|---|
| `tools/geo/join` | 93,3 % | Igual con `-tags geo` y `GEO_OUT` |
| `cmd/geokeys` | 66,7 % | `run` al 83,3 %; lo que falta es `main` (justificado abajo) |
| `contexts/geography/infrastructure` | 63,8 % | En `origin/main` estaba en 60,5 %; anotado en BACKLOG |
| `host/serve` | 46,2 % | En `origin/main` estaba en 39,6 %; anotado; `MountPMTiles` al 100 % |

- **Mutación** con `go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --workers 4 --timeout-coefficient 10 ./contexts/geography/domain`:
  - `Killed: 28, Lived: 3, Not covered: 22`, eficacia 90,32 %;
  - en `mapcode.go` mueren todos.
  - Supervivientes vivos: `geography.go:160:13` (CONDITIONALS_NEGATION), `geography.go:161:15` (INCREMENT_DECREMENT) y `holiday.go:53:59` (CONDITIONALS_BOUNDARY). Todos son anteriores a esta tarea.
  - Las cifras de la entrega anterior (82 muertos, 16 vivos, 62 sin cubrir) se midieron sobre varios paquetes de la rama a la vez, no sólo sobre el dominio tocado. Ya no figuran: la cifra válida es la del dominio.

**Comando para Javier:**
```
cd C:/Git/Paranoia/karpo-fw-go-pmtiles && go test -count=1 -run "MapKeys|MapCode" -v ./contexts/geography/...
```
Debería ver en PASS `TestMapCode_DoesNotDependOnActive`, `TestMapKeys_LeavesOutInactive`, `TestMapKeys_FailsOnInactiveParent` y `TestMapKeys_LadderIsComplete`.

## Lo que no se ha ejecutado
- **`GetBoundary`:** no reutiliza los `Ancestors` que ya ha cargado (sigue haciendo hasta 3 consultas). Está anotado en BACKLOG §2g, que es lo que permitía el juez.
- **El front:** sigue fuera del alcance.
- **El asesor señaló un caso que no he verificado:** qué hace el front con un `code` que no está en la tesela. Es de la sesión del front.

## Desviaciones y por qué
1. **`TestProgram` sobre una base aparte** (`karpo_program`) del PostgreSQL del slot 60, no sobre la base `karpo`. La suite usaba `karpo` a la vez. La base aparte era nueva y se borró con el slot.
2. **`cmd/geokeys` se queda en 66,7 %.** Lo que falta es `main`, que sólo llama a `run` y sale. Se justifica como capa fina (lo admitía el juez): `run` está probada y `MapKeys` cubierta.
3. Las de la vuelta anterior siguen igual: z8, `--drop-rate=1`, BDDAE por el WFS, sin OSM, D-c (i) y Navarra fuera.

## Ficheros
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/infrastructure/mapkeys.go`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/infrastructure/mapkeys_test.go`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/domain/mapcode_test.go`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/tools/geo/join/main.go` y `run_test.go`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/cmd/geokeys/main.go` y `main_test.go`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/docs/GEOGRAFIA.md`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/BACKLOG.md`
- `C:/Git/Paranoia/geo-downloads/integration-s60.log`
