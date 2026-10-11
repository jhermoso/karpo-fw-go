# Extensibilidad: añadir datos a una entidad que es de otro

> **Estado: propuesta, sin aprobar.** Escrita el 2026-10-12 a petición de Javier. Las decisiones
> que le tocan están en §7.
>
> Viene de [PARTIES-MODELO-COMUN.md](PARTIES-MODELO-COMUN.md): D-6 (un tipo de rol nuevo con
> atributos va por «la interfaz de extensibilidad», con tablas uno a uno) y D-12 (la
> extensibilidad es un requisito del programa generado: primero la implementan Karpo y
> Trench, y de ahí salen las plantillas de Theros).

## 1. El problema, con tres ejemplos

Una entidad pertenece a un contexto, y otro —un sector, un módulo, un cliente— necesita
guardarle datos que el dueño no conoce.

1. **Un rol.** Un cliente quiere el tipo de rol «Avalista», con el importe máximo que avala.
   Parties no sabe nada de avales.
2. **Una relación.** El sector de cambio de divisa guarda, en la relación de colaborador, su
   código de promoción. Parties no sabe nada de promociones.
3. **Un pedido o una reserva.** Un sector quiere que su pedido lleve matrícula del vehículo y
   kilómetros. Pedidos no sabe nada de vehículos.

Lo que se pide: poder hacerlo **sin tocar el contexto dueño**, con las reglas del dato en quien
lo añade, y que se pueda leer y escribir por HTTP junto a la entidad.

## 2. Lo que ya existe (leído en el código)

Hay cuatro mecanismos distintos, y conviene no confundirlos.

| # | Mecanismo | Dónde | Cómo guarda | Estado |
|---|---|---|---|---|
| 1 | **Perfil uno a uno, en otra capa** | C#, en los dos: `ErpDetail` sobre `ErpKernel`. `InternalOrganizationProfile` cuelga del rol de organización interna; `CustomerRelationshipCommercialProfile`, de la relación de cliente | Tabla propia con la clave de la entidad base | **En uso**: 30 tablas de ErpDetail en Parties. Es código, con sus reglas |
| 2 | **`IExtensible`** del framework de C# | `Trench.Fw.Domain.Contracts/…/IExtensible.cs` en Trench. En Karpo C# se **retiró** (0 usos; queda la prueba `RetiredTraitsTests`) | Un diccionario en memoria por instancia: propiedades y métodos registrados en ejecución. **No se guarda en la base** | En Trench lo usa una sola pieza (la fila 3) como sitio donde registrar nombres |
| 3 | **Campos de captura** del sector financiero | Trench: `Trench.FinancialKernel.Application/…/CaptureFields/` | La definición del producto declara los campos (nombre, tipo, obligatorio) y la operación los guarda como un documento JSON validado contra esa definición; los que apuntan a un atributo real se escriben en él | En uso sólo en el sector financiero de Trench. No es código por campo: son datos |
| 4 | **Detalles por tipo**, en Go | `contexts/parties`: `RelationshipDetails` (prueba, participación, código de promoción) | Columnas anulables en la **misma** tabla del dueño (`party_relationships.trial_until`…), discriminadas por el código del tipo | En uso (decisión PU-1 del 2026-10-04). Exige tocar el contexto dueño |

Y en Go, además, lo que ya hacen `receivables`, `purchases` y `hr`: un agregado propio en su
contexto, enlazado por la **pareja** empresa–tercero. Es el mecanismo 1 con otra clave.

Lectura:

- El «ya tenemos algún caso en Trench» es el mecanismo 1 (perfiles) y, para campos
  declarados como datos, el 3 (captura).
- El mecanismo 2 es el que se llamaba «interfaz de extensibilidad», y no sirve tal cual: lo que
  se registra no se persiste ni se audita. Por eso Karpo C# lo retiró.
- El ejemplo 2 de §1 está resuelto hoy en Go con el mecanismo 4: `promotion_code` es una
  columna de la tabla de Parties aunque el dato es del sector de cambio (decisión 38 del
  2026-10-10). Funciona, pero es justo lo que la extensibilidad quiere evitar.

## 3. Son dos necesidades, no una

| | **A. Extensión por código** | **B. Campos declarados** |
|---|---|---|
| Quién la crea | Un programador (o Theros, generando) | Un administrador, desde la aplicación |
| Cuándo | Al construir el programa | Con el programa en marcha |
| Tiene reglas propias | Sí («el importe avalado no puede ser negativo») | Sólo tipo y obligatorio |
| Cómo se guarda | Tabla uno a uno con columnas de verdad | Definiciones más valores (JSON o filas nombre–valor) |
| Se puede buscar y ordenar por el dato | Sí, como cualquier columna | Depende del motor; es lo caro |
| Precedente | Perfiles de ErpDetail; perfiles por pareja en Go | Campos de captura de Trench |
| En el modelo de Theros | Otro agregado, declarado como extensión de uno existente | Un catálogo de definiciones; el modelo sólo ve «campos extra» |

D-6 pide tablas uno a uno, que es la **A**. La B es otro trabajo, con otro coste, y se puede
hacer después sin deshacer la A. Es la primera decisión de §7.

## 4. Propuesta para la A: la extensión como agregado uno a uno

### 4.1 La idea

Una **extensión** es un agregado del contexto que la añade, cuya identidad **es** la de la
entidad que extiende. No tiene clave propia: tiene la del rol, la relación o el pedido.

```text
parties.party_roles (party_id, id, role_type, valid_from, valid_to)      <- del dueño, intacta
guarantees.guarantor_roles (role_id, max_amount, currency, …)            <- la extensión
```

- El dueño **no cambia**: ni tabla, ni clase, ni rutas.
- Las reglas del dato viven en el agregado de la extensión.
- Para el esquema de C# es exactamente una tabla más de las que ya hay con herencia por tabla;
  para el de Go, una tabla nueva en otro contexto. Vale para los dos «colores».

### 4.2 Las cuatro costuras que hacen falta

Lo que hoy no existe en ninguno de los dos y hay que construir una vez, en el framework:

| Costura | Qué resuelve | Ejemplo |
|---|---|---|
| **Declaración** | El dueño dice qué entidades suyas admiten extensiones; la extensión dice a cuál se engancha y, si la entidad tiene tipos, a qué tipo | Parties declara extensibles `PartyRole` y `Relationship`. «Avalista» se engancha a `PartyRole` cuando el tipo de rol tiene el código `guarantor` |
| **Alta conjunta** | Crear la entidad y su extensión de una vez, o ninguna | `POST /api/parties/{id}/roles` con `{ "roleType": "…", "extensions": { "guarantor": { "maxAmount": "50000.00" } } }`. Si el importe es inválido, el rol no se crea |
| **Lectura compuesta** | Al leer la entidad vienen sus extensiones, sin que el dueño sepa cuáles hay | El rol se devuelve con `"extensions": { "guarantor": { … } }` |
| **Veto** | La extensión puede impedir que el dueño termine, desactive o anule | No se puede terminar el rol de avalista mientras tenga un aval vivo. Es el mismo mecanismo que pide D-4 («no se puede desactivar algo de lo que dependen otras cosas») |

En un monolito modular (D-2) las cuatro son llamadas en el mismo proceso y la misma
transacción: el dueño ofrece un **registro de extensiones** y el anfitrión enchufa las que
haya. Cuando haya varios procesos, el alta conjunta y el veto pasan a necesitar una llamada
entre servicios; por eso conviene que la costura sea un puerto desde el primer día.

### 4.3 El tipo de catálogo dice qué extensión lleva

Go ya tiene la pieza: `relationship_types.code` (decisión PU-3). El tipo es una fila; el código
es lo que el programa entiende.

- Tipo **sin** código: no lleva datos. Añadirlo es añadir una fila con su padre (D-6, caso 1).
- Tipo **con** código que alguien ha registrado como extensión: lleva esa extensión (D-6, caso 2).
- Falta darle código también a los tipos de **rol** (hoy sólo lo tienen los de relación).

### 4.4 Qué pasa con lo que ya hay

| Hoy | Propuesta |
|---|---|
| Detalles por tipo de Go (prueba del cliente potencial, participación): columnas de `party_relationships` | **Se quedan.** Son datos de Parties, con reglas de Parties. La extensión es para lo que añade **otro** |
| `promotion_code` en `party_relationships` (es del sector de cambio) | Buen candidato a **primera extensión real**: se movería a una tabla del contexto de cambio. No se toca hasta que Javier lo diga: está aprobado así desde el 2026-10-10 |
| Perfiles por pareja de Go (`receivables`, `purchases`) | **Se quedan.** No extienden una entidad, sino una pareja; sobreviven a que la relación termine y se vuelva a abrir |
| Perfiles de ErpDetail en C# | Ya son extensiones uno a uno. Les faltan las costuras: hoy el alta no es conjunta y la lectura no es compuesta (cada perfil tiene sus rutas aparte) |
| `IExtensible` de Trench | Retirarlo, como hizo Karpo C#, cuando los campos de captura no lo necesiten |

## 5. Si se quiere la B: campos declarados

Sólo el esquema, para que conste qué costaría.

- **Definiciones**: por entidad (y tipo, si los tiene), una lista de campos con nombre, tipo de
  valor de una lista cerrada (texto, entero, decimal, fecha, sí/no, lista) y si es obligatorio.
- **Valores**: o un documento JSON por entidad (como los campos de captura de Trench), o una
  fila por campo y entidad.
- **Lo caro**: buscar y ordenar por un campo declarado. Con JSON, cada uno de los motores tiene
  sus funciones; con filas, una unión por campo. El framework de Go promete lo mismo en todos
  los motores, así que habría que limitar qué se puede filtrar.
- **Lo que no puede tener**: reglas entre campos. En cuanto hace falta una, es una extensión A.

Recomendación: no hacerla ahora. La A cubre D-6, y el precedente de Trench demuestra que la
B se puede añadir después sobre la misma declaración de «entidad extensible».

## 6. Pasos propuestos

| # | Paso | Dónde | Hecho cuando |
|---|---|---|---|
| E-1 | Aprobar o corregir este documento | Javier | §7 contestada |
| E-2 | Las cuatro costuras en el framework de Go, sin tocar ningún contexto | `pkg/` | Una extensión de prueba sobre la entidad de ejemplo: alta conjunta, lectura compuesta, veto, y rechazo de una extensión no declarada |
| E-3 | Parties declara extensibles el rol y la relación; código en los tipos de rol; añadir por HTTP un tipo sin datos (D-6, caso 1) | `contexts/parties` | Un tipo de rol nuevo con su padre se crea por HTTP y se puede asignar; uno con un ciclo en la jerarquía se rechaza |
| E-4 | Primera extensión real (la que se elija en §7) | Su contexto | La batería HTTP la ejercita en memoria y en los motores |
| E-5 | Lo mismo en Karpo C#: las costuras sobre los perfiles de ErpDetail | Sesión de Karpo | Un perfil se crea y se lee junto a su rol o su relación |
| E-6 | La plantilla en Theros: «extensión de la entidad X por el módulo Y» | Sesión de Theros | El modelo declara una extensión y lo generado pasa la batería de E-2 |

E-2 a E-4 son de este repositorio. No he estimado tiempos.

## 7. Decisiones que tocan a Javier

1. **¿Sólo extensión por código (A), o también campos que un administrador declara con el
   programa en marcha (B)?** Recomiendo A ahora y B cuando haya un cliente que lo pida.
   *Ejemplo:* con A, «Avalista con importe» llega en una versión del programa; con B, lo crea
   el administrador una tarde, pero sin reglas y sin poder ordenar la lista por importe.
2. **¿Las extensiones viajan dentro de la entidad o como recurso aparte?** Recomiendo dentro
   (`"extensions": { … }` en el rol, la relación o el pedido), porque es lo que permite el alta
   conjunta y que el front pinte un solo formulario. La alternativa (rutas propias por
   extensión, como los perfiles de C# hoy) es más simple y deja el alta en dos pasos.
3. **¿Cuál es la primera extensión real?** Recomiendo el código de promoción del colaborador:
   ya existe, es de un sector y hoy ocupa una columna de Parties. La alternativa es un caso
   nuevo en pedidos o reservas, que tendría la ventaja de no mover nada aprobado.

## 8. Límites

- De C# he leído el contrato `IExtensible`, el registrador de campos de captura y un perfil de
  ErpDetail. No he leído cómo se cablea un perfil de punta a punta ni cómo extienden los
  sectores un pedido o una reserva.
- No he comprobado si la persistencia de Go puede escribir una entidad y su extensión de otro
  contexto en la misma transacción hoy; en un solo proceso debería, pero es lo primero que
  E-2 tiene que demostrar.
- Los campos de captura de Trench los describo por el comentario de cabecera de su
  validador, no por haberlos ejecutado.
