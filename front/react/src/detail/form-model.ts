// Modelo del formulario del detalle: valores iniciales, esquema zod (`zod/mini`, D-R26) generado de los `FieldDef` (con los
// mismos códigos que `FieldTypeDef.validate`), cambios y cuerpo del PUT/POST. Sin React.
import * as z from 'zod/mini';
import type { FieldDef, FieldKind, Row } from '../contracts';
import type { FieldDefExt } from '../config';
import { getFieldType } from '../fields';
import { isEmpty, sameValue } from '../fields/values';

export type FormMode = 'read' | 'edit' | 'create';

/** Clave de formulario de un campo: react-hook-form trata el punto como anidamiento, aquí no. */
export function fieldKey(field: string): string {
  return field.replaceAll('.', '__');
}

export function getIn(source: unknown, path: string): unknown {
  let current: unknown = source;
  for (const part of path.split('.')) {
    if (typeof current !== 'object' || current === null) return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return current;
}

/** Copia de `source` con `value` puesto en `path` (los tramos intermedios se clonan, no se mutan). */
export function setIn(source: Record<string, unknown>, path: string, value: unknown): Record<string, unknown> {
  const [head, ...rest] = path.split('.');
  if (head === undefined) return source;
  if (rest.length === 0) return { ...source, [head]: value };
  const child = source[head];
  const base = typeof child === 'object' && child !== null && !Array.isArray(child) ? (child as Record<string, unknown>) : {};
  return { ...source, [head]: setIn(base, rest.join('.'), value) };
}

const TEXT_KINDS: ReadonlySet<FieldKind> = new Set(['text', 'textarea', 'email', 'phone', 'url']);

/** ¿Se puede escribir en el campo en este modo? Los `immutable` sólo en el alta. */
export function isWritable(def: FieldDef, mode: FormMode): boolean {
  if (mode === 'read') return false;
  if (def.editable === false) return false;
  if (def.immutable && mode === 'edit') return false;
  return true;
}

function normalizeInitial(def: FieldDef, value: unknown): unknown {
  if (def.kind === 'boolean') return value === undefined || value === null ? false : value;
  if (def.kind === 'multiselect') return Array.isArray(value) ? value : isEmpty(value) ? [] : value;
  return value === undefined ? null : value;
}

/** Valores iniciales (por clave de formulario) de la lista de campos, desde el registro o los valores por omisión. */
export function initialValues(defs: readonly FieldDef[], row: Row | undefined): Record<string, unknown> {
  const values: Record<string, unknown> = {};
  for (const def of defs) {
    const fromRow = row ? getIn(row, def.field) : (def as FieldDefExt).defaultValue;
    values[fieldKey(def.field)] = normalizeInitial(def, fromRow);
  }
  return values;
}

/** Esquema de un campo: delega en `validate` del registro de tipos y devuelve sus códigos como mensajes. */
export function buildFieldSchema(def: FieldDef): z.ZodMiniType<unknown> {
  const type = getFieldType(def.kind);
  return z.unknown().check(
    z.superRefine((value, context) => {
      for (const code of type.validate(value, def)) context.addIssue({ code: 'custom', message: code });
    }),
  );
}

/** Esquema del formulario de los campos dados (los ocultos o de sólo lectura no se incluyen). */
export function buildFormSchema(defs: readonly FieldDef[]) {
  const shape: Record<string, z.ZodMiniType<unknown>> = {};
  for (const def of defs) shape[fieldKey(def.field)] = buildFieldSchema(def);
  return z.looseObject(shape);
}

export interface FieldChange {
  def: FieldDef;
  key: string;
  value: unknown;
  before: unknown;
}

/** Campos escribibles cuyo valor difiere del inicial (la comparación tolera vacíos y números/cadenas). */
export function changedFields(
  defs: readonly FieldDef[],
  baseline: Record<string, unknown>,
  values: Record<string, unknown>,
): FieldChange[] {
  const changes: FieldChange[] = [];
  for (const def of defs) {
    const key = fieldKey(def.field);
    const before = baseline[key];
    const value = values[key];
    if (!sameValue(before, value)) changes.push({ def, key, value, before });
  }
  return changes;
}

/** Valor tal como viaja al servidor: vacío = `''` en texto y `null` en el resto. */
export function serializeValue(def: FieldDef, value: unknown): unknown {
  if (isEmpty(value)) return TEXT_KINDS.has(def.kind) ? '' : def.kind === 'multiselect' ? [] : null;
  return value;
}

/** Cuerpo del PUT: sólo lo cambiado. Un campo con punto envía su objeto de primer nivel completo. */
export function buildPatch(changes: readonly FieldChange[], row: Row | undefined): Record<string, unknown> {
  let patch: Record<string, unknown> = {};
  for (const { def, value } of changes) {
    const serialized = serializeValue(def, value);
    if (def.field.includes('.')) {
      const top = def.field.split('.')[0] as string;
      const current = (patch[top] ?? row?.[top]) as Record<string, unknown> | undefined;
      patch = setIn({ ...patch, ...(current ? { [top]: current } : {}) }, def.field, serialized);
    } else {
      patch[def.field] = serialized;
    }
  }
  return patch;
}

/** Cuerpo del POST: todo lo que no esté vacío (los booleanos y los números cero cuentan). */
export function buildCreateBody(defs: readonly FieldDef[], values: Record<string, unknown>): Record<string, unknown> {
  let body: Record<string, unknown> = {};
  for (const def of defs) {
    const value = values[fieldKey(def.field)];
    if (isEmpty(value)) continue;
    body = setIn(body, def.field, value);
  }
  return body;
}

/** Campos que la configuración declara escribibles, sin repetir el mismo campo en dos secciones. */
export function uniqueFields(defs: readonly FieldDef[]): FieldDef[] {
  const seen = new Set<string>();
  const out: FieldDef[] = [];
  for (const def of defs) {
    if (seen.has(def.field)) continue;
    seen.add(def.field);
    out.push(def);
  }
  return out;
}

/** Cuántas columnas de 12 ocupa un campo: su `width` o el reparto igual de `columns`. */
export function fieldSpan(def: FieldDef, columns: number): number {
  const wide = def.kind === 'textarea' || def.kind === 'multiselect';
  const base = def.width !== undefined && def.width >= 1 ? def.width : Math.floor(12 / Math.min(Math.max(columns, 1), 12));
  return Math.min(12, Math.max(1, wide && def.width === undefined ? 12 : Math.round(base)));
}
