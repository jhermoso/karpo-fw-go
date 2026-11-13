# Contexto Instalaciones

Decisiones aprobadas por Javier el 2026-09-27 (ver [PARTIES.md](PARTIES.md), sección 2):

1. `PartyFacility` + `FacilityRoleType` se quedan en **Parties**, que referencia la instalación por
   su Id.
2. La instalación tiene **su propia ubicación**: dirección con Ids de Geografía, teléfono y correo
   electrónico. Ya no toma prestados los `ContactMechanism` de Party.
3. `WorkCenter` de RRHH referenciará `FacilityID`, en lugar de duplicar la dirección como texto.

## Evaluación del C#

| # | Pieza C# | Decisión → Go |
|---|---|---|
| 1 | `Facility` (TPH: Warehouse, Plant, Building, Office, Floor, Room) | Se mantiene. `Facility`, con el tipo como dato; sin subclases, porque no tenían campos propios |
| 2 | `PartOfFacility` (autorreferencia) | Se mantiene, con reglas nuevas: la misma organización, sin ciclos y un máximo de 10 niveles |
| 3 | `FacilityContactMechanism` → `ContactMechanism` de Party | Se retira. Se sustituye por el objeto valor `Location`, validado con Geografía (`AddressChecker`) mediante un puerto propio y un adaptador |
| 4 | `ValidateContactRequirementsAsync` («una oficina necesita teléfono y dirección»), que **solo se comprobaba si se llamaba al endpoint** | Pasa a ser invariante del agregado: `FacilityType.RequiresContact` (oficina y oficina de cambio) se exige al registrar y al reubicar |
| 5 | Ámbito por organización calculado recorriendo `PartyFacility` | **Organización propietaria** en la instalación (`Owner`): visible solo en su ámbito (404 uniforme fuera de él) y editable con acceso completo |
| 6 | `PartyFacility` (party, instalación, rol, `ExpirationDate`) | Se queda en Parties como hijo de `Party` (`FacilityRole`, con vigencia y sin solapamientos del mismo rol en la misma instalación). La instalación se valida con el directorio de Instalaciones: existe, está activa y es de una organización del ámbito |
| 7 | Consultas directas `ctx.Set<Facility>()` desde FinancialDetail; joins desde Product, Shipments y WorkEffort | Se sustituyen por `contracts.Directory.Resolve` (por lotes) y el lenguaje publicado (`facility-registered`, `-renamed`, `-relocated` y `-activation-changed` v1) para las cachés de nombres |
| 8 | Catálogo de código con 8 tipos y 7 roles; la semilla tenía 9 y 9 («Currency Exchange Office», «Own/Third-Party Operator») | Se unifican los dos catálogos, con los mismos GUID |
| 9 | Las 20 instalaciones de la semilla | No se siembran: son datos del cliente (importación), no referencia |

## Diseño

```
contexts/facilities/
├── domain/          # Facility, Location, Address (GeoRef), FacilityType, eventos, especificaciones
├── contracts/       # Directory + lenguaje publicado v1
├── application/     # casos de uso con permisos (Facilities.Facility.Read/Create/Update) y ámbito
├── infrastructure/  # esquema fac_* de 5 motores, semilla de tipos, fábricas, adaptador de Geografía
└── module.go        # composición y rutas /api/facilities, /api/catalogs/facility-types
```

En Parties:

- `FacilityRole` y el catálogo `facility_role_types` (migraciones 8 y 9);
- `AssignFacilityRole` y `EndFacilityRole`, y `GET /api/parties?facility=…` (la plantilla de una
  oficina);
- el puerto `FacilityDirectory`, con el adaptador `infrastructure.FacilitiesDirectory` sobre el
  directorio de Instalaciones;
- eventos nuevos: `party-facility-role-assigned.v1` y `-ended.v1`. RRHH obtendrá con ellos el
  centro de trabajo de cada empleado.

## Validación

- **Dominio:** contacto obligatorio al registrar y al reubicar, misma organización en la jerarquía,
  sin ciclos, área no negativa, e instalación inactiva no editable.
- **Extremo a extremo (tres contextos sobre el mismo backend, en memoria y en SQLite migrada):**
  - oficina sin teléfono: 422;
  - código postal de otro municipio: 400;
  - oficina en el ámbito de otra organización: 404;
  - jerarquía edificio → planta → sala, y el ciclo se rechaza;
  - el ajeno no ve nada;
  - Ana tiene como centro de trabajo la oficina y aparece en la plantilla;
  - rol en una instalación de otra organización: 404; en una inactiva: 422;
  - cambio de nombre publicado y consumido por una caché de Product con inbox, y auditoría con el
    actor.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con los tres contextos migrados en la
  misma base: área decimal, correo, `part_of` anulable en SQL, rol en instalación, plantilla y
  directorio.

## Pendiente

- ~~El `WorkCenter` de RRHH (decisión 3)~~: hecho en [RRHH.md](RRHH.md). Referencia `FacilityID`,
  validado con el directorio de Instalaciones (existe, está activa y es del empleador).
- La importación de las instalaciones de cada cliente (Sage, Personio) debe crear las instalaciones
  y los roles con estos casos de uso.

## Dirección: las piezas españolas (2026-10-12)

La dirección de una instalación lleva, como la de una party, las piezas que los formularios
españoles piden por separado: `number`, `numberQualifier`, `block`, `portal`, `staircase`,
`floor` y `door`. Todas opcionales, de diez caracteres como mucho, junto al nombre de la vía
(`line1`).

*Ejemplo:* la oficina de «Puerta del Sol 1, planta 3, puerta B» se registra con
`"line1": "Puerta del Sol", "number": "1", "floor": "3", "door": "B"`.

- Son el mismo objeto valor que en Parties (`vocab.Premises`); cada contexto sigue teniendo su
  propia dirección, como se decidió el 2026-09-27.
- Migración 4: siete columnas `addr_*` en `fac_facilities`. Ninguna ruta ni evento cambia.
- Explicación de los campos y de por qué son esos: [PARTIES.md](PARTIES.md) §7.

## El contrato de mantenimiento (front; añadido el 2026-11-13)

Las instalaciones se mantienen también por el **contrato de mantenimiento**, el que lee el motor de
Angular de Karpo C# (el contrato `crud` del lenguaje de Theros), bajo `/api/v1/facilities`. Es otra
puerta a los mismos casos de uso; la configuración de su pantalla está en
`front/angular/config/entities/facilities/facility.config.json` (ver front/README.md). Es el mismo
contrato que el de Productos (docs/PRODUCTOS.md).

| Operación | Ruta | Qué hace |
|---|---|---|
| Lista | `GET /api/v1/facilities?page&pageSize&sortBy&sortOrder&search&name&organization&type&partOf&isActive`, o `POST /api/v1/facilities/search` con `{filter, pagination: {pageNumber, pageSize, orderBy, ascending}}` | la página del motor: `{items, total, totalCount, pageNumber, pageSize, totalPages}`. Orden por `name` (por omisión; una instalación no tiene código), con la identidad para deshacer empates; otro, 400. `search` y `name` buscan en el nombre; `isActive=true` deja sólo las activas. Un filtro que no es de la lista se ignora |
| Lectura | `GET /api/v1/facilities/{id}` | la instalación |
| Alta | `POST /api/v1/facilities` | 201 con `Location`; lo que sobra en el cuerpo se ignora. Una instalación nace activa: `active: false` en el alta es 400 (se desactiva después) |
| Cambio | `PUT /api/v1/facilities/{id}` con lo leído y su `version` | 200; lo que el cuerpo no trae se queda como está. Sin `version`, 400; con una que ya no es la suya, 409. Se traduce a los casos de uso de siempre, sólo los de los campos que cambian: activar, renombrar (`name`), reubicar (`location`, que viaja entera; vacía o null es «no viene» y se queda la que había), mover (`partOf`) y desactivar, en ese orden y cada uno con la versión que dejó el anterior. Todo el cambio es una sola unidad de trabajo: si un paso falla, no queda hecho ninguno. `organization`, `type`, `description` y `areaM2` no los cambia ningún caso de uso: cambiarlos es 400 (se comparan por su valor: identidades sin distinguir mayúsculas, superficie como número) |
| Baja | `DELETE /api/v1/facilities/{id}` | 204: **desactiva** la instalación (no se borra: otros contextos la citan); repetirla vuelve a dar 204 sin versión nueva, pero exige el mismo permiso de escritura (403 sin él) |

- **El objeto:** `id`, `organization`, `type`, `typeName`, `name`, `description`, `partOf`,
  `areaM2`, `location` (la ubicación, como en `/api/facilities`), `active`, `version`.
- **La versión:** los casos de uso de cambio aceptan la versión leída (`Version`, fuera del JSON de
  `/api/facilities`) y dan conflicto si la instalación ya está en otra, como `ChangeProduct`; con
  ella el conflicto es definitivo y no se reintenta. Activar o desactivar la que ya lo está no
  guarda nada.
- **El error:** RFC 9457 con `errors` como diccionario (campo → mensajes) y `errorCodes`, en el
  mismo orden; en un 422, `code`. El resto de la API de Instalaciones sigue con la lista del framework.
- **Permisos y ámbito:** los de siempre (`Facilities.Facility.Read`, `…Create` y `…Update`).
- **Prueba:** `TestFacilities_MaintenanceContract_MemoryThenSQLite`.
