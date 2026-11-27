// URL <-> EntityRoute (PLAN §4.7, D-R11). La ruta es la fuente de verdad de la pantalla:
//   /:entity                       lista (vista por defecto o ?view=grid|tree|map)
//   /:entity/nuevo                 alta (EntityRoute con mode 'edit' y sin id)
//   /:entity/:id[?mode=edit]       detalle (lectura o edición)
// El resto de parámetros de la consulta (filtros, orden, nivel del mapa…) son de la vista: viajan en
// `EntityRoute.params` y se conservan al abrir o cerrar un detalle.
import type { EntityConfig, EntityRoute } from '../contracts';

export const NEW_SEGMENT = 'nuevo';
export type ViewKind = NonNullable<EntityRoute['view']>;

/** Parámetros de la consulta que el marco reserva (no van en `EntityRoute.params`). */
const RESERVED = new Set(['view', 'mode', 'page']);

export function isViewKind(value: string | null | undefined): value is ViewKind {
  return value === 'grid' || value === 'tree' || value === 'map';
}

export interface ParsedLocation {
  entity: string;
  /** `nuevo` no es un id: es el alta. */
  id?: string;
  create: boolean;
  search: URLSearchParams;
}

/** Ruta de entidad que resulta de lo que dice la URL. */
export function parseEntityRoute(location: ParsedLocation): EntityRoute {
  const { entity, id, create, search } = location;
  const params: Record<string, string> = {};
  for (const [key, value] of search) if (!RESERVED.has(key)) params[key] = value;
  const view = search.get('view');
  const route: EntityRoute = { entity, params };
  if (isViewKind(view)) route.view = view;
  if (create) route.mode = 'edit';
  else if (id !== undefined) {
    route.id = id;
    route.mode = search.get('mode') === 'edit' ? 'edit' : 'read';
  }
  return route;
}

export interface BuildContext {
  /** Entidad de la pantalla actual: si cambia, no se conservan los parámetros de la vista. */
  currentEntity?: string;
  /** Consulta actual (para conservar lo que `to` no dice). */
  currentSearch?: URLSearchParams;
  /** Vista por defecto de la entidad destino: no se escribe `?view=` si coincide. */
  defaultView?: ViewKind;
}

export interface BuiltTarget {
  pathname: string;
  search: string;
}

/**
 * Destino de un `navigate(EntityRoute)`:
 *  - `params` indicado = conjunto completo de parámetros de la vista; sin indicar = se conservan;
 *  - `view` sin indicar = se conserva; la vista por defecto no se escribe;
 *  - sin id y con `mode: 'edit'` = alta (`/:entity/nuevo`).
 */
export function buildEntityTarget(to: EntityRoute, context: BuildContext = {}): BuiltTarget {
  const same = context.currentEntity === to.entity;
  const next = new URLSearchParams();
  const current = context.currentSearch ?? new URLSearchParams();

  if (to.params) {
    for (const [key, value] of Object.entries(to.params)) {
      if (value !== '' && !RESERVED.has(key)) next.set(key, value);
    }
  } else if (same) {
    for (const [key, value] of current) if (!RESERVED.has(key)) next.append(key, value);
  }

  const view = to.view ?? (same ? (isViewKind(current.get('view')) ? (current.get('view') as ViewKind) : undefined) : undefined);
  if (view && view !== context.defaultView) next.set('view', view);

  const create = to.id === undefined && to.mode === 'edit';
  if (to.id !== undefined && to.mode === 'edit') next.set('mode', 'edit');
  if (to.id !== undefined && same && current.get('page') === '1') next.set('page', '1');

  const base = `/${encodeURIComponent(to.entity)}`;
  const pathname = create
    ? `${base}/${NEW_SEGMENT}`
    : to.id !== undefined
      ? `${base}/${encodeURIComponent(to.id)}`
      : base;
  const text = next.toString();
  return { pathname, search: text === '' ? '' : `?${text}` };
}

export function targetToString(target: BuiltTarget): string {
  return `${target.pathname}${target.search}`;
}

/** Vista que se pinta: la de la URL si la entidad la declara; si no, la de por defecto. */
export function pickView(config: EntityConfig, route: Pick<EntityRoute, 'view'>): ViewKind {
  const wanted = route.view;
  if (wanted && config.views.some((view) => view.kind === wanted)) return wanted;
  return config.defaultView;
}
