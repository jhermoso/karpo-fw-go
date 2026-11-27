// España para la demostración: la escalera NUTS (7 agrupaciones, 19 comunidades, 59 provincias e
// islas) y una muestra recortada de municipios de Madrid (ES300) y Barcelona (ES511) con su código
// INE y un código postal de cada uno (para el sembrador contra Go, que resuelve la delimitación
// por código postal y país). Datos escritos a mano; la muestra de municipios no es completa.
//
// `cell` es la posición de cada comunidad en el cartograma en rejilla de scripts/gen-geo.mjs
// (columna, fila, ancho y alto en celdas): geometría esquemática, no cartográfica.

/** @type {{ code: string, name: string }[]} */
export const SPAIN_NUTS1 = [
  { code: 'ES1', name: 'Noroeste' },
  { code: 'ES2', name: 'Noreste' },
  { code: 'ES3', name: 'Comunidad de Madrid' },
  { code: 'ES4', name: 'Centro' },
  { code: 'ES5', name: 'Este' },
  { code: 'ES6', name: 'Sur' },
  { code: 'ES7', name: 'Canarias' },
];

/** @type {{ code: string, name: string, nuts1: string, cell: { x: number, y: number, w: number, h: number } }[]} */
export const SPAIN_NUTS2 = [
  { code: 'ES11', name: 'Galicia', nuts1: 'ES1', cell: { x: 0, y: 0, w: 1, h: 2 } },
  { code: 'ES12', name: 'Principado de Asturias', nuts1: 'ES1', cell: { x: 1, y: 0, w: 1, h: 1 } },
  { code: 'ES13', name: 'Cantabria', nuts1: 'ES1', cell: { x: 2, y: 0, w: 1, h: 1 } },
  { code: 'ES21', name: 'País Vasco', nuts1: 'ES2', cell: { x: 3, y: 0, w: 1, h: 1 } },
  { code: 'ES22', name: 'Comunidad Foral de Navarra', nuts1: 'ES2', cell: { x: 4, y: 0, w: 1, h: 1 } },
  { code: 'ES23', name: 'La Rioja', nuts1: 'ES2', cell: { x: 4, y: 1, w: 1, h: 1 } },
  { code: 'ES24', name: 'Aragón', nuts1: 'ES2', cell: { x: 5, y: 0, w: 1, h: 2 } },
  { code: 'ES30', name: 'Comunidad de Madrid', nuts1: 'ES3', cell: { x: 3, y: 3, w: 1, h: 1 } },
  { code: 'ES41', name: 'Castilla y León', nuts1: 'ES4', cell: { x: 1, y: 1, w: 3, h: 2 } },
  { code: 'ES42', name: 'Castilla-La Mancha', nuts1: 'ES4', cell: { x: 4, y: 2, w: 2, h: 2 } },
  { code: 'ES43', name: 'Extremadura', nuts1: 'ES4', cell: { x: 1, y: 3, w: 2, h: 2 } },
  { code: 'ES51', name: 'Cataluña', nuts1: 'ES5', cell: { x: 6, y: 0, w: 2, h: 2 } },
  { code: 'ES52', name: 'Comunitat Valenciana', nuts1: 'ES5', cell: { x: 6, y: 2, w: 2, h: 2 } },
  { code: 'ES53', name: 'Illes Balears', nuts1: 'ES5', cell: { x: 8, y: 1, w: 1, h: 2 } },
  { code: 'ES61', name: 'Andalucía', nuts1: 'ES6', cell: { x: 1, y: 5, w: 5, h: 1 } },
  { code: 'ES62', name: 'Región de Murcia', nuts1: 'ES6', cell: { x: 6, y: 4, w: 1, h: 1 } },
  { code: 'ES63', name: 'Ciudad de Ceuta', nuts1: 'ES6', cell: { x: 1, y: 6, w: 1, h: 1 } },
  { code: 'ES64', name: 'Ciudad de Melilla', nuts1: 'ES6', cell: { x: 3, y: 6, w: 1, h: 1 } },
  { code: 'ES70', name: 'Canarias', nuts1: 'ES7', cell: { x: 0, y: 7, w: 4, h: 1 } },
];

/** @type {{ code: string, name: string, nuts2: string, ine: string }[]} */
export const SPAIN_NUTS3 = [
  { code: 'ES111', name: 'A Coruña', nuts2: 'ES11', ine: '15' },
  { code: 'ES112', name: 'Lugo', nuts2: 'ES11', ine: '27' },
  { code: 'ES113', name: 'Ourense', nuts2: 'ES11', ine: '32' },
  { code: 'ES114', name: 'Pontevedra', nuts2: 'ES11', ine: '36' },
  { code: 'ES120', name: 'Asturias', nuts2: 'ES12', ine: '33' },
  { code: 'ES130', name: 'Cantabria', nuts2: 'ES13', ine: '39' },
  { code: 'ES211', name: 'Araba/Álava', nuts2: 'ES21', ine: '01' },
  { code: 'ES212', name: 'Gipuzkoa', nuts2: 'ES21', ine: '20' },
  { code: 'ES213', name: 'Bizkaia', nuts2: 'ES21', ine: '48' },
  { code: 'ES220', name: 'Navarra', nuts2: 'ES22', ine: '31' },
  { code: 'ES230', name: 'La Rioja', nuts2: 'ES23', ine: '26' },
  { code: 'ES241', name: 'Huesca', nuts2: 'ES24', ine: '22' },
  { code: 'ES242', name: 'Teruel', nuts2: 'ES24', ine: '44' },
  { code: 'ES243', name: 'Zaragoza', nuts2: 'ES24', ine: '50' },
  { code: 'ES300', name: 'Madrid', nuts2: 'ES30', ine: '28' },
  { code: 'ES411', name: 'Ávila', nuts2: 'ES41', ine: '05' },
  { code: 'ES412', name: 'Burgos', nuts2: 'ES41', ine: '09' },
  { code: 'ES413', name: 'León', nuts2: 'ES41', ine: '24' },
  { code: 'ES414', name: 'Palencia', nuts2: 'ES41', ine: '34' },
  { code: 'ES415', name: 'Salamanca', nuts2: 'ES41', ine: '37' },
  { code: 'ES416', name: 'Segovia', nuts2: 'ES41', ine: '40' },
  { code: 'ES417', name: 'Soria', nuts2: 'ES41', ine: '42' },
  { code: 'ES418', name: 'Valladolid', nuts2: 'ES41', ine: '47' },
  { code: 'ES419', name: 'Zamora', nuts2: 'ES41', ine: '49' },
  { code: 'ES421', name: 'Albacete', nuts2: 'ES42', ine: '02' },
  { code: 'ES422', name: 'Ciudad Real', nuts2: 'ES42', ine: '13' },
  { code: 'ES423', name: 'Cuenca', nuts2: 'ES42', ine: '16' },
  { code: 'ES424', name: 'Guadalajara', nuts2: 'ES42', ine: '19' },
  { code: 'ES425', name: 'Toledo', nuts2: 'ES42', ine: '45' },
  { code: 'ES431', name: 'Badajoz', nuts2: 'ES43', ine: '06' },
  { code: 'ES432', name: 'Cáceres', nuts2: 'ES43', ine: '10' },
  { code: 'ES511', name: 'Barcelona', nuts2: 'ES51', ine: '08' },
  { code: 'ES512', name: 'Girona', nuts2: 'ES51', ine: '17' },
  { code: 'ES513', name: 'Lleida', nuts2: 'ES51', ine: '25' },
  { code: 'ES514', name: 'Tarragona', nuts2: 'ES51', ine: '43' },
  { code: 'ES521', name: 'Alicante/Alacant', nuts2: 'ES52', ine: '03' },
  { code: 'ES522', name: 'Castellón/Castelló', nuts2: 'ES52', ine: '12' },
  { code: 'ES523', name: 'Valencia/València', nuts2: 'ES52', ine: '46' },
  { code: 'ES531', name: 'Eivissa y Formentera', nuts2: 'ES53', ine: '07' },
  { code: 'ES532', name: 'Mallorca', nuts2: 'ES53', ine: '07' },
  { code: 'ES533', name: 'Menorca', nuts2: 'ES53', ine: '07' },
  { code: 'ES611', name: 'Almería', nuts2: 'ES61', ine: '04' },
  { code: 'ES612', name: 'Cádiz', nuts2: 'ES61', ine: '11' },
  { code: 'ES613', name: 'Córdoba', nuts2: 'ES61', ine: '14' },
  { code: 'ES614', name: 'Granada', nuts2: 'ES61', ine: '18' },
  { code: 'ES615', name: 'Huelva', nuts2: 'ES61', ine: '21' },
  { code: 'ES616', name: 'Jaén', nuts2: 'ES61', ine: '23' },
  { code: 'ES617', name: 'Málaga', nuts2: 'ES61', ine: '29' },
  { code: 'ES618', name: 'Sevilla', nuts2: 'ES61', ine: '41' },
  { code: 'ES620', name: 'Murcia', nuts2: 'ES62', ine: '30' },
  { code: 'ES630', name: 'Ceuta', nuts2: 'ES63', ine: '51' },
  { code: 'ES640', name: 'Melilla', nuts2: 'ES64', ine: '52' },
  { code: 'ES703', name: 'El Hierro', nuts2: 'ES70', ine: '38' },
  { code: 'ES704', name: 'Fuerteventura', nuts2: 'ES70', ine: '35' },
  { code: 'ES705', name: 'Gran Canaria', nuts2: 'ES70', ine: '35' },
  { code: 'ES706', name: 'La Gomera', nuts2: 'ES70', ine: '38' },
  { code: 'ES707', name: 'La Palma', nuts2: 'ES70', ine: '38' },
  { code: 'ES708', name: 'Lanzarote', nuts2: 'ES70', ine: '35' },
  { code: 'ES709', name: 'Tenerife', nuts2: 'ES70', ine: '38' },
];

/**
 * Muestra de municipios por provincia (NUTS 3): código INE (5 cifras, igual al `lauCode`), nombre
 * y un código postal del municipio.
 * @type {Record<string, { ine: string, name: string, postalCode: string }[]>}
 */
export const MUNICIPALITIES = {
  ES300: [
    { ine: '28079', name: 'Madrid', postalCode: '28001' },
    { ine: '28005', name: 'Alcalá de Henares', postalCode: '28801' },
    { ine: '28006', name: 'Alcobendas', postalCode: '28100' },
    { ine: '28007', name: 'Alcorcón', postalCode: '28920' },
    { ine: '28058', name: 'Fuenlabrada', postalCode: '28940' },
    { ine: '28065', name: 'Getafe', postalCode: '28901' },
    { ine: '28074', name: 'Leganés', postalCode: '28910' },
    { ine: '28080', name: 'Majadahonda', postalCode: '28220' },
    { ine: '28092', name: 'Móstoles', postalCode: '28930' },
    { ine: '28106', name: 'Parla', postalCode: '28980' },
    { ine: '28115', name: 'Pozuelo de Alarcón', postalCode: '28223' },
    { ine: '28123', name: 'Rivas-Vaciamadrid', postalCode: '28521' },
    { ine: '28127', name: 'Las Rozas de Madrid', postalCode: '28230' },
    { ine: '28148', name: 'Torrejón de Ardoz', postalCode: '28850' },
  ],
  ES511: [
    { ine: '08019', name: 'Barcelona', postalCode: '08001' },
    { ine: '08015', name: 'Badalona', postalCode: '08911' },
    { ine: '08066', name: 'Castelldefels', postalCode: '08860' },
    { ine: '08073', name: 'Cornellà de Llobregat', postalCode: '08940' },
    { ine: '08096', name: 'Granollers', postalCode: '08400' },
    { ine: '08101', name: "L'Hospitalet de Llobregat", postalCode: '08901' },
    { ine: '08113', name: 'Manresa', postalCode: '08240' },
    { ine: '08121', name: 'Mataró', postalCode: '08301' },
    { ine: '08169', name: 'El Prat de Llobregat', postalCode: '08820' },
    { ine: '08184', name: 'Rubí', postalCode: '08191' },
    { ine: '08187', name: 'Sabadell', postalCode: '08201' },
    { ine: '08205', name: 'Sant Cugat del Vallès', postalCode: '08172' },
    { ine: '08245', name: 'Santa Coloma de Gramenet', postalCode: '08921' },
    { ine: '08279', name: 'Terrassa', postalCode: '08221' },
  ],
};

/**
 * Nombres cortos para los rótulos del mapa (M-3), por código de delimitación: cuando el nombre completo no
 * cabe en su tesela se escribe éste (y el completo queda en `title` y en la tabla). Las que no figuran aquí
 * usan su código si tampoco cabe el nombre. Datos escritos a mano, como el resto de la demo.
 * @type {Record<string, string>}
 */
export const SHORT_NAMES = {
  // Comunidades (NUTS 2).
  ES12: 'Asturias',
  ES22: 'Navarra',
  ES30: 'Madrid',
  ES41: 'Cast. y León',
  ES42: 'C.-La Mancha',
  ES52: 'C. Valenciana',
  ES53: 'Baleares',
  ES62: 'Murcia',
  ES63: 'Ceuta',
  ES64: 'Melilla',
  // Provincias (NUTS 3).
  ES211: 'Álava',
  ES521: 'Alicante',
  ES522: 'Castellón',
  ES523: 'Valencia',
  ES531: 'Eivissa',
  // Municipios de la muestra (código INE).
  '28005': 'Alcalá',
  '28115': 'Pozuelo',
  '28123': 'Rivas',
  '28127': 'Las Rozas',
  '28148': 'Torrejón',
  '08073': 'Cornellà',
  '08101': "L'Hospitalet",
  '08169': 'El Prat',
  '08205': 'Sant Cugat',
  '08245': 'Santa Coloma',
};
