# Extensibilidad: añadir datos a una entidad sin una versión nueva del programa

> **Estado: el requisito está decidido; el cómo y el dónde, no.** Segunda versión, del
> 2026-10-12. La primera recomendaba empezar por la extensión hecha con código; Javier decidió
> lo contrario (§1) y esta versión parte de lo que Trench ya tiene funcionando (§3).
> Lo que queda por definir con él está en §6.
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

## 6. Lo que queda por definir con Javier

Las tres primeras son las del recordatorio; las otras dos salen de leer Trench.

1. **¿Dónde vive la pantalla, y con qué permiso?** Recomiendo un apartado de administración por
   entidad («Campos de Pedido»), con un permiso propio distinto del de administrar usuarios.
   *Alternativa:* un botón «añadir campo» en cada formulario, visible sólo con el permiso.
2. **¿Los campos son por empresa, como en Trench, o también para toda la instalación?**
   Recomiendo los dos ámbitos: un campo de la instalación lo ven todas las empresas; uno de una
   empresa, sólo ella. *Ejemplo:* «matrícula» para todo el grupo; «código de obra» sólo para
   la constructora.
3. **¿Cuál es la primera entidad fuera del sector financiero?** Recomiendo el tipo de rol de
   Parties («Avalista con importe»), porque es el caso de D-6 y ejercita campos por tipo.
   *Alternativa:* el pedido de ventas.
4. **¿Se mantiene «sin lenguaje de reglas»?** En Trench lo decidiste así. Recomiendo
   mantenerlo: tipo, obligatorio y «obligatorio si». Lo que pida más es código.
5. **¿Quién lo generaliza primero?** Trench tiene el mecanismo hecho y es quien menos tiene
   que escribir; Go tiene que escribirlo entero. Recomiendo que Trench lo saque de
   `FinancialKernel` a su framework y que Go lo implemente contra el mismo contrato, para que
   el documento de campos sea el mismo en los tres.

## 7. Límites

- De Trench he leído los contratos (`ExtensionSchemaContracts.cs`), la forma normalizada de
  la definición (`DefinitionShape.cs`), el portador de valores y las secciones §45.2 y §46.6.1
  del plan. No he leído el aplicador entero ni el front, ni he ejecutado nada.
- No he comprobado qué de esto llegó a Karpo C#. La búsqueda de `IExtensible` allí sólo
  encontró su retirada.
- §5.3 son riesgos que veo, no problemas comprobados.
