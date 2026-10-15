# Geografía: la clasificación NUTS y la dirección postal española

> **Estado: aprobado por Javier el 2026-10-12 (las tres decisiones de §6) e implementado en
> parte.** Hechos los pasos N-1 a N-4, completos; lo que se hizo y lo que queda está en §5. Javier decidió
> ese día que la dirección postal lleva los campos españoles y que la geografía tiene que
> apoyarse en NUTS, para que el programa sirva en otro país europeo, y que había que resolverlo
> cuanto antes. Los §2 y §3 describen el punto de partida.
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

La columna de España está contrastada con la tabla oficial (§5, N-1). Las de Alemania y Francia
son lo que conozco de la clasificación y **no están contrastadas**: son un ejemplo, no un dato.

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
| 3 | **Tres provincias llevan un código NUTS 3 que no es el suyo**, porque en las islas el NUTS 3 es la isla y no la provincia | Illes Balears lleva `ES530`, Las Palmas `ES703`, Santa Cruz de Tenerife `ES707`. **Comprobado contra la tabla oficial:** `ES530` no existe (hay `ES531` Eivissa y Formentera, `ES532` Mallorca, `ES533` Menorca), `ES703` es El Hierro y `ES707` es La Palma. Los otros 75 códigos de la semilla son correctos | Un informe por NUTS 3 saldría mal para las islas |
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

## 5. Pasos y estado

| # | Paso | Estado |
|---|---|---|
| N-1 | Comprobar los códigos NUTS contra la tabla oficial | **Hecho.** La tabla de Eurostat (GISCO, `NUTS_AT_2024.csv`, descargada el 2026-10-12) está en `contexts/geography/infrastructure/seed/official/`. Los 86 códigos españoles son los mismos en NUTS 2021 y NUTS 2024. `TestSeed_NUTSCodesAreTheOfficialOnes` compara la semilla con ella |
| N-2 | Nivel y país en cada delimitación; nombres de los niveles por país | **Hecho.** Columnas `country` y `boundary_level` en `geo_boundaries` y tabla `geo_country_levels`, con España sembrada (migraciones 5 y 6). `GET /api/geography/boundaries?country=ES&level=2` devuelve las 19 comunidades y ciudades autónomas; `GET /api/reference/boundary-levels?country=ES` dice cómo se llama cada nivel y cuál es la división principal |
| N-3 | Corregir las provincias insulares y añadir las islas | **Hecho.** Las tres provincias ya no llevan código NUTS 3; existen las diez islas, cada una dentro de su provincia y con su código oficial; y los 155 municipios insulares cuelgan de su isla (migración 7), según la tabla oficial de municipios de Eurostat (`LAU_ES_2024`, también en `seed/official/`). Los 8.132 municipios de la semilla son exactamente los de esa tabla, y `TestSeed_MunicipalitiesAreInTheirOfficialRegion` comprueba que cada uno está en su región de nivel 3 |
| N-4 | Las comunidades autónomas, con su tipo propio | **Hecho.** Las 17 comunidades y las 2 ciudades autónomas son «Autonomous Community»; los identificadores no cambian. Las 7 agrupaciones de nivel 1 siguen siendo «Region» |
| N-5 | Campos españoles en la dirección de Parties | Sin hacer |
| N-6 | Un segundo país de prueba | Sin hacer |
| N-7 | Trasladarlo a Karpo C# y Trench | Sin hacer; ver «Para C#» |

**Cómo queda una delimitación:**

```json
{ "name": "Mallorca", "typeName": "Island", "nutsCode": "ES532", "country": "ES", "level": "3",
  "parent": "<Illes Balears>" }
```

**Decisiones técnicas tomadas al implementar** (reversibles; se cuentan aquí):

- El nivel es un texto: `country`, `1`, `2`, `3` o `local`. Una delimitación fuera de la
  escalera (un continente, una unión, una provincia insular) no tiene nivel.
- Cada delimitación guarda su país (`ES`). Es un dato repetido —se podría deducir subiendo por
  la jerarquía—, pero permite pedir «el nivel 2 de España» con una sola consulta.
- La versión de NUTS es una constante del programa (`domain.NUTSVersion`, hoy `2024`) y sale en
  la ruta de niveles; no hay una tabla para ella.
- Buscar una región por su código ISO 3166-2 (`ES-MD`, lo usa el calendario de festivos) ya no
  mira el tipo «Region», sino los niveles 1 y 2 del país.
- Tipo de delimitación nuevo, «Island», con el identificador
  `10000000-0000-0000-0005-000000000019`.

**Cómo queda una isla.** Palma → Mallorca (`ES532`, nivel 3) → Illes Balears (provincia `07`,
sin nivel) → Illes Balears (comunidad, nivel 2) → Este (nivel 1) → España. Un festivo declarado
en La Palma lo heredan sus 14 municipios y ninguno de El Hierro.

Dos cosas que conviene saber al consultar (las señaló la revisión independiente):

- **`country=ES&level=3` devuelve 49 provincias y 10 islas, y no las tres provincias
  insulares**, que no tienen nivel. Para «las 52 provincias» hay que filtrar por tipo.
- Los hijos de una provincia insular son ahora sus islas, no sus municipios.
- El importador de festivos todavía no sabe nombrar una isla por su código (un país es `ES`,
  una comunidad `ES-MD`, una provincia `28`, un municipio `28079`). Por la ruta sí se puede.

**Para C#** (lo que tendrían que igualar Karpo C# y Trench, que comparten estos GUID):

- El tipo «Island» y las diez islas son filas nuevas con identificadores elegidos aquí
  (`b3100000-0006-0000-0006-…01` a `…0a`, en el orden de su código NUTS). Si C# las crea, que
  sea con los mismos.
- En su semilla las tres provincias insulares llevan los mismos códigos equivocados, porque la
  de Go salió de ella, y los municipios insulares cuelgan de la provincia.

## 6. Decisiones (aprobadas por Javier el 2026-10-12: «sí a las 3»)

1. **Se separa «nivel estadístico» de «división principal»** (§4.1).
2. **Provincia e isla son delimitaciones separadas** (§4.2).
3. **Autorizada la descarga de la tabla oficial de Eurostat.** Hecha: dos ficheros CSV de unos
   90 KB (NUTS 2021 y 2024) de `gisco-services.ec.europa.eu`; en el repositorio queda el de
   2024.

4. **Autorizada la descarga de la tabla de municipios** (LAU ↔ NUTS 3), el 2026-10-12. Hecha:
   un Excel de 19 MB de `ec.europa.eu/eurostat` con todos los países; en el repositorio queda
   sólo la hoja de España, en CSV, con cuatro columnas.

## 7. Límites

- Los nombres de nivel de España («Agrupación de comunidades autónomas», «Provincia o isla»)
  los he puesto yo; no salen de ninguna tabla oficial.
- Ceuta y Melilla quedan con el tipo «Autonomous Community», aunque son ciudades autónomas.
- No he mirado cómo guardan esto Karpo C# ni Trench más allá de que Trench tiene las
  columnas de dirección.
