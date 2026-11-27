// Nombres de catálogo según `Accept-Language` (R2, M-8). Los catálogos de Go devuelven el nombre en el idioma
// de la base de datos (inglés, en `facility-types` y `boundary-types`) y el front pinta `typeName` tal cual:
// con la interfaz en español salían «Building» y «Room». El backend falso devuelve el nombre en el idioma
// que pide la petición; sin cabecera, o con otro idioma, devuelve el dato como está en el conjunto de datos.
//
// Con el Go real, que no traduce catálogos, esto queda como petición al backend (docs/PETICIONES-BACKEND.md).

export type CatalogLanguage = 'es' | 'en';

/** Idioma que pide la petición (`Accept-Language`), por orden de preferencia; `undefined` si no es es ni en. */
export function requestLanguage(request: Request): CatalogLanguage | undefined {
  const header = request.headers.get('Accept-Language');
  if (!header) return undefined;
  const ranked = header
    .split(',')
    .map((part, index) => {
      const [tag = '', ...params] = part.trim().split(';');
      const q = params.map((p) => /^\s*q\s*=\s*([\d.]+)\s*$/i.exec(p)?.[1]).find((v) => v !== undefined);
      return { base: tag.trim().toLowerCase().split('-')[0] ?? '', q: q === undefined ? 1 : Number(q), index };
    })
    .filter((entry) => entry.base !== '' && entry.q > 0)
    .sort((a, b) => b.q - a.q || a.index - b.index);
  for (const { base } of ranked) if (base === 'es' || base === 'en') return base;
  return undefined;
}

/** Valor del conjunto de datos -> su texto en cada idioma. */
const CATALOG: Record<string, Record<CatalogLanguage, string>> = {};
function entry(source: string, es: string, en: string): void {
  CATALOG[source] = { es, en };
}

// Tipos de instalación (el conjunto de datos los trae en inglés, como Go).
entry('Warehouse', 'Almacén', 'Warehouse');
entry('Plant', 'Fábrica', 'Plant');
entry('Building', 'Edificio', 'Building');
entry('Office', 'Oficina', 'Office');
entry('Floor', 'Planta', 'Floor');
entry('Room', 'Sala', 'Room');
entry('Data Center', 'Centro de datos', 'Data Center');
entry('Distribution Center', 'Centro de distribución', 'Distribution Center');
entry('Currency Exchange Office', 'Oficina de cambio de divisas', 'Currency Exchange Office');
entry('Storage facility for goods and materials', 'Instalación de almacenamiento de bienes y materiales', 'Storage facility for goods and materials');
entry('Manufacturing or production facility', 'Instalación de fabricación o producción', 'Manufacturing or production facility');
entry('Physical building structure', 'Estructura física de un edificio', 'Physical building structure');
entry('Administrative or work office space', 'Espacio de oficina administrativo o de trabajo', 'Administrative or work office space');
entry('Floor level within a building', 'Planta dentro de un edificio', 'Floor level within a building');
entry('Individual room within a facility', 'Sala individual dentro de una instalación', 'Individual room within a facility');
entry('IT infrastructure and server hosting facility', 'Infraestructura informática y alojamiento de servidores', 'IT infrastructure and server hosting facility');
entry('Logistics and distribution hub', 'Nodo logístico y de distribución', 'Logistics and distribution hub');
entry('Currency exchange office', 'Oficina de cambio de divisas', 'Currency exchange office');

// Tipos de delimitación geográfica (también en inglés en el conjunto de datos).
entry('Country', 'País', 'Country');
entry('Region', 'Región', 'Region');
entry('Autonomous Community', 'Comunidad autónoma', 'Autonomous Community');
entry('Province', 'Provincia', 'Province');
entry('Island', 'Isla', 'Island');
entry('Municipality', 'Municipio', 'Municipality');

// Niveles de delimitación de España (`boundary-levels`, en español en el conjunto de datos).
entry('País', 'País', 'Country');
entry('Agrupación de comunidades autónomas', 'Agrupación de comunidades autónomas', 'Grouping of autonomous communities');
entry('Comunidad autónoma', 'Comunidad autónoma', 'Autonomous community');
entry('Provincia o isla', 'Provincia o isla', 'Province or island');
entry('Municipio', 'Municipio', 'Municipality');

// Estados y clases de puesto (en español en el conjunto de datos). Sólo se traducen en el catálogo: el campo
// `statusName` de los puestos es la clave del `badgeMap` y se queda como viene; su texto sale de la configuración.
entry('Planificado', 'Planificado', 'Planned');
entry('Abierto', 'Abierto', 'Open');
entry('Cubierto', 'Cubierto', 'Filled');
entry('Cerrado', 'Cerrado', 'Closed');
entry('Puesto previsto, sin abrir', 'Puesto previsto, sin abrir', 'Planned position, not yet opened');
entry('Puesto abierto y sin titular', 'Puesto abierto y sin titular', 'Open position without a holder');
entry('Puesto con titular', 'Puesto con titular', 'Position with a holder');
entry('Puesto cerrado', 'Puesto cerrado', 'Closed position');
entry('Dirección', 'Dirección', 'Management');
entry('Mando intermedio', 'Mando intermedio', 'Middle management');
entry('Técnico', 'Técnico', 'Specialist');

// Títulos de puesto.
const TITLES: [es: string, en: string][] = [
  ['Director/a General', 'General Manager'],
  ['Director/a de Operaciones', 'Operations Director'],
  ['Director/a Financiero/a', 'Finance Director'],
  ['Director/a Comercial', 'Sales Director'],
  ['Director/a de Tecnología', 'Technology Director'],
  ['Responsable de Almacén', 'Warehouse Manager'],
  ['Responsable de Compras', 'Purchasing Manager'],
  ['Responsable de Mantenimiento', 'Maintenance Manager'],
  ['Controller', 'Controller'],
  ['Responsable de Tesorería', 'Treasury Manager'],
  ['Responsable de Ventas', 'Sales Manager'],
  ['Responsable de Marketing', 'Marketing Manager'],
  ['Jefe/a de Desarrollo', 'Development Lead'],
  ['Jefe/a de Sistemas', 'Systems Lead'],
  ['Jefe/a de Producto', 'Product Lead'],
  ['Supervisor/a de Almacén', 'Warehouse Supervisor'],
  ['Jefe/a de Turno', 'Shift Lead'],
  ['Comprador/a Senior', 'Senior Buyer'],
  ['Técnico/a Jefe de Mantenimiento', 'Head Maintenance Technician'],
  ['Contable Senior', 'Senior Accountant'],
  ['Analista Financiero/a', 'Financial Analyst'],
  ['Tesorero/a Adjunto/a', 'Deputy Treasurer'],
  ['Key Account Manager', 'Key Account Manager'],
  ['Responsable de Cuentas Regionales', 'Regional Accounts Manager'],
  ['Especialista de Marketing', 'Marketing Specialist'],
  ['Arquitecto/a de Software', 'Software Architect'],
  ['Líder Técnico/a', 'Technical Lead'],
  ['Administrador/a de Sistemas', 'Systems Administrator'],
  ['Product Owner', 'Product Owner'],
  ['Mozo/a de Almacén', 'Warehouse Operative'],
  ['Carretillero/a', 'Forklift Operator'],
  ['Auxiliar de Compras', 'Purchasing Assistant'],
  ['Técnico/a de Mantenimiento', 'Maintenance Technician'],
  ['Contable', 'Accountant'],
  ['Analista Junior', 'Junior Analyst'],
  ['Auxiliar de Tesorería', 'Treasury Assistant'],
  ['Ejecutivo/a de Cuentas', 'Account Executive'],
  ['Diseñador/a de Contenidos', 'Content Designer'],
  ['Desarrollador/a', 'Developer'],
  ['Técnico/a de Soporte', 'Support Technician'],
  ['Analista de Producto', 'Product Analyst'],
];
for (const [es, en] of TITLES) entry(es, es, en);

// Divisas, idiomas y tipos de vía (`reference/*`).
entry('US Dollar', 'Dólar estadounidense', 'US Dollar');
entry('Euro', 'Euro', 'Euro');
entry('Pound Sterling', 'Libra esterlina', 'Pound Sterling');
entry('Spanish', 'Español', 'Spanish');
entry('English', 'Inglés', 'English');
entry('Catalan', 'Catalán', 'Catalan');
entry('Calle', 'Calle', 'Street');
entry('Avenida', 'Avenida', 'Avenue');
entry('Paseo', 'Paseo', 'Promenade');
entry('Plaza', 'Plaza', 'Square');
entry('Carretera', 'Carretera', 'Road');
entry('Ronda', 'Ronda', 'Ring road');

/** Texto de un valor del conjunto de datos en el idioma pedido; lo que no está en la tabla queda como está. */
export function translateCatalog(value: string, language: CatalogLanguage | undefined): string {
  if (!language) return value;
  return CATALOG[value]?.[language] ?? value;
}

type Json = unknown;

/**
 * Copia de `body` con los campos `fields` traducidos (en cualquier nivel). Con `language` indefinido
 * devuelve el cuerpo sin tocar.
 */
export function localizeFields<T extends Json>(body: T, language: CatalogLanguage | undefined, fields: readonly string[]): T {
  if (!language) return body;
  const wanted = new Set(fields);
  const walk = (value: Json): Json => {
    if (Array.isArray(value)) return value.map(walk);
    if (typeof value !== 'object' || value === null) return value;
    const out: Record<string, Json> = {};
    for (const [key, child] of Object.entries(value)) {
      out[key] = wanted.has(key) && typeof child === 'string' ? translateCatalog(child, language) : walk(child);
    }
    return out;
  };
  return walk(body) as T;
}

/** Campos de las filas de entidades que son valores de catálogo: el tipo de instalación o delimitación y el título del puesto. */
const ROW_FIELDS = ['typeName', 'typeTitle'] as const;

/** Filas de una entidad (lista, detalle, organigrama) con `typeName` y `typeTitle` en el idioma de la petición. */
export function localizeRows<T extends Json>(request: Request, body: T): T {
  return localizeFields(body, requestLanguage(request), ROW_FIELDS);
}

/** Nombres y descripciones de un catálogo en el idioma de la petición. */
export function localizeCatalog<T extends Json>(request: Request, rows: T): T {
  return localizeFields(rows, requestLanguage(request), ['Name', 'Description', 'Title', 'name']);
}

/** Descripción del tipo de puesto en el idioma pedido (la del conjunto de datos es «Puesto de <título>»). */
export function positionTypeDescription(request: Request, title: string, description: string): string {
  return requestLanguage(request) === 'en' ? `${translateCatalog(title, 'en')} position` : description;
}
