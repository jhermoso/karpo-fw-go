# Parties: el modelo común de Go, Karpo C# y Trench

> Empezado el 2026-10-11. Recoge **lo que Javier ha decidido** ese día (§1) y **lo que falta por
> decidir** para que los tres guarden los mismos datos y cumplan las mismas reglas (§3). Las
> tablas pueden seguir siendo distintas: eso ya está decidido (§1, D-1).
>
> Continúa [PARTIES-COMPARACION-CON-CSHARP.md](PARTIES-COMPARACION-CON-CSHARP.md). Las filas
> salen del estudio de convergencia de Karpo (`Karpo_Estudio_Convergencia_Modelos.md` §3 y §4);
> entre paréntesis va su número allí (R = Go corrigió, F = a Go le falta, CV = pregunta abierta).
>
> «C#» quiere decir Karpo C# **y** Trench, que comparten esquema y código de origen. Donde
> Trench tiene algo más, se dice.

## 1. Decidido por Javier el 2026-10-11

| # | Decisión | Ejemplo |
|---|---|---|
| D-1 | **Los esquemas siguen distintos.** Go conserva el suyo y C# el suyo, para poder compararlos. Lo que se iguala es el modelo, las reglas y el contrato HTTP. La forma de guardar una jerarquía es una opción de arquitectura de Theros | Una sociedad son 4 filas en C# y 1 en Go, y el front no lo nota |
| D-2 | **Go sigue como monolito modular** (un proceso, contextos separados dentro). Los macroservicios, cuando el ERP esté más implementado | — |
| D-3 | **El contacto es propio de cada party**, no se comparte (CV-7) | Ana y Luis viven juntos: dos direcciones iguales, una de cada uno |
| D-4 | **Se termina con fecha de fin; «borrar» es desactivar** con la marca de activo. No hay borrado físico. No se puede desactivar algo de lo que dependen otras cosas vigentes, y eso lo comprueba el comando antes de ejecutarse (CV-10) | Talleres Pérez deja de ser cliente en marzo: el rol queda con fin el 15 de marzo. No se puede desactivar a Talleres Pérez mientras tenga una relación de cliente vigente |
| D-5 | **Un documento de identidad inválido se rechaza siempre.** Las importaciones generan un documento con los rechazados (CV-4) | De 400 clientes de Sage, 12 con el NIF mal: no entran con ese documento y salen en el informe |
| D-6 | **Catálogos híbridos** (CV-9). Se siembra todo lo identificado. Un tipo de rol nuevo **sin** atributos se añade como dato, diciendo quién es su padre. Uno **con** atributos (suyos o de sus relaciones) va por la interfaz de extensibilidad, con tablas uno a uno | «Avalista» sin datos: una fila que cuelga de «Persona». «Avalista» con importe avalado: una extensión |
| D-7 | **El límite de crédito es de la pareja empresa–cliente**, no del cliente | Talleres Pérez: 10.000 € con Acme y 3.000 € con Acme Levante |
| D-8 | **Un alta equivocada se quita con un comando «anular»**, que matiza D-4. El comando mira qué depende del dato: si nada depende, lo borra (de forma lógica o física, la que se pueda); si algo depende, lo deja marcado como anulado | A Ana se le da «Proveedor» por error y se corrige al minuto: nada cuelga de ese rol y se borra. Si ya tuviera una relación de proveedor, queda anulado |
| D-9 | **Una party importada con el documento rechazado entra**, marcada con el error «documento identificativo erróneo». Hay un filtro para buscar las que lo tienen, y además queda en el informe de la importación (completa D-5) | El cliente 301 de Sage entra sin NIF válido, aparece al filtrar «con documento erróneo» y sale en el informe |

## 2. Qué cambia en cada lado con lo decidido

| # | Go | C# |
|---|---|---|
| D-3 | Nada: ya es así | Pasa de 10 tablas de contacto a contactos de la party. **Rompe** esquema, rutas y front (paso P-41 del estudio). Antes hay que medir cuántos mecanismos están enlazados a más de una party |
| D-4 | `Party.Deactivate` (`domain/party.go:266`) **no comprueba nada** hoy: hay que añadir la precondición. Falta también decidir qué pasa con un alta equivocada (§3, fila 1) | Retirar los `DELETE` de party, rol, relación y contacto, o convertirlos en terminar o desactivar. Añadir «terminar relación», que hoy no existe (P-32). El front tiene acciones «Eliminar» en sus configuraciones |
| D-5 | Ya rechaza. **Cambia la decisión 32 del 2026-10-08**: los documentos que Parties rechazaría se guardaban como «otro documento»; ahora no se guardan y van al informe. Falta el documento de rechazados: cada ejecución guarda sus mensajes, pero no he comprobado si se pueden descargar como fichero | Pasa de marcar («formato inválido») a rechazar. Hay que medir antes cuántos documentos guardados están marcados como inválidos, y decidir qué se hace con ellos (§3, fila 2) |
| D-6 | Los catálogos son hoy de sólo lectura: falta el caso de uso para añadir un tipo de rol sin datos. La extensión con tablas uno a uno **no existe**: hoy los datos por tipo de relación son columnas de `party_relationships` (decisión PU-1). Hay que diseñarla (§3, fila 3) | Ya tiene altas de tipos por HTTP, sin protección de los sembrados. El patrón de extensión uno a uno ya existe: los perfiles de ErpDetail (`InternalOrganizationProfile` cuelga del rol; `CustomerRelationshipCommercialProfile`, de la relación) |
| D-7 | Nada: `receivables` ya lo hace | El dato está en tres sitios (`customer.CreditLimit`, `customer_relationship.CreditLimit`, `…commercial_profile.MaxRisk`). Medido por la sesión de Trench: ninguna regla lo consulta y en su base local están todos vacíos, así que no hay datos que migrar |

## 3. Lo que falta por decidir

Cada fila lleva un ejemplo y mi recomendación. **Ninguna está decidida.** Van ordenadas por lo
que bloquean: primero las que nacen de las decisiones de hoy, luego las de datos (sin ellas no
se guarda lo mismo), luego las de reglas y por último las de alcance.

### 3.1 Flecos de las decisiones de hoy

| # | Pregunta | Ejemplo | Recomendación |
|---|---|---|---|
| 1 | ~~Un alta equivocada, ¿cómo se quita?~~ | | **Decidido: D-8.** Queda por fijar si anular pide un permiso propio y qué cuenta como «depende» (misma lista que la fila 3) |
| 2 | **Supresión de datos personales (RGPD).** ¿Cómo se atiende a quien pide que se borren sus datos? | Un antiguo cliente particular pide la supresión | **Pendiente de consulta legal** (Javier, 2026-10-11); ver §3.5 |
| 3 | **¿Qué bloquea una desactivación?** (el detalle de D-4) | ¿Se puede desactivar a Talleres Pérez con una factura pendiente de cobro? | Dentro de Parties: bloquean las relaciones y los roles vigentes. Lo de otros contextos (facturas, pedidos) no lo sabe Parties: o cada contexto escucha `party-deactivated` y avisa, o Parties pregunta por un puerto. Recomiendo lo primero ahora |
| 4 | ~~Importación con documento rechazado: ¿entra la party?~~ | | **Decidido: D-9.** Queda un detalle: si el número rechazado se conserva junto a la marca, para que quien lo corrige lo vea sin abrir el informe. Recomiendo que sí, sin que cuente como identificación |
| 5 | **Diseño de la extensibilidad** (D-6): quién puede crear una extensión, si es código o configuración, y cómo la ve el front | «Avalista» con importe avalado | Necesita su propio documento. La pregunta de fondo: una extensión con reglas es código (la genera Theros); sin reglas pueden ser campos declarados. Son dos mecanismos distintos |
| 6 | **Tipos de relación nuevos sin datos**: D-6 habla de roles. ¿Vale igual para tipos de relación? | «Avalista de» entre dos parties | Sí, con el mismo criterio: una fila que dice qué rol juega cada lado |

### 3.2 Datos: lo que uno guarda y el otro no

| # | Dato | En C# | En Go | Recomendación |
|---|---|---|---|---|
| 7 | **Nombre de la persona** (R2) | La clase tiene nombre, apellidos y estado civil y **no los guarda**; el nombre vale «JOHN DOE» por defecto | Nombre, dos apellidos, género, nacimiento, estado civil | El de Go. En C# son columnas nuevas en `person` |
| 8 | **Número de cliente** (F9) | En `customer` (uno por cliente) y en el perfil comercial (uno por empresa). Trench tiene 1.888 valores en el primero | En ningún sitio; decidido que sea de la relación de cliente (PU-2), sin hacer | Por pareja, como el límite de crédito. Pendiente: a qué empresa se asignan los 1.888 de Trench |
| 9 | **Bloqueo del cliente** | `customer.BlockReason` y, por empresa, `BlockOrders`/`BlockDelivery` en el perfil | En `receivables` y `orders`, por pareja | Por pareja |
| 10 | **Estado y prioridad de la relación** (F7, R10) | Dos catálogos (9 estados, 5 prioridades), obligatorios en cada relación | No existen; decidido no hacerlos (PU-8): el estado se deriva de las fechas | Mantener PU-8 y retirar de C# los que repiten las fechas. Si «pendiente» o «suspendida» tienen uso real, decirlo |
| 11 | **Participación en el capital** (F8) | Porcentaje en tres sitios; además importes y participación indirecta | Un porcentaje directo en la relación `Ownership` | Un solo sitio. Importes e indirecta, cuando alguien los pida |
| 12 | **Cargo y administrador único** de un representante | `RoleTitleCode`, `IsSoleAdministrator` en la relación | Nada | Detalle de la relación de representación; hace falta para Fiscal y Asesoría |
| 13 | **Forma jurídica y «catálogo público»** (R17) | Tipos de party 3 a 9, mezclados con las clasificaciones | `legal_form` y `shared` en la party | El de Go. En C#, dos columnas y migrar los tipos |
| 14 | **Datos de la sociedad**: NIF en `legal_organization.TaxIdentity`; en Trench además nombre fiscal, LEI, capital social | Columnas de las subclases | El NIF es una identificación; lo demás no está | NIF como identificación (una fuente). LEI y capital social: decidir si son de Parties o de Fiscal |
| 15 | **Autoridad emisora y fechas del documento** (R11) | No hay dónde guardar la autoridad | Autoridad, expedición y caducidad | El de Go. Una columna en C# |
| 16 | **País del documento y de la dirección** | Identificador de la delimitación geográfica | Código ISO de dos letras | Es de convención, pero afecta a la importación: fijar ISO en el contrato y que cada lado lo guarde como quiera |
| 17 | **Dirección postal**: Trench añade tipo de vía, escalera, planta, puerta | Columnas en `contact_mechanism` | Tipo de vía, dos líneas, indicaciones | Decidir si los campos españoles (escalera, planta, puerta) entran en el modelo |
| 18 | **Lo que sólo tiene Trench**: fusión de duplicados, idioma preferido, discapacidad y personas a cargo, correo de factura electrónica | ~45 columnas y 9 tablas | Nada | Fila a fila; discapacidad y personas a cargo son de Nóminas, no de Parties |
| 19 | **Datos del empleado** en el rol (`EmployeeNumber`, alta, baja, categoría) | En `employee` | En `hr`, por empleador | El de Go (ya decidido el 2026-09-27); falta trasladarlo |
| 20 | **Varios padres para un tipo de rol** (R5) | `role_type_hierarchy` lo permite | Un padre | Un padre. D-6 ya habla de «su parent». Comprobar que en C# ningún rol tiene dos |

### 3.3 Reglas: los dos guardan lo mismo, pero uno no lo comprueba

Ninguna toca tablas. Las cuatro primeras ya están hechas en Karpo C# (7 de octubre); falta
Trench.

| # | Regla | Estado en C# |
|---|---|---|
| 21 | Un rol no se asigna dos veces a la vez; exige tipo existente, compatible con persona u organización, y party activa (R3) | Hecho en Karpo C#, en dos de las cinco rutas que asignan roles |
| 22 | Una relación exige partes distintas que jueguen el rol de su lado (R7) | Hecho en la ruta principal de Karpo C# |
| 23 | La jerarquía de roles tiene una sola fuente (R5) | Hecho en Karpo C# |
| 24 | La jerarquía organizativa cuenta sólo lo vigente (R18) | Hecho en Karpo C# |
| 25 | Familias de clasificación exclusivas: una hoja vigente a la vez (R14) | Sin hacer |
| 26 | El alta lleva la relación con la empresa, en una sola transacción (R16) | Sin hacer: el alta rápida puede quedar a medias |
| 27 | Un documento identifica a una sola party (R11) | Se comprueba en la aplicación, sin índice único |
| 28 | Un propósito de contacto tiene un titular vigente por tipo (R13) | No existe |
| 29 | Permiso en cada operación (R19) | Muchas rutas no piden ninguno. Arreglarlo cambia el comportamiento: 403 donde hoy se entra |

**Pregunta de fondo (una sola):** la visibilidad por empresa. Go la **guarda** (afiliaciones: a
qué empresa pertenece cada party); C# la **calcula** en cada consulta. Con D-1 las dos formas
valen si dan el mismo resultado, pero hoy no lo dan: C# cuenta cualquier relación, vigente o no.
Recomiendo fijar la regla («ve a la party quien pertenece a una empresa con la que tiene una
relación vigente, o si está compartida») y que cada lado la cumpla a su manera.

### 3.4 Alcance: lo que uno tiene y el otro no

| # | Qué | Quién lo tiene | Recomendación |
|---|---|---|---|
| 30 | Cuentas bancarias de terceros (F4) | C# | A Go, en Parties |
| 31 | Consentimiento de protección de datos | C# | A Go, en Parties |
| 32 | Registro mercantil (F16) | C# | A Go, en Parties |
| 33 | Características de la party (F3): 2 tablas, ningún tipo sembrado | C# | Retirar: D-6 (extensibilidad) cubre el mismo caso |
| 34 | Roles válidos por mecanismo de contacto (F5), clasificación de personas (F6) | C# | Retirar, salvo uso que el código no enseñe |
| 35 | Comunicaciones, casos y plantillas (F1, F2, F15) | C# | Contexto propio de interacciones, fuera de Parties, en los tres |
| 36 | Tarifas, zonas, rutas, comisionistas | C# | Contexto propio de ventas y precios, fuera de Parties |
| 37 | Callejero e IAE (F17, F18) | C# | A `geography` y `fiscal` de Go |
| 38 | Afiliaciones, 18 eventos publicados, relación de cliente potencial con prueba, código de promoción | Go | A C#; todo aditivo |
| 39 | Lecturas para la pantalla: búsqueda por campos, estadísticas, vista agregada (F12) | C# | Se decide con el contrato HTTP común (§4) |

### 3.5 Supresión de datos personales: lo que hay que consultar

Javier no lo ha decidido: quiere consultar antes la ley, al menos la española. Lo que ha dejado
dicho y lo que hay hoy:

- **El documento tiene que guardar el nombre por sí mismo**, exista o no el cliente. En Go la
  factura ya lo hace: `bil_invoices` guarda `customer_nif`, `customer_name` y `customer_country`
  además del identificador (`contexts/billing/infrastructure`, línea 46). Anonimizar la party no
  cambiaría la factura emitida.
- **En el sector financiero no hay facturas sino movimientos**, y los informes al banco central
  exigen los datos. En Go los titulares de una cuenta (`fin_account_holders`) guardan sólo el
  identificador de la party: si la party se anonimiza, la cuenta se queda sin nombre de titular.
- **Puede que no se puedan borrar los registros y sí haya que dejar de enviar comunicaciones
  comerciales.** Son dos cosas distintas y el modelo tendría que separarlas.

Lo que hay que preguntar a quien sepa de la ley (lo que sigue es lo que yo conozco, **sin
contrastar hoy con el texto legal; no es asesoramiento**):

1. El derecho de supresión del RGPD tiene excepciones cuando una ley obliga a conservar el dato.
2. La ley española de protección de datos prevé el **bloqueo**: el dato no se borra, se aparta
   de todo uso y queda sólo a disposición de jueces y administraciones mientras puedan exigirse
   responsabilidades; después se destruye. Si se confirma, el modelo no necesita «anonimizar»
   sino «bloquear», con un plazo por tipo de dato.
3. Plazos de conservación que conviene confirmar: los libros y facturas del Código de Comercio,
   la prescripción tributaria, y la documentación de prevención del blanqueo en entidades
   financieras, que es más larga.
4. Oponerse a recibir publicidad es un derecho distinto y más sencillo: no exige borrar nada.

## 4. Lo que no entra aquí

- **El contrato HTTP común** (rutas, nombres de campo, paginación, errores). Es la otra mitad de
  «un mismo front para los dos» y necesita su propio documento. Depende de §3.1 y §3.2.
- **La importación y exportación que dependa del modelo y no de las tablas.** Javier lo dejó
  para investigar más despacio. Necesita que §3.2 esté cerrado.
- **Los demás contextos.** Todo esto es Parties.

## 5. Límites

- Las filas de §3 salen del estudio de convergencia (4 de octubre) y de lo leído hoy; no he
  vuelto a comprobar una a una las de §3.3 y §3.4 contra el código de C#.
- Las cifras de Trench (1.895 clientes, 1.888 números de cliente) son de la sesión de
  Trench, sobre su base local. No las he comprobado.
- «Rompe» y «aditivo» en §2 son la clasificación del estudio; no he medido el esfuerzo.
