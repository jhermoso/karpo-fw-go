# Extensibilidad: añadir datos a una entidad sin una versión nueva del programa

> **Estado: definida con Javier el 2026-10-12** (§1 y §6). Sin implementar. Tercera versión: la
> primera recomendaba empezar por la extensión hecha con código; Javier decidió lo contrario, y
> el diseño parte de lo que Trench ya tiene funcionando (§3). Los pasos están en §7.
>
> Viene de [PARTIES-MODELO-COMUN.md](PARTIES-MODELO-COMUN.md), decisiones D-6, D-12 y D-16.

## 1. Lo decidido por Javier

| | Decisión |
|---|---|
| 1 | La extensibilidad se hace **con el programa en marcha**, por alguien con permisos de administración |
| 2 | Aplica a **cualquier entidad**: roles, relaciones, pedidos, reservas… |
| 3 | Tiene que ser accesible **en algún espacio del front** para un usuario con esos permisos |
| 4 | El ejemplo a seguir son los **configuradores de producto de Trench**, que dejan añadir campos al definir un producto |
| 5 | Los datos nuevos van en **tablas uno a uno** con la entidad (D-6) |
| 6 | Es un **requisito del programa generado**, no de Theros: primero la implementan Karpo y Trench, y de ahí salen las plantillas (D-12) |
| 7 | Hay que **llevarla también a Trench** (fuera del sector financiero, donde ya está) |
| 8 | **Un campo es de toda la instalación o de una empresa** (los dos ámbitos). Ejemplo: «matrícula» para todo el grupo; «código de obra» sólo para la constructora |
| 9 | **La pantalla es un apartado de administración**, «Campos personalizados», con una entrada por entidad y **un permiso propio**, distinto del de administrar usuarios |
| 10 | **Reglas, para empezar, las de Trench**: tipo de valor de una lista cerrada, obligatorio y «obligatorio si aquel otro campo sí/no vale sí». Sin lenguaje de expresiones |
| 11 | **Más adelante** se estudia añadir tipos que sean objetos valor o colecciones de entidades. Eso equivale a añadir relaciones y no sólo campos, así que **se piensa antes de intentarlo**: no está diseñado |
| 12 | **La primera entidad** fuera del sector financiero es **el tipo de rol de Parties**: «Avalista» con el campo «importe máximo» |
| 13 | **Lo generaliza primero Trench**, que ya lo tiene hecho; Go lo implementa después contra el mismo contrato |

Se heredan de Trench, sin que Javier haya dicho otra cosa (§3.2): retirar un campo lo quita de
la definición y conserva la columna y sus datos; y el programa enseña el plan antes de aplicarlo.

Tomado por defecto, pendiente de que Javier lo confirme: los campos de la instalación sólo los
crea un administrador global; los de una empresa, también el administrador de esa empresa.

## 2. El problema, con tres ejemplos

1. **Un rol.** Un cliente quiere el tipo de rol «Avalista», con el importe máximo que avala.
2. **Una relación.** El sector de cambio de divisa guarda, en la relación de colaborador, su
   código de promoción.
3. **Un pedido o una reserva.** Un sector quiere que su pedido lleve matrícula y kilómetros.

En los tres, el dueño de la entidad no sabe nada del dato nuevo, y no debería tener que
cambiar para admitirlo.

## 3. Lo que Trench ya tiene (leído en su código y en su plan)

El sector financiero de Trench lo resolvió entre agosto y septiembre, y **Javier ya tomó
allí las decisiones de fondo** (`doc/Plan_Legs_Autoconocimiento_2026-08-31.md`, §45 y §46.6.1,
8 de septiembre). Es más de lo que yo había visto al escribir la primera versión.

### 3.1 Cómo funciona

1. **Un documento declara los campos.** La definición de un producto (un JSON que se diseña en
   una pantalla del front, `views/product-designer`) lleva una lista de campos. Cada uno tiene
   nombre, tipo de valor de una lista cerrada, ámbito (el pedido o cada línea), si es
   obligatorio, y sus opciones si es una lista.

   ```jsonc
   { "name": "Motivo del viaje", "kind": "Marker", "valueType": "enumerated", "scope": "order",
     "options": ["Turismo", "Negocios", "Estudios"], "required": true }
   ```

2. **Cada campo dice dónde se guarda**, y el programa lo deduce:

   | Dónde | Cuándo | Ejemplo |
   |---|---|---|
   | En un atributo que **ya existe** | El campo cita una entidad y un atributo reales | «Nota del coleccionista» se guarda en el comentario de la línea del pedido |
   | En una **tabla de extensión** | El campo declara que extiende una entidad | «Coleccionista» va a una tabla nueva junto a la línea del pedido |

3. **Al publicar la definición, el programa crea las tablas.** Por cada entidad extendida, una
   tabla uno a uno **por entidad y por empresa**, llamada `<tabla>_ext_<acrónimo de la empresa>`,
   con la misma clave que la entidad y una columna por campo. Esto es «DDL en caliente»: el
   programa ejecuta `CREATE TABLE` y `ALTER TABLE` mientras está en marcha, en vez de esperar a
   una versión nueva.

   *Ejemplo:* la empresa con acrónimo `mac` extiende las líneas de pedido financiero y obtiene
   `financial_order_leg_ext_mac` con la columna `coleccionista`.

4. **Primero se planifica, luego se aplica.** El programa compara lo que la definición pide con
   lo que hay en la base y enseña el plan (qué crearía, qué conservaría) antes de ejecutar nada.
   Aplicar dos veces da el mismo resultado.

5. **Al operar, los valores se validan contra la definición.** Un campo no declarado se
   rechaza nombrándolo; un valor que no casa con su tipo, también; un obligatorio vacío,
   también. La única regla entre campos admitida es «obligatorio si aquel otro campo, que es un
   sí/no, vale sí».

### 3.2 Lo que Javier decidió allí

| Decisión (Trench, 2026-09-08) | Qué significa |
|---|---|
| Tabla uno a uno **por entidad y por empresa** | Cada empresa del grupo tiene sus propios campos; no se pisan |
| **El ciclo de vida de la definición y el de los datos son distintos** | Retirar un campo lo quita de la definición: deja de pedirse y de enseñarse. La columna y sus datos **se conservan**. Sólo se borra de verdad una columna que nunca recibió un valor |
| Se diseña contra una copia de preproducción y se **importa el JSON** en producción cuando está probado | El documento es portable entre instalaciones |
| **No hay lenguaje de reglas**: sólo tipo, obligatorio y «obligatorio si» | Una regla de verdad es código, no configuración |

### 3.3 Lo que le falta para ser «para cualquier entidad»

| Límite de hoy | Por qué importa |
|---|---|
| Los campos se declaran **dentro de la definición de un producto financiero**. No hay forma de declarar un campo para una entidad sin pasar por un producto | Un rol de Parties o un pedido de ventas no tienen «definición de producto» |
| Sólo se extienden **las entidades que toca una operación financiera** (el pedido y sus líneas) | Hay que poder nombrar cualquier entidad extensible |
| Vive en `FinancialKernel`, no en el framework | Karpo C# no lo tiene; Go tampoco |
| La pantalla es el diseñador de productos | Hace falta un sitio general de administración |

No he ejecutado nada de esto: lo describo por el código, sus contratos y el plan. No he mirado
cómo pinta el front los campos al capturar ni si se pueden filtrar listas por ellos.

## 4. Los otros mecanismos que existen, para no confundirlos

| Mecanismo | Dónde | Qué es | Qué pasa con él |
|---|---|---|---|
| **Perfil uno a uno hecho con código** | C#: `ErpDetail` sobre `ErpKernel` (`InternalOrganizationProfile`, `CustomerRelationshipCommercialProfile`) | Una clase y una tabla escritas por un programador, con sus reglas | Sigue siendo lo correcto cuando el dato tiene reglas. No es lo que pide §1 |
| **`IExtensible`** del framework de C# | Trench; retirado en Karpo C# | Un diccionario en memoria por instancia. No se guarda | Trench lo usa sólo como sitio donde registrar los nombres de los campos de §3. Sobra |
| **Detalles por tipo** en Go | `contexts/parties`: prueba del cliente potencial, participación, código de promoción | Columnas en la misma tabla del dueño, con reglas en su agregado | Se quedan para lo que es de Parties. El código de promoción (del sector de cambio) es candidato a pasar a extensión |
| **Perfil por pareja** en Go | `receivables`, `purchases` | Un agregado en otro contexto, por empresa y tercero | Se queda: no extiende una entidad, sino una pareja |

## 5. Propuesta: generalizar lo de Trench

La idea es sacar el mecanismo de §3 del sector financiero y dejarlo como una pieza del
framework, igual en Go, en Karpo C# y en Trench.

### 5.1 Las piezas

| Pieza | Qué hace | En Trench hoy |
|---|---|---|
| **Entidad extensible** | Cada contexto declara cuáles de sus entidades admiten campos, con su tabla y su clave | Lista fija de las entidades de una operación |
| **Definición de campos** | Por entidad, empresa y —si la entidad tiene tipos— tipo: nombre, tipo de valor, obligatorio, «obligatorio si», opciones | Dentro de la definición del producto |
| **Aplicador de esquema** | Planifica y aplica las tablas uno a uno en el motor activo | Hecho para cuatro motores |
| **Validador** | Comprueba los valores contra la definición al crear o cambiar la entidad | Hecho, para operaciones |
| **Almacén de valores** | Lee y escribe la fila de extensión junto a la entidad, en su misma transacción | Hecho, para pedido y líneas |
| **Lectura compuesta** | La entidad se devuelve con un bloque `extensions` | Parcial |
| **Pantalla de administración** | Un espacio del front donde quien tiene el permiso declara campos y ve el plan antes de aplicarlo | El diseñador de productos |
| **Descripción para el front** | El front pregunta qué campos tiene una entidad y pinta el formulario solo | Sin comprobar |

### 5.2 Cómo encaja con los tipos de catálogo (D-6)

- Tipo de rol **sin** campos: una fila con su padre.
- Tipo de rol **con** campos: la misma fila, más una definición de campos enganchada a ese
  tipo. *Ejemplo:* «Avalista» con el campo «importe máximo», que sólo se pide al asignar ese
  rol.
- Lo mismo para los tipos de relación.

### 5.3 Lo que hay que mirar con cuidado en Go

1. **El programa crea tablas mientras sirve.** Hoy en Go migrar y servir están separados a
   propósito, y el servidor se niega a arrancar con un esquema que no es el suyo (decisión 29).
   Las tablas de extensión tienen que quedar fuera de esa comprobación, o registrarse en ella.
2. **Cinco motores.** El aplicador de Trench cubre cuatro; Go promete además MySQL, donde
   crear o cambiar una tabla confirma la transacción en curso: no se puede deshacer.
3. **Permisos.** El usuario de base de datos del programa necesita poder crear tablas. Hoy sólo
   lo necesita quien migra.
4. **Nombres.** `<tabla>_ext_<acrónimo>` tiene que caber en el límite de cada motor.
5. **Buscar y ordenar por un campo declarado.** Con tabla uno a uno es una unión normal, que es
   la ventaja frente a guardar un JSON; pero el framework de Go escribe las consultas a partir
   de campos conocidos al compilar, y estos no lo son.
6. **El «color» de Theros.** Una tabla de extensión es, para C#, una tabla más de las de
   herencia por tabla; para Go es una tabla nueva. Vale para los dos.

## 6. Cómo queda, con el ejemplo elegido

Un administrador abre «Campos personalizados → Tipo de rol → Avalista» y añade «importe
máximo», decimal y obligatorio, para toda la instalación.

1. El programa le enseña el plan: «se creará la tabla de extensión de los roles de party, con
   la columna `importe_maximo`».
2. Al aplicar, la tabla existe. Nadie ha instalado una versión nueva.
3. Desde ese momento, asignar el rol «Avalista» pide el importe; sin él, la asignación se
   rechaza nombrando el campo. Asignar cualquier otro rol no lo pide.
4. Al leer la party, el rol trae su bloque de campos añadidos.
5. Si un día se retira el campo, deja de pedirse y de enseñarse; lo guardado se conserva.

La empresa constructora del grupo añade después «código de obra» sólo para ella: va a su propia
tabla de extensión, y las demás empresas ni lo ven ni lo piden.

**Lo que este ejemplo obliga a resolver** y Trench no tiene (allí los campos cuelgan de un
producto, no de un tipo de catálogo):

- Que una definición de campos se enganche a **un tipo** de una entidad, no a la entidad entera.
  Hace falta dar un código estable a los tipos de rol, como ya lo tienen los de relación.
- Que convivan **dos tablas** de extensión por entidad: la de la instalación y la de cada
  empresa.
- Qué empresa «es» la de un rol de party, para saber qué campos de empresa se le aplican. Un
  rol no pertenece a una empresa; una relación sí. **Sin resolver**: puede que los campos por
  empresa sólo tengan sentido en entidades que ya son de una empresa (relaciones, pedidos) y que
  en los roles sólo quepan los de la instalación.

## 7. Pasos

| # | Paso | Quién | Hecho cuando |
|---|---|---|---|
| X-1 | Escribir el **contrato común**: el documento que declara los campos (el de §3.1, sin lo que es propio de un producto financiero) y las rutas de administración | Trench, que parte del suyo; Go lo revisa | Las dos sesiones dan el mismo documento por bueno |
| X-2 | Sacar el mecanismo de `FinancialKernel` al framework de Trench, con los dos ámbitos y el enganche a un tipo | Trench | El caso «Avalista» funciona en Trench |
| X-3 | Código estable en los tipos de rol; añadir por HTTP un tipo de rol sin campos (D-6, caso 1) | Go | Un tipo de rol nuevo con su padre se crea y se asigna; uno que cierra un ciclo se rechaza |
| X-4 | El mecanismo en el framework de Go contra el contrato de X-1: definición, plan, aplicación en los motores, validación, lectura compuesta | Go | El caso «Avalista» funciona en Go, en memoria y en los motores |
| X-5 | Karpo C# lo recibe de Trench | Karpo C# | El mismo caso funciona allí |
| X-6 | La pantalla de administración en el front | Front de Karpo | Un administrador añade el campo sin tocar código |
| X-7 | Plantillas en Theros, cuando las tres implementaciones coincidan | Theros | Lo generado pasa la batería de X-4 |

No he estimado tiempos. X-3 no depende de nadie y se puede empezar ya en Go.

## 8. Límites

- De Trench he leído los contratos (`ExtensionSchemaContracts.cs`), la forma normalizada de
  la definición (`DefinitionShape.cs`), el portador de valores y las secciones §45.2 y §46.6.1
  del plan. No he leído el aplicador entero ni el front, ni he ejecutado nada.
- No he comprobado qué de esto llegó a Karpo C#. La búsqueda de `IExtensible` allí sólo
  encontró su retirada.
- §5.3 son riesgos que veo, no problemas comprobados.
