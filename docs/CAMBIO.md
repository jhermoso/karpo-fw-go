# Contexto Cambio de divisas

Port del vertical de casa de cambio de C# (`MaccorpKernel`: `CurrencyReservation` y
`CurrencyExchange`) al contexto `contexts/exchange`.

Es el negocio de una casa de cambio: **a cuánto vende cada divisa** (un tipo de referencia, un
margen por segmento de cliente y el billete más pequeño que entrega) y **las reservas** que hacen
los clientes para recoger ese efectivo en una oficina.

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

Es el único vertical con reglas reales, pantallas propias (asistente de reserva, listado y cuadro
de mando) y 28 pruebas de integración. Pero:

- **La reserva se fía del tipo que envía el cliente.** El alta no vuelve a cotizar ni aplica el
  margen: guarda el `QuotedRate` que llega en la petición, y acepta un tipo cero.
- **Nada caduca.** `ExpireIfDueAsync` existe, pero nadie lo llama: ni tarea programada, ni ruta,
  ni comprobación al leer.
- **El redondeo a billetes no se aplica donde importa.** El asistente nunca envía la oficina a la
  cotización (así que el redondeo del servidor no actúa en ese camino), el alta no redondea, y la
  pantalla redondea por su cuenta con otra regla.
- **El tipo de referencia** viene de un servicio HTTP (un simulador en desarrollo) que, si falla,
  devuelve **0** en lugar de un error.
- **Sin ámbito por empresa:** márgenes y configuración son globales; el nivel de precio activo y
  el interruptor de criptomonedas son ajustes de la aplicación que no se pueden cambiar en marcha.
- **«Verificar email» no verifica nada:** es un cambio de estado sin código ni enlace. Los avisos
  se envían a direcciones ficticias (`cliente+{ref}@…local`, `+000000000`).
- **Transición inválida:** devuelve un error 500.
- **El cuadro de mando** suma también las reservas canceladas y caducadas.
- **Existencias de efectivo** (`CashHoldingStock`): solo lectura, sin semilla, y no se comprueban
  ni descuentan al reservar o entregar.
- **El código de promoción** no cambia el precio: atribuye la reserva a un colaborador.
- **Los guiones de base de datos van por detrás del modelo:** no tienen la tabla de configuración
  ni las columnas del código de promoción.
- **Sin permisos:** cualquier usuario autenticado puede hacer cualquier transición.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `CurrencyQuoteApplicationService` (tipo + margen + redondeo + contravalor) | 5 | 3 | 3 | 3 | **76** | Mantener → función de dominio `Quote` con las mismas fórmulas y los mismos resultados que las pruebas de C# |
| 2 | `CurrencyExchangeMargin` + `Margin` (porcentaje, absoluto, pip; tres niveles) | 4 | 4 | 3 | 3 | **73** | Mantener → agregado `Margin` **por empresa**, divisa y segmento |
| 3 | `FxRateProviderHttp` (devuelve 0 si falla) | 3 | 1 | 2 | 3 | **46** | Modificar → el **tipo de referencia es un dato de la divisa** (`Currency.Rate`, con su fecha), que se fija a mano o desde una fuente. Sin tipo no se cotiza (422) |
| 4 | `Currency.Facial` + `StockFacialResolver` + `CashHoldingStock` | 3 | 2 | 2 | 3 | **51** | Modificar → billete mínimo por divisa. El billete según existencias de la oficina, en la fase 2 con la caja |
| 5 | `CurrencyReservation` + `…Item` (subtipo de pedido de venta; tipo enviado por el cliente) | 5 | 2 | 2 | 3 | **67** | Modificar → agregado `Reservation` propio, **cotizado por el servidor al registrarse**: cada línea guarda lo pedido, lo entregado en billetes, el tipo de referencia, el ofrecido, el margen y los euros |
| 6 | Ciclo de vida (Registrado → EmailVerificado → Notificado → Completado; Cancelado; Expired) | 4 | 2 | 3 | 3 | **63** | Modificar → los mismos estados y el mismo orden; repetir el estado actual no cambia nada; transición inválida 422 |
| 7 | `ExpireIfDueAsync` (sin llamador) y `PickupPlusWindowExpiryPolicy` (+4 h) | 3 | 1 | 3 | 3 | **50** | Modificar → `ExpireDue` por empresa para una tarea programada; además, **una reserva vencida ya no avanza**. Las horas de espera son un ajuste (4 por defecto) |
| 8 | `ReservationConfiguration` (global) + ajustes de aplicación | 3 | 2 | 2 | 3 | **51** | Modificar → agregado `Settings` **por empresa**: nivel de precio, modo y validación del código de promoción, horas de espera y criptomonedas |
| 9 | Código de promoción y `ICollaboratorPromotionResolver` | 3 | 3 | 3 | 3 | **60** | Modificar → mismos tres modos (`disabled`, `optional`, `required`) y puerto `Collaborators` |
| 10 | Envío de emails y SMS a direcciones ficticias | 2 | 1 | 2 | 3 | **38** | Retirar → eventos de integración; el aviso al cliente es de quien tenga sus datos de contacto |
| 11 | Prospecto → cliente al completar (guardado aparte, no atómico) | 3 | 2 | 2 | 3 | **51** | Sustituido → el evento `reservation-status-changed` con `completed`; Parties reaccionará |
| 12 | Búsqueda y cuadro de mando | 4 | 3 | 3 | 3 | **68** | Modificar → mismos filtros esenciales; el cuadro de mando **no suma canceladas ni caducadas** (las cuenta aparte por estado) |
| 13 | `ClientSegment` (solo `WEB`) | 2 | 3 | 2 | 3 | **48** | Modificar → código de segmento libre en el margen y en la reserva; `WEB` por defecto |
| 14 | Endpoints sin permisos | — | — | — | — | — | Sustituido → `Exchange.Pricing.Read/Update` y `Exchange.Reservation.Read/Create/Progress` |

## Diseño

```
contexts/exchange/
├── domain/          # Currency, Margin, Settings, Quote, Reservation (+Line, Step), puerto Collaborators
├── contracts/       # lenguaje publicado v1
├── application/     # casos de uso con permisos y ámbito
├── infrastructure/  # esquema exg_* de 5 motores, mapeos
└── module.go        # composición y rutas /api/exchange/...
```

- **Cotización** (las fórmulas de C#, comprobadas con sus mismos números):
  1. tipo de referencia: euros por una unidad de la divisa;
  2. tipo ofrecido = referencia más margen del nivel activo, a seis decimales:
     porcentaje `ref × (1 + v/100)`, absoluto `ref + v`, pip `ref + v × 0,0001`;
  3. importe entregado = lo pedido redondeado **hacia abajo** a billetes enteros (un importe menor
     que un billete se deja como está);
  4. contravalor = entregado × ofrecido, en céntimos.

  | Ejemplo (USD a 0,92) | Ofrecido | Euros |
  |---|---|---|
  | 500 USD, margen 2 % (nivel 2) | 0,9384 | 469,20 |
  | 500 USD, margen 2,5 % (nivel 3) | 0,943 | 471,50 |
  | 123 USD, billete de 5 → 120 | 0,9384 | 112,61 |
  | 500 GBP a 1,17, sin margen | 1,17 | 585,00 |

  Sin margen para el segmento se ofrece el tipo de referencia y la cotización lo indica.
- **Tipos:** uno por divisa y día. No se fija un tipo de un día futuro ni anterior al vigente.
- **Reserva.**
  - El cliente envía divisa e importe; **el tipo lo pone el servidor** y queda guardado: un tipo
    nuevo no cambia lo ya reservado.
  - La recogida es posterior al alta; la reserva espera las horas configuradas tras la recogida.
  - Una divisa una sola vez por reserva; una pieza de coleccionista necesita su nota.
  - Referencia `RES-AAAA-XXXXXXXXXXX`, única.
- **Ciclo:** `registered → email-verified → notified → completed`; `cancelled` desde cualquiera de
  los tres primeros; `expired` cuando pasa la espera sin recogerse. Una reserva vencida no avanza
  aunque la tarea todavía no la haya cerrado.
- **Código de promoción:** atribuye la reserva a un colaborador; no cambia el precio.
- **Lenguaje publicado:** `exchange.reservation-registered.v1` y
  `exchange.reservation-status-changed.v1`.
- **Tablas:** `exg_currencies`, `exg_margins`, `exg_settings`, `exg_reservations`
  (+ `exg_reservation_lines`, `exg_reservation_history`), las bandejas de salida y la auditoría.

## Decisiones (aprobadas por Javier el 2026-10-07)

1. **El servidor cotiza al registrar la reserva**; no se acepta el tipo que envíe el cliente.
   Sugerencia: sí; es el fallo más serio del C#: con una petición manipulada se reservaba a
   cualquier precio.
2. **El redondeo a billetes se aplica siempre**, en la cotización y en la reserva, con la regla
   del servidor de C# (hacia abajo; menos de un billete se deja igual). Sugerencia: sí.
3. **El tipo de referencia se guarda por divisa y día**, fijado a mano o por una fuente externa
   que llame al mismo caso de uso; sin tipo no se cotiza. Sugerencia: sí; la conexión con un
   proveedor de tipos se añade después sin tocar el dominio.
4. **Márgenes y ajustes por empresa**, incluidos el nivel de precio activo y las criptomonedas,
   que en C# eran ajustes de la aplicación. Sugerencia: sí.
5. **La reserva caduca de verdad:** una tarea llama a `ExpireDue`, y una reserva vencida no
   avanza. La espera tras la recogida es configurable (4 horas por defecto, como en C#).
   Sugerencia: sí. Queda programar la tarea al montar el servidor.
6. **La reserva es un agregado propio**, no un subtipo del pedido de venta. Sugerencia: sí; no
   comparte reglas con Pedidos (no hay stock, albarán ni factura), solo heredaba columnas.
7. **Este contexto no envía emails ni SMS**: publica eventos. «Verificar email» sigue siendo un
   paso manual del ciclo. Sugerencia: sí; el envío real, con un código de verificación, es un
   contexto de notificaciones que hoy no existe.
8. **Completar la reserva no toca Parties:** el paso de prospecto a cliente lo hará Parties al
   oír el evento. Sugerencia: sí; en C# era un guardado aparte que podía fallar en silencio.
9. **El cuadro de mando no suma canceladas ni caducadas.** Sugerencia: sí.
10. **Las existencias de efectivo por oficina quedan fuera** (en C# solo se leían, sin datos). El
    billete mínimo es el de la divisa. Sugerencia: sí; la caja de la oficina es un tema propio.

## Validación

- **Dominio:**
  - cotización con los números de las pruebas de C# (nivel 2 y 3, billetes, sin margen), margen
    absoluto y en pips con su equivalente en porcentaje, importe menor que un billete;
  - sin tipo, divisa bloqueada, criptomoneda desactivada, importe cero;
  - divisa (el euro no), tipo (cero, siete decimales, de un día anterior), margen, ajustes y sus
    valores por defecto;
  - reserva: recogida pasada, canal, sin líneas, divisa repetida, pieza de coleccionista sin
    nota, código largo; el ejemplo de C# (500 USD a 0,92 y 300 a 1,00 = 760,00; espera de 4 h);
  - ciclo completo, repetir el estado, vencida que no avanza, cancelar (una vez; no lo recogido
    ni lo caducado), caducar a su hora.
- **Extremo a extremo** (Cambio sobre el backend, con Parties simulado para los códigos; en
  memoria y en SQLite migrada):
  - divisas, tipos y márgenes: solo lectura 403, ajeno 404, euro 400, tipo cero 422, divisa
    desconocida 422, clase de margen inválida 400;
  - ajustes por defecto; nivel 4 → 400; nivel 3 cambia la cotización;
  - cotización: fijar precios no es cotizar (403), ajeno 404, importe 0, divisa desconocida, sin
    tipo y criptomoneda → 422; los cuatro ejemplos de la tabla;
  - reserva: mover no es reservar (403), ajeno 404, código obligatorio 422, código desconocido
    422, divisa repetida 400, coleccionista sin nota 400, recogida pasada 422; reserva de 503 USD
    (entregados 500) y 300 GBP = 820,20 €, con su colaborador;
  - tipo de mañana 422, tipo anterior 422; el tipo nuevo no cambia la reserva hecha y sí la
    siguiente (100 USD = 96,90 €);
  - ciclo: reservar no es mover (403), estado inválido 400, saltarse pasos 422, repetir el paso
    no cambia la versión; recogida; no se cancela lo recogido;
  - cancelación con motivo (una vez), y caducidad con el reloj adelantado (una vez);
  - por referencia (sin distinguir mayúsculas), búsquedas por estado, divisa, cliente, oficina y
    fechas; ajeno, lista vacía;
  - cuadro de mando: una reserva viva o recogida, 820,20 €, tres estados contados, por divisa y
    por oficina;
  - ocho eventos publicados.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: altas por clave natural (versión 2
  al repetir), cotización, reserva de ida y vuelta con líneas, historial e instantes, ciclo,
  cancelación, caducidad, búsquedas por una divisa de las líneas y por días, cuadro de mando y
  bandeja de salida.

## Pendiente

- Programar `ExpireDue` y una fuente de tipos de cambio al montar el servidor.
- Adaptador del puerto `Collaborators` sobre Parties (hoy solo en pruebas).
- Notificaciones al cliente con verificación real del email.
- Parties: prospecto → cliente al oír `completed`.
- Caja de la oficina: existencias por billete, reserva y entrega de efectivo, billete mínimo
  según lo que hay.
- Compra de divisa (el cliente vende), con tipo comprador.
- Cobro de la reserva y su factura o justificante.
- Importar de C#: los márgenes sembrados (USD, GBP, CHF, MAD, JPY para `WEB`) y los billetes de
  las divisas. No hay reservas que migrar.
