# B-15: veredictos

## Construcción, vuelta 1 (commit `eb8577d`): no aprobado

Comprobado por el juez: Comprobar en verde; el filtro está en las 8 listas `/api/v1` del contrato
(las rutas `/approvals` no son de él); `ParseIDs` e `IDsFromFilter` mutados a mano en una copia
(8 y 5 mutantes) mueren todos; `IN` vacío se traduce a `1=0`; 1000 ids caben en un solo grupo en
Oracle. Ningún fallo de corrección en el código.

Hallazgos:

1. `docs/CONTRATO-MANTENIMIENTO.md` decía que el permiso va siempre antes; no es así para `type`
   (distribución) ni para el `format` de países (manejador).
2. Faltaban pruebas de países sin permiso (`?ids=` da 403; `?ids=ESP` da 400 `ids/format`).
3. Falta en `BACKLOG.md` la entrada de cobertura de `hr/application` (72,9 → 73,0 %) y
   `products/application` (77,3 → 77,6 %), medidos con `-coverpkg` frente a `origin/main`.
4. Mutantes vivos anteriores en los dominios tocados, sin listar uno a uno.
5. Faltaba esta carpeta de memoria de trabajo.

Gremlins v0.5.0 en Windows da todos los mutantes por tiempo agotado: no sirve como medida aquí.

**Pendiente de Javier:** los puntos 3 y 4 aplican dos lecturas de §5 que hizo el asesor
(`consultas.md`): el «trinquete» de cobertura y qué cuenta como mutante explicado. Son
interpretaciones que Javier aún tiene que confirmar.

## Construcción, vuelta 2

Correcciones en `03-construccion.md`, a la espera del veredicto.
