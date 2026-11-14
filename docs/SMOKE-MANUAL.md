# Smoke manual: el ciclo de venta de punta a punta

Guía para recorrer a mano, con `curl.exe` desde PowerShell, el ciclo comercial completo sobre la
[publicadora de humo](PUBLICADORA-DE-HUMO.md):

**pedido → reserva → albarán → factura (borrador desde el albarán, emisión) → vencimiento → cobro
→ asientos.**

Todo va por las rutas del anfitrión, como iría un front. No hay que preparar nada a mano: `-seed`
carga lo que el recorrido necesita. La prueba `TestSeed_LoadsWhatTheWalkNeeds`
(`host/smoke/smoke_test.go`) recorre los mismos pasos en cada ejecución de las pruebas, con las
mismas rutas y los mismos importes, y comprueba lo que la guía dice que debe verse en cada uno;
así que si la semilla deja de bastar, se nota allí.

## 1. Arrancar

```powershell
$env:KARPO_ENV = 'development'
go run -tags smoke ./cmd/karpo-smoke -port 8092 -seed
```

Debería verse, antes del aviso en mayúsculas de que no hay autenticación:

```text
seed: loaded a company and its customer, the Spanish VAT rates, a product with stock in a warehouse, ...
  Paste these lines into PowerShell (docs/SMOKE-MANUAL.md):
  $company   = '01a1...'   # Humo Distribuciones, S.L. (empresa interna, NIF B98765431)
  $customer  = '01a1...'   # Cliente de Humo, S.L. (cliente, NIF B12345674)
  $product   = '01a1...'   # HUMO-01 Caja de humo, 12.50 + IVA G21
  $warehouse = '01a1...'   # AL1 Almacén central, 100 unidades
  $series    = '01a1...'   # serie FA del año en curso
  $terms     = '01a1...'   # 30D: un vencimiento a 30 días
```

Cada arranque es una base nueva, con identificadores nuevos. La publicadora se queda en primer
plano: el recorrido se hace **en otra ventana de PowerShell**, y para pararla basta `Ctrl+C` en
ésta.

### Arrancar sobre PostgreSQL

Lo mismo sobre PostgreSQL, el destino de producción, con `cmd/karpo-smoke-postgres`. La
publicadora **sólo arranca sobre una base sin ninguna tabla y nunca borra nada**, así que cada
vez se levanta un PostgreSQL desechable propio: un contenedor de Docker con nombre único, que se
borra solo al pararlo (`--rm`). Nunca se usa para esto un contenedor que ya exista.

```powershell
netstat -ano | findstr :55532                     # no debe salir nada: el puerto está libre
$pg = "humo-pg-$(Get-Random)"                    # un nombre único
$pw = [guid]::NewGuid().ToString('N')            # una contraseña nueva, sólo para este contenedor
docker run --rm -d --name $pg -p 127.0.0.1:55532:5432 -e POSTGRES_PASSWORD=$pw postgres:17
do { Start-Sleep 1; docker exec $pg pg_isready -U postgres -h 127.0.0.1 } until ($LASTEXITCODE -eq 0)

$env:KARPO_ENV = 'development'
$env:KARPO_SMOKE_POSTGRES_URL = "postgres://postgres:$pw@127.0.0.1:55532/postgres?sslmode=disable"
go -C cmd/karpo-smoke-postgres run -tags smoke . -port 8092 -seed
```

Debería verse lo mismo que con SQLite, y en el aviso `on PostgreSQL postgres@127.0.0.1:55532/postgres`
(sin la contraseña). El resto de la guía es igual. Para pararlo todo, en esta ventana:

```powershell
# Ctrl+C para la publicadora; después:
docker stop $pg                                  # con --rm, el contenedor y sus datos desaparecen
Remove-Item Env:KARPO_SMOKE_POSTGRES_URL
docker ps -a --filter "name=$pg"                 # sólo la cabecera: ya no existe
netstat -ano | findstr :55532                    # ninguna línea LISTENING
```

Si la cadena apunta a otra máquina se niega sin abrirla (`not on this machine`). Si la base ya
tiene algo (una tabla, una secuencia, una función, un tipo...), se niega antes de migrar y no
toca nada: `... already has N objects (...): the smoke publisher only starts on a new, empty database and never deletes anything`.

### Lo que carga la semilla

| Contexto | Qué | Para qué |
|---|---|---|
| Parties | **Humo Distribuciones, S.L.**: organización interna, NIF `B98765431`, domicilio fiscal en Madrid | Quien vende |
| Parties | **Cliente de Humo, S.L.**: NIF `B12345674`, domicilio en Barcelona, relación de cliente con la empresa | A quien se vende |
| Fiscal | IVA de territorio común desde 2012-09-01: `G21` (21 %, recargo 5,2), `R10` (10 %, 1,4) y `SR4` (4 %, 0,5); la empresa, contribuyente en territorio común | Que Facturación calcule el IVA |
| Products | `HUMO-01` Caja de humo: bien vendible y almacenable, 12,50 €, IVA `G21` | Lo que se vende |
| Inventory | Almacén `AL1` con 100 unidades de `HUMO-01` a 6 € | Que el pedido se reserve y se sirva |
| Billing | Serie `FA` del año en curso | Que la factura se emita y se numere |
| Receivables | Condiciones `30D` (un plazo a 30 días), las del cliente, sin límite de crédito | El vencimiento |
| Accounting | Cuentas `4300` Clientes, `4312`, `4770` IVA repercutido, `4775`, `5700` Caja, `5720` Bancos, `7000` Ventas; el libro de la empresa (ejercicio desde enero) con su perfil | Que la factura y el cobro se asienten |

Si la base ya tiene alguna party, no carga nada y lo dice (`there is data already`).

## 2. Preparar la ventana

En **otra** ventana de PowerShell (vale Windows PowerShell 5.1 o PowerShell 7):

1. Pegar las seis líneas `$company = ...` que imprimió la semilla.
2. Pegar esto:

```powershell
$base  = 'http://127.0.0.1:8092'
$token = (curl.exe -s "$base/api/smoke/token?as=admin" | ConvertFrom-Json).accessToken
# k envía una petición con la sesión y devuelve la respuesta ya leída. El cuerpo va en un fichero
# temporal: así PowerShell no se come las comillas del JSON.
function k([string]$method, [string]$path, $body) {
  $a = @('-s', '-X', $method, "$base$path", '-H', "Authorization: Bearer $token")
  $f = $null
  if ($null -ne $body) {
    $f = [IO.Path]::GetTempFileName()
    [IO.File]::WriteAllText($f, ($body | ConvertTo-Json -Depth 10 -Compress))
    $a += @('-H', 'Content-Type: application/json', '--data-binary', "@$f")
  }
  $out = curl.exe @a
  if ($f) { Remove-Item $f }
  $out | ConvertFrom-Json
}
$token.Length
```

Debería verse un número de unos 300 (la longitud del token). El token dura lo que dura una sesión
del anfitrión; si una orden responde 401, se vuelve a pedir con la línea `$token = ...`.

`k GET /api/...` es lo mismo que `curl.exe -s http://127.0.0.1:8092/api/... -H "Authorization:
Bearer $token"`; por ejemplo, sin la función:

```powershell
curl.exe -s "$base/api/parties/$company" -H "Authorization: Bearer $token"
```

Debería verse el JSON de Humo Distribuciones, con `"createdBy":"smoke-admin"`, el rol
`Internal Organization`, el NIF `B98765431` y el contacto postal de Madrid con usos `billing` y
`default`.

> **Los mensajes entre contextos van cada 2 segundos.** La reserva, el borrador de factura, el
> vencimiento y los asientos los crea otro contexto al recibir un hecho. Si una consulta no
> enseña todavía lo que dice la guía, se espera un par de segundos y se repite.

## 3. Lo sembrado

```powershell
k GET "/api/inventory/stock?company=$company" | Format-Table sku, onHand, reserved, available
```

Debería verse `HUMO-01  100  0  100`.

## 4. El pedido

```powershell
$order = k POST /api/orders/orders @{ company = $company; customer = $customer; warehouse = $warehouse }
$order = k POST "/api/orders/orders/$($order.id)/lines" @{ product = $product; quantity = '10' }
$order = k POST "/api/orders/orders/$($order.id)/confirm" @{}
$order | Format-List number, status, total
```

Debería verse `number : PED-<año>-000001`, `status : confirmed` y `total : 125.00` (10 × 12,50,
sin IVA: el pedido lleva importes netos).

Al confirmar, Inventario reserva las existencias. Pasados un par de segundos:

```powershell
(k GET "/api/orders/orders/$($order.id)").lines | Format-Table no, sku, quantity, reserved, pending, short
```

Debería verse la línea 1 de `HUMO-01` con `quantity 10`, `reserved 10`, `pending 10` y `short 0`
(antes de la reserva, `reserved 0` y `short 10`).

## 5. El albarán

Sin líneas en el cuerpo, se sirve todo lo pendiente:

```powershell
$note = k POST "/api/orders/orders/$($order.id)/deliveries" @{}
$note | Format-List number, orderStatus, total
```

Debería verse `number : ALB-<año>-000001`, `orderStatus : delivered` y `total : 125.00`. Las
existencias bajan:

```powershell
k GET "/api/inventory/stock?company=$company" | Format-Table sku, onHand, reserved, available
```

Debería verse `HUMO-01  90  0  90`.

## 6. La factura

Facturación prepara un borrador a partir del albarán en cuanto recibe el hecho:

```powershell
$draft = (k GET "/api/billing/invoices?seller=$company&status=draft").items[0]
$draft | Format-List status, description, net, sourceType, sourceRef
```

Debería verse `status : draft`, `description : Albarán ALB-<año>-000001 (pedido PED-<año>-000001)`,
`net : 125.00`, `sourceType : orders.delivery` y `sourceRef : ALB-<año>-000001`. Un borrador no
tiene número ni impuestos todavía.

Se emite en la serie sembrada (sin fecha: la de hoy):

```powershell
$invoice = k POST "/api/billing/invoices/$($draft.id)/issue" @{ series = $series }
$invoice | Format-List number, status, issueDate, sellerNif, customerNif, customerName
$invoice.taxes | Format-List net, tax, total
$invoice.taxes.lines | Format-Table taxCode, rate, base, amount
```

Debería verse:

- `number : FA-<año>-000001`, `status : issued`, `issueDate` de hoy;
- `sellerNif : B98765431`, `customerNif : B12345674`, `customerName : Cliente de Humo, S.L.`;
- `net : 125.00`, `tax : 26.25`, `total : 151.25`;
- una línea de impuesto `G21  21.00  125.00  26.25`.

El albarán queda facturado:

```powershell
k GET "/api/orders/deliveries/$($note.id)" | Format-List number, invoiceNumber
```

Debería verse `invoiceNumber : FA-<año>-000001`.

## 7. El vencimiento

Cobros abre la cuenta por cobrar al recibir la factura emitida. Como la factura no dice
vencimiento, toma las condiciones del cliente (`30D`):

```powershell
$receivable = k GET "/api/receivables/invoices/$($invoice.id)"
$receivable | Format-List number, total, open, settled
$receivable.installments | Format-Table no, due, amount, collected, open
```

Debería verse `number : FA-<año>-000001`, `total : 151.25`, `open : 151.25`, `settled : False` y
un plazo `1` con `due` = la fecha de emisión + 30 días, `151.25  0.00  151.25`.

## 8. El cobro

El cliente paga por transferencia, el mismo día de la factura, y el cobro se aplica al plazo 1:

```powershell
$collection = k POST /api/receivables/collections @{ seller = $company; payer = $customer; date = $invoice.issueDate;
  amount = '151.25'; method = 'transfer'; allocations = @(@{ invoice = $invoice.id; installment = 1; amount = '151.25' }) }
$collection | Format-List method, amount, allocated, unallocated
```

Debería verse `method : transfer`, `amount : 151.25`, `allocated : 151.25` y `unallocated : 0.00`.

```powershell
k GET "/api/receivables/invoices/$($invoice.id)" | Format-List open, settled
```

Debería verse `open : 0.00` y `settled : True`.

## 9. Los asientos

Contabilidad asienta la factura emitida y el cobro aplicado:

```powershell
(k GET "/api/accounting/entries?company=$company").items | Format-Table number, date, description, source, total
```

Debería verse:

```text
number date       description            source                              total
     1 <hoy>      Factura FA-<año>-000001 billing.invoice-issued.v1           151.25
     2 <hoy>      Cobro (transfer)       receivables.collection-allocated.v1 151.25
```

Las líneas del primero:

```powershell
(k GET "/api/accounting/entries?company=$company").items[0].lines | Format-Table account, debit, credit, description
```

Debería verse `4300` al debe por 151.25 (con el cliente en `party`), `7000` al haber por 125.00 y
`4770` al haber por 26.25 (`Cuota G21`).

Las sumas y saldos:

```powershell
k GET "/api/accounting/trial-balance?company=$company" | Format-Table account, name, debit, credit, balance
```

Debería verse:

```text
account name                              debit  credit balance
4300    Clientes                          151.25 151.25 0.00
4770    Hacienda Pública, IVA repercutido 0.00   26.25  -26.25
5720    Bancos                            151.25 0.00   151.25
7000    Ventas de mercaderías             0.00   125.00 -125.00
```

Y nada esperando a asentarse:

```powershell
(k GET "/api/accounting/parked?company=$company").total
```

Debería verse `0`. Si se ve más, `k GET "/api/accounting/parked?company=$company"` dice por qué
(por ejemplo `accounting.no_ledger` si la empresa no tuviera libro).

## 10. Probar permisos

Además de `admin` hay dos perfiles de la empresa sembrada, con los roles de Security
([PUBLICADORA-DE-HUMO.md](PUBLICADORA-DE-HUMO.md#quién-entra)): `reader` (sólo lee) y `clerk`
(vende, pero no es administrador). `k` usa la sesión que haya en `$token`; un rechazo llega como
un JSON con `status` y `title`.

```powershell
$admin  = $token
$reader = (curl.exe -s "$base/api/smoke/token?as=reader" | ConvertFrom-Json).accessToken
$clerk  = (curl.exe -s "$base/api/smoke/token?as=clerk"  | ConvertFrom-Json).accessToken
$token = $reader; (k GET /api/auth/context).context | Format-List subjectName, isGlobalAdmin, grants
```

Debería verse `subjectName : smoke-reader`, `isGlobalAdmin : False` y un solo acceso, a `$company`,
con `level=ReadOnly`.

**1. El lector lee y no escribe:**

```powershell
$token = $reader
(k GET "/api/billing/invoices?seller=$company").items | Format-Table number, status, net
k POST /api/orders/orders @{ company = $company; customer = $customer; warehouse = $warehouse } | Format-List status, title
```

Debería verse la factura del recorrido (`FA-<año>-000001  issued  125.00`) y después
`status : 403`, `title : Forbidden`.

**2. El comercial vende; el lector no puede emitir:**

```powershell
$token = $clerk
$order = k POST /api/orders/orders @{ company = $company; customer = $customer; warehouse = $warehouse }
$order = k POST "/api/orders/orders/$($order.id)/lines" @{ product = $product; quantity = '2' }
$order = k POST "/api/orders/orders/$($order.id)/confirm" @{}
Start-Sleep 3
$note  = k POST "/api/orders/orders/$($order.id)/deliveries" @{}
Start-Sleep 3
$draft = (k GET "/api/billing/invoices?seller=$company&status=draft").items[0]
$token = $reader; k POST "/api/billing/invoices/$($draft.id)/issue" @{ series = $series } | Format-List status, title
$token = $clerk;  k POST "/api/billing/invoices/$($draft.id)/issue" @{ series = $series } | Format-List number, status
```

Debería verse primero `status : 403` (el lector) y después `number : FA-<año>-000002`,
`status : issued` (el comercial; `000001` si no se hizo antes el recorrido).

**3. El comercial no administra:**

```powershell
$token = $clerk; k GET /api/security/users | Format-List status, title
```

Debería verse `status : 403`, `title : Forbidden`.

**4. Ninguno ve otra empresa.** El administrador da de alta otra empresa interna con su serie:

```powershell
$token = $admin
$other = k POST /api/organizations @{ legalName = 'Otra de Humo, S.L.'; roles = @('10000000-0000-0000-0001-000000000001') }
k POST /api/billing/series @{ seller = $other.id; code = 'FA'; year = (Get-Date).Year } | Out-Null
$token = $reader
"propia: $((k GET "/api/billing/series?seller=$company").Count)  otra: $((k GET "/api/billing/series?seller=$($other.id)").Count)"
$token = $clerk; k POST /api/billing/series @{ seller = $other.id; code = 'FB'; year = (Get-Date).Year } | Format-List status, title
$token = $admin
```

Debería verse `propia: 1  otra: 0` (el lector no ve la serie de la otra empresa) y
`status : 404`, `title : Resource Not Found` (para el comercial, la otra empresa no existe). El
identificador del rol es el de «organización interna» de Parties.

## 11. Parar

`Ctrl+C` en la ventana de la publicadora. Para comprobar que el puerto queda libre:

```powershell
netstat -ano | findstr 8092
```

No debería verse ninguna línea `LISTENING`. La base queda en la carpeta temporal que dijo al
arrancar; la publicadora no vuelve a arrancar sobre ella (cada arranque, base nueva).

## Si algo no sale

| Se ve | Qué es |
|---|---|
| `401` en cualquier orden | El token caducó: repetir `$token = ...` |
| La consulta del borrador, del vencimiento o de los asientos sale vacía | El mensaje aún no ha llegado: esperar 2 segundos y repetir |
| `seed: there is data already` | La base no era nueva: arrancar sin `-db`, o con un fichero que no exista |
| `the file ... already exists` al arrancar | `-db` apunta a un fichero que ya existe |
| `Unable to connect` | La publicadora no está arrancada, o escucha en otro puerto |
| `409` y `start the publisher with -seed` al pedir `reader` o `clerk` | Se arrancó sin `-seed`: esos perfiles los crea la semilla |
| `already has N objects` al arrancar sobre PostgreSQL | La base no es nueva: levantar otro contenedor desechable |
| `not on this machine` al arrancar sobre PostgreSQL | La cadena apunta a otra máquina: sólo vale un PostgreSQL de ésta (`127.0.0.1`) |
| `the database does not answer` | El contenedor no está listo o la cadena de `KARPO_SMOKE_POSTGRES_URL` no es la suya |
