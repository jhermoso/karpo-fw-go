// Delimitaciones geográficas (A2 §5): `GET /api/geography/boundaries` y `/{id}` con la cadena de
// antecesores. Fuera de /api/v1: páginas {items,total,number,size,totalPages}, errores en lista.
// La geografía de la demo es la de España (7 + 19 + 59 + muestra de municipios) y los países.
import { http, type HttpHandler } from 'msw';
import type { DemoBoundary } from '../../data/dataset.mjs';
import { fold, isUuid, json, numberSizePage, parseNumberSize, preflight, problem, slice } from '../http';
import { localizeRows } from '../catalog-i18n';
import { mock } from '../state';

const PERMISSION = 'Geography.Boundary.Read';
const LEVELS = ['country', '1', '2', '3', 'local'];

function view(boundary: DemoBoundary) {
  return { ...boundary };
}

/** Antecesores del más cercano al más lejano (de la provincia hasta el país). */
function ancestorsOf(boundary: DemoBoundary) {
  const chain: { id: string; name: string; type: string; active: boolean }[] = [];
  let cursor = boundary.parent ? mock.data.boundaries.find((b) => b.id === boundary.parent) : undefined;
  for (let guard = 0; cursor && guard < 10; guard += 1) {
    chain.push({ id: cursor.id, name: cursor.name, type: cursor.type, active: cursor.active });
    cursor = cursor.parent ? mock.data.boundaries.find((b) => b.id === cursor!.parent) : undefined;
  }
  return chain;
}

export const handlers: HttpHandler[] = [
  http.get('*/api/geography/boundaries', async ({ request }) => {
    const blocked = await preflight(request, PERMISSION);
    if (blocked) return blocked;
    const url = new URL(request.url);
    const q = url.searchParams;
    const level = q.get('level');
    if (level && !LEVELS.includes(level)) {
      return problem({ status: 400, form: 'list', instance: url.pathname, detail: 'One or more fields are invalid', issues: [{ field: 'level', code: 'enum', message: LEVELS.join(', ') }] });
    }
    const needle = fold(q.get('q'));
    const type = q.get('type');
    const parent = q.get('parent');
    const country = q.get('country');
    const rows = mock.data.boundaries.filter(
      (b) =>
        (!level || b.level === level) &&
        (!parent || b.parent === parent.toLowerCase()) &&
        (!country || b.country.toLowerCase() === country.toLowerCase()) &&
        (!type || b.type === type || fold(b.typeName) === fold(type)) &&
        (!needle || [b.name, b.nutsCode, b.lauCode, b.ineCode, b.geoCode, b.abbreviation].some((v) => fold(v).includes(needle))),
    );
    const { number, size } = parseNumberSize(url);
    return json(localizeRows(request, numberSizePage(slice(rows, number, size).map(view), rows.length, number, size)));
  }),

  http.get('*/api/geography/boundaries/:id', async ({ request, params }) => {
    const blocked = await preflight(request, PERMISSION);
    if (blocked) return blocked;
    const path = new URL(request.url).pathname;
    const id = String(params.id);
    if (!isUuid(id)) {
      return problem({ status: 400, form: 'list', instance: path, detail: 'invalid id', issues: [{ field: 'id', code: 'format', message: 'the identity is not valid' }] });
    }
    const boundary = mock.data.boundaries.find((b) => b.id.toLowerCase() === id.toLowerCase());
    if (!boundary) return problem({ status: 404, form: 'list', instance: path });
    return json(localizeRows(request, { ...view(boundary), ancestors: ancestorsOf(boundary), groupings: [] }));
  }),
];
