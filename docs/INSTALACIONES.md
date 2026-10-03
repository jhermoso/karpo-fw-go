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
| 7 | Consultas directas `ctx.Set<Facility>()` desde Maccorp; joins desde Product, Shipments y WorkEffort | Se sustituyen por `contracts.Directory.Resolve` (por lotes) y el lenguaje publicado (`facility-registered`, `-renamed`, `-relocated` y `-activation-changed` v1) para las cachés de nombres |
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
