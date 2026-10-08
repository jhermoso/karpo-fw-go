# Contexto Cuentas de clientes

Port del núcleo financiero de C# (`FinancialKernel`) al contexto `contexts/financial`.

Es el registro de **las cuentas que una entidad lleva a sus clientes**: su número, quién es
titular, para qué se usa y si puede operar. Es el vertical de Maccorp como entidad de pago: las
2.964 cuentas que la importación de Apiscore cargaba en C#.

> **Lo que se pidió y lo que hay.** El encargo era portar cuentas, acuerdos y productos
> financieros. Al leer el C#, los tres resultan ser cascarones sin datos ni reglas; lo único real
> del núcleo es `BankAccount`, que es otra tabla. Este contexto porta eso, en lo que toca a
> clientes, y propone retirar el resto (decisión 1).

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

Unas 11.550 líneas en ocho tablas. Dos modelos paralelos que nunca se enlazaron:

- **`FinancialAccount`, `FinancialAgreement` y `FinancialProduct`** (unas 2.400 líneas): alta,
  lectura y borrado, sin modificación.
  - Sin datos: ninguna semilla, ningún importador escribe en ellas y ninguna prueba crea una.
  - Sin reglas: `Validate()` devuelve siempre «correcto» y nadie la llama. Ni estado, ni saldo,
    ni numeración, ni unicidad del número de cuenta.
  - El acuerdo no tiene contraparte ni líneas; el producto es una tabla de dos columnas colgada
    de tres niveles de herencia.
  - La cuenta financiera ni siquiera aparece en el menú del frontal, y las tres pantallas
    permiten «editar» algo que el servidor no sabe guardar.
- **`BankAccount`** (el resto): donde está todo lo real. IBAN, estado (activa, bloqueada,
  abandonada), cuenta virtual, cuenta de pruebas, usos con vigencia y, en Parties, quién es
  titular, autorizado o beneficiario.
  - El IBAN solo se comprueba por longitud.
  - El estado se cambia con una modificación cualquiera, sin motivo ni reglas.
  - Mezcla las cuentas de los clientes con las de la propia empresa y las de proveedores, que se
    distinguen por si el banco es una organización interna.
- **Los movimientos y las líneas de extracto** cuelgan de `FinancialAccount`, es decir, de la
  tabla vacía. Nada calcula un saldo.
- **Seguridad:**
  - ninguna ruta pide más que estar autenticado, y cualquiera puede borrar;
  - en la búsqueda, **quien llama puede sustituir el ámbito de empresas** por el que quiera;
  - las estadísticas ignoran el ámbito y se guardan en una caché compartida entre usuarios.

En Go, parte de esto ya existe: las cuentas de la propia empresa, los mandatos y los extractos
están en Tesorería; las cuentas de proveedores, en Pagos; las de empleados, en Nóminas. Faltaban
las cuentas de los clientes de la entidad.

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `BankAccount` de clientes (IBAN, virtual, pruebas) | 5 | 3 | 3 | 3 | **76** | Mantener → agregado `Account`; el IBAN se valida con sus dígitos de control |
| 2 | `party_bank_account` (titular, autorizado, beneficiario; principal) | 4 | 3 | 3 | 3 | **68** | Modificar → los titulares van dentro de la cuenta, y siempre hay uno a cuyo nombre está |
| 3 | `bank_account_use` + catálogo de diez usos | 4 | 3 | 2 | 3 | **64** | Modificar → usos con vigencia dentro de la cuenta; el catálogo, en el código |
| 4 | `BankAccountStatus` (se cambia sin reglas) | 4 | 2 | 2 | 3 | **59** | Modificar → `active`, `blocked`, `abandoned`, `closed`, con transiciones y motivo |
| 5 | Búsqueda enriquecida y estadísticas | 3 | 1 | 2 | 3 | **46** | Modificar → búsqueda y recuento dentro del ámbito de quien llama, sin caché |
| 6 | `BankStatementLine` sobre la cuenta financiera | 2 | 2 | 1 | 3 | **39** | Retirar → los extractos son los de Tesorería |
| 7 | `AccountTransaction` (movimientos sin saldo) | 2 | 1 | 2 | 3 | **38** | Retirar → movimientos y saldo, si se quieren, son otra fase |
| 8 | `FinancialAccount` | 1 | 1 | 2 | 3 | **30** | Retirar → lo que pretendía ser es `Account` |
| 9 | `FinancialProduct` | 1 | 2 | 1 | 2 | **28** | Retirar → la cuenta apunta a un producto de Productos |
| 10 | `FinancialAgreement` | 1 | 1 | 1 | 3 | **26** | Retirar |
| 11 | Rutas sin permisos y borrado físico | — | — | — | — | — | Sustituido → cinco permisos; una cuenta se cierra, no se borra |

## Diseño en Go

- **`Account`**: entidad (una organización interna), número, divisa, nombre, producto, estado,
  si es de pruebas, día de apertura y de cierre, titulares y usos.
- **Número:**
  - una cuenta normal tiene un **IBAN** y sus dígitos de control tienen que cuadrar;
  - una cuenta **virtual** (solo existe en los libros de la entidad) tiene un identificador propio
    de 8 a 34 letras, cifras y guiones;
  - se guarda sin espacios y en mayúsculas, y es único dentro de la entidad.
- **Titulares:** cada uno con su papel (`holder`, `authorized`, `beneficiary`) y desde cuándo
  hasta cuándo. La cuenta está siempre a nombre de uno de sus titulares; ese no puede irse sin
  que otro ocupe su lugar, y el último titular no se va.
- **Usos:** `customer-payment`, `virtual-multicurrency`, `agent`, `player`, `player-operating`,
  `player-transit`, `cash-operating`, `segregated` y `abandonment`, cada uno con su vigencia.
- **Estados:**
  - `active` → `blocked` (con motivo) → `active`;
  - `active` o `blocked` → `abandoned` (con motivo) → `active`;
  - cualquiera → `closed`: terminan sus titulares y sus usos, y ya no cambia. Su número no se
    reutiliza.
- **Consultas:** por identidad, por número, búsqueda (estado, persona relacionada hoy, uso
  vigente, divisa, nombre, pruebas) y recuento por estado, divisa y uso, con las cuentas de
  pruebas contadas aparte.
- **Para otros contextos** (`contracts.Accounts`): qué cuentas tiene una persona en una entidad y
  si cada una puede operar.
- **Permisos** (`application/service.go`): `Financial.Account.Read`, `Create` (abrir), `Update`
  (datos, titulares y usos), `Block` (bloquear, apartar por abandono y liberar) y `Close`.
- **Rutas:** `POST/GET /api/financial/accounts`, `GET /api/financial/accounts/stats`,
  `GET …/by-number/{number}`, `GET/PUT …/{id}`, `POST …/{id}/holders`, `…/holders/end`,
  `…/holders/primary`, `…/uses`, `…/uses/end`, `…/block`, `…/abandon`, `…/release` y `…/close`.
- **Eventos publicados:** `financial.account-opened.v1` y `financial.account-status-changed.v1`.
- **Almacenamiento:** `fin_accounts` (+ `fin_account_holders`, `fin_account_uses`), las bandejas
  de salida y la auditoría.

## Decisiones propuestas (pendientes de confirmar)

1. **Se retiran `FinancialAccount`, `FinancialAgreement` y `FinancialProduct`.** Sugerencia: sí;
   no tienen datos, reglas ni uso. Es la decisión de más alcance: si alguno responde a un plan
   que yo no veo en el código (hipotecas, depósitos, contratos), dímelo y lo trato como
   funcionalidad nueva, no como port.
2. **El contexto es el registro de cuentas de clientes de la entidad.** Las cuentas propias
   siguen en Tesorería, las de proveedores en Pagos y las de empleados en Nóminas. Sugerencia: sí.
3. **IBAN validado de verdad**, e identificador propio para las cuentas virtuales. El número es
   único por entidad (en C#, el IBAN era único en toda la instalación). Sugerencia: sí. Al
   importar las cuentas de Apiscore, las que tengan un IBAN mal formado se rechazarán con su
   línea; conviene saberlo antes de la primera carga.
4. **Los titulares van dentro de la cuenta** (en C# estaban en Parties) y la cuenta está siempre
   a nombre de uno. Sugerencia: sí; es una regla de la cuenta, no de la persona.
5. **El catálogo de usos va en el código**, sin los dos de cuentas propias (`OWN_OPERATING`,
   `OWN_SEGREGATED`), que son de Tesorería. Sugerencia: sí; si hace falta añadir usos sin
   desplegar, se convierte en tabla.
6. **Estados con reglas y motivo; la cuenta se cierra, no se borra**, y su número no se
   reutiliza. Sugerencia: sí.
7. **Cinco permisos separados:** leer, abrir, mantener, bloquear y cerrar. Sugerencia: sí;
   bloquear una cuenta es una decisión de cumplimiento, no de quien la da de alta.
8. **Sin saldo ni movimientos en esta fase.** Por eso cerrar una cuenta no comprueba que esté a
   cero. Sugerencia: sí por ahora; el C# tampoco calculaba saldo. Si las cuentas van a llevarlo,
   es la fase siguiente de este contexto.
9. **Las estadísticas respetan el ámbito** de quien las pide, cuentan las cuentas de pruebas
   aparte y no se guardan en caché. Sugerencia: sí.
10. **El producto es una referencia a Productos** y no hay acuerdos. Sugerencia: sí.
11. **Publica la apertura y los cambios de estado**, y ofrece a otros contextos las cuentas de una
    persona. Sugerencia: sí; el número de cuenta viaja en el evento y es un dato personal.

## Validación

- **Dominio:** IBAN normalizado y con su país; una relación o un uso no terminan antes de
  empezar, y un cambio que no vale deja todo como estaba; un beneficiario no es titular;
  abandono con motivo; transiciones inválidas; cierre el mismo día de la apertura; cuenta
  cerrada que no cambia y se reconstruye; cuenta viva sin titular principal; alta sin titular.
- **Extremo a extremo** (en memoria y en SQLite migrada):
  - apertura: solo lectura 403, ajeno 404; dígitos de control erróneos, sin titular,
    identificador corto, uso desconocido, uso de cuenta propia, divisa y BIC → 400; número
    repetido 422; cuenta virtual en dólares; cuenta de pruebas;
  - datos: bloquear no es mantener (403), BIC inválido 400;
  - titulares: papel inválido 400, autorizado como principal 400, repetido 422, la principal no
    se va 422, se pone a nombre de otro titular, la anterior se va, ya se había ido 422, el
    último titular se queda 422;
  - usos: desconocido 400, repetido 422, terminar, ya terminado 422, volver a asignar;
  - bloqueo: mantener no es bloquear (403), sin motivo 422, dos veces 422, abandono desde
    bloqueada, liberar (borra el motivo), liberar una activa 422; una bloqueada sigue pudiendo
    cambiar sus datos;
  - lectura por identidad y por número (con espacios y minúsculas); búsquedas por empresa,
    persona (la que se fue ya no sale), estado, uso, divisa, nombre y pruebas; ajeno, lista vacía;
  - recuento: ajeno 404, sin empresa 400, pruebas aparte;
  - cuentas de una persona para otros contextos, con si pueden operar;
  - cierre: bloquear no es cerrar (403), antes de la apertura 422; terminan titulares y usos;
    después no cambia nada (422) y su número sigue ocupado;
  - ocho eventos publicados.
- **Integración** en PostgreSQL, SQL Server, Oracle y MySQL: cuenta de ida y vuelta con
  titulares, usos, fechas y acentos; número único en su entidad y libre en otra; búsquedas por
  titulares y usos vigentes; recuento; directorio; cierre; bandeja de salida.

## Pendiente

- Cargador de la importación para las cuentas de Apiscore (titular, uso, estado, virtual y
  pruebas), con sus equivalencias como datos.
- Saldo y movimientos, y con ellos la comprobación al cerrar.
- Comprobar en Parties que el titular existe y es cliente de la entidad (hoy se confía en el
  identificador).
- Qué puede hacer cada papel (un autorizado, un beneficiario): hoy solo se registra.
- Que Cambio de divisas y Cobros consulten `Accounts.OfParty` antes de operar con un cliente
  bloqueado.
- Pantalla: la del C# apuntaba a `/api/bank-accounts`.
