# Contexto Documentos

Port del subdominio `Documents` de C# (`ErpKernel.Domain/Subdominios/Documents`) al contexto
`contexts/documents`.

La fase 1 es el **registro de documentos emitidos**: una entrada por cada factura, pedido,
albarán, factura recibida, nómina y modelo fiscal que emiten los demás contextos, con una búsqueda
única y el **rastro** que une pedido → albarán → factura → rectificativa.

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

- **Un «documento» es solo una fila numerada** que ancla un hecho de negocio (`FactId` y
  `FactTypeCode`) a una serie, un número y una fecha. No hay archivo, contenido, hash, plantilla
  ni PDF en ninguna parte.
- **Documentos numeraba por todos.** Diez servicios (factura, rectificativa, pedido, presupuesto,
  pago, nómina, albarán, trabajo, modelo fiscal y línea de extracto) llamaban al puerto
  `IDocumentIssuer` con una serie elegida por quien llama, y recibían `{CÓDIGO}-{año}-{secuencia}`.
- **La numeración sin huecos no era segura.**
  - El contador de la serie debía protegerse con concurrencia optimista sobre `row_version`, pero
    nada incrementa esa columna (el propio código lo dice: «row_version es INERTE»). Dos emisiones
    simultáneas pueden leer el mismo número y emitirlo las dos.
  - No hay índice único sobre serie y número, y el índice único por hecho solo existe en el
    modelo de EF y en la migración de PostgreSQL: falta en los esquemas de SQL Server y Oracle.
  - El mismo hecho dos veces en un lote recibe dos números.
  - No se comprueba que el año de la fecha sea el de la serie, ni hay cambio de año.
- **Cuando Documentos no está desplegado**, emitir lanza una excepción: los publicadores por
  rebanada no registran el puerto a propósito.
- **Lo que no se usa:**
  - `DocumentRelationship` (cadenas presupuesto → pedido → factura, rectificaciones): modelado y
    sembrado con seis tipos, pero nada lo escribe;
  - `DocumentRole` (emisor, receptor, firmante…): altas sin validar y borrado físico;
  - seis de los siete estados del ciclo de vida (solo se usa `ISSUED`);
  - `Prefix` de la serie.
- **Otros errores:** se puede desactivar un documento legal ya emitido; los tipos de hecho se
  borran por la API; el evento `FactIssued` no tiene ningún consumidor.
- **Sin permisos:** 15 rutas solo con autenticación.
- **Sin datos:** 28 filas de catálogos; ninguna serie ni documento. Sin pantallas en Angular.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `IDocumentIssuer` + `DocumentSeries` (numeración central con contador sin protección) | 4 | 1 | 2 | 2 | **51** | Sustituido → **cada contexto numera lo suyo** dentro de su propia transacción: las series sin huecos de Facturación, el registro de Compras y los contadores de Pedidos ya existen, con control de versión real e índices únicos. Documentos no numera |
| 2 | `Document` + `FactReference` (fila que ancla un hecho) | 4 | 3 | 3 | 3 | **68** | Modificar → agregado `Document`: empresa, tipo y **hecho** (`Ref{Type, ID}`), número, referencia externa, fecha, tercero, total y anulación. **Una entrada por hecho**, con índice único en los cinco motores. Lo escriben solo los eventos |
| 3 | `DocumentFactType` (10 tipos sembrados) | 3 | 3 | 2 | 3 | **56** | Modificar → lista cerrada con los tipos que tienen un contexto emisor: `invoice`, `credit-note`, `order`, `delivery-note`, `received-invoice`, `payslip`, `tax-filing`. Presupuestos, pagos, extractos y trabajos entran cuando publiquen su evento |
| 4 | `DocumentRelationship` + 6 tipos (nadie lo escribe) | 3 | 1 | 3 | 3 | **50** | Modificar → el **origen** de cada entrada (`originates-from`, `rectifies`), rellenado desde los eventos, y la consulta del **rastro** completo. Era lo más útil del modelo y lo único que nunca funcionó |
| 5 | `IDocumentResolver` (número y fecha de un hecho) | 3 | 3 | 3 | 3 | **60** | Modificar → puerto `contracts.Register.ByFact` y ruta `GET /api/documents/by-fact/{type}/{factId}` |
| 6 | `DocumentLifecycleStateType` (7 estados, se usa uno) | 1 | 2 | 2 | 3 | **35** | Retirar → emitido o **anulado** (con motivo), según diga el contexto emisor |
| 7 | `DocumentRole` + 5 tipos | 1 | 1 | 2 | 3 | **30** | Retirar → el tercero del documento va en la entrada; la firma y la aprobación, si llegan, serán de un flujo propio |
| 8 | `FactIssuedDomainEvent` (sin consumidores) | 2 | 3 | 3 | 3 | **52** | Sustituido → los eventos de integración que cada contexto ya publica; Documentos es su consumidor |
| 9 | Desactivar documentos y borrar tipos por la API | 1 | 1 | 1 | 3 | **26** | Retirar → el registro no se edita a mano |
| 10 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Documents.Document.Read`, con el ámbito por empresa de todos los contextos |
| 11 | Archivo del documento (PDF, adjuntos) | — | — | — | — | — | No existía. Aplazado → fase 2, con un puerto de almacenamiento |

## Diseño

```
contexts/documents/
├── domain/          # Document (+Ref), tipos y relaciones
├── contracts/       # puerto Register (documento de un hecho)
├── application/     # consultas con permiso y ámbito; suscripciones que llevan el registro
├── infrastructure/  # esquema doc_* de 5 motores, mapeo, bandeja de entrada
└── module.go        # composición y rutas /api/documents/...
```

- **El registro se alimenta de eventos**, sin llamadas entre contextos y sin que nadie dependa de
  que Documentos esté desplegado:

  | Evento | Entrada | Origen |
  |---|---|---|
  | `orders.order-confirmed.v1` | `order` (fecha: el día de la confirmación) | — |
  | `orders.delivery-issued.v1` | `delivery-note` | el pedido (`originates-from`) |
  | `billing.invoice-issued.v1` | `invoice`, o `credit-note` si es rectificativa | el albarán del que nace (`originates-from`), o la factura que rectifica (`rectifies`) |
  | `purchases.invoice-registered.v1` | `received-invoice`: nuestro número de registro, y el del proveedor como referencia | la factura que rectifica |
  | `purchases.invoice-cancelled.v1` | anula la entrada, con su motivo | |
  | `payroll.payslip-approved.v1` | `payslip` (número: clase y mes; fecha de pago; neto) | — |
  | `payroll.payslip-cancelled.v1` | anula la entrada | |
  | `fiscal.filing-submitted.v1` | `tax-filing` (`111-2026-2T`; el día de la presentación) | — |
  | `fiscal.filing-reverted.v1` | anula la entrada | |

- **Una entrada por hecho.** El mismo mensaje dos veces lo descarta la bandeja de entrada; el
  mismo hecho en otro mensaje no crea otra entrada; una anulación repetida no cambia nada; y la
  anulación de algo que el registro no conoce se ignora.
- **Rastro.** Desde cualquier documento se sube hasta el primero del que procede y se baja por
  todo lo que salió de él, en orden de fecha. Da igual por dónde se entre: desde el pedido o
  desde la rectificativa se obtiene la misma cadena.
- **Consultas:** por empresa, tipo, tercero, texto del número, fechas y «no anulados»; por
  identidad o por hecho.
- **Tablas:** `doc_documents`, la auditoría y `documents_inbox`. No hay bandeja de salida: el
  contexto no publica nada.

## Decisiones (aprobadas por Javier el 2026-10-06)

1. **Documentos no numera: cada contexto numera sus documentos.** Se retira el emisor central
   (`IDocumentIssuer`) y las series compartidas. Sugerencia: sí; en Go cada serie avanza dentro de
   la transacción de su propio agregado, con control de versión e índice único, que es justo lo
   que al C# le faltaba.
2. **La fase 1 es un registro de solo lectura alimentado por eventos.** Nadie da de alta ni edita
   una entrada a mano. Sugerencia: sí; así el registro no puede contradecir a quien emitió el
   documento.
3. **Siete tipos de documento**, los que hoy tienen un contexto que publica su emisión.
   Presupuestos, pagos, líneas de extracto y trabajos entran cuando su contexto publique el evento
   correspondiente. Sugerencia: sí.
4. **Dos relaciones** (`originates-from` y `rectifies`) y la consulta del rastro, en lugar de las
   seis sembradas. Sugerencia: sí; las otras cuatro (sustituye, anula, versión, consolida) no
   tienen todavía ningún hecho que las produzca.
5. **El estado es emitido o anulado**; se retiran los otros seis estados y los roles del
   documento (firmante, aprobador, testigo). Sugerencia: sí mientras no haya firma ni envío.
6. **El archivo del documento (PDF generado, adjuntos escaneados) va a la fase 2**, con un puerto
   de almacenamiento y el hash del contenido. El C# no lo tenía. Sugerencia: sí; es la pieza que
   convertiría el registro en un archivo documental, y merece su propia decisión sobre dónde se
   guardan los ficheros.

## Validación

- **Dominio:** empresa, tipo, hecho, número, fecha; origen y relación van juntos y el origen no es
  el propio documento; el alta nunca nace anulada; anular una sola vez.
- **Extremo a extremo** (Documentos sobre el backend, en memoria y en SQLite migrada; la prueba
  hace de contextos emisores enviando sus hechos al broker):
  - venta completa (pedido, albarán, factura desde el albarán, rectificativa), factura recibida
    anulada, nómina y modelo fiscal revertido;
  - el mismo mensaje dos veces, el mismo hecho en otro mensaje y una anulación repetida no
    duplican ni cambian nada (versión 2 tras anular);
  - anulación de un hecho desconocido ignorada; un hecho sin fecha válida, rechazado;
  - registro por fecha con sus siete entradas; sin permiso 403, tipo inválido 400;
  - búsquedas por tipo, tercero, texto del número, fechas y «no anulados»; otra empresa solo ve lo
    suyo;
  - por hecho: tipo inválido 400, desconocido 404, ajeno 404;
  - rastro de cuatro documentos, idéntico desde la rectificativa y desde el pedido; ajeno 404;
  - puerto `Register.ByFact`.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL, alimentado con **los tipos de
  contrato de los contextos emisores** (si un contexto renombra un campo, la prueba falla):
  - registro por la bandeja `documents_inbox`, cada mensaje entregado dos veces;
  - ida y vuelta de las columnas opcionales (referencia, tercero, origen, motivo);
  - búsquedas (texto sin distinguir mayúsculas, fechas, tercero), rastro y puerto.

## Pendiente

- Fase 2:
  - archivo del documento: PDF generado y adjuntos, con puerto de almacenamiento y hash;
  - envío y firma (estados `sent` y `signed`) cuando exista el flujo;
  - más emisores: presupuestos, pagos y transferencias, extractos bancarios, trabajos, activos;
  - más relaciones (sustituye, consolida) cuando haya facturas recapitulativas;
  - la anulación de pedidos y albaranes, cuando Pedidos publique su evento.
- Importar de C#: nada (ninguna serie ni documento).
