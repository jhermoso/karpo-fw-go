// Organizaciones internas y partes (Parties): lo que el front necesita para resolver claves ajenas
// (empresa, proveedor, titular de un puesto). Formas de Go: `/internal-organizations/names` es una
// lista simple y `/parties` una página {items,total,number,size,totalPages}.
import { http, type HttpHandler } from 'msw';
import type { DemoParty } from '../../data/dataset.mjs';
import { fold, isUuid, json, numberSizePage, parseNumberSize, preflight, problem, slice } from '../http';
import { mock } from '../state';

function partyView(party: DemoParty) {
  return {
    id: party.id,
    kind: party.kind,
    name: party.name,
    active: party.active,
    language: 'es',
    dataQualityIssues: [],
    organizations: party.organizations,
    roles: party.roles.map((role) => ({ roleType: role })),
    identifications: [],
    contacts: [],
    classifications: [],
    facilityRoles: [],
    version: 1,
  };
}

export const handlers: HttpHandler[] = [
  http.get('*/api/internal-organizations/names', async ({ request }) => {
    const blocked = await preflight(request);
    if (blocked) return blocked;
    return json(mock.data.organizations.map((o) => ({ id: o.id, name: o.name })));
  }),

  http.get('*/api/internal-organizations', async ({ request }) => {
    const blocked = await preflight(request);
    if (blocked) return blocked;
    return json(
      mock.data.organizations.map((o) => ({
        id: o.id,
        kind: 'Organization',
        name: o.name,
        organization: { legalName: o.legalName, tradeName: o.tradeName },
        active: true,
        version: 1,
      })),
    );
  }),

  http.get('*/api/parties', async ({ request }) => {
    const blocked = await preflight(request);
    if (blocked) return blocked;
    const url = new URL(request.url);
    const needle = fold(url.searchParams.get('q'));
    const kind = url.searchParams.get('kind');
    const role = url.searchParams.get('role');
    const rows = mock.data.parties.filter(
      (p) =>
        (!needle || fold(p.name).includes(needle)) &&
        (!kind || p.kind.toLowerCase() === kind.toLowerCase()) &&
        (!role || p.roles.includes(role.toLowerCase())),
    );
    const { number, size } = parseNumberSize(url);
    return json(numberSizePage(slice(rows, number, size).map(partyView), rows.length, number, size));
  }),

  http.get('*/api/parties/:id', async ({ request, params }) => {
    const blocked = await preflight(request);
    if (blocked) return blocked;
    const path = new URL(request.url).pathname;
    const id = String(params.id);
    if (!isUuid(id)) {
      return problem({ status: 400, form: 'list', instance: path, detail: 'invalid id', issues: [{ field: 'id', code: 'format', message: 'the identity is not valid' }] });
    }
    const party = mock.data.parties.find((p) => p.id.toLowerCase() === id.toLowerCase());
    return party ? json(partyView(party)) : problem({ status: 404, form: 'list', instance: path });
  }),
];
