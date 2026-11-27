// Datos deterministas de la demostración (ESM puro, sin dependencias). Lo comparten el backend
// falso de MSW (demo/mocks) y el sembrador contra cmd/karpo (scripts/seed-karpo.mjs).
//
// Semilla fija 20261127 con el generador mulberry32 y «hoy» fijo en 2026-11-27: dos generaciones dan
// exactamente los mismos datos (hay una prueba con el SHA-256). Todo con id UUID determinista y
// `version`. Las formas son las de Go (docs/analisis/02-api-karpo-go.md): importes como cadena,
// fechas YYYY-MM-DD, identificadores y fechas vacías como "", catálogos con los nombres de Go.
import { COUNTRY_SEEDS } from './countries.mjs';
import { MUNICIPALITIES, SPAIN_NUTS1, SPAIN_NUTS2, SPAIN_NUTS3 } from './spain.mjs';

export const SEED = 20261127;
export const TODAY = '2026-11-27';
/** Instante fijo de «ahora» dentro de los datos (RFC 3339). */
export const NOW_ISO = '2026-11-27T09:00:00Z';

// ---------------------------------------------------------------------------------------------
// Utilidades deterministas
// ---------------------------------------------------------------------------------------------

/** PRNG mulberry32: devuelve una función que da números en [0, 1). */
export function mulberry32(seed) {
  let a = seed >>> 0;
  return function next() {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const HEX = '0123456789abcdef';

/** Identificador con forma de UUID v7 (versión 7, variante 10) sacado del generador. */
function uuidFrom(rng) {
  const chars = [];
  for (let i = 0; i < 32; i += 1) chars.push(HEX[Math.floor(rng() * 16)]);
  chars[12] = '7';
  chars[16] = HEX[8 + Math.floor(rng() * 4)];
  const s = chars.join('');
  return `${s.slice(0, 8)}-${s.slice(8, 12)}-${s.slice(12, 16)}-${s.slice(16, 20)}-${s.slice(20)}`;
}

/** Identificador fijo para catálogos con valor conocido: 10000000-0000-0000-<grupo>-<n>. */
function wellKnown(group, n) {
  return `10000000-0000-0000-${String(group).padStart(4, '0')}-${String(n).padStart(12, '0')}`;
}

function makeKit(rng) {
  const uuid = () => uuidFrom(rng);
  const int = (min, max) => min + Math.floor(rng() * (max - min + 1));
  const pick = (list) => list[Math.floor(rng() * list.length)];
  /** Baraja determinista (Fisher-Yates) de una copia. */
  const shuffle = (list) => {
    const out = [...list];
    for (let i = out.length - 1; i > 0; i -= 1) {
      const j = Math.floor(rng() * (i + 1));
      [out[i], out[j]] = [out[j], out[i]];
    }
    return out;
  };
  return { uuid, int, pick, shuffle };
}

function pad(n, width = 2) {
  return String(n).padStart(width, '0');
}

/** Suma días a una fecha YYYY-MM-DD sin depender de la zona horaria. */
export function addDays(date, days) {
  const [y, m, d] = date.split('-').map(Number);
  const t = new Date(Date.UTC(y, m - 1, d + days));
  return `${t.getUTCFullYear()}-${pad(t.getUTCMonth() + 1)}-${pad(t.getUTCDate())}`;
}

/** Diferencia en meses completos entre dos fechas YYYY-MM-DD (desde ≤ hasta). */
function monthsBetween(from, to) {
  const [fy, fm, fd] = from.split('-').map(Number);
  const [ty, tm, td] = to.split('-').map(Number);
  let months = (ty - fy) * 12 + (tm - fm);
  if (td < fd) months -= 1;
  return Math.max(0, months);
}

/** Céntimos (entero) a cadena decimal con dos cifras. */
function cents(n) {
  const sign = n < 0 ? '-' : '';
  const abs = Math.abs(Math.round(n));
  return `${sign}${Math.floor(abs / 100)}.${pad(abs % 100)}`;
}

/** Instante RFC 3339 en UTC, a partir de una fecha y minutos desde las 00:00. */
function instant(date, minutes) {
  return `${date}T${pad(Math.floor(minutes / 60))}:${pad(minutes % 60)}:00Z`;
}

/** IBAN español con dígitos de control válidos (módulo 97) a partir de 20 cifras. */
function spanishIban(twentyDigits) {
  const numeric = `${twentyDigits}142800`; // «ES00» = 14 28 00
  let remainder = 0;
  for (const ch of numeric) remainder = (remainder * 10 + Number(ch)) % 97;
  const check = 98 - remainder;
  return `ES${pad(check)}${twentyDigits}`;
}

// ---------------------------------------------------------------------------------------------
// Geografía
// ---------------------------------------------------------------------------------------------

const CURRENCY = { US: 'USD', GB: 'GBP', JP: 'JPY', CH: 'CHF', CN: 'CNY', IN: 'INR', BR: 'BRL', MX: 'MXN' };
const LANGUAGE = { ES: 'es', FR: 'fr', DE: 'de', IT: 'it', PT: 'pt', GB: 'en', US: 'en', NL: 'nl' };
const ZONE = { ES: 'Europe/Madrid', FR: 'Europe/Paris', DE: 'Europe/Berlin', IT: 'Europe/Rome', PT: 'Europe/Lisbon', GB: 'Europe/London' };

function buildGeography(kit) {
  const boundaryTypes = [
    { id: wellKnown(9, 1), name: 'Country' },
    { id: wellKnown(9, 2), name: 'Region' },
    { id: wellKnown(9, 3), name: 'Autonomous Community' },
    { id: wellKnown(9, 4), name: 'Province' },
    { id: wellKnown(9, 5), name: 'Island' },
    { id: wellKnown(9, 6), name: 'Municipality' },
  ];
  const typeId = (name) => boundaryTypes.find((t) => t.name === name).id;

  const boundaries = [];
  const countries = [];
  const boundaryOf = {};
  for (const c of COUNTRY_SEEDS) {
    const id = kit.uuid();
    boundaryOf[c.alpha2] = id;
    boundaries.push({
      id,
      name: c.name,
      type: typeId('Country'),
      typeName: 'Country',
      geoCode: c.alpha2,
      abbreviation: c.alpha3,
      country: c.alpha2,
      level: 'country',
      active: true,
    });
    const isEs = c.alpha2 === 'ES';
    countries.push({
      id: c.alpha2,
      alpha2: c.alpha2,
      alpha3: c.alpha3,
      numeric: c.numeric,
      name: c.name,
      boundary: id,
      callingCode: c.callingCode,
      defaultLocale: isEs ? 'es-ES' : '',
      eu: c.eu,
      eurozone: c.eurozone,
      sepa: c.sepa,
      eea: c.eea,
      currencies: c.eurozone ? ['EUR'] : CURRENCY[c.alpha2] ? [CURRENCY[c.alpha2]] : [],
      languages: LANGUAGE[c.alpha2] ? [LANGUAGE[c.alpha2]] : [],
      timeZones: ZONE[c.alpha2] ? [ZONE[c.alpha2]] : [],
      ibanLength: isEs ? 24 : 0,
      ibanPattern: isEs ? 'ES\\d{22}' : '',
      postalCodePattern: isEs ? '^\\d{5}$' : '',
      postalCodeRequired: isEs,
      requiresSubdivision: false,
    });
  }

  const byNuts = {};
  const esId = boundaryOf.ES;
  for (const n1 of SPAIN_NUTS1) {
    const b = {
      id: kit.uuid(),
      name: n1.name,
      type: typeId('Region'),
      typeName: 'Region',
      nutsCode: n1.code,
      country: 'ES',
      level: '1',
      parent: esId,
      active: true,
    };
    byNuts[n1.code] = b;
    boundaries.push(b);
  }
  for (const n2 of SPAIN_NUTS2) {
    const b = {
      id: kit.uuid(),
      name: n2.name,
      type: typeId('Autonomous Community'),
      typeName: 'Autonomous Community',
      nutsCode: n2.code,
      country: 'ES',
      level: '2',
      parent: byNuts[n2.nuts1].id,
      active: true,
    };
    byNuts[n2.code] = b;
    boundaries.push(b);
  }
  const islandCodes = new Set(['ES531', 'ES532', 'ES533', 'ES703', 'ES704', 'ES705', 'ES706', 'ES707', 'ES708', 'ES709']);
  for (const n3 of SPAIN_NUTS3) {
    const island = islandCodes.has(n3.code);
    const b = {
      id: kit.uuid(),
      name: n3.name,
      type: typeId(island ? 'Island' : 'Province'),
      typeName: island ? 'Island' : 'Province',
      nutsCode: n3.code,
      ineCode: n3.ine,
      country: 'ES',
      level: '3',
      parent: byNuts[n3.nuts2].id,
      active: true,
    };
    byNuts[n3.code] = b;
    boundaries.push(b);
  }
  const municipalities = [];
  for (const [nuts3, list] of Object.entries(MUNICIPALITIES)) {
    for (const m of list) {
      const b = {
        id: kit.uuid(),
        name: m.name,
        type: typeId('Municipality'),
        typeName: 'Municipality',
        ineCode: m.ine,
        lauCode: m.ine,
        country: 'ES',
        level: 'local',
        parent: byNuts[nuts3].id,
        active: true,
      };
      municipalities.push({ ...m, nuts3, boundary: b.id });
      boundaries.push(b);
    }
  }
  return { boundaryTypes, boundaries, countries, boundaryOf, byNuts, municipalities };
}

// ---------------------------------------------------------------------------------------------
// Personas y organizaciones
// ---------------------------------------------------------------------------------------------

const GIVEN = ['Lucía', 'Mateo', 'Carmen', 'Javier', 'Marta', 'Pablo', 'Elena', 'Andrés', 'Sofía', 'Daniel', 'Paula', 'Álvaro', 'Irene', 'Rubén', 'Nuria', 'Sergio', 'Laura', 'Hugo', 'Alba', 'Iván', 'Clara', 'Óscar', 'Inés', 'Adrián', 'Beatriz', 'Marcos'];
const SURNAME = ['Almeida', 'Bermejo', 'Cordero', 'Delgado', 'Escudero', 'Fontana', 'Gallardo', 'Herrero', 'Iglesias', 'Lorenzo', 'Montero', 'Navarro', 'Ortega', 'Palacios', 'Quintana', 'Rivera', 'Salvador', 'Tapia', 'Uribe', 'Vidal', 'Zamora', 'Arroyo', 'Campos', 'Duarte'];

/** Tipos de rol de Parties (los mismos UUID que la semilla de Go). */
export const ROLE = {
  internalOrganization: wellKnown(1, 1),
  supplier: wellKnown(1, 3),
  carrier: wellKnown(1, 0x24),
  department: wellKnown(1, 9),
  employee: wellKnown(1, 0x10),
};

const INTERNAL_ORGANIZATIONS = [
  { legalName: 'Karpo Industrial, S.A.', tradeName: 'Karpo Industrial' },
  { legalName: 'Karpo Logística, S.L.', tradeName: 'Karpo Logística' },
  { legalName: 'Karpo Servicios Digitales, S.L.', tradeName: 'Karpo Digital' },
];

const COMPANY_PREFIX = ['Metalúrgica', 'Distribuciones', 'Suministros', 'Industrias', 'Servicios', 'Talleres', 'Maquinaria', 'Embalajes', 'Papelería', 'Informática', 'Eléctricas', 'Hidráulica', 'Plásticos', 'Herramientas', 'Mantenimiento'];
const COMPANY_NAME = ['Aldeana', 'Bermeja', 'Cantábrica', 'Duero', 'Ebro', 'Fénix', 'Garona', 'Henares', 'Iberia', 'Jarama', 'Levante', 'Mediterránea', 'Nervión', 'Océano', 'Pirineos', 'Rioja', 'Sierra', 'Tajo', 'Ulía', 'Vega', 'Xanadú', 'Zafra', 'Alcarria', 'Bética', 'Cierzo'];
const LEGAL_FORM = ['S.L.', 'S.A.', 'S.L.U.', 'S.Coop.'];

// ---------------------------------------------------------------------------------------------
// Instalaciones
// ---------------------------------------------------------------------------------------------

const FACILITY_TYPES = [
  { ID: wellKnown(8, 1), Name: 'Warehouse', Description: 'Storage facility for goods and materials', RequiresContact: false, Active: true },
  { ID: wellKnown(8, 2), Name: 'Plant', Description: 'Manufacturing or production facility', RequiresContact: false, Active: true },
  { ID: wellKnown(8, 3), Name: 'Building', Description: 'Physical building structure', RequiresContact: false, Active: true },
  { ID: wellKnown(8, 4), Name: 'Office', Description: 'Administrative or work office space', RequiresContact: true, Active: true },
  { ID: wellKnown(8, 5), Name: 'Floor', Description: 'Floor level within a building', RequiresContact: false, Active: true },
  { ID: wellKnown(8, 6), Name: 'Room', Description: 'Individual room within a facility', RequiresContact: false, Active: true },
  { ID: wellKnown(8, 7), Name: 'Data Center', Description: 'IT infrastructure and server hosting facility', RequiresContact: false, Active: true },
  { ID: wellKnown(8, 8), Name: 'Distribution Center', Description: 'Logistics and distribution hub', RequiresContact: false, Active: true },
  { ID: wellKnown(8, 9), Name: 'Currency Exchange Office', Description: 'Currency exchange office', RequiresContact: true, Active: true },
];

/** Edificios: [organización, nombre, municipio (INE), calle, número, plantas, salas]. */
const BUILDINGS = [
  [0, 'Sede Central Madrid', '28079', ['Paseo', 'de la Castellana', '112'], 3, ['Sala de Juntas', 'Sala Atlas', 'Archivo', 'Sala Becario', 'Recepción']],
  [0, 'Nave Logística Alcalá', '28005', ['Calle', 'del Hierro', '8'], 1, ['Muelle de Carga', 'Oficina de Tráfico']],
  [1, 'Oficina Barcelona Diagonal', '08019', ['Avenida', 'Diagonal', '421'], 2, ['Sala Gaudí', 'Sala Cerdà', 'Sala Mercè']],
  [1, 'Plataforma El Prat', '08169', ['Calle', 'de la Logística', '15'], 1, ['Cámara Fría']],
  [2, 'Centro de Datos Getafe', '28065', ['Calle', 'de los Servidores', '3'], 2, ['Sala Fría A', 'Sala Fría B']],
  [2, 'Estudio Sabadell', '08187', ['Calle', 'de la Indústria', '27'], 1, ['Sala Creativa']],
];

function buildFacilities(kit, orgs, municipalities) {
  const typeIdOf = (name) => FACILITY_TYPES.find((t) => t.Name === name).ID;
  const phoneBase = 910000000;
  const facilities = [];
  let order = 0;
  for (const [orgIndex, name, ine, street, floors, rooms] of BUILDINGS) {
    const muni = municipalities.find((m) => m.ine === ine);
    const region = muni.nuts3 === 'ES300' ? 'Madrid' : 'Barcelona';
    const location = {
      streetType: street[0],
      line1: street[1],
      number: street[2],
      postalCode: muni.postalCode,
      locality: muni.name,
      region,
      country: 'ES',
      geoBoundary: muni.boundary,
      phone: `+34${phoneBase + order * 1117}`,
      email: `${name.toLowerCase().normalize('NFD').replace(/[^a-z0-9]+/g, '.').replace(/^\.+|\.+$/g, '')}@karpo.example`,
    };
    order += 1;
    const buildingId = kit.uuid();
    const area = kit.int(8, 60) * 100;
    const mk = (id, type, label, partOf, areaM2, extra) => ({
      id,
      organization: orgs[orgIndex].id,
      type: typeIdOf(type),
      typeName: type,
      name: label,
      description: '',
      partOf,
      areaM2: String(areaM2),
      location: { ...location, ...extra },
      active: true,
      version: 1,
    });
    facilities.push(mk(buildingId, 'Building', name, '', area, {}));
    const floorIds = [];
    for (let f = 0; f < floors; f += 1) {
      const id = kit.uuid();
      floorIds.push(id);
      facilities.push(mk(id, 'Floor', f === 0 ? `${name} · Planta baja` : `${name} · Planta ${f}`, buildingId, Math.round(area / (floors + 1)), { floor: f === 0 ? 'Baja' : String(f) }));
    }
    rooms.forEach((label, r) => {
      const floorIndex = r % floors;
      facilities.push(mk(kit.uuid(), 'Room', `${label} (${name.split(' ')[0]} ${name.split(' ')[1] ?? ''})`.trim(), floorIds[floorIndex], kit.int(12, 90), { floor: floorIndex === 0 ? 'Baja' : String(floorIndex), door: `S${r + 1}` }));
    });
  }
  return facilities;
}

function buildWorkCenters(kit, orgs, facilities) {
  const upper = (s) => s.normalize('NFD').replace(/[^A-Za-z]/g, '').slice(0, 3).toUpperCase();
  const rows = [];
  for (let i = 0; i < 12; i += 1) {
    const orgIndex = i % 3;
    // Cada centro, en una instalación distinta (edificio o planta) de su empresa.
    const pool = facilities.filter((f) => f.organization === orgs[orgIndex].id && f.typeName !== 'Room');
    const facility = pool[Math.floor(i / 3) % pool.length];
    const closed = i === 5 || i === 9;
    const opened = addDays('2018-02-01', kit.int(0, 2400));
    rows.push({
      id: kit.uuid(),
      employer: orgs[orgIndex].id,
      facility: facility.id,
      code: `WC-${upper(facility.location.locality)}-${pad(i + 1)}`,
      headquarters: i < 3,
      opened,
      closed: closed ? addDays(opened, kit.int(400, 900)) : '',
      active: !closed,
      version: 1,
    });
  }
  return rows;
}

// ---------------------------------------------------------------------------------------------
// Festivos
// ---------------------------------------------------------------------------------------------

function buildHolidays(kit, geo) {
  const es = geo.boundaryOf.ES;
  const madridRegion = geo.byNuts.ES30.id;
  const madridCity = geo.municipalities.find((m) => m.ine === '28079').boundary;
  const cataluna = geo.byNuts.ES51.id;
  const barcelonaCity = geo.municipalities.find((m) => m.ine === '08019').boundary;
  const nameOf = new Map(geo.boundaries.map((b) => [b.id, b.name]));
  const specs = [
    [es, '2026-01-01', 'Año Nuevo'],
    [es, '2026-01-06', 'Epifanía del Señor'],
    [es, '2026-04-03', 'Viernes Santo'],
    [es, '2026-05-01', 'Fiesta del Trabajo'],
    [es, '2026-08-15', 'Asunción de la Virgen'],
    [es, '2026-10-12', 'Fiesta Nacional de España'],
    [es, '2026-11-01', 'Todos los Santos'],
    [es, '2026-12-08', 'Inmaculada Concepción'],
    [es, '2026-12-25', 'Natividad del Señor'],
    [es, '2027-01-01', 'Año Nuevo'],
    [es, '2027-01-06', 'Epifanía del Señor'],
    [es, '2027-03-26', 'Viernes Santo'],
    [es, '2027-05-01', 'Fiesta del Trabajo'],
    [es, '2027-08-15', 'Asunción de la Virgen'],
    [es, '2027-10-12', 'Fiesta Nacional de España'],
    [es, '2027-11-01', 'Todos los Santos'],
    [es, '2027-12-08', 'Inmaculada Concepción'],
    [es, '2027-12-25', 'Natividad del Señor'],
    [madridRegion, '2026-04-02', 'Jueves Santo'],
    [madridRegion, '2026-05-02', 'Fiesta de la Comunidad de Madrid'],
    [madridRegion, '2027-03-25', 'Jueves Santo'],
    [madridRegion, '2027-05-02', 'Fiesta de la Comunidad de Madrid'],
    [madridCity, '2026-05-15', 'San Isidro Labrador'],
    [madridCity, '2026-11-09', 'Nuestra Señora de la Almudena'],
    [madridCity, '2027-05-15', 'San Isidro Labrador'],
    [madridCity, '2027-11-09', 'Nuestra Señora de la Almudena'],
    [cataluna, '2026-04-06', 'Lunes de Pascua'],
    [cataluna, '2026-06-24', 'San Juan'],
    [cataluna, '2026-09-11', 'Fiesta Nacional de Cataluña'],
    [cataluna, '2026-12-26', 'San Esteban'],
    [cataluna, '2027-03-29', 'Lunes de Pascua'],
    [cataluna, '2027-06-24', 'San Juan'],
    [cataluna, '2027-09-11', 'Fiesta Nacional de Cataluña'],
    [cataluna, '2027-12-26', 'San Esteban'],
    [barcelonaCity, '2026-09-24', 'La Mercè'],
    [barcelonaCity, '2027-09-24', 'La Mercè'],
    [geo.boundaryOf.FR, '2026-07-14', 'Fête nationale'],
    [geo.boundaryOf.FR, '2027-07-14', 'Fête nationale'],
    [geo.boundaryOf.DE, '2026-10-03', 'Tag der Deutschen Einheit'],
    [geo.boundaryOf.DE, '2027-10-03', 'Tag der Deutschen Einheit'],
  ];
  return specs.map(([boundary, date, name]) => ({
    id: kit.uuid(),
    boundary,
    boundaryName: nameOf.get(boundary),
    date,
    name,
    version: 1,
  }));
}

// ---------------------------------------------------------------------------------------------
// Productos
// ---------------------------------------------------------------------------------------------

const GOOD_NOUNS = ['Tornillo', 'Tuerca', 'Arandela', 'Rodamiento', 'Correa', 'Filtro', 'Válvula', 'Sensor', 'Cable', 'Conector', 'Placa', 'Motor', 'Bomba', 'Junta', 'Perno', 'Soporte', 'Palet', 'Caja', 'Cinta', 'Etiqueta'];
const GOOD_ATTRS = ['M6', 'M8', 'M10', 'inox', 'galvanizado', 'reforzado', 'industrial', 'compacto', 'de precisión', 'premium', 'estándar', 'de alta resistencia'];
const SERVICE_NOUNS = ['Mantenimiento preventivo', 'Instalación', 'Formación', 'Auditoría', 'Consultoría', 'Soporte técnico', 'Transporte urgente', 'Calibración', 'Reparación', 'Inspección'];
const SERVICE_ATTRS = ['básico', 'avanzado', 'anual', 'por horas', 'premium', 'in situ'];
const GOOD_CATEGORIES = ['Materias primas', 'Componentes', 'Herramientas', 'Embalaje', 'Consumibles'];
const SERVICE_CATEGORIES = ['Servicios técnicos', 'Consultoría', 'Logística', 'Mantenimiento'];
const TAX_CODES = ['IVA21', 'IVA21', 'IVA21', 'IVA10', 'IVA4', 'EXENTO'];

function buildProducts(kit, orgs) {
  const goodPairs = kit.shuffle(GOOD_NOUNS.flatMap((n) => GOOD_ATTRS.map((a) => `${n} ${a}`)));
  const servicePairs = kit.shuffle(SERVICE_NOUNS.flatMap((n) => SERVICE_ATTRS.map((a) => `${n} ${a}`)));
  const products = [];
  for (let i = 0; i < 120; i += 1) {
    const service = i % 4 === 3; // 30 servicios, 90 bienes
    const name = service ? servicePairs[Math.floor(i / 4)] : goodPairs[i - Math.floor(i / 4)];
    const price = service ? kit.int(4500, 38000) : kit.int(35, 24000);
    const discontinued = !service && kit.int(0, 14) === 0 ? addDays(TODAY, -kit.int(10, 400)) : '';
    products.push({
      id: kit.uuid(),
      company: orgs[i % 2 === 0 ? 0 : 1].id,
      sku: `SKU-${pad(i + 1, 4)}`,
      name,
      description: service ? `Servicio: ${name.toLowerCase()}.` : `${name}, referencia de catálogo ${pad(i + 1, 4)}.`,
      kind: service ? 'service' : 'good',
      uom: service ? 'h' : kit.pick(['ea', 'ea', 'ea', 'kg', 'm', 'l']),
      category: service ? kit.pick(SERVICE_CATEGORIES) : kit.pick(GOOD_CATEGORIES),
      taxCode: kit.pick(TAX_CODES),
      expenseCategory: service ? 'services' : 'goods',
      basePrice: cents(price),
      standardCost: cents(Math.round(price * (0.45 + kit.int(0, 25) / 100))),
      forSale: !(i % 13 === 0),
      forPurchase: !service || i % 8 === 3,
      stocked: !service,
      tracking: service ? 'none' : kit.pick(['none', 'none', 'lot', 'serial']),
      blockedSales: i % 17 === 5,
      blockedPurchase: i % 29 === 7,
      serviceFulfilmentMode: service ? (i % 8 === 3 ? 'direct_invoice' : 'delivery_note') : '',
      discontinued,
      version: 1,
    });
  }
  return products;
}

// ---------------------------------------------------------------------------------------------
// Proveedores y transportistas
// ---------------------------------------------------------------------------------------------

const SUPPLIER_CATEGORIES = ['goods', 'rent', 'repairs', 'professional-services', 'transport', 'insurance', 'advertising', 'supplies', 'other-services', 'fixed-asset'];
const BLOCK_REASONS = ['Datos bancarios sin verificar', 'Incidencia de calidad reiterada', 'Pendiente de revisión fiscal', 'Solicitud del proveedor', 'Facturas duplicadas detectadas'];
const UNBLOCK_REASONS = ['Datos verificados', 'Incidencia resuelta', 'Revisión fiscal completada'];
const OPERATORS = ['Lucía Almeida', 'Mateo Cordero', 'Carmen Fontana'];
const OPERATOR_ID = wellKnown(12, 1);

function buildParties(kit, orgs) {
  const names = kit.shuffle(COMPANY_PREFIX.flatMap((p) => COMPANY_NAME.map((n) => `${p} ${n}`)));
  const company = (index) => `${names[index]} ${LEGAL_FORM[index % LEGAL_FORM.length]}`;
  const parties = [];
  const supplierParties = [];
  for (let i = 0; i < 25; i += 1) {
    const p = { id: kit.uuid(), kind: 'Organization', name: company(i), active: true, roles: [ROLE.supplier], organizations: [orgs[i % 3].id] };
    parties.push(p);
    supplierParties.push(p);
  }
  const carrierParties = [];
  for (let i = 0; i < 10; i += 1) {
    const p = { id: kit.uuid(), kind: 'Organization', name: company(25 + i), active: true, roles: [ROLE.carrier], organizations: [orgs[i % 3].id] };
    parties.push(p);
    carrierParties.push(p);
  }
  return { parties, supplierParties, carrierParties };
}

function buildSuppliers(kit, orgs, supplierParties) {
  const suppliers = [];
  const events = [];
  supplierParties.forEach((party, i) => {
    const company = orgs[i % 3].id;
    const blocked = i === 4 || i === 11 || i === 18 || i === 23;
    const previouslyBlocked = blocked || i === 7 || i === 15;
    const id = kit.uuid();
    let version = 1;
    let blockReason = '';
    let unblockReason = '';
    let at = addDays(TODAY, -kit.int(120, 700));
    if (previouslyBlocked) {
      const reason = kit.pick(BLOCK_REASONS);
      events.push({ id: kit.uuid(), profile: id, company, supplier: party.id, blocked: true, reason, userId: OPERATOR_ID, userName: OPERATORS[i % 3], at: instant(at, 540 + i * 7) });
      version += 1;
      blockReason = reason;
      if (!blocked) {
        const back = kit.pick(UNBLOCK_REASONS);
        at = addDays(at, kit.int(5, 60));
        events.push({ id: kit.uuid(), profile: id, company, supplier: party.id, blocked: false, reason: back, userId: OPERATOR_ID, userName: OPERATORS[(i + 1) % 3], at: instant(at, 600 + i * 5) });
        version += 1;
        unblockReason = back;
      } else if (i === 11) {
        // Bloqueado, desbloqueado y vuelto a bloquear: historial de tres entradas.
        const back = kit.pick(UNBLOCK_REASONS);
        at = addDays(at, 12);
        events.push({ id: kit.uuid(), profile: id, company, supplier: party.id, blocked: false, reason: back, userId: OPERATOR_ID, userName: OPERATORS[1], at: instant(at, 615) });
        const again = BLOCK_REASONS[4];
        at = addDays(at, 40);
        events.push({ id: kit.uuid(), profile: id, company, supplier: party.id, blocked: true, reason: again, userId: OPERATOR_ID, userName: OPERATORS[2], at: instant(at, 702) });
        version += 2;
        unblockReason = back;
        blockReason = again;
      }
    }
    const account = `${kit.int(2000, 2999)}${kit.int(1000, 9999)}${pad(kit.int(10, 99))}${pad(kit.int(0, 9999999999), 10)}`;
    suppliers.push({
      id,
      company,
      supplier: party.id,
      category: SUPPLIER_CATEGORIES[i % SUPPLIER_CATEGORIES.length],
      withholdingRate: kit.pick(['0', '0', '7', '15']),
      paymentDays: kit.pick([0, 15, 30, 30, 45, 60, 90]),
      iban: spanishIban(account),
      blocked,
      blockReason,
      unblockReason,
      version,
    });
  });
  return { suppliers, supplierBlockEvents: events };
}

function buildCarriers(kit, orgs, carrierParties) {
  const carriers = [];
  const events = [];
  carrierParties.forEach((party, i) => {
    const company = orgs[i % 3].id;
    const blocked = i === 3 || i === 8;
    const id = kit.uuid();
    let version = 1;
    let blockReason = '';
    if (blocked) {
      blockReason = kit.pick(BLOCK_REASONS);
      events.push({ id: kit.uuid(), carrier: id, company, blocked: true, reason: blockReason, userId: OPERATOR_ID, byName: OPERATORS[i % 3], at: instant(addDays(TODAY, -kit.int(30, 300)), 560 + i) });
      version += 1;
    }
    const slug = party.name.toLowerCase().normalize('NFD').replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
    carriers.push({
      id,
      company,
      code: `CAR-${pad(i + 1)}`,
      name: party.name,
      party: party.id,
      trackingUrl: `https://seguimiento.${slug.slice(0, 18)}.example/envio/{tracking}`,
      blocked,
      blockReason,
      unblockReason: '',
      version,
    });
  });
  return { carriers, carrierBlockEvents: events };
}

// ---------------------------------------------------------------------------------------------
// Activos
// ---------------------------------------------------------------------------------------------

/** Por clase: nombres, coste en céntimos (mín, máx) y vida útil en meses (0 = no se amortiza). */
const ASSET_CLASSES = {
  land: { names: ['Parcela industrial', 'Solar de ampliación'], cost: [8000000, 25000000], life: 0 },
  buildings: { names: ['Nave de almacenaje', 'Oficinas planta 2', 'Caseta de control'], cost: [6000000, 30000000], life: 400 },
  machinery: { names: ['Torno CNC', 'Prensa hidráulica', 'Línea de envasado', 'Fresadora', 'Carretilla elevadora'], cost: [900000, 9000000], life: 120 },
  tooling: { names: ['Juego de moldes', 'Utillaje de montaje', 'Banco de pruebas'], cost: [120000, 900000], life: 60 },
  furniture: { names: ['Mobiliario de oficina', 'Estanterías metálicas', 'Mesa de reuniones'], cost: [30000, 400000], life: 120 },
  computers: { names: ['Servidor de aplicaciones', 'Portátil de ingeniería', 'Estación de trabajo', 'Cabina de almacenamiento'], cost: [90000, 1500000], life: 48 },
  vehicles: { names: ['Furgoneta de reparto', 'Camión rígido', 'Turismo de empresa'], cost: [1500000, 7500000], life: 72 },
  software: { names: ['Licencia ERP', 'Suite de diseño', 'Plataforma de analítica'], cost: [200000, 3500000], life: 36 },
  other: { names: ['Instalación fotovoltaica', 'Sistema de climatización'], cost: [400000, 4000000], life: 120 },
};

function buildAssets(kit, orgs, facilities, supplierParties) {
  const classKeys = Object.keys(ASSET_CLASSES);
  const buildings = facilities.filter((f) => f.typeName === 'Building');
  const assets = [];
  for (let i = 0; i < 60; i += 1) {
    const cls = classKeys[i % classKeys.length];
    const spec = ASSET_CLASSES[cls];
    const name = `${kit.pick(spec.names)} ${pad(i + 1, 2)}`;
    const acquired = addDays('2016-01-15', kit.int(0, 3800));
    const inService = addDays(acquired, kit.int(0, 40));
    const cost = kit.int(spec.cost[0] / 100, spec.cost[1] / 100) * 100;
    const residual = spec.life === 0 ? 0 : Math.round((cost * kit.int(0, 10)) / 100 / 100) * 100;
    const life = spec.life;
    const monthly = life === 0 ? 0 : Math.round((cost - residual) / life);
    const elapsed = life === 0 ? 0 : Math.min(life, monthsBetween(inService, TODAY));
    const disposedAsset = i % 10 === 7;
    const accumulated = life === 0 ? 0 : Math.min(cost - residual, monthly * elapsed);
    const net = cost - accumulated;
    const disposed = disposedAsset ? addDays(TODAY, -kit.int(20, 300)) : '';
    const disposal = disposedAsset ? kit.pick(['sale', 'scrap']) : '';
    const proceeds = disposal === 'sale' ? Math.round(net * (0.4 + kit.int(0, 80) / 100)) : 0;
    const status = disposedAsset ? 'disposed' : life > 0 && elapsed >= life ? 'depreciated' : 'in-service';
    const building = buildings[i % buildings.length];
    assets.push({
      id: kit.uuid(),
      company: building.organization,
      code: `ACT-${pad(i + 1, 4)}`,
      name,
      class: cls,
      serial: cls === 'land' || cls === 'buildings' ? '' : `SN-${kit.int(100000, 999999)}`,
      location: building.id,
      supplier: kit.pick(supplierParties).id,
      document: `FAC-${acquired.slice(0, 4)}-${kit.int(100, 999)}`,
      acquired,
      inService,
      cost: cents(cost),
      residual: cents(residual),
      lifeMonths: life,
      monthly: cents(monthly),
      accumulated: cents(accumulated),
      netBookValue: cents(net),
      status,
      disposed,
      disposal,
      proceeds: cents(proceeds),
      result: disposedAsset ? cents(proceeds - net) : '0.00',
      version: 1,
    });
  }
  return assets;
}

// ---------------------------------------------------------------------------------------------
// Puestos de trabajo (organigrama)
// ---------------------------------------------------------------------------------------------

const UNITS = ['Dirección General', 'Operaciones', 'Finanzas', 'Comercial', 'Tecnología', 'Recursos Humanos'];

/** Puestos: [nivel, título, unidad, índice del jefe (-1 = raíz)]. 1 + 4 + 10 + 14 + 16 = 45. */
const POSITION_SPEC = [
  [1, 'Director/a General', 0, -1], // 0
  [2, 'Director/a de Operaciones', 1, 0], // 1
  [2, 'Director/a Financiero/a', 2, 0], // 2
  [2, 'Director/a Comercial', 3, 0], // 3
  [2, 'Director/a de Tecnología', 4, 0], // 4
  [3, 'Responsable de Almacén', 1, 1], // 5
  [3, 'Responsable de Compras', 1, 1], // 6
  [3, 'Responsable de Mantenimiento', 1, 1], // 7
  [3, 'Controller', 2, 2], // 8
  [3, 'Responsable de Tesorería', 2, 2], // 9
  [3, 'Responsable de Ventas', 3, 3], // 10
  [3, 'Responsable de Marketing', 3, 3], // 11
  [3, 'Jefe/a de Desarrollo', 4, 4], // 12
  [3, 'Jefe/a de Sistemas', 4, 4], // 13
  [3, 'Jefe/a de Producto', 4, 4], // 14
  [4, 'Supervisor/a de Almacén', 1, 5], // 15
  [4, 'Jefe/a de Turno', 1, 5], // 16
  [4, 'Comprador/a Senior', 1, 6], // 17
  [4, 'Técnico/a Jefe de Mantenimiento', 1, 7], // 18
  [4, 'Contable Senior', 2, 8], // 19
  [4, 'Analista Financiero/a', 2, 8], // 20
  [4, 'Tesorero/a Adjunto/a', 2, 9], // 21
  [4, 'Key Account Manager', 3, 10], // 22
  [4, 'Responsable de Cuentas Regionales', 3, 10], // 23
  [4, 'Especialista de Marketing', 3, 11], // 24
  [4, 'Arquitecto/a de Software', 4, 12], // 25
  [4, 'Líder Técnico/a', 4, 12], // 26
  [4, 'Administrador/a de Sistemas', 4, 13], // 27
  [4, 'Product Owner', 4, 14], // 28
  [5, 'Mozo/a de Almacén', 1, 15], // 29
  [5, 'Mozo/a de Almacén', 1, 15], // 30
  [5, 'Carretillero/a', 1, 16], // 31
  [5, 'Auxiliar de Compras', 1, 17], // 32
  [5, 'Técnico/a de Mantenimiento', 1, 18], // 33
  [5, 'Técnico/a de Mantenimiento', 1, 18], // 34
  [5, 'Contable', 2, 19], // 35
  [5, 'Analista Junior', 2, 20], // 36
  [5, 'Auxiliar de Tesorería', 2, 21], // 37
  [5, 'Ejecutivo/a de Cuentas', 3, 22], // 38
  [5, 'Ejecutivo/a de Cuentas', 3, 23], // 39
  [5, 'Diseñador/a de Contenidos', 3, 24], // 40
  [5, 'Desarrollador/a', 4, 25], // 41
  [5, 'Desarrollador/a', 4, 26], // 42
  [5, 'Técnico/a de Soporte', 4, 27], // 43
  [5, 'Analista de Producto', 4, 28], // 44
];

const VACANT = new Set([7, 11, 14, 23, 34, 39, 44]);

function buildPositions(kit, orgs) {
  const organization = orgs[0].id;
  const units = UNITS.map((name) => ({ id: kit.uuid(), name, organization }));
  const titles = [...new Set(POSITION_SPEC.map((p) => p[1]))];
  const positionTypes = titles.map((title, i) => ({
    ID: wellKnown(10, i + 1),
    Title: title,
    Description: `Puesto de ${title.toLowerCase()}`,
    Active: true,
  }));
  const positionStatuses = [
    { ID: wellKnown(11, 1), Name: 'Planificado', Description: 'Puesto previsto, sin abrir' },
    { ID: wellKnown(11, 2), Name: 'Abierto', Description: 'Puesto abierto y sin titular' },
    { ID: wellKnown(11, 3), Name: 'Cubierto', Description: 'Puesto con titular' },
    { ID: wellKnown(11, 4), Name: 'Cerrado', Description: 'Puesto cerrado' },
  ];
  const persons = [];
  const positions = [];
  POSITION_SPEC.forEach(([level, title, unitIndex], i) => {
    const id = kit.uuid();
    const vacant = VACANT.has(i);
    let holder = '';
    const holders = [];
    const from = addDays('2019-01-07', kit.int(0, 2200));
    if (!vacant) {
      const given = GIVEN[(i * 5 + 3) % GIVEN.length];
      const s1 = SURNAME[(i * 7 + 1) % SURNAME.length];
      const s2 = SURNAME[(i * 11 + 5) % SURNAME.length];
      const person = { id: kit.uuid(), kind: 'Person', name: `${given} ${s1} ${s2}`, givenName: given, firstSurname: s1, secondSurname: s2, active: true, roles: [ROLE.employee], organizations: [organization] };
      persons.push(person);
      holder = person.id;
      holders.push({ person: person.id, from: instant(from, 480) });
    }
    const type = positionTypes[titles.indexOf(title)];
    const status = vacant ? positionStatuses[1] : positionStatuses[2];
    positions.push({
      id,
      unit: units[unitIndex].id,
      organization,
      type: type.ID,
      typeTitle: title,
      status: status.ID,
      statusName: status.Name,
      vacant,
      holder,
      reportsTo: '',
      plannedFrom: instant(addDays(from, -30), 0),
      flags: { Salaried: true, Exempt: level <= 3, FullTime: i % 9 !== 8, Temporary: i % 14 === 13 },
      holders,
      lines: [],
      version: 1,
    });
  });
  positions.forEach((p, i) => {
    const bossIndex = POSITION_SPEC[i][3];
    if (bossIndex >= 0) {
      const boss = positions[bossIndex];
      p.reportsTo = boss.id;
      p.lines = [{ supervisor: boss.id, primary: true, from: p.plannedFrom }];
    }
  });
  return { units, positionTypes, positionStatuses, persons, positions };
}

// ---------------------------------------------------------------------------------------------
// Conjunto completo
// ---------------------------------------------------------------------------------------------

/**
 * Genera el conjunto completo. Dos llamadas devuelven datos idénticos.
 * @returns {import('./dataset.d.mts').Dataset}
 */
export function generateDataset() {
  const rng = mulberry32(SEED);
  const kit = makeKit(rng);

  const geo = buildGeography(kit);
  const organizations = INTERNAL_ORGANIZATIONS.map((o) => ({ id: kit.uuid(), ...o, name: o.tradeName }));
  const facilities = buildFacilities(kit, organizations, geo.municipalities);
  const workCenters = buildWorkCenters(kit, organizations, facilities);
  const holidays = buildHolidays(kit, geo);
  const products = buildProducts(kit, organizations);
  const { parties, supplierParties, carrierParties } = buildParties(kit, organizations);
  const { suppliers, supplierBlockEvents } = buildSuppliers(kit, organizations, supplierParties);
  const { carriers, carrierBlockEvents } = buildCarriers(kit, organizations, carrierParties);
  const assets = buildAssets(kit, organizations, facilities, supplierParties);
  const hr = buildPositions(kit, organizations);
  const internalParties = organizations.map((o) => ({ id: o.id, kind: 'Organization', name: o.legalName, active: true, roles: [ROLE.internalOrganization], organizations: [o.id] }));
  const unitParties = hr.units.map((u) => ({ id: u.id, kind: 'Organization', name: u.name, active: true, roles: [ROLE.department], organizations: [u.organization] }));

  return {
    meta: { seed: SEED, today: TODAY, now: NOW_ISO },
    organizations,
    units: hr.units,
    parties: [...internalParties, ...parties, ...unitParties, ...hr.persons],
    facilityTypes: FACILITY_TYPES,
    facilities,
    workCenters,
    holidays,
    products,
    assets,
    suppliers,
    supplierBlockEvents,
    carriers,
    carrierBlockEvents,
    positions: hr.positions,
    positionTypes: hr.positionTypes,
    positionStatuses: hr.positionStatuses,
    countries: geo.countries,
    boundaryTypes: geo.boundaryTypes,
    boundaries: geo.boundaries,
    municipalities: geo.municipalities,
  };
}

/** Serialización canónica (claves ordenadas) para calcular el hash del conjunto. */
export function stableStringify(value) {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(',')}]`;
  if (value && typeof value === 'object') {
    const keys = Object.keys(value).sort();
    return `{${keys.map((k) => `${JSON.stringify(k)}:${stableStringify(value[k])}`).join(',')}}`;
  }
  return JSON.stringify(value);
}

/** Copia profunda de datos JSON (para reiniciar el estado en memoria sin regenerar). */
export function cloneDataset(dataset) {
  return JSON.parse(JSON.stringify(dataset));
}
