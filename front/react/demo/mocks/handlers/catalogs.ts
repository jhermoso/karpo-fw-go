// Catálogos auxiliares. Respetan la incoherencia de Go (A2 §3): los de instalaciones, RRHH y gran
// parte de /api/reference van en PascalCase; `boundary-levels` va en camelCase.
import { http, type HttpHandler } from 'msw';
import { localizeCatalog, positionTypeDescription } from '../catalog-i18n';
import { json, preflight } from '../http';
import { mock } from '../state';

/** Los nombres de catálogo salen en el idioma de `Accept-Language` (M-8); sin cabecera, como en el conjunto de datos. */
const catalog = (path: string, permission: string | undefined, rows: (request: Request) => unknown): HttpHandler =>
  http.get(path, async ({ request }) => {
    const blocked = await preflight(request, permission);
    if (blocked) return blocked;
    return json(localizeCatalog(request, rows(request)));
  });

const STREET_TYPES = [
  ['CL', 'Calle'],
  ['AV', 'Avenida'],
  ['PS', 'Paseo'],
  ['PZ', 'Plaza'],
  ['CR', 'Carretera'],
  ['RD', 'Ronda'],
] as const;

export const handlers: HttpHandler[] = [
  catalog('*/api/catalogs/facility-types', undefined, () => mock.data.facilityTypes),

  catalog('*/api/hr/catalogs/position-types', 'HR.Catalog.Read', (request) =>
    mock.data.positionTypes.map((t) => ({ ...t, Description: positionTypeDescription(request, t.Title, t.Description), Classes: [] })),
  ),
  catalog('*/api/hr/catalogs/position-statuses', 'HR.Catalog.Read', () => mock.data.positionStatuses),
  catalog('*/api/hr/catalogs/position-classes', 'HR.Catalog.Read', () => [
    { ID: '10000000-0000-0000-0013-000000000001', Name: 'Dirección' },
    { ID: '10000000-0000-0000-0013-000000000002', Name: 'Mando intermedio' },
    { ID: '10000000-0000-0000-0013-000000000003', Name: 'Técnico' },
  ]),

  catalog('*/api/reference/boundary-types', 'Geography.Reference.Read', () =>
    mock.data.boundaryTypes.map((t) => ({ ID: t.id, Name: t.name })),
  ),
  catalog('*/api/reference/boundary-levels', 'Geography.Reference.Read', () => [
    { country: 'ES', level: 'country', name: 'País', administrative: true },
    { country: 'ES', level: '1', name: 'Agrupación de comunidades autónomas', administrative: false, classification: 'NUTS 1' },
    { country: 'ES', level: '2', name: 'Comunidad autónoma', administrative: true, classification: 'NUTS 2' },
    { country: 'ES', level: '3', name: 'Provincia o isla', administrative: true, classification: 'NUTS 3' },
    { country: 'ES', level: 'local', name: 'Municipio', administrative: true, classification: 'LAU' },
  ]),
  catalog('*/api/reference/currencies', 'Geography.Reference.Read', () => [
    { Code: 'EUR', Name: 'Euro', Symbol: '€' },
    { Code: 'USD', Name: 'US Dollar', Symbol: '$' },
    { Code: 'GBP', Name: 'Pound Sterling', Symbol: '£' },
  ]),
  catalog('*/api/reference/languages', 'Geography.Reference.Read', () => [
    { Code: 'es', Name: 'Spanish' },
    { Code: 'en', Name: 'English' },
    { Code: 'ca', Name: 'Catalan' },
  ]),
  catalog('*/api/reference/time-zones', 'Geography.Reference.Read', () => [
    { ID: 'Europe/Madrid', Name: 'Europe/Madrid' },
    { ID: 'Atlantic/Canary', Name: 'Atlantic/Canary' },
  ]),
  catalog('*/api/reference/street-types', 'Geography.Reference.Read', () =>
    STREET_TYPES.map(([Code, Name]) => ({ Code, Name })),
  ),
];
