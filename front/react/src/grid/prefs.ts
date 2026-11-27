// Preferencias de columnas por usuario y entidad (orden, ocultas, fijadas y anchos) en localStorage.
// Siempre con try/catch (`readStored`/`writeStored`): sin almacenamiento la cuadrícula funciona igual.
import type { ColumnDef } from '../contracts';
import { readStored, writeStored } from '../theme/storage';

export interface ColumnPrefs {
  /** Orden de los campos (todos los de la configuración, también los ocultos). */
  order: string[];
  hidden: string[];
  pinned: { start: string[]; end: string[] };
  /** Anchos en píxeles fijados por el usuario. */
  sizes: Record<string, number>;
  /**
   * Ajustar las columnas al ancho disponible (D-R34, por omisión sí): encoge y, si no basta, oculta las menos
   * importantes sin tocar `hidden`. Sólo se guarda `false`; ausente vale `true`.
   */
  autoFit?: false;
}

export const MIN_COLUMN_WIDTH = 64;
export const MAX_COLUMN_WIDTH = 800;
export const DEFAULT_COLUMN_WIDTH = 160;

export function prefsKey(entity: string, user?: string): string {
  return `karpo.grid.${user ?? 'anon'}.${entity}`;
}

/** Valores de la configuración: orden, `hidden` y `pinned` de cada columna. */
export function defaultPrefs(columns: readonly ColumnDef[]): ColumnPrefs {
  return {
    order: columns.map((column) => column.field),
    hidden: columns.filter((column) => column.hidden).map((column) => column.field),
    pinned: {
      start: columns.filter((column) => column.pinned === 'start').map((column) => column.field),
      end: columns.filter((column) => column.pinned === 'end').map((column) => column.field),
    },
    sizes: {},
  };
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
}

/** Combina lo guardado con la configuración: ignora campos que ya no existen y añade los nuevos al final. */
export function mergePrefs(columns: readonly ColumnDef[], stored: unknown): ColumnPrefs {
  const base = defaultPrefs(columns);
  if (typeof stored !== 'object' || stored === null) return base;
  const raw = stored as Record<string, unknown>;
  const known = new Set(base.order);
  const keep = (list: string[]) => list.filter((field) => known.has(field));

  const savedOrder = keep(strings(raw.order));
  const order = [...savedOrder, ...base.order.filter((field) => !savedOrder.includes(field))];
  const pinned = raw.pinned as Record<string, unknown> | undefined;
  const start = keep(strings(pinned?.start));
  const end = keep(strings(pinned?.end)).filter((field) => !start.includes(field));
  const sizes: Record<string, number> = {};
  if (typeof raw.sizes === 'object' && raw.sizes !== null) {
    for (const [field, width] of Object.entries(raw.sizes as Record<string, unknown>)) {
      if (known.has(field) && typeof width === 'number' && Number.isFinite(width)) sizes[field] = clampWidth(width);
    }
  }
  return {
    order,
    hidden: Array.isArray(raw.hidden) ? keep(strings(raw.hidden)) : base.hidden,
    pinned: pinned ? { start, end } : base.pinned,
    sizes,
    ...(raw.autoFit === false ? { autoFit: false as const } : {}),
  };
}

export function clampWidth(width: number): number {
  return Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, Math.round(width)));
}

export function loadPrefs(key: string, columns: readonly ColumnDef[]): ColumnPrefs {
  const text = readStored(key);
  if (!text) return defaultPrefs(columns);
  try {
    return mergePrefs(columns, JSON.parse(text));
  } catch {
    return defaultPrefs(columns);
  }
}

export function savePrefs(key: string, prefs: ColumnPrefs): void {
  writeStored(key, JSON.stringify(prefs));
}

// ---------------------------------------------------------------------------------------------
// Operaciones puras sobre las preferencias (las usan el menú de columnas y las cabeceras)
// ---------------------------------------------------------------------------------------------

export function setHidden(prefs: ColumnPrefs, field: string, hidden: boolean): ColumnPrefs {
  const rest = prefs.hidden.filter((item) => item !== field);
  return { ...prefs, hidden: hidden ? [...rest, field] : rest };
}

/** Mueve una columna una posición (`-1` hacia el principio, `1` hacia el final). */
export function moveColumn(prefs: ColumnPrefs, field: string, delta: -1 | 1): ColumnPrefs {
  const index = prefs.order.indexOf(field);
  const target = index + delta;
  if (index < 0 || target < 0 || target >= prefs.order.length) return prefs;
  const order = [...prefs.order];
  order.splice(index, 1);
  order.splice(target, 0, field);
  return { ...prefs, order };
}

/** Coloca `field` justo antes de `before` (arrastrar y soltar); sin `before`, al final. */
export function placeColumn(prefs: ColumnPrefs, field: string, before: string | undefined): ColumnPrefs {
  if (field === before) return prefs;
  const order = prefs.order.filter((item) => item !== field);
  const index = before === undefined ? order.length : order.indexOf(before);
  order.splice(index < 0 ? order.length : index, 0, field);
  return { ...prefs, order };
}

export function setPinned(prefs: ColumnPrefs, field: string, side: 'start' | 'end' | false): ColumnPrefs {
  const start = prefs.pinned.start.filter((item) => item !== field);
  const end = prefs.pinned.end.filter((item) => item !== field);
  if (side === 'start') start.push(field);
  if (side === 'end') end.push(field);
  return { ...prefs, pinned: { start, end } };
}

/** Activa o desactiva el ajuste de columnas al ancho (desactivado: la lista se desplaza en horizontal). */
export function setAutoFit(prefs: ColumnPrefs, on: boolean): ColumnPrefs {
  const { autoFit: _previous, ...rest } = prefs;
  return on ? rest : { ...rest, autoFit: false };
}

export function setWidth(prefs: ColumnPrefs, field: string, width: number | undefined): ColumnPrefs {
  const sizes = { ...prefs.sizes };
  if (width === undefined) delete sizes[field];
  else sizes[field] = clampWidth(width);
  return { ...prefs, sizes };
}
