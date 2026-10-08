# Sectorial financiero

Port del núcleo financiero de C# (`FinancialKernel`) al contexto `contexts/financial`.

Es el **sectorial de finanzas** de Karpo: lo que solo tiene una empresa que es entidad
financiera. Tres cosas:

- los **productos** que ofrece (una cuenta de pago, un depósito, un préstamo);
- los **acuerdos** que firma con cada cliente;
- las **cuentas** que lleva a sus clientes: su número, quién es titular, para qué se usa y si
  puede operar. Son las 2.964 cuentas que la importación de Apiscore cargaba en C#.

> **Para quién existe.** Solo para una empresa que en Parties es organización interna **y además**
> tiene el rol de entidad financiera. Para cualquier otra, este contexto no tiene nada: no puede
> dar de alta productos, acuerdos ni cuentas.

> **Corrección del 2026-10-08.** La primera versión de este documento proponía retirar
> `FinancialAccount`, `FinancialAgreement` y `FinancialProduct` porque en el código de C# son
> cascarones sin datos ni reglas. Javier aclaró que **no se retiran**: son el sectorial de
> finanzas. Productos y acuerdos están ahora en el contexto, y la cuenta es lo que
> `FinancialAccount` quería ser, con los datos reales que el C# guardaba en `BankAccount`.

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
| 8 | `FinancialAccount` | 1 | 1 | 2 | 3 | **30** | Mantener por decisión de Javier → es `Account`, con los datos reales de `BankAccount` |
| 9 | `FinancialProduct` | 1 | 2 | 1 | 2 | **28** | Mantener por decisión de Javier → agregado `Product`, propio de la entidad, sin la herencia de tres niveles |
| 10 | `FinancialAgreement` | 1 | 1 | 1 | 3 | **26** | Mantener por decisión de Javier → agregado `Agreement`, con el cliente con quien se firma y estado |
| 11 | Rutas sin permisos y borrado físico | — | — | — | — | — | Sustituido → cinco permisos; una cuenta se cierra, no se borra |

Las notas de las filas 8 a 10 miden el código de C# tal como está (cascarones), no el valor de la
pieza: se mantienen porque son producto planificado.

## Diseño en Go

- **Entidad financiera** (`domain.Institutions`): el contexto pregunta si la empresa lo es antes
  de crear nada. En el anfitrión responde Parties: organización interna con el rol de entidad
  financiera, los dos en vigor. Si no lo es: 422 `financial.not_an_institution`.
- **`Product`**: código propio (único en la entidad), nombre, descripción, familia (`payment`,
  `deposit`, `loan`, `investment`, `leasing`, `other`), código regulatorio y, si se deja de
  ofrecer, desde qué día. Lo ya contratado sigue; se puede volver a ofrecer.
- **`Agreement`**: número propio (único en la entidad), el cliente con quien se firma, nombre,
  familia (la del producto si no se dice), producto, día de firma, vigencia y estado (`in-force`,
  `terminated` con su motivo). Un acuerdo terminado no cambia.
- **`Account`**: entidad (una organización interna), número, divisa, nombre, **producto**,
  **acuerdo**, estado, si es de pruebas, día de apertura y de cierre, titulares y usos.
  - El producto tiene que ser de la entidad y, al abrir, seguir ofreciéndose.
  - El acuerdo tiene que ser de la entidad, estar en vigor ese día y estar firmado con uno de los
    titulares.
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
- **Permisos**: `Financial.Account.Read`, `Create` (abrir), `Update` (datos, titulares y usos),
  `Block` (bloquear, apartar por abandono y liberar) y `Close`; `Financial.Product.Read` y
  `Update`; `Financial.Agreement.Read` y `Update`.
- **Rutas:** `POST/GET /api/financial/accounts`, `GET /api/financial/accounts/stats`,
  `GET …/by-number/{number}`, `GET/PUT …/{id}`, `POST …/{id}/holders`, `…/holders/end`,
  `…/holders/primary`, `…/uses`, `…/uses/end`, `…/block`, `…/abandon`, `…/release` y `…/close`.
  Además `POST/GET /api/financial/products`, `GET/PUT …/products/{id}`, `…/discontinue`,
  `…/reinstate`; y `POST/GET /api/financial/agreements`, `GET/PUT …/agreements/{id}`,
  `…/terminate`.
- **Eventos publicados:** `financial.account-opened.v1`, `financial.account-status-changed.v1`,
  `financial.agreement-signed.v1` y `financial.agreement-terminated.v1`.
- **Almacenamiento:** `fin_accounts` (+ `fin_account_holders`, `fin_account_uses`),
  `fin_products`, `fin_agreements`, las bandejas de salida y la auditoría.

## Decisiones (aprobadas por Javier el 2026-10-08)

1. ~~Se retiran `FinancialAccount`, `FinancialAgreement` y `FinancialProduct`.~~ **Corregida por
   Javier el 2026-10-08: no se retiran.** Pertenecen al sectorial de finanzas, que se carga si la
   empresa es organización interna y además entidad financiera. Ver «Sectorial: pendiente de
   confirmar» más abajo para cómo lo he interpretado.
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
10. ~~El producto es una referencia a Productos y no hay acuerdos.~~ **Sustituida por la
    corrección de la 1:** el producto y el acuerdo son de este contexto.
11. **Publica la apertura y los cambios de estado**, y ofrece a otros contextos las cuentas de una
    persona. Sugerencia: sí; el número de cuenta viaja en el evento y es un dato personal.

## Sectorial (confirmado por Javier el 2026-10-08)

Lo que hice a partir de la corrección; Javier confirmó los tres puntos que estaban en duda (la
cuenta es `FinancialAccount`, la capacidad se deriva del rol y basta con impedir crear):

1. **Quién es entidad financiera lo dice Parties** (organización interna con el rol de entidad
   financiera, los dos en vigor) y el contexto lo comprueba **al crear**: productos, acuerdos y
   cuentas. Leer no se bloquea: una empresa que no lo es simplemente no tiene nada. Si deja de
   serlo, lo que tenía se conserva y se puede consultar, pero no crea más.
2. **La cuenta que ya había es `FinancialAccount`.** No he creado otra tabla: es la misma cuenta,
   con los datos reales de `BankAccount`, que ahora apunta a su producto y a su acuerdo.
3. **El producto financiero es de la entidad**, no del catálogo general de Productos. En C#
   heredaba de `Product` y `Service`; aquí es un agregado propio con su código y su familia.
4. **El acuerdo tiene cliente y estado**, que en C# no tenía. Un acuerdo se firma con una persona
   y una cuenta solo se abre bajo un acuerdo firmado con uno de sus titulares.
5. **Familias en lugar de texto libre** para producto, acuerdo y cuenta (en C# eran cadenas como
   `savings_account` o `loan_agreement` sin catálogo).
6. **La capacidad `financial` de Módulos se deriva del mismo rol.** Sustituye a la decisión 4 de
   [MODULOS.md](MODULOS.md), que la dejaba como activación manual. El anfitrión usa una sola
   comprobación para las dos cosas, así que lo que enseña el menú y lo que permite el sectorial
   no pueden discrepar.

## Validación

- **Dominio:** IBAN normalizado y con su país; una relación o un uso no terminan antes de
  empezar, y un cambio que no vale deja todo como estaba; un beneficiario no es titular;
  abandono con motivo; transiciones inválidas; cierre el mismo día de la apertura; cuenta
  cerrada que no cambia y se reconstruye; cuenta viva sin titular principal; alta sin titular.
- **Extremo a extremo** (en memoria y en SQLite migrada):
  - una empresa que no es entidad financiera no crea productos, acuerdos ni cuentas (422);
  - productos: solo lectura 403, ajeno 404, familia y código inválidos 400, código repetido 422;
    cambio; dejar de ofrecer y volver a ofrecer; no se abre una cuenta con un producto que ya no
    se ofrece ni con uno que no existe; búsquedas por familia y por si se ofrece;
  - acuerdos: sin familia ni producto 400, termina antes de empezar 400, producto inexistente
    422, número repetido 422; la familia sale del producto; terminar antes de su inicio 422, con
    motivo, dos veces 422, y terminado no cambia; no se abre una cuenta bajo un acuerdo terminado
    ni bajo el de otra persona; búsquedas por cliente, producto y estado;
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
  - cuentas por producto y por acuerdo;
  - once eventos publicados.
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
