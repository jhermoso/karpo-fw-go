# Geografía: la clasificación NUTS y la dirección postal española

> **Estado: análisis y propuesta, sin aprobar.** Escrito el 2026-10-12. Javier decidió ese día
> que la dirección postal lleva los campos españoles y que la geografía tiene que apoyarse en
> NUTS, para que el programa sirva en otro país europeo, y que hay que resolverlo cuanto antes.
> Este documento dice qué hay hoy, qué falla y qué propongo. Las decisiones están en §6.
>
> Continúa [GEOGRAFIA.md](GEOGRAFIA.md).

## 1. Qué es NUTS, en corto

NUTS es la clasificación de Eurostat que parte cada país de la Unión en tres niveles de
regiones, con un código que crece una letra o cifra por nivel. Por debajo están las LAU
(unidades locales: los municipios).

*Ejemplo:* `ES` España → `ES6` Sur → `ES61` Andalucía → `ES618` Sevilla → LAU `41091` Sevilla
capital.

Dos cosas que conviene saber antes de apoyarse en ella:

1. **Es una clasificación estadística, no administrativa.** Los niveles se eligen para que las
   regiones sean comparables en población, así que **el mismo nivel no significa lo mismo en
   cada país**:

   | Nivel | España | Alemania | Francia |
   |---|---|---|---|
   | NUTS 1 | Agrupaciones de comunidades (Noroeste, Sur…): **no son una administración** | Länder (estados federados) | Regiones |
   | NUTS 2 | Comunidades y ciudades autónomas | Regierungsbezirke (distritos de gobierno) | Regiones anteriores a 2016 |
   | NUTS 3 | Provincias; en Baleares y Canarias, **islas** | Kreise (distritos) | Departamentos |

   La comunidad autónoma (NUTS 2) y el Land (NUTS 1) son el mismo concepto político —la primera
   división del país— y están en niveles NUTS distintos. NUTS por sí sola no traduce
   «comunidad autónoma» por «Land».

2. **Sólo cubre la Unión** (y, con otro nombre, los países candidatos y de la EFTA). El Reino
   Unido tiene su propia clasificación desde que salió. Fuera de Europa no hay nada parecido.

Lo que sí da NUTS, y es lo que hace falta: **una escalera común y con código oficial** sobre la
que colgar los nombres de cada país. Para traducir el concepto político hace falta, además, que
cada país diga cómo se llama cada peldaño y cuál es su «primera división».

Lo de esta sección es lo que conozco de la clasificación (versión 2021); **no lo he contrastado
hoy con las tablas de Eurostat**.

## 2. Lo que hay hoy en Go (leído en la semilla)

`contexts/geography`: `geo_boundaries` ya tiene las columnas `nuts_code` y `lau_code`, y la API
las devuelve (`nutsCode`).

| Tipo de delimitación | Filas | Con código NUTS | Con código LAU |
|---|---:|---:|---:|
| Municipio | 8.132 | 0 | 8.132 (igual al código del INE) |
| País | 248 | 1 (España) | 0 |
| Provincia | 52 | 52 | 0 |
| Región | 26 | 26 | 0 |
| El resto (continentes, uniones, foros…) | 39 | 0 | 0 |

Es decir: **España está entera y el resto del mundo sólo tiene países.** La base ya existe.

## 3. Lo que falla

| # | Defecto | Ejemplo | Consecuencia |
|---|---|---|---|
| 1 | **El nivel no es un dato.** Las 7 agrupaciones NUTS 1 y las 19 comunidades NUTS 2 tienen el mismo tipo, «Región». El nivel sólo se adivina por la longitud del código | «Noroeste» (`ES1`) y «Galicia» (`ES11`) son las dos «Región» | No se puede pedir «las comunidades autónomas» sin contar letras |
| 2 | **El tipo «Comunidad Autónoma» existe y no tiene ninguna fila.** Tampoco «Estado», «Departamento» ni «Cantón» | Andalucía es «Región», no «Comunidad Autónoma» | El nombre español del nivel no está en ningún sitio |
| 3 | **Tres provincias llevan un código NUTS 3 que no es el suyo**, porque en las islas el NUTS 3 es la isla y no la provincia | Illes Balears lleva `ES530`, Las Palmas `ES703`, Santa Cruz de Tenerife `ES707`. Según lo que conozco, `ES530` no existe (hay `ES531` Eivissa y Formentera, `ES532` Mallorca, `ES533` Menorca), `ES703` es El Hierro y `ES707` es La Palma | Un informe por NUTS 3 saldría mal para las islas. **Hay que comprobarlo contra la tabla oficial antes de tocar nada** |
| 4 | **Las islas no existen** como delimitación | No hay «Mallorca» entre la provincia y el municipio | No se puede situar una dirección en su NUTS 3 real |
| 5 | **No hay nombre local del nivel por país.** Los tipos son una lista única en inglés | Para Alemania haría falta decir que su NUTS 1 se llama «Land» | El formulario de dirección no sabe qué etiqueta poner en otro país |
| 6 | **La versión de NUTS no consta.** La clasificación cambia cada pocos años | — | Un código puede dejar de valer sin que se sepa |
| 7 | **La dirección de Parties guarda la región como texto libre** (`region`), además de los identificadores de geografía | «Andalucia», «ANDALUCÍA», «AN» | El texto no se puede traducir ni agregar |

Además, de la dirección postal: Go guarda tipo de vía, dos líneas, indicaciones, código postal,
localidad, región y país (`contexts/parties/domain/contacts.go:68`). **No tiene número, escalera,
planta, puerta ni letra**, que Trench sí tiene como columnas.

## 4. Propuesta

### 4.1 El nivel como dato, con nombre por país

Dos piezas pequeñas, las dos aditivas:

1. **Cada delimitación dice su nivel** en la escalera: país, primer nivel, segundo, tercero,
   local. Para un país de la Unión coincide con NUTS 1, 2, 3 y LAU.
2. **Cada país dice cómo se llama cada peldaño y cuál es su división principal.**

   | País | Nivel 1 | Nivel 2 | Nivel 3 | Local | División principal |
   |---|---|---|---|---|---|
   | España | Agrupación (estadística) | Comunidad autónoma | Provincia o isla | Municipio | **Nivel 2** |
   | Alemania | Land | Regierungsbezirk | Kreis | Gemeinde | **Nivel 1** |
   | Francia | Région | (región anterior) | Département | Commune | **Nivel 1** |

   La columna «división principal» es la que contesta la pregunta de Javier: «lo que en España
   es la comunidad autónoma, en Alemania es el Land». El formulario pide «la división principal
   del país» y la etiqueta sale de esta tabla.

Con esto la traducción entre países es: mismo peldaño para la estadística, misma «división
principal» para lo político.

### 4.2 La provincia y la isla

La provincia española es una administración real (con su código del INE, que usan Hacienda y
el código postal) y no siempre es una región NUTS 3. Propongo **no forzar que coincidan**:

- La provincia se queda como está, con su código del INE.
- Se añaden las islas de Baleares y Canarias como delimitaciones de nivel 3, dentro de su
  provincia.
- Una provincia peninsular es a la vez provincia y región de nivel 3: lleva su código NUTS 3.
  Una provincia insular no lleva código NUTS 3; lo llevan sus islas.

### 4.3 La versión

Una fila de referencia que diga qué versión de NUTS lleva la semilla, y la fecha. Cambiar de
versión es una migración de datos.

### 4.4 La dirección postal

Campos nuevos, todos opcionales, en la dirección de Parties: **número, bloque, portal, escalera,
planta, puerta**. El tipo de vía ya está. La región deja de ser texto libre cuando la dirección
lleva los identificadores de geografía: se lee de la delimitación.

Los nombres exactos conviene tomarlos de Trench (`StreetNumber2`, `Staircase`, `Floor`,
`Door`, `Letter`), que ya los tiene, para que el modelo común sea uno.

### 4.5 De dónde salen los datos

Los códigos NUTS y los nombres de nivel no deben escribirse de memoria: son datos oficiales.
Propongo cargarlos de la tabla que publica Eurostat, descargada una vez por una persona y
guardada en el repositorio como el resto de la semilla, igual que se hizo con el calendario de
festivos (nada se descarga de internet desde el programa).

## 5. Pasos

| # | Paso | Rompe | Hecho cuando |
|---|---|---|---|
| N-1 | Comprobar los 52 códigos NUTS 3 y los 26 de región contra la tabla oficial | Nada | Una prueba compara la semilla con el fichero oficial guardado en el repositorio |
| N-2 | Nivel en cada delimitación y tabla de niveles por país (España sembrada) | Nada: columnas y tabla nuevas | `GET /api/geography/boundaries?country=ES&level=2` devuelve las 19 comunidades y ciudades autónomas |
| N-3 | Corregir las tres provincias insulares y añadir las islas | Datos: cambian tres códigos | Mallorca existe, cuelga de Illes Balears y lleva `ES532` |
| N-4 | Las comunidades autónomas pasan a su tipo propio | Datos: cambia el tipo de 19 filas (los identificadores no cambian) | Andalucía es «Comunidad Autónoma» |
| N-5 | Campos españoles en la dirección de Parties | Nada: columnas nuevas | Una dirección con escalera y planta se guarda y se lee; los eventos `v1` no cambian |
| N-6 | Un segundo país de prueba (Alemania o Portugal), sólo niveles 1 a 3 | Nada | El formulario de dirección de una empresa alemana pide «Land» |
| N-7 | Trasladar N-2 a N-5 a Karpo C# y Trench | Lo decide cada sesión | — |

No he estimado tiempos. N-1 necesita que alguien descargue el fichero oficial.

## 6. Decisiones que tocan a Javier

1. **¿Vale separar «nivel estadístico» de «división principal»?** (§4.1). Sin eso, NUTS no
   traduce comunidad autónoma por Land, porque están en niveles distintos. *Alternativa:* usar
   sólo el nivel NUTS y aceptar que el nivel 1 español (Noroeste, Sur…) no significa nada para
   un usuario.
2. **¿Provincia e isla separadas?** (§4.2). *Alternativa:* partir las provincias insulares y
   dejar que el NUTS 3 mande; rompe la correspondencia con el código del INE que usan Hacienda
   y los códigos postales.
3. **¿Quién descarga la tabla oficial de Eurostat?** Yo no descargo ficheros sin que lo
   apruebes; si me lo autorizas, lo hago y lo dejo en el repositorio con su fecha y su origen.

## 7. Límites

- Los códigos NUTS «correctos» de §3 fila 3 y la tabla de §1 son de mi conocimiento de NUTS
  2021, no de una comprobación hecha hoy.
- No he mirado cómo guardan esto Karpo C# ni Trench más allá de que Trench tiene las
  columnas de dirección.
- Sólo he leído la semilla y el esquema de Go; no he ejecutado nada.
