# Contexto Módulos

Port del subdominio `Sectorial` de C# (`ErpDetail.Domain/Subdominios/Sectorial`) al contexto
`contexts/modules`.

Responde a una sola pregunta: **qué tiene activado cada empresa**. Los módulos que ha contratado
(contabilidad, ventas, tesorería…), las capacidades transversales (financiera, logística…) y su
sector económico. La interfaz lo consulta para mostrar u ocultar menús, y los demás contextos
pueden preguntarlo por un puerto.

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

- **Tres modelos para lo mismo**, cada uno con su tabla, su proveedor y su servicio:
  - `ModuleType` + `OrganizationModuleActivation`: módulos contratados por organización (N:M);
  - `Capability` + `CapabilityAssignment`: capacidades por organización (N:M);
  - `InternalOrganizationSector`: un sector por organización (1:1), con código y nombre libres.
- **La activación sí tiene comportamiento:** una fila por organización y módulo que se activa y
  desactiva, con quién y cuándo. Activar lo ya activo «refresca» la fecha y el autor.
- **La regla «una fila activa» solo está en un índice filtrado**, que no todos los motores tienen.
- **«Sin ámbito» devuelve todo.** Cuando la petición no trae ámbito de organización, los
  proveedores devuelven el catálogo completo. Ya causó un error de fuga (el propio código lo
  documenta: un usuario sin acceso a la organización elegida veía las cinco capacidades), y el
  caso «ámbito vacío» se parcheó aparte.
- **Una capacidad se deriva de un rol.** `financial` no se lee de la tabla: se añade si alguna
  organización del ámbito tiene el rol `FinancialInstitution` de Parties.
- **El ámbito se resuelve en memoria:** se cargan todas las organizaciones internas y todos los
  roles de tercero para traducir identificadores, porque la consulta con `Contains` devolvía cero
  filas en Oracle.
- **Semillas:** 8 módulos (`contabilidad`, `rrhh`, `compras`, `ventas`, `tesoreria`,
  `inmovilizado`, `inventario`, `crm`). Ninguna capacidad ni sector.
- **Sin permisos:** unas 25 rutas solo con autenticación.
- **La interfaz depende de ello:** el servicio de activación de módulos y las reglas de
  visibilidad por capacidad están por toda la aplicación Angular.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `ModuleType` + `Capability` (dos catálogos gemelos) | 4 | 3 | 2 | 3 | **64** | Modificar → un solo agregado `Feature` con **clase** (`module`, `capability`, `sector`), código estable, nombre, descripción y **retirada**. Clase y código no cambian: son lo que referencian las empresas y la interfaz |
| 2 | `OrganizationModuleActivation` + `CapabilityAssignment` (dos tablas gemelas) | 4 | 3 | 3 | 3 | **68** | Modificar → un solo agregado `Activation` por empresa y característica: activa o no, quién y cuándo la activó y la desactivó, y notas. **Única por empresa y característica en los cinco motores** |
| 3 | `InternalOrganizationSector` (1:1, código y nombre libres) | 3 | 3 | 2 | 3 | **56** | Modificar → el sector es una clase más del catálogo, **exclusiva**: activar uno desactiva el que hubiera |
| 4 | Tres proveedores (`IModuleActivationProvider`, `ICapabilityProvider`, `ISectorProfileProvider`) | 4 | 2 | 2 | 3 | **59** | Modificar → un puerto `contracts.Features` (`Has`, `Of`) y la consulta «lo mío» |
| 5 | «Sin ámbito» devuelve el catálogo completo | 2 | 1 | 1 | 3 | **34** | Sustituido → **sin empresas en el ámbito no se ve nada**; solo un administrador global ve todo lo ofrecido |
| 6 | `financial` derivada del rol `FinancialInstitution` | 2 | 2 | 1 | 3 | **39** | Retirar → se activa como las demás. Si se quiere automática, la activará un consumidor del evento de roles de Parties |
| 7 | Resolución del ámbito cargando todas las organizaciones en memoria | 1 | 2 | 1 | 2 | **28** | Sustituido → el ámbito ya llega resuelto en el contexto de autorización |
| 8 | Activar lo activo «refresca» fecha y autor | 2 | 2 | 2 | 3 | **43** | Retirar → activar lo activo no cambia nada (solo las notas, si se dan), y no publica evento |
| 9 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Modules.Catalog.Read/Update` y `Modules.Activation.Read/Update`; el catálogo solo lo cambia un **administrador global** |

## Diseño

```
contexts/modules/
├── domain/          # Feature (catálogo), Activation (por empresa), semilla
├── contracts/       # puerto Features y lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito
├── infrastructure/  # esquema mod_* de 5 motores, mapeos
└── module.go        # composición, EnsureCatalog y rutas /api/modules/...
```

- **Catálogo**, igual para todas las empresas. Arranca con los ocho módulos de C# (con sus
  códigos, que la interfaz ya usa) y cuatro capacidades (`financial`, `logistics`,
  `manufacturing`, `consulting`). `EnsureCatalog` añade al arrancar las entradas que falten y no
  toca las que existan.
- **Retirar** una característica no la quita a quien la tiene: sigue activa donde lo estaba, pero
  ya no se activa en ninguna empresa nueva (ni se reactiva).
- **Activar y desactivar** son idempotentes: repetirlo no cambia la versión ni publica nada.
- **«Lo mío»** (`GET /api/modules/current`), sin permiso propio porque es de uno mismo:
  - un usuario ve la **unión** de lo que tienen activo sus empresas;
  - sin empresas, nada;
  - un administrador global, todo lo que se ofrece.
- **Puerto para otros contextos:** `Features.Has(empresa, clase, código)` y `Features.Of(empresa,
  clase)`.
- **Lenguaje publicado:** `modules.feature-activated.v1` y `modules.feature-deactivated.v1`.
- **Tablas:** `mod_features`, `mod_activations`, las bandejas de salida y la auditoría.

## Decisiones (aprobadas por Javier el 2026-10-07)

1. **Un solo catálogo y una sola activación** para módulos, capacidades y sectores, distinguidos
   por la clase. Sugerencia: sí; en C# eran el mismo modelo escrito tres veces.
2. **Se conservan los códigos de los módulos de C#** (`contabilidad`, `rrhh`, `compras`, `ventas`,
   `tesoreria`, `inmovilizado`, `inventario`, `crm`), en castellano, porque la interfaz ya los usa.
   Sugerencia: sí; renombrarlos obligaría a tocar todas las reglas de visibilidad.
3. **Sin empresas en el ámbito no se ve nada.** Se retira el «sin ámbito, todo». Sugerencia: sí;
   era la causa de la fuga que el propio C# tuvo que parchear.
4. ~~`financial` deja de derivarse del rol de Parties y se activa como cualquier otra
   capacidad.~~ **Cambiada por Javier el 2026-10-08: la capacidad se deriva del rol.** Una empresa
   la tiene si en Parties es organización interna y además entidad financiera, que es la misma
   regla que abre el sectorial financiero ([CUENTAS-CLIENTES.md](CUENTAS-CLIENTES.md)): el menú y
   el contexto dicen lo mismo.
   - Módulos no conoce Parties: el anfitrión le pasa una `Derivation` (qué funcionalidades se
     derivan, cuáles tiene una empresa y qué empresas tienen una).
   - Lo derivado **no se activa ni se desactiva a mano** (422 `modules.derived`).
   - Una activación a mano que ya estuviera guardada deja de contar: manda el rol.
   - `Current`, `Features.Has`, `Features.Of`, la lista de una empresa (marcada como `derived`) y
     la lista de empresas con la funcionalidad responden todas igual.
5. **El catálogo solo lo cambia un administrador global**; las activaciones, quien tenga el
   permiso en esa empresa. Sugerencia: sí.
6. **Este contexto informa, no bloquea.** Hoy ningún contexto rechaza una operación porque el
   módulo esté apagado: la interfaz oculta, y el puerto `Features` queda disponible. Sugerencia:
   sí en esta fase; convertirlo en un control (403 si la empresa no tiene el módulo) es una
   decisión de producto y de licencias que conviene tomar aparte, contexto a contexto.
7. **La correspondencia entre módulos y contextos queda fuera** (qué rutas enciende cada módulo):
   la mantiene la interfaz. Sugerencia: sí mientras valga la decisión 6.

## Validación

- **Dominio:** códigos válidos e inválidos, clase, nombre; cambiar y retirar sin tocar el código;
  activación apagada al nacer; activar, activar lo activo (nada), solo notas, desactivar,
  reactivar (limpia la desactivación); un evento por cambio real; el sector es exclusivo.
- **Extremo a extremo** (Módulos sobre el backend, en memoria y en SQLite migrada):
  - semilla: 12 entradas la primera vez, 0 la segunda;
  - catálogo: sin permiso 403, clase inválida 400, ordenado por clase y código;
  - solo un administrador global lo cambia (403 aun con el permiso); repetido 422; código
    inválido 400;
  - activar: solo lectura 403, ajeno 404, clase inválida 400, fuera de catálogo 422; activar lo
    activo no cambia la versión; las notas sí;
  - un sector sustituye al anterior;
  - «lo mío»: la unión de la empresa; nada sin empresas; todo para el administrador global;
  - desactivar: nunca lo tuvo 422; idempotente; reactivar conserva las notas;
  - retirada: no se activa (422), sigue activa donde lo estaba y, una vez apagada, no vuelve;
    catálogo con y sin retiradas;
  - activaciones de la empresa (ajeno 404), empresas con una característica, puerto `Features`;
  - nueve eventos publicados.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: semilla una sola vez, código único,
  catálogo ordenado, activaciones con sus fechas de ida y vuelta, sector único, retirada, unión
  de dos empresas, administrador global, puerto y bandeja de salida.

## Pendiente

- Decidir si el módulo apagado bloquea (decisión 6) y, entonces, la correspondencia entre módulos
  y contextos.
- Activación automática de capacidades a partir de roles de Parties.
- Vigencia de las activaciones (desde y hasta) y periodos de prueba, si se licencia por fechas.
- Importar de C#: las activaciones de módulos por organización, si las hay en producción (la
  semilla no trae ninguna).
