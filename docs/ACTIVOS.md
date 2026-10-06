# Contexto Activos (inmovilizado)

Port del subdominio `Assets` de C# (`ErpKernel.Domain/Subdominios/Assets`) al contexto
`contexts/assets`, más sus efectos en Contabilidad y Compras.

La fase 1 es el **registro de inmovilizado con su amortización lineal y su baja**: lo que hace
falta para que el balance refleje lo que la empresa tiene y la cuenta de resultados lo que se
desgasta cada mes.

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

- **Un inmovilizado sin importes.** `FixedAsset` tiene nombre, tipo, propietario (`Party` y
  `RoleType`), unidad, fecha de adquisición, fechas de revisión y capacidad de producción. **No
  tiene coste, valor residual ni vida útil**, así que no se puede amortizar ni saber cuánto vale.
- **La amortización no se calcula.**
  - `DepreciationMethod` es un nombre y una `Formula` de **texto libre** que ningún código
    interpreta.
  - `FixedAssetDepreciationMethod` enlaza activo y método con una fecha de caducidad, nada más.
  - `Depreciation` hereda de `InternalAccountingTransaction` (una cabecera contable) y solo añade
    el activo: **sin importe, sin periodo y sin líneas**. Nada la crea a partir de un método.
- **Sin baja.** No hay venta, baja por siniestro ni resultado de la enajenación.
- **Subtipos vacíos.** `Equipment`, `Vehicle`, `Property` y `OtherFixedAsset` son cuatro tablas
  con solo la clave del activo; `FixedAssetType` es un catálogo de nombres sin semilla.
- **Asignaciones.** `PartyFixedAssetAssignment` (quién usa el activo, con estado y comentarios) y
  `WorkEffortFixedAssetAssignment` (qué activo usa una tarea) existen como tablas con CRUD.
- **Sin flujo:** 31 rutas CRUD en `AssetsEndpoints.cs`, sin permisos (solo autenticación), sin
  semillas ni pruebas, y sin relación con las facturas de compra ni con la contabilidad.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `FixedAsset` (sin coste ni vida útil) | 4 | 1 | 2 | 3 | **54** | Modificar → agregado `Asset` por empresa: código único, clase, nombre, número de serie, ubicación (Instalaciones), proveedor y factura de compra, fecha de adquisición y de **puesta en servicio**, **coste, valor residual y vida útil en meses**. Coste, residual y vida útil no cambian después del alta: con ellos se calcularon las cuotas ya cargadas |
| 2 | `FixedAssetType` + `Equipment`, `Vehicle`, `Property`, `OtherFixedAsset` | 2 | 2 | 1 | 2 | **36** | Sustituido → `Class` cerrada (`land`, `buildings`, `machinery`, `tooling`, `furniture`, `computers`, `vehicles`, `software`, `other`). **El terreno no se amortiza** |
| 3 | `DepreciationMethod` (fórmula de texto libre) + `FixedAssetDepreciationMethod` | 2 | 1 | 1 | 2 | **31** | Sustituido → amortización **lineal** calculada por el dominio: cuota mensual = (coste − residual) / meses; el **primer mes se prorratea por días** desde la puesta en servicio y la última cuota absorbe el redondeo. Los métodos degresivos van a la fase 2 |
| 4 | `Depreciation` (cabecera contable sin importe) | 3 | 1 | 1 | 2 | **39** | Sustituido → `Charge` (mes e importe) dentro del activo, y evento `assets.depreciation-charged.v1`; el asiento lo hace **Contabilidad**, no Activos |
| 5 | Baja del inmovilizado | — | — | — | — | — | Nuevo → `Dispose` por **venta** (con importe) o **baja** (sin él): amortiza antes hasta el mes anterior y calcula el resultado (importe − valor neto contable). Evento `assets.asset-disposed.v1` |
| 6 | Ejecución de la amortización | — | — | — | — | — | Nuevo → `RunDepreciation` por empresa y mes: carga a cada activo **todos los meses atrasados hasta el pedido**, una sola vez cada uno; repetirla no carga nada |
| 7 | `PartyFixedAssetAssignment` / `WorkEffortFixedAssetAssignment` | 2 | 2 | 3 | 3 | **47** | Aplazado → fase 2 (quién usa el activo), con RRHH y, cuando exista, el contexto de trabajos |
| 8 | Fechas de revisión y capacidad de producción | 1 | 2 | 2 | 3 | **35** | Retirar → es mantenimiento, no inmovilizado; volverá con un contexto de mantenimiento si hace falta |
| 9 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Assets.Asset.Read/Update`, `Assets.Asset.Dispose` y `Assets.Depreciation.Run` (**llevar el registro, amortizar y dar de baja están separados**) |

## Diseño

```
contexts/assets/
├── domain/          # Asset (+Details, Charge, Period), clases, amortización lineal, baja
├── contracts/       # lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito
├── infrastructure/  # esquema ast_* de 5 motores, mapeo
└── module.go        # composición y rutas /api/assets/...
```

- **Amortización lineal, mes a mes.** Ejemplo de las pruebas: furgoneta de 12 000 € con 2 000 € de
  valor residual y 48 meses, en servicio el 16 de enero de 2026:
  - cuota mensual 208,33 €;
  - enero 107,53 € (16 de 31 días);
  - el mes 49 (enero de 2030) carga los 100,96 € que quedan: en total, exactamente 10 000 €.
- **Los meses no se saltan ni se repiten.** `DepreciateThrough` carga en orden desde el último mes
  cargado; un activo dado de baja, totalmente amortizado o un terreno no carga nada.
- **Baja.** No se puede dar de baja en un mes ya amortizado (422): antes de la baja se amortiza
  hasta el mes anterior. Vendida la furgoneta el 10 de julio por 11 000 €: amortización acumulada
  1 149,18 €, valor neto 10 850,82 €, **beneficio de 149,18 €**.
- **Sin escrituras entre contextos.** Activos publica y Contabilidad asienta con seis roles nuevos
  en el perfil del libro:

  | Hecho | Debe | Haber |
  |---|---|---|
  | Cuota de un mes (último día del mes) | `depreciation-expense` (681) | `accumulated-depreciation` (281) |
  | Baja o venta (fecha de la baja) | `accumulated-depreciation` (281) por la acumulada, `asset-sale-receivable` (543) por el importe de la venta, `asset-disposal-loss` (671) si hay pérdida | `fixed-assets` (21x) por el coste, `asset-disposal-gain` (771) si hay beneficio |

  Cada mes de cada activo se asienta una sola vez (clave `activo|periodo`).
- **La compra de un inmovilizado no es un gasto.** Compras tiene una categoría nueva,
  `fixed-asset`, que Contabilidad carga al rol `fixed-assets` (21x) en lugar de a una cuenta del
  grupo 6. El alta en el registro de Activos es aparte: guarda el proveedor y el número de la
  factura como referencia.
- **Consultas:** ficha con sus cuotas, búsqueda por empresa, clase y «en servicio», y **libro de
  inmovilizado** de la empresa (coste, amortización acumulada y valor neto contable).
- **Tablas:** `ast_assets` (+ `ast_asset_charges`), las bandejas de salida y la auditoría.

## Decisiones propuestas (pendientes de confirmar)

1. **La fase 1 de Activos es el registro con amortización lineal mensual y baja.** Los métodos
   degresivos, los cambios de vida útil, las mejoras que aumentan el coste y el deterioro van a la
   fase 2. Sugerencia: sí; el lineal es el método habitual y el único que admite sin más la tabla
   fiscal de amortización.
2. **Coste, valor residual y vida útil no se modifican después del alta.** Un error se corrige
   dando de baja el activo y registrándolo de nuevo. Sugerencia: sí mientras no exista la
   reestimación de la fase 2; así las cuotas ya asentadas nunca dejan de cuadrar con la ficha.
3. **El primer mes se prorratea por días desde la puesta en servicio y el mes de la baja no se
   amortiza** (se amortiza hasta el mes anterior). Sugerencia: sí; es el criterio más extendido y
   evita cuotas de un día.
4. **Un solo juego de cuentas de inmovilizado por empresa** (roles `fixed-assets`, 281 y 681), sin
   distinguir por clase. Las cuentas por clase (211 construcciones, 217 equipos informáticos, 218
   elementos de transporte, 206 aplicaciones informáticas y sus 280x/281x y 680/681) van a la fase
   2. Sugerencia: sí para empezar; si prefieres las cuentas por clase desde ya, es añadir un mapa
   por clase al perfil del libro, como el de los códigos de IVA.
5. **El alta en el registro es manual y separada de la factura de compra.** La factura con
   categoría `fixed-asset` lleva el coste al 21x; el activo se da de alta después con la
   referencia de la factura. Sugerencia: sí en la fase 1; el alta automática desde la factura
   (una línea puede ser varios activos, o un activo varias facturas) se decide en la fase 2.
6. **La venta de un inmovilizado asienta el importe neto contra un deudor (543), sin IVA.** La
   factura de la venta, con su IVA repercutido, no se emite todavía desde Facturación: si se
   emitiera hoy, iría a ingresos (700) y duplicaría el importe. Sugerencia: sí en la fase 1, y en
   la fase 2 una factura de Facturación marcada como «venta de inmovilizado» que sustituya al 543.

## Validación

- **Dominio:**
  - código, clase, nombre, fechas (puesta en servicio no anterior a la adquisición), coste
    positivo en céntimos, residual menor que el coste, vida útil de 1 a 1 200 meses (cero en el
    terreno);
  - lineal con prorrata del primer mes, cuota única por mes, última cuota con el redondeo, vida
    completa que suma exactamente lo amortizable;
  - el terreno no se amortiza;
  - baja: fecha, tipo, importe (venta con importe, baja sin él), una sola vez, mes ya amortizado;
    beneficio de la venta y pérdida de la baja.
- **Extremo a extremo** (Parties, Fiscal, Compras, Activos y Contabilidad sobre el mismo backend,
  con un único broker, en memoria y en SQLite migrada):
  - factura de compra con categoría `fixed-asset`: 21x, IVA soportado y proveedores;
  - alta: solo lectura 403, ajeno 404, clase inválida 400, código repetido 422; furgoneta,
    ordenador y terreno;
  - amortización del primer trimestre: llevar el registro no es amortizar (403), mes 13 (400), dos
    activos y cinco cuotas por 624,19 €; repetida no carga nada; 681 y 281 en Contabilidad; libro
    de inmovilizado;
  - baja del ordenador (llevar el registro no es dar de baja: 403; mes ya amortizado: 422) con
    pérdida de 1 100 €, y venta de la furgoneta con beneficio de 149,18 €; una sola vez (422);
  - Contabilidad: 21x y 281 a cero, 681, 543, 671 y 771, con las sumas y saldos cuadrados;
  - fin de año sin nada que amortizar; cambio de nombre (amortizar no es llevar el registro: 403);
    ficha con sus seis cuotas; búsquedas por «en servicio» y por clase; ajeno 404 y lista vacía.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, con los cinco contextos migrados en
  la misma base:
  - compra asentada como inversión por `accounting_inbox`;
  - activo de ida y vuelta con sus cuotas en orden, código único;
  - amortización (una vez por mes), baja y venta de ida y vuelta, con sus asientos;
  - cambio de nombre (versión 2), libro y búsquedas.

## Pendiente

- Fase 2:
  - métodos degresivos y por unidades de producción; tabla fiscal de coeficientes y diferencias
    entre amortización contable y fiscal;
  - reestimación de la vida útil, mejoras y ampliaciones, deterioro;
  - cuentas por clase de activo;
  - alta desde la factura de compra y venta facturada con IVA;
  - inmovilizado en curso, subvenciones de capital y leasing;
  - asignación del activo a personas y trabajos.
- Ejecutar la amortización de forma programada al cierre de cada mes.
- Importar de C#: `FixedAsset` solo aporta nombre, tipo y fecha de adquisición; el coste y la vida
  útil hay que cargarlos a mano o desde la contabilidad anterior.
