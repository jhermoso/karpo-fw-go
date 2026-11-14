# CLAUDE.md — `docs/`

- **Un documento por contexto** (`<CONTEXTO>.md`, en español y en mayúsculas) y uno por tema del
  framework. Los de contexto y los temas principales están en el índice de `README.md`; los
  análisis y propuestas de un tema se enlazan desde su documento principal. `plan/` guarda planes
  de trabajo.
- **Forma de un documento de contexto:** de dónde viene en C#, qué se porta y qué no, las
  decisiones aprobadas por Javier (con la fecha), cómo se usa (rutas, permisos, eventos,
  migraciones), la validación y lo pendiente. Una decisión aprobada no se reescribe: si cambia, se
  dice qué decisión nueva la sustituye y cuándo.
- **Cabecera de estado:** un documento que propone o evalúa empieza con `> **Estado: …**`
  (propuesta, aprobada, implementada) y se mantiene al día.
- **`DECISIONES-COMUNES.md`** es de los cuatro proyectos y lo mantiene «Orquestador Karpo Go».
  Ninguna fila que empiece por `| D-n |` se borra, en ninguna sección: a una decisión que cambia
  sólo se le añade «sustituida por D-n», y la nueva va en fila propia (la forma de las columnas, la
  de las filas vecinas). Un carril sólo arregla su forma (enlaces, cabeceras) y propone filas en
  su PR.
- **Nombres:** ver `CLAUDE.md`, «Nombres». Las rutas de los repositorios hermanos se citan como
  texto (`000-Docs/...` de Karpo C#), sin enlace.
- **Lo que deja de valer** se mueve a `archivo/`, con una fila en `archivo/README.md` (qué fue y
  por qué se archivó), y se corrigen los enlaces que apuntaban a él. No se borra.
- Antes de dar por terminado un cambio aquí, ningún enlace relativo debe apuntar a un fichero que
  no exista: con un fichero movido o renombrado, `git grep -n '<nombre>.md'` lo encuentra.
