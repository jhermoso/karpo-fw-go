# Plan: separar karpo.fw.go (público) y karpo.erp.go (privado)

**Fecha**: 2026-11-14 · **Estado**: propuesta, pendiente de las decisiones de §6 · **Quién lo ejecuta**: la
sesión «Orquestador Karpo Go» (el repositorio es suyo) · **Leído de**: `jhermoso/karpo-fw-go`,
`origin/main` `ea4eba0` (13-nov). No se ha compilado ni ejecutado nada.

## 0. En una pantalla

- Hoy hay **un repositorio público** (`jhermoso/karpo-fw-go`, GPL v2) con **dos cosas**: el framework
  (`pkg/`, 182 ficheros Go) y el ERP (`contexts/`, 459 ficheros Go en 25 contextos, más `host/`,
  `front/`, `contracts/`, `e2e/`).
- **La frontera ya está limpia en un sentido**: ningún fichero de `pkg/` importa el ERP. La única
  pieza mezclada es `integration/`: 35 de sus 36 ficheros (todos pruebas) importan contextos.
- Resultado: **`karpo-fw-go`** (público, framework, licencia permisiva, versiones etiquetadas) y
  **`karpo-erp-go`** (privado, el ERP, que importa el framework como cualquier otra aplicación).
- **El ERP ya es público hoy**: separarlo no borra lo publicado (historia del repositorio y
  clones ajenos). Cuanto antes se haga, menos se expone.

## 1. Qué va a cada repositorio

| Ruta actual | Destino | Nota |
|---|---|---|
| `pkg/` | fw | El framework |
| `integration/` (driver por motor) | fw | Sólo el código no ERP: el adaptador de cada motor y su conformidad |
| `integration/*_context_test.go` y demás pruebas que importan contextos | erp | Pasan a un `integration/` del ERP |
| `examples/` | fw | No importa el ERP (comprobado) |
| `cmd/karpo-postgres`, `cmd/karpo-smoke*`, `cmd/karpo` | erp, salvo los que sólo usen `pkg/` | Revisar uno a uno |
| `contexts/`, `host/`, `front/`, `contracts/`, `e2e/` | erp | |
| `docs/` | repartir | Arquitectura, patrones, rasgos, esquema → fw; un documento por módulo (PARTIES, RRHH…) → erp |
| `docs/DECISIONES-COMUNES.md` | erp | Es negocio de todos los proyectos; privado |
| `README.md`, `CLAUDE.md`, `BACKLOG.md`, `.github/workflows/ci.yml` | los dos | Cada uno el suyo |

## 2. Pasos

1. **Congelar** `main` de `karpo-fw-go` mientras dura la separación (sin PR nuevas).
2. **Crear `jhermoso/karpo-erp-go` privado** a partir de un clon completo, conservando la historia,
   con `git filter-repo` quedándose con las rutas del ERP. *(`git filter-repo` reescribe un clon
   dejando sólo las rutas indicadas, con su historia.)*
3. **Módulo del ERP**: `module github.com/jhermoso/karpo-erp-go`; cambiar las importaciones de
   `karpo-fw-go/contexts/...` → `karpo-erp-go/contexts/...`; `require github.com/jhermoso/karpo-fw-go vX`
   (mientras no haya etiqueta, un `replace` local o una pseudo-versión).
4. **Limpiar `karpo-fw-go`**: borrar las rutas del ERP en un commit («el ERP pasa a karpo-erp-go»),
   dejar `integration/` sólo con lo del framework, rehacer README y CI.
5. **Licencia del framework** (decisión §6.1) y **primera etiqueta** `v0.1.0`.
6. **¿Reescribir la historia pública?** (decisión §6.2). Si sí, como el 2026-10-16/20: respaldo,
   `filter-repo` quitando las rutas del ERP de toda la historia, archivo del viejo, aviso a quien
   tenga clones. Si no, el ERP viejo sigue legible en la historia pública.
7. **Verificar**: `go build ./... && go vet ./... && go test ./...` en los dos, el ERP contra la
   etiqueta del framework; la batería de motores en los dos.
8. **Avisar** a Theros (§4) y a Karpo C# si cita rutas.

## 3. Riesgos

- `integration/` es el único corte con dependencias en los dos sentidos: hay que separar el
  adaptador por motor (fw) de las pruebas de contextos (erp) sin perder la batería de conformidad.
- Rutas y enlaces en documentos de otros proyectos (Theros, Karpo C#, registro común).
- Copias locales: `C:\Git\Paranoia\Karpo.Fw.Go` y `C:\Git\Paranoia\karpo-fw-go` son dos clones del
  mismo repositorio, con `main` locales viejas; tras la separación conviene volver a clonar.

## 4. Impacto en Theros

- **Lo que genera Theros importa sólo el framework** (`pkg/`), así que su dependencia sigue siendo
  `karpo-fw-go`. Hay que comprobarlo: `grep` de `karpo-fw-go/contexts` en `packs/karpo-go` y en
  `testdata/harness`.
- **Lo que Theros mide sí es el ERP**: el medidor, el extractor y los modelos `testdata/karpo_go/*`
  leen Parties, Seguridad, etc. de la copia fijada `.work/ref/karpo-fw-go-<commit>`. Pasarán a dos
  copias: el framework (para compilar lo generado, `.work/karpo-fw-go`) y el ERP (para medir,
  `.work/ref/karpo-erp-go-<commit>`), con dos commits fijados en vez de uno
  (`measure.FrameworkCommit` se desdobla).
- El CI de Theros saca el framework de un repositorio público sin secretos; el ERP privado
  necesitará credenciales en el CI (hoy desactivado) o medirse sólo en local.
- Mover las copias fijadas es una tarea explícita de Theros que vuelve a medir; no la hace el
  orquestador de Karpo Go.

## 5. Orden con lo que está en curso

Theros prepara la demo (Go, monolito, Parties + Seguridad) contra `3d70d7f`. La separación no la
rompe mientras Theros no mueva su copia fijada: conviene hacerla **después de la demo** o, si es
antes, que Theros no mueva su copia hasta terminarla.

## 6. Decisiones para Javier (de producto, irreversibles)

1. **Licencia de `karpo-fw-go`**: seguir GPL v2 o pasar a una permisiva (Apache 2.0, propuesta en
   comercialización). Con GPL, toda aplicación que lo use y se distribuya debe ser GPL.
2. **Historia pública**: reescribirla para quitar el ERP de toda la historia, o dejarla.
3. **Nombre**: `karpo-erp-go` (propuesto) u otro.

## 7. Comprobado por el orquestador de Karpo Go (main ea4eba0, 2026-11-14)
- pkg/ y examples/ no importan contexts/ ni host/: 0 ficheros.
- integration/ es un módulo Go propio (su go.mod) con 40 ficheros; 35 importan contexts o host y van al ERP. Se quedan en el framework docker-compose.yml, go.mod, go.sum, integration_test.go y run.ps1; los adaptadores por motor viven en pkg/, así que la conformidad se queda entera en el framework.
- Los cuatro de cmd/ (karpo, karpo-postgres, karpo-smoke, karpo-smoke-postgres) importan el ERP: van todos al ERP.
- Estado: sólo preparación. No se ha creado el repositorio privado, no se han borrado rutas ni reescrito historia; espera a las tres decisiones de Javier (§6) y a la demo de Theros (§5).
