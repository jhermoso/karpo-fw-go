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
| 14 | **Los campos por empresa sólo caben en entidades que son de una empresa** (relaciones, pedidos, reservas). En las que no lo son, como un rol de party, sólo caben campos de la instalación |

Se heredan de Trench, sin que Javier haya dicho otra cosa (§3.2): retirar un campo lo quita de
la definición y conserva la columna y sus datos; y el programa enseña el plan antes de aplicarlo.

Tomado por defecto, sin que Javier lo haya corregido: los campos de la instalación sólo los crea
un administrador global; los de una empresa, también el administrador de esa empresa.

## 2. El problema, con tres ejemplos

1. **Un rol.** Un cliente quiere el tipo de rol «Avalista», con el importe máximo que avala.
2. **Una relación.** El sector de cambio de divisa guarda, en la relación de colaborador, su
   código de promoción.
3. **Un pedido o una reserva.** Un sector quiere que su pedido lleve matrícula y kilómetros.

En los tres, el dueño de la entidad no sabe nada del dato nuevo, y no debería tener que
cambiar para admitirlo.

## 3. Lo que Trench ya tiene, y el contrato común

> **Reescrita el 2026-10-13.** La versión anterior de esta sección describía la *propuesta* del
> plan de Trench como si fuera su código. La sesión de Trench la corrigió punto por punto
> (catorce correcciones) al escribir el contrato común. **La fuente es ese contrato**, no esta
> sección: `doc/Contrato_Extensibilidad_Campos_Personalizados_2026-10-13.md`, PR
> [Trench#27](https://github.com/Trench/pull/27). Lo de aquí es un
> resumen de lo que dice, leído el 13 de octubre; ni el contrato ni este resumen están aprobados
> por Javier.

### 3.1 Cómo es hoy en Trench (según su sesión, leyendo su código)

| Tema | Cómo es |
|---|---|
| Dónde se declara un campo | Sólo dentro de la definición de un **producto financiero** (formato `schema 4`): un campo `kind: "Field"` con `extends: { entity }` |
| Tipos de valor | Once: sí/no, entero, decimal, porcentaje, texto, fecha, lista, y cuatro referencias a catálogo por identificador (país, divisa, instalación, transportista) que no se comprueba que existan |
| Dónde se guarda | Tabla uno a uno `<tabla>_ext_<acrónimo>`, creada **al publicar** el producto. La empresa es la que oferta el producto |
| Motores | Oracle, SQL Server y PostgreSQL (SQLite sólo en pruebas) |
| Plan antes de aplicar | Se puede pedir, pero publicar **lo recalcula**: no se aplica «el plan que se enseñó». Si falla a medias, lo ya creado queda |
| Valores | Se guardan **sólo para el pedido y sus líneas**, **sólo en el alta** y **fuera de la transacción** del pedido |
| Retirar un campo | La columna se conserva si tiene valores y **se borra si nunca los tuvo** (decisión de Javier del 8 de septiembre) |
| Reglas | Tipo, obligatorio y «obligatorio si aquel otro sí/no vale sí», este último sólo entre campos del mismo bloque |
| Permisos | Ninguno propio: basta con estar autenticado |
| Pantalla | El diseñador de operaciones financieras declara los campos; el asistente los pinta y evalúa «obligatorio si» en vivo |

### 3.2 Lo que propone el contrato común

| Tema | Propuesta |
|---|---|
| Documento | Uno por **(entidad, ámbito, empresa)**, formato `custom-fields` versión 1 |
| Campo | `code` estable (es el nombre de la columna y la clave del valor, y no cambia), etiqueta, tipo, opciones, `required`, `requiredIf`, y `onlyForType` para engancharlo a un tipo |
| Tipos de valor | Seis para empezar: sí/no, entero, decimal, texto, fecha y lista |
| Entidades | Cada contexto registra **en código** cuáles admite, si son de una empresa y si tienen tipos. Se registra la base de una jerarquía, nunca la subclase: `parties.relationship` con el tipo `customer`, no «relación de cliente» |
| Tablas | `<tabla>_ext` para la instalación y `<tabla>_ext_<acrónimo>` para cada empresa. El tipo no crea tabla |
| Ciclo | Borrador → plan con huella → aplicar (exige la huella del plan enseñado) → retirar, que no ejecuta nada en la base |
| Valores | En un bloque plano `customFields` de la propia entidad, en su misma transacción, al crear y al modificar |
| Permisos | Ver, gestionar los de la instalación y gestionar los de una empresa |
| Rutas | Bajo `/api/custom-fields` |

Esta sesión revisó el contrato y Trench incorporó los siete puntos (su §6): la base tipada en
vez de la subclase; claves de varias columnas y sin exigir borrado en cascada; los códigos de tipo
de Go; el acrónimo de empresa como requisito previo de los tres; fijar los códigos de regla y no
la forma del error; el permiso de lectura con un nombre que la regla estándar no reparte; y los
tipos de columna, orientativos para Go.

### 3.3 Lo que Go tiene que resolver antes de implementarlo

| # | Qué | Por qué |
|---|---|---|
| 1 | **Las filas hijas se borran y se reinsertan en cada guardado.** Así guarda Go un agregado (`sqlrepo`, `replaceChildren`) | Un rol es hijo de la party. Con una tabla de extensión enganchada al rol, guardar la party por cualquier motivo se llevaría los campos añadidos de todos sus roles. O el guardado del agregado escribe también la extensión, o el repositorio deja de reemplazar hijos |
| 2 | **El servidor crea tablas mientras sirve.** Hoy migrar y servir están separados y el servidor no arranca con un esquema que no es el suyo | Las tablas de extensión tienen que quedar fuera de esa comprobación, y el usuario de base de datos necesita permiso para crear tablas |
| 3 | **Acrónimo de empresa.** Go no lo tiene | Es parte del nombre de las tablas por empresa |
| 4 | **Decimales.** Go guarda todos como texto exacto | Para ordenar y filtrar por un campo decimal conviene una columna numérica; falta comprobar que la persistencia la lee sin pasar por coma flotante |
| 5 | **Cinco motores.** El contrato cubre tres; Go promete además MySQL y SQLite | En MySQL crear o cambiar una tabla confirma la transacción en curso |
| 6 | **Consultas por un campo añadido.** Go escribe las consultas a partir de campos conocidos al compilar | Hace falta decidir si se puede filtrar y ordenar por ellos, y cómo |

Hecho ya en Go, y que el contrato adopta: código estable en los tipos de rol y de relación, y el
alta de un tipo sin campos desde la aplicación ([PARTIES.md](PARTIES.md) §8).

## 4. Los otros mecanismos que existen, para no confundirlos

| Mecanismo | Dónde | Qué es | Qué pasa con él |
|---|---|---|---|
| **Perfil uno a uno hecho con código** | C#: `ErpDetail` sobre `ErpKernel` (`InternalOrganizationProfile`, `CustomerRelationshipCommercialProfile`) | Una clase y una tabla escritas por un programador, con sus reglas | Sigue siendo lo correcto cuando el dato tiene reglas. No es lo que pide §1 |
| **`IExtensible`** del framework de C# | Trench; retirado en Karpo C# | Un diccionario en memoria por instancia. No se guarda | Trench lo usa sólo como sitio donde registrar los nombres de los campos de §3. Sobra |
| **Detalles por tipo** en Go | `contexts/parties`: prueba del cliente potencial, participación, código de promoción | Columnas en la misma tabla del dueño, con reglas en su agregado | Se quedan para lo que es de Parties. El código de promoción (del sector de cambio) es candidato a pasar a extensión |
| **Perfil por pareja** en Go | `receivables`, `purchases` | Un agregado en otro contexto, por empresa y tercero | Se queda: no extiende una entidad, sino una pareja |

## 5. Propuesta: generalizar lo de Trench

> Esta sección es anterior al contrato común y queda como contexto. Donde difiera de él, manda el
> contrato: por ejemplo, el bloque de valores se llama `customFields`, no `extensions`.

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

La empresa constructora del grupo añade después «código de obra» a **sus pedidos**: va a su
propia tabla de extensión, y las demás empresas ni lo ven ni lo piden. No puede añadirlo a un
rol de party, porque un rol no es de ninguna empresa (decisión 14).

**Lo que este ejemplo obliga a resolver** y Trench no tiene (allí los campos cuelgan de un
producto, no de un tipo de catálogo):

- Que una definición de campos se enganche a **un tipo** de una entidad, no a la entidad entera.
  Hace falta dar un código estable a los tipos de rol, como ya lo tienen los de relación.
- Que convivan **dos tablas** de extensión por entidad: la de la instalación y la de cada
  empresa.
- Que cada entidad extensible declare **si es de una empresa** y, si lo es, cuál es el dato que
  lo dice. De eso depende qué ámbitos admite (decisión 14).

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
