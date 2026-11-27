// Tipos de demo/data/dataset.mjs (el módulo es ESM puro y se comparte con scripts de Node).

/** Una fila cualquiera del conjunto: todas llevan `id` y las editables, `version`. */
export interface DemoRow {
  id: string;
  version?: number;
  [field: string]: unknown;
}

export interface DemoLocation {
  streetType?: string;
  line1?: string;
  number?: string;
  postalCode?: string;
  locality?: string;
  region?: string;
  country?: string;
  geoBoundary?: string;
  geoPostalCode?: string;
  floor?: string;
  door?: string;
  phone?: string;
  email?: string;
}

export interface DemoOrganization extends DemoRow {
  legalName: string;
  tradeName: string;
  name: string;
}

export interface DemoParty extends DemoRow {
  kind: 'Organization' | 'Person';
  name: string;
  active: boolean;
  roles: string[];
  organizations: string[];
  givenName?: string;
  firstSurname?: string;
  secondSurname?: string;
}

export interface DemoFacility extends DemoRow {
  organization: string;
  type: string;
  typeName: string;
  name: string;
  description: string;
  partOf: string;
  areaM2: string;
  location: DemoLocation;
  active: boolean;
}

export interface DemoCountry extends DemoRow {
  alpha2: string;
  alpha3: string;
  numeric: number;
  name: string;
  boundary: string;
  callingCode: string;
  defaultLocale: string;
  eu: boolean;
  eurozone: boolean;
  sepa: boolean;
  eea: boolean;
  currencies: string[];
  languages: string[];
  timeZones: string[];
  ibanLength: number;
  ibanPattern: string;
  postalCodePattern: string;
  postalCodeRequired: boolean;
  requiresSubdivision: boolean;
}

export interface DemoBoundary extends DemoRow {
  name: string;
  type: string;
  typeName: string;
  geoCode?: string;
  abbreviation?: string;
  nutsCode?: string;
  ineCode?: string;
  lauCode?: string;
  country: string;
  level: 'country' | '1' | '2' | '3' | 'local';
  parent?: string;
  active: boolean;
}

export interface DemoMunicipality {
  ine: string;
  name: string;
  postalCode: string;
  nuts3: string;
  boundary: string;
}

export interface DemoPosition extends DemoRow {
  unit: string;
  organization: string;
  type: string;
  typeTitle: string;
  status: string;
  statusName: string;
  vacant: boolean;
  holder: string;
  reportsTo: string;
  plannedFrom: string;
  flags: { Salaried: boolean; Exempt: boolean; FullTime: boolean; Temporary: boolean };
  holders: { person: string; from: string }[];
  lines: { supervisor: string; primary: boolean; from: string }[];
}

export interface Dataset {
  meta: { seed: number; today: string; now: string };
  organizations: DemoOrganization[];
  units: { id: string; name: string; organization: string }[];
  parties: DemoParty[];
  facilityTypes: { ID: string; Name: string; Description: string; RequiresContact: boolean; Active: boolean }[];
  facilities: DemoFacility[];
  workCenters: DemoRow[];
  holidays: DemoRow[];
  products: DemoRow[];
  assets: DemoRow[];
  suppliers: DemoRow[];
  supplierBlockEvents: DemoRow[];
  carriers: DemoRow[];
  carrierBlockEvents: DemoRow[];
  positions: DemoPosition[];
  positionTypes: { ID: string; Title: string; Description: string; Active: boolean }[];
  positionStatuses: { ID: string; Name: string; Description: string }[];
  countries: DemoCountry[];
  boundaryTypes: { id: string; name: string }[];
  boundaries: DemoBoundary[];
  municipalities: DemoMunicipality[];
}

export const ROLE: Record<'internalOrganization' | 'supplier' | 'carrier' | 'department' | 'employee', string>;
export const SEED: number;
export const TODAY: string;
export const NOW_ISO: string;
export function mulberry32(seed: number): () => number;
export function addDays(date: string, days: number): string;
export function generateDataset(): Dataset;
export function stableStringify(value: unknown): string;
export function cloneDataset<T>(dataset: T): T;
