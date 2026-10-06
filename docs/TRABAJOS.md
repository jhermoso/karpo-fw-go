# Contexto Trabajos

Port del subdominio `WorkEffort` de C# (`ErpKernel.Domain/Subdominios/WorkEffort`) al contexto
`contexts/work`.

La fase 1 es **el trabajo y sus horas**: proyectos, tareas y órdenes con un estado que avanza por
reglas, las personas asignadas con su tarifa, y los partes de horas con aprobación. Con eso se
sabe qué se está haciendo, quién, cuántas horas y cuánto cuestan.

## Método

| Criterio | Peso |
|---|---|
| **U** Utilidad (uso real en contextos, sin Fw ni tests) | 40 % |
| **C** Corrección de la implementación | 25 % |
| **D** Diseño de la firma | 20 % |
| **G** Encaje en Go | 15 % |

Puntuación de 1 a 5 por criterio; nota ponderada sobre 100. **Mantener** ≥ 70 · **Modificar** 45–69 ·
**Retirar** < 45 · **Sustituido**.

## Estado del C#

- **Una cabecera sin comportamiento.** `WorkEffort` tiene nombre, tipo, estado, propósito, fechas
  previstas y reales, horas y presupuesto. Todos los `Validate()` devuelven éxito: no hay
  invariantes ni reglas de cambio de estado.
- **Estado e historial independientes.** Crear una fila de `WorkEffortStatus` no cambia el estado
  de la cabecera, y la clave ajena del historial apunta a `parties.status_type`, no al catálogo de
  estados del trabajo.
- **Las horas reales se teclean.** `ActualHours` es un campo libre; nada lo calcula.
- **Partes de horas inservibles.** `TimeEntry` tiene horas pero **no tiene fecha**; `Timesheet` no
  tiene periodo ni estado. Ninguno tiene repositorio, servicio, endpoint ni pantalla.
- **La tarifa no se usa.** `WorkEffortAssignmentRate` guarda un importe que nunca se multiplica:
  no hay coste, facturación ni aprobación.
- **Sin jerarquía.** «Proyecto» y «Tarea» son solo nombres del catálogo de tipos: las clases de
  desglose, dependencia y precedencia no están en el modelo de EF.
- **Errores:**
  - `UpdateAsync` borra el presupuesto y las horas permitidas en cada modificación
    (`SetEstimates(null, null, …)`), y no deja cambiar la instalación ni el activo;
  - una asignación sin instalación guarda `Guid.Empty`;
  - los repositorios leen como mucho 1 000 o 5 000 filas sin orden y filtran en memoria: la
    búsqueda y la paginación pierden filas en silencio;
  - un usuario con ámbito no ve los trabajos sin personas asignadas;
  - el borrado es físico y en cascada (asignaciones, tarifas, historial y horas).
- **Unas 35 clases solo de dominio** (requisitos, entregables, estándares, habilidades, órdenes
  de trabajo, consumo de inventario…), sin tabla, repositorio ni servicio.
- **Sin permisos:** 45 operaciones solo con autenticación. Los códigos `WorkEffort.*` están
  sembrados pero ningún endpoint los comprueba.
- **Sin datos:** cero filas de trabajos, asignaciones u horas; solo los catálogos están sembrados.
- **Único flujo real:** `IssueAsync` emite un documento numerado a través de Documents.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `WorkEffort` (cabecera sin reglas; la modificación borra el presupuesto) | 4 | 2 | 2 | 3 | **59** | Modificar → agregado `Work` por empresa: código único, clase, **trabajo del que forma parte** (`parent`), plan (nombre, propósito, cliente, instalación, activo, fechas previstas, horas estimadas y presupuesto), estado, fechas reales de inicio y fin. Las horas y el coste reales **se calculan** de los partes aprobados |
| 2 | Catálogos `WorkEffortType`, `WorkEffortStatusType`, `WorkEffortPurposeType` | 3 | 3 | 2 | 3 | **56** | Modificar → enumeraciones cerradas con los valores sembrados: clases `project`, `task`, `maintenance`, `production`; estados `created`, `scheduled`, `in-progress`, `on-hold`, `completed`, `cancelled`; propósitos `improvement`, `repair`, `preventive-check`, `audit`, `training` |
| 3 | `WorkEffortStatus` (historial desligado de la cabecera) | 3 | 1 | 2 | 3 | **46** | Modificar → el estado **solo cambia por sus reglas** y cada cambio deja su paso en el historial, dentro del agregado y en la misma transacción |
| 4 | `WorkEffortPartyAssignment` + `WorkEffortAssignmentRate` | 3 | 2 | 2 | 3 | **51** | Modificar → una sola `Assignment` por persona: función, **tarifa por hora**, desde y hasta |
| 5 | `TimeEntry` (sin fecha) + `Timesheet` (sin periodo ni estado) | 3 | 1 | 1 | 2 | **39** | Sustituido → agregado `TimeEntry`: persona, trabajo, **día**, horas, tarifa fijada al imputar, facturable y comentario; borrador → **aprobado** (definitivo). El «parte» es una consulta de las imputaciones de una persona en un periodo |
| 6 | `WorkEffortFixedAssetAssignment` + su catálogo de estados | 2 | 2 | 2 | 3 | **43** | Retirar → el trabajo guarda el activo sobre el que se hace; la reserva de activos con coste va a la fase 2 |
| 7 | `IssueAsync` (documento numerado) | 2 | 3 | 3 | 3 | **52** | Aplazado → cuando exista el contexto de Documentos. El código del trabajo lo pone quien lo abre |
| 8 | Búsqueda enriquecida y estadísticas | 3 | 2 | 2 | 3 | **51** | Modificar → búsqueda en SQL por empresa, clase, estado, trabajo padre y cliente, con el ámbito por empresa de todos los contextos. Las estadísticas, en la fase 2 |
| 9 | ~35 clases sin tabla (requisitos, entregables, estándares, habilidades, órdenes de trabajo, consumo de inventario…) | 1 | 1 | 1 | 2 | **23** | Retirar. De la jerarquía se conserva lo útil: `parent` |
| 10 | Códigos `WorkEffort.*` que nadie comprueba | — | — | — | — | — | Sustituido → `Work.Work.Read/Update/Progress` y `Work.Time.Read/Record/Approve` (**planificar, mover el estado, imputar horas y aprobarlas están separados**) |

## Diseño

```
contexts/work/
├── domain/          # Work (+Plan, Assignment, Step), TimeEntry, reglas del estado
├── contracts/       # lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito
├── infrastructure/  # esquema wrk_* de 5 motores, mapeos
└── module.go        # composición y rutas /api/work/... y /api/time/...
```

- **El estado avanza por su regla**, en una fecha no anterior a la del cambio previo:

  | Desde | Puede pasar a |
  |---|---|
  | `created` | `scheduled` (necesita fechas previstas), `in-progress`, `cancelled` |
  | `scheduled` | `in-progress`, `cancelled` |
  | `in-progress` | `on-hold`, `completed`, `cancelled` |
  | `on-hold` | `in-progress`, `cancelled` |

  `completed` y `cancelled` son finales: el trabajo cerrado no admite cambios de plan, personas ni
  horas. Cancelar exige motivo.
- **Completar** exige que no queden partes abiertas del trabajo (tareas de un proyecto) ni horas
  pendientes de aprobar. Al completar se guardan las horas aprobadas y su coste.
- **Horas.**
  - Solo imputa quien está **asignado ese día**, en un trabajo **en curso** y no antes de su
    inicio.
  - Más de 0 y hasta 24 horas por imputación, y **24 horas por persona y día** entre todos los
    trabajos de la empresa.
  - La tarifa se fija al imputar: cambiarla después en la asignación no altera lo ya imputado.
  - Un borrador se corrige o se retira; **una imputación aprobada es definitiva**.
  - «Facturable» vale por defecto lo que diga el trabajo: sí cuando tiene cliente.
- **Lenguaje publicado** (sin consumidores todavía): `work.time-approved.v1` (persona, día, horas,
  coste, facturable) y `work.work-completed.v1` (cliente, fechas, horas y coste).
- **Tablas:** `wrk_works` (+ `wrk_work_assignments`, `wrk_work_history`), `wrk_time_entries`, las
  bandejas de salida y la auditoría.

## Decisiones propuestas (pendientes de confirmar)

1. **La fase 1 de Trabajos es el trabajo con su estado, sus personas y sus horas.** Requisitos,
   entregables, dependencias entre tareas, consumo de materiales, reserva de activos y habilidades
   van a la fase 2. Sugerencia: sí; es lo único que el C# tenía a medio usar, y lo demás eran
   clases sin tabla.
2. **Clases, estados y propósitos son listas cerradas** con los valores sembrados en C#, y el
   estado solo cambia por sus reglas (tabla de arriba). Sugerencia: sí; si necesitas reabrir un
   trabajo completado, se añade una transición `completed → in-progress` con su permiso.
3. **Las horas reales y el coste se calculan de los partes aprobados**; no se teclean. El coste es
   horas × tarifa de la asignación, fijada al imputar. Sugerencia: sí. La tarifa es un **coste
   interno**; el precio de venta de la hora llega con la facturación (decisión 6).
4. **Imputar y aprobar son permisos distintos, y cualquiera con `Work.Time.Record` imputa horas de
   cualquier persona asignada** (un encargado puede pasar los partes de su cuadrilla). Sugerencia:
   sí en la fase 1; el autoservicio («solo mis horas») necesita enlazar el usuario con su persona
   y se añade como restricción en la fase 2.
5. **El «parte de horas» es una consulta**, no un documento: las imputaciones de una persona en un
   periodo, con sus totales. La aprobación es por imputación. Sugerencia: sí; el parte semanal que
   se envía y se aprueba en bloque se puede añadir encima sin cambiar los datos.
6. **Los eventos se publican pero nadie los consume todavía.** Facturar las horas facturables
   (Facturación), llevar el coste a la contabilidad analítica y cruzar las horas con la nómina se
   deciden en la fase 2. Sugerencia: sí; el siguiente paso natural es «facturar desde el trabajo»,
   igual que se hizo con el albarán.
7. **El código del trabajo lo pone quien lo abre** (único por empresa), sin numeración automática.
   Sugerencia: sí por ahora; si prefieres una serie (`OT-2026-000001`) se añade un contador como
   el de Pedidos.

## Validación

- **Dominio:**
  - plan (nombre, propósito, fechas previstas coherentes, horas y presupuesto), código, clase,
    trabajo que no es parte de sí mismo;
  - todas las transiciones válidas e inválidas, fechas que no retroceden, planificar exige fechas,
    reanudar conserva el inicio, cancelar exige motivo, trabajo cerrado inmutable;
  - asignar, cambiar la tarifa y liberar; tarifa del día; imputar fuera de curso, antes del inicio
    o sin asignación;
  - horas (0, 24,5 y tres decimales rechazadas), coste, corrección, aprobación definitiva.
- **Extremo a extremo** (Parties y Trabajos sobre el mismo backend, en memoria y en SQLite
  migrada):
  - proyecto con cliente y dos tareas: solo lectura 403, ajeno 404, clase inválida 400, código
    repetido 422;
  - asignaciones (imputar no es planificar: 403);
  - estado: planificar no es mover el estado (403), estado inválido 400, planificar sin fechas
    422, completar sin empezar 422; planificado, en curso;
  - horas: solo lectura 403, ajeno 404, no asignado 422, 25 horas 400, más de 24 en el día 422;
    corrección (aprobar no es imputar: 403), aprobación (imputar no es aprobar: 403), aprobada
    definitiva (422 al corregir y al retirar);
  - completar: con horas pendientes 422, retirada del borrador (204), liberación de la persona e
    imputación posterior 422, proyecto con partes abiertas 422, cancelación con motivo, tarea
    completada con 12 horas y 260 €;
  - trabajo cerrado: sin horas, sin cambios de plan y sin partes nuevas (422); proyecto en pausa,
    reanudado y completado;
  - cuatro eventos publicados; ficha con asignaciones e historial; búsquedas por padre, clase y
    estado; horas del trabajo, pendientes y parte de la persona (periodo obligatorio: 400).
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL:
  - trabajo de ida y vuelta con fechas opcionales, asignaciones e historial en orden;
  - imputación, límite diario sobre las filas, corrección (versión 2), retirada y aprobación;
  - completar con las horas y el coste de lo aprobado; cancelación;
  - búsquedas por padre, estado, cliente y fechas; parte de la persona; bandeja de salida.

## Pendiente

- Fase 2:
  - facturar las horas facturables desde el trabajo (precio de venta por hora o importe cerrado);
  - coste del trabajo en contabilidad analítica y cruce de horas con Nóminas;
  - autoservicio de horas y parte semanal aprobado en bloque;
  - dependencias entre tareas, hitos y entregables;
  - materiales consumidos (Inventario) y reserva de activos (Activos) con su coste;
  - mantenimiento preventivo periódico sobre los activos;
  - estadísticas y desviación entre lo estimado y lo real;
  - documento numerado del trabajo cuando exista el contexto de Documentos.
- Importar de C#: nada (cero filas de trabajos y de horas).
