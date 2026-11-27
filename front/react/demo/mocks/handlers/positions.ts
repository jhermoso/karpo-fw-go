// Puestos de RRHH (A2 §4): lista plana paginada, detalle y organigrama anidado desde una raíz.
// Rutas fuera de /api/v1: páginas {items,total,number,size,totalPages} y errores en lista.
import { http, type HttpHandler } from 'msw';
import type { DemoPosition } from '../../data/dataset.mjs';
import { isUuid, json, numberSizePage, parseNumberSize, preflight, problem, slice } from '../http';
import { localizeRows } from '../catalog-i18n';
import { mock } from '../state';

const PERMISSION = 'HR.Position.Read';
const DEFAULT_DEPTH = 3;
const MAX_DEPTH = 20;

interface ChartNode {
  position: DemoPosition;
  reports: ChartNode[];
}

function build(root: DemoPosition, depth: number): ChartNode {
  const reports =
    depth > 1
      ? mock.data.positions.filter((p) => p.reportsTo === root.id).map((child) => build(child, depth - 1))
      : [];
  return { position: root, reports };
}

const same = (a: unknown, b: string | null) => !b || String(a ?? '').toLowerCase() === b.toLowerCase();

export const handlers: HttpHandler[] = [
  http.get('*/api/hr/positions', async ({ request }) => {
    const blocked = await preflight(request, PERMISSION);
    if (blocked) return blocked;
    const url = new URL(request.url);
    const q = url.searchParams;
    const rows = mock.data.positions.filter(
      (p) =>
        same(p.unit, q.get('unit')) &&
        same(p.organization, q.get('organization')) &&
        same(p.type, q.get('type')) &&
        same(p.status, q.get('status')) &&
        same(p.holder, q.get('holder')) &&
        (q.get('vacant') !== 'true' || p.vacant),
    );
    const { number, size } = parseNumberSize(url);
    return json(localizeRows(request, numberSizePage(slice(rows, number, size), rows.length, number, size)));
  }),

  http.get('*/api/hr/positions/:id', async ({ request, params }) => {
    const blocked = await preflight(request, PERMISSION);
    if (blocked) return blocked;
    const path = new URL(request.url).pathname;
    const id = String(params.id);
    if (!isUuid(id)) {
      return problem({ status: 400, form: 'list', instance: path, detail: 'invalid id', issues: [{ field: 'id', code: 'format', message: 'the identity is not valid' }] });
    }
    const position = mock.data.positions.find((p) => p.id.toLowerCase() === id.toLowerCase());
    return position ? json(localizeRows(request, position)) : problem({ status: 404, form: 'list', instance: path });
  }),

  http.get('*/api/hr/positions/:id/chart', async ({ request, params }) => {
    const blocked = await preflight(request, PERMISSION);
    if (blocked) return blocked;
    const url = new URL(request.url);
    const id = String(params.id);
    if (!isUuid(id)) {
      return problem({ status: 400, form: 'list', instance: url.pathname, detail: 'invalid id', issues: [{ field: 'id', code: 'format', message: 'the identity is not valid' }] });
    }
    const rawDepth = url.searchParams.get('depth');
    let depth = DEFAULT_DEPTH;
    if (rawDepth !== null && rawDepth !== '') {
      depth = /^-?\d+$/.test(rawDepth) ? Number(rawDepth) : 0;
      if (depth < 1 || depth > MAX_DEPTH) {
        return problem({ status: 400, form: 'list', instance: url.pathname, detail: 'One or more fields are invalid', issues: [{ field: 'depth', code: 'range', message: 'a depth from 1 to 20' }] });
      }
    }
    const root = mock.data.positions.find((p) => p.id.toLowerCase() === id.toLowerCase());
    if (!root) return problem({ status: 404, form: 'list', instance: url.pathname });
    return json(localizeRows(request, build(root, depth)));
  }),
];
