// Navegación del mapa por niveles (PLAN §4.12): mundo → país → región → ciudad. El estado vive en
// la URL (`?view=map&level=region&code=ES30&layout=list`). Puro.
import type { MapLevelDef, ViewDef } from '../contracts';
import type { BBox } from './geo';

export type MapViewDef = Extract<ViewDef, { kind: 'map' }>;
export type MapLayout = 'map' | 'list';

/** Lo que se mira: un nivel y la delimitación que se abre (`null` en el nivel de arriba). */
export interface MapNav {
  /** Posición del nivel en `view.levels`. */
  index: number;
  code: string | null;
}

export function mapLayoutOf(params: Record<string, string> | undefined): MapLayout {
  return params?.layout === 'list' ? 'list' : 'map';
}

/**
 * Nivel y código de la URL. Un nivel desconocido o un nivel inferior sin código vuelven al primero
 * (el mundo): es mejor enseñar algo que una pantalla vacía.
 */
export function parseNav(view: MapViewDef, params: Record<string, string> | undefined): MapNav {
  const wanted = params?.level;
  const index = view.levels.findIndex((level) => level.id === wanted);
  if (index <= 0) return { index: 0, code: null };
  const code = (params?.code ?? '').trim();
  return code === '' ? { index: 0, code: null } : { index, code };
}

/** Parámetros de la URL de una vista del mapa: se conservan los ajenos (`layout`, filtros) y se reescriben `level` y `code`. */
export function navParams(
  view: MapViewDef,
  nav: MapNav,
  base: Record<string, string> | undefined,
): Record<string, string> {
  const out: Record<string, string> = { ...base };
  delete out.level;
  delete out.code;
  if (nav.index > 0 && nav.code) {
    out.level = view.levels[nav.index]!.id;
    out.code = nav.code;
  }
  return out;
}

export function sameNav(a: MapNav, b: MapNav): boolean {
  return a.index === b.index && a.code === b.code;
}

/** Nivel siguiente (el que se abre al entrar en una delimitación), si lo hay. */
export function childLevel(view: MapViewDef, index: number): MapLevelDef | undefined {
  return view.levels[index + 1];
}

/** Nombre del fichero (sin extensión) de un nivel: `world` o `<nivel>-<código>`. */
export function levelStem(level: MapLevelDef, index: number, code: string | null): string {
  return index === 0 || code === null ? 'world' : `${level.id}-${code}`;
}

/** Dirección del GeoJSON de un lugar: `{level}` es el nombre del fichero sin extensión (`world`, `region-ES30`). */
export function geojsonUrlFor(template: string, view: MapViewDef, nav: MapNav): string {
  const stem = levelStem(view.levels[nav.index]!, nav.index, nav.code);
  return template.includes('{level}') ? template.replaceAll('{level}', stem) : template;
}

/** Códigos posibles del padre de `code` si los códigos son jerárquicos (NUTS: ES300 → ES30 → ES). */
export function parentCandidates(code: string): string[] {
  const out: string[] = [];
  for (let length = code.length - 1; length >= 2; length -= 1) out.push(code.slice(0, length));
  return out;
}

/** Un eslabón de las migas. */
export interface Crumb {
  index: number;
  code: string | null;
  name: string;
  /** Caja de la delimitación: a donde se encuadra el mapa al abrirla (si se conoce). */
  bbox?: BBox;
  /**
   * El nivel de este eslabón sólo tiene un elemento y se salta (M-4): la miga nombra el lugar pero no es
   * un lugar al que se pueda volver, así que no es un botón y «Subir» pasa de largo.
   */
  skipped?: boolean;
}

/** ¿Se saltan los niveles con una sola delimitación? Por omisión sí (Q-10); `skipSingle: false` los conserva. */
export function skipsSingleLevels(view: Pick<MapViewDef, 'skipSingle'>): boolean {
  return view.skipSingle !== false;
}

/** Lo mínimo que se necesita saber de una delimitación del nivel de abajo para decidir si se salta. */
export interface EntryFeature {
  code: string;
  name: string;
  hasChildren: boolean;
  bbox: BBox;
}

export interface EntryTrail {
  /** Dónde se acaba: el lugar pedido o, si su nivel tenía un solo elemento, el de más abajo. */
  nav: MapNav;
  /** Eslabones nuevos de las migas, del lugar pedido al final (los saltados llevan `skipped`). */
  crumbs: Crumb[];
}

/**
 * Entrar en una delimitación (`entered` = su eslabón) y, mientras el nivel al que se baja tenga una sola
 * delimitación con detalle, seguir bajando (M-4). `load` da las del nivel `nav` o `null` si no hay
 * geometría (no se salta nada: se enseña lo pedido). Con `skipSingle` apagado se entra sin más.
 */
export async function resolveEntry(
  view: MapViewDef,
  entered: Crumb,
  load: (nav: MapNav) => Promise<readonly EntryFeature[] | null>,
): Promise<EntryTrail> {
  const crumbs: Crumb[] = [entered];
  let nav: MapNav = { index: entered.index, code: entered.code };
  if (!skipsSingleLevels(view)) return { nav, crumbs };
  // Un nivel por vuelta como mucho: la jerarquía es finita aunque los datos sean raros.
  for (let guard = 0; guard < view.levels.length; guard += 1) {
    if (!childLevel(view, nav.index)) break;
    const list = await load(nav);
    const only = list?.length === 1 ? list[0]! : undefined;
    if (!only?.hasChildren) break;
    crumbs[crumbs.length - 1] = { ...crumbs[crumbs.length - 1]!, skipped: true };
    nav = { index: nav.index + 1, code: only.code };
    crumbs.push({ index: nav.index, code: only.code, name: only.name, bbox: only.bbox });
  }
  return { nav, crumbs };
}

/** Lo que `redirectTarget` necesita saber de lo que se está viendo (un subconjunto de `LevelShown`). */
export interface ShownLevel {
  nav: MapNav;
  features: readonly Pick<EntryFeature, 'code' | 'hasChildren'>[];
  /** Geometría del nivel si viene de un GeoJSON; `null` con teselas vectoriales. */
  collection: unknown;
}

/**
 * La única delimitación del nivel que se ve si éste es «saltable» (R3-4, M-5): una sola delimitación con detalle,
 * un nivel debajo, `skipSingle` activo y fuente GeoJSON. No es el nivel de arriba (el mundo no tiene URL propia).
 *
 * Sólo GeoJSON: con teselas vectoriales no se puede mirar un nivel sin abrirlo (el comentario de `useEntryResolver`),
 * así que no se sabe cuántas delimitaciones tiene hasta pintarlo y ahí se conserva lo que pide la URL.
 */
export function skippableFeature(view: MapViewDef, shown: ShownLevel | null): { code: string } | null {
  if (!shown || shown.collection === null || shown.nav.index === 0) return null;
  if (!skipsSingleLevels(view) || !childLevel(view, shown.nav.index)) return null;
  const only = shown.features.length === 1 ? shown.features[0]! : undefined;
  return only?.hasChildren ? { code: only.code } : null;
}

/**
 * A dónde hay que llevar la URL cuando se entra por ella en un nivel saltable (R3-4, M-5): al siguiente nivel, con
 * `replace` (la URL intermedia no queda en el historial y «Atrás» no rebota). `null` si no hay nada que corregir:
 * el nivel cargado no es el que pide la URL, tiene varias delimitaciones o `skipSingle` está apagado.
 * Si el siguiente también es saltable, el efecto vuelve a decidir al cargarlo y encadena otro `replace`.
 */
export function redirectTarget(view: MapViewDef, shown: ShownLevel | null, nav: MapNav): MapNav | null {
  if (!shown || !sameNav(shown.nav, nav)) return null;
  const only = skippableFeature(view, shown);
  return only ? { index: nav.index + 1, code: only.code } : null;
}

/** Posición de la miga a la que lleva «Subir un nivel» desde la última: la anterior que no se salta. */
export function upTarget(crumbs: readonly Crumb[]): number {
  for (let position = crumbs.length - 2; position >= 0; position -= 1) if (!crumbs[position]!.skipped) return position;
  return -1;
}

/** ¿Las migas conocidas son las del lugar al que apunta la URL? */
export function crumbsMatch(crumbs: readonly Crumb[] | null, nav: MapNav): crumbs is readonly Crumb[] {
  if (!crumbs || crumbs.length === 0) return false;
  const last = crumbs[crumbs.length - 1]!;
  return last.index === nav.index && last.code === nav.code && crumbs.length === nav.index + 1;
}
