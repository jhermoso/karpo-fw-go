// Registro único de tipos de campo (PLAN §4.8). Lo usan la cuadrícula, el detalle, los formularios,
// los filtros y la edición masiva: cada tipo declara visor, editor, `format`/`parse`/`validate`,
// alineación y (en `paste.ts`) analizador de pegado.
import type { ComponentType } from 'react';
import type { FieldKind, FieldTypeDef } from '../contracts';
import { codecs } from './codecs';
import type { EditorProps } from './editor-props';
import { simpleEditors } from './editors';
import { LookupEditor } from './LookupEditor';
import { MultiSelectEditor } from './MultiSelectEditor';
import { viewers } from './viewers';

export const FIELD_KINDS: readonly FieldKind[] = [
  'text',
  'textarea',
  'number',
  'decimal',
  'money',
  'percentage',
  'date',
  'datetime',
  'boolean',
  'select',
  'multiselect',
  'badge',
  'lookup',
  'email',
  'phone',
  'url',
];

const editors: Readonly<Record<FieldKind, ComponentType<EditorProps>>> = {
  ...simpleEditors,
  multiselect: MultiSelectEditor,
  lookup: LookupEditor,
};

function buildType(kind: FieldKind): FieldTypeDef {
  const codec = codecs[kind];
  return {
    kind,
    align: codec.align,
    format: codec.format,
    parse: codec.parse,
    validate: codec.validate,
    Viewer: viewers[kind],
    Editor: editors[kind],
  };
}

/** Los 16 tipos de campo; `registerFieldType` añade o sustituye uno. */
export const fieldTypes: Record<FieldKind, FieldTypeDef> = Object.fromEntries(
  FIELD_KINDS.map((kind) => [kind, buildType(kind)]),
) as Record<FieldKind, FieldTypeDef>;

/** Tipo de campo de un `kind`; uno desconocido se trata como `text`. */
export function getFieldType(kind: FieldKind): FieldTypeDef {
  return fieldTypes[kind] ?? fieldTypes.text;
}

/** Extensión: registra (o sustituye) el tipo de campo de `def.kind`. */
export function registerFieldType(def: FieldTypeDef): void {
  fieldTypes[def.kind] = def;
}
