// Campos del organigrama: la definición de cada campo que se pinta en un nodo y su texto plano.
import { getCodec } from '../fields';
import type { EntityConfig, FieldDef, Row, TextResolver } from '../contracts';
import { readPath, type TreeViewDef } from './model';

/** Definición del campo `name` según la configuración (columnas, detalle, formulario) o una mínima. */
export function fieldDefFor(config: EntityConfig, name: string): FieldDef {
  const column = config.columns.find((c) => c.field === name);
  if (column) return column;
  const sections = [...config.detail.sections, ...(config.form?.sections ?? [])];
  for (const section of sections) {
    const found = section.fields.find((f) => f.field === name);
    if (found) return found;
  }
  return { field: name, label: { literal: name }, kind: 'text', required: false, editable: false };
}

export interface NodeFieldDefs {
  label: FieldDef;
  subtitle?: FieldDef;
  badge?: FieldDef;
}

export function nodeFieldDefs(config: EntityConfig, view: TreeViewDef): NodeFieldDefs {
  return {
    label: fieldDefFor(config, view.labelField),
    ...(view.subtitleField ? { subtitle: fieldDefFor(config, view.subtitleField) } : {}),
    ...(view.badgeField ? { badge: fieldDefFor(config, view.badgeField) } : {}),
  };
}

/** Un `lookup` guarda el identificador; su etiqueta se resuelve en pantalla, no aquí. */
export function isPlainKind(def: FieldDef): boolean {
  return def.kind !== 'lookup';
}

/**
 * Texto plano de un campo (con el formato del idioma); vacío para lo que sólo es un identificador. El
 * resolvedor de textos de la configuración llega a los códecs para que las etiquetas traducidas (las de
 * `select`, `badge` o `boolean`) salgan en el idioma activo: lo que se ve es lo que se busca (B-2).
 */
export function plainFieldText(row: Row, def: FieldDef, locale: string, resolve?: TextResolver): string {
  if (!isPlainKind(def)) return '';
  const value = readPath(row, def.field);
  if (value === undefined || value === null || value === '') return '';
  return getCodec(def.kind).format(value, def, locale, resolve);
}

/** Texto que cuenta para la búsqueda y la búsqueda por letra: etiqueta y subtítulo. */
export function searchText(row: Row, defs: NodeFieldDefs, locale: string, resolve?: TextResolver): string {
  return [
    plainFieldText(row, defs.label, locale, resolve),
    defs.subtitle ? plainFieldText(row, defs.subtitle, locale, resolve) : '',
  ]
    .filter(Boolean)
    .join(' ');
}

/** Etiqueta del nodo; si la etiqueta es un identificador (lookup), el propio `id` para no quedar en blanco. */
export function labelText(row: Row, defs: NodeFieldDefs, locale: string, resolve?: TextResolver): string {
  const text = plainFieldText(row, defs.label, locale, resolve);
  return text !== '' ? text : row.id;
}
