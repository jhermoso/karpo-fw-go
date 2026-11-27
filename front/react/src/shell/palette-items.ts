// Modelo de la paleta de órdenes: elementos, agrupación y búsqueda difusa (fuse.js). Sin React.
import Fuse from 'fuse.js';

export type PaletteGroup = 'selection' | 'go' | 'create' | 'general' | 'theme' | 'mode' | 'locale';

/** Orden de los grupos cuando no hay búsqueda. */
export const GROUP_ORDER: readonly PaletteGroup[] = ['selection', 'go', 'create', 'general', 'theme', 'mode', 'locale'];

export interface PaletteItem {
  id: string;
  group: PaletteGroup;
  label: string;
  /** Texto secundario (la entidad, el número de filas elegidas…). */
  hint?: string;
  /** Palabras que también casan en la búsqueda (sinónimos, nombre en singular…). */
  keywords: string;
  icon?: string;
  /** Combinación del registro de atajos (`Mod+K`) para mostrarla junto al elemento. */
  shortcut?: string;
  run(): void;
}

export function filterPalette(items: readonly PaletteItem[], query: string): PaletteItem[] {
  const text = query.trim();
  if (text === '') return [...items];
  const fuse = new Fuse(items, {
    keys: [
      { name: 'label', weight: 3 },
      { name: 'hint', weight: 1 },
      { name: 'keywords', weight: 1 },
    ],
    threshold: 0.4,
    ignoreLocation: true,
    ignoreDiacritics: true,
  });
  return fuse.search(text).map((hit) => hit.item);
}

export interface PaletteSection {
  group: PaletteGroup;
  items: PaletteItem[];
}

/** Agrupa conservando el orden: el de `GROUP_ORDER` sin búsqueda, el de la primera coincidencia con ella. */
export function groupPalette(items: readonly PaletteItem[], searching: boolean): PaletteSection[] {
  const byGroup = new Map<PaletteGroup, PaletteItem[]>();
  for (const item of items) {
    const list = byGroup.get(item.group) ?? [];
    list.push(item);
    byGroup.set(item.group, list);
  }
  const order = searching ? [...byGroup.keys()] : GROUP_ORDER.filter((group) => byGroup.has(group));
  return order.map((group) => ({ group, items: byGroup.get(group) ?? [] }));
}
