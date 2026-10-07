# Contexto Historial

Port del subdominio `Audit` de C# (`ErpKernel.Application/Subdominios/Audit`) al contexto
`contexts/audit`.

Responde a **quién cambió qué y cuándo** en cualquier agregado del sistema: la pestaña «Historial»
de una factura, un activo, un pedido… El contexto no guarda nada: lee el registro de auditoría que
cada contexto ya escribe, en la misma transacción que el cambio.

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

- **Solo lectura.** `AuditTrailApplicationService` lee la tabla única `transversal.audit_log` y
  convierte el JSON de cambios en pares «campo, valor anterior, valor nuevo». Dos rutas `GET`.
- **Una tabla para todo el sistema**, que todos los subdominios comparten.
- **Sin permisos:** las rutas solo exigen autenticación; no hay código de permiso ni se comprueba
  que quien pregunta pueda ver la entidad cuyo historial pide.
- **`/api/domain-events`**: una ruta que lista la tabla `domain_event_log`.
- **La interfaz lo usa:** `audit-trail.service.ts` alimenta la pestaña de historial.
- 6 pruebas.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `AuditTrailApplicationService` (campo, antes, después) | 4 | 3 | 3 | 3 | **68** | Modificar → consulta `Trail` por tipo e identidad del agregado: versión, operación, fecha, autor, canal, importación, **campos cambiados con su valor anterior y nuevo**, y eventos emitidos |
| 2 | Tabla única `transversal.audit_log` | 3 | 3 | 2 | 2 | **53** | Sustituido → cada contexto guarda su propio registro (`<contexto>_audit_log`), en su transacción. El Historial los lee a través de un **registro de tipos** que rellena el anfitrión |
| 3 | Lectura sin control por empresa | 2 | 1 | 1 | 3 | **34** | Sustituido → **ve el historial quien puede ver el agregado**: cada tipo se registra con un guarda, que es la consulta con la que el contexto dueño ya carga el agregado (con su permiso y su ámbito). Además hace falta `Audit.Trail.Read` |
| 4 | `/api/domain-events` (listado de la tabla de eventos) | 2 | 2 | 2 | 3 | **43** | Retirar → los eventos emitidos por cada cambio ya van en su entrada del historial; las bandejas de salida son técnicas |
| 5 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Audit.Trail.Read` |

## Diseño

```
contexts/audit/
├── application/     # Registry (tipo → registro de auditoría + guarda), consultas Trail y Types
└── module.go        # composición, Register y rutas /api/audit/...
```

- **Sin dominio ni tablas.** El contexto no depende de ningún otro: el anfitrión declara, para
  cada tipo de agregado, dónde está su registro y cómo saber si quien pregunta puede verlo:

  ```go
  history := audit.Compose().
      Register("assets.asset", assetsModule.Audit,
          aapp.Seeing(adomain.ParseAssetID, func(id adomain.AssetID) sapp.GetAsset { return sapp.GetAsset{ID: id} },
              assetsModule.Service.GetAsset)).
      Register("modules.feature", modulesModule.Audit, nil) // sin empresa: solo administradores globales
  ```

- **El guarda es la consulta del dueño.** `Seeing` ejecuta la consulta con la que el contexto
  carga el agregado y descarta el resultado. Así el Historial devuelve exactamente lo mismo que
  el dueño: 404 fuera de las empresas del usuario, 403 sin el permiso de lectura de ese contexto.
- **Dos llaves:** hace falta `Audit.Trail.Read` *y* poder ver el agregado. Ver una factura no da
  derecho a ver quién la tocó, y poder leer historiales no da acceso a lo que no se puede ver.
- **Tipos sin guarda** (los que no pertenecen a una empresa, como el catálogo de Módulos): solo
  un administrador global lee su historial.
- **Rutas:** `GET /api/audit/trail/{tipo}/{id}` y `GET /api/audit/types` (los tipos con historial,
  su contexto y si están restringidos).

## Decisiones propuestas (pendientes de confirmar)

1. **El Historial no guarda nada**: lee los registros de auditoría que cada contexto ya escribe
   en su transacción. Sugerencia: sí; así no hay una segunda copia que pueda discrepar ni una
   tabla compartida entre contextos.
2. **Ve el historial quien puede ver el agregado**, y además tiene `Audit.Trail.Read`.
   Sugerencia: sí; cierra el hueco del C#, donde bastaba estar autenticado y conocer el
   identificador.
3. **Los tipos con historial los declara el anfitrión**, uno a uno, con su guarda. Un tipo sin
   registrar no tiene historial visible, y un tipo sin guarda solo lo lee un administrador
   global. Sugerencia: sí; es explícito y no acopla el Historial a ningún contexto. Al montar el
   servidor hay que registrar los tipos que se quieran exponer.
4. **Se retira el listado de eventos de dominio** (`/api/domain-events`). Los eventos de cada
   cambio ya aparecen en su entrada. Sugerencia: sí.
5. **Fase 1 sin búsqueda transversal** («todo lo que hizo Ana ayer», «todo lo que cambió en esta
   empresa»). Hoy el registro solo se consulta por agregado. Sugerencia: sí; esa búsqueda
   necesita que el registro guarde la empresa de cada entrada y un índice por autor y fecha, y es
   un cambio del framework que conviene decidir aparte.

## Validación

- **Extremo a extremo** (Activos, Módulos e Historial sobre el mismo backend, en memoria y en
  SQLite migrada):
  - un activo dado de alta, amortizado un trimestre y vendido: tres entradas en orden de versión,
    con el cambio de la amortización acumulada (de `0` a `524.19`), el del estado (de
    `in-service` a `disposed`) y los eventos de cada cambio;
  - ver el activo no es leer su historial (403), ni leer historiales es ver el activo (403);
    ajeno 404; tipo desconocido 400; identificador inválido 400; activo inexistente 404;
  - catálogo de Módulos: 403 para un usuario de empresa; para el administrador global, dos
    entradas con el cambio de nombre y de retirada; agregado sin cambios, lista vacía;
  - tipos con historial: sin permiso 403; los dos tipos, con su contexto y su restricción;
  - la capa de aplicación no importa ningún otro contexto.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: el historial leído de los registros
  SQL de dos contextos, con los valores anteriores y nuevos tal como los devuelve cada motor
  (texto y booleanos), los eventos, el miembro de la empresa, el de otra empresa (404) y el
  catálogo restringido.

## Pendiente

- Registrar en el servidor los tipos de agregado de cada contexto con su guarda.
- Búsqueda transversal por autor, empresa y fechas (decisión 5).
- Paginación del historial de agregados con muchos cambios.
- Historial de los hijos de un agregado con nombre propio (líneas de una factura), hoy resumidos
  en la instantánea del agregado.
- Importar de C#: el contenido de `transversal.audit_log`, si se quiere conservar el historial
  anterior a la migración.
