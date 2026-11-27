// Valores de celda para el almacén de edición: igualdad por tipo y valor «vacío» de cada tipo.
// Sin React: lo usa el almacén (store.ts) y lo prueban las unitarias.
import type { FieldKind } from '../contracts';
import { compareDecimalStrings, toDecimalString } from '../fields/decimal';
import { isEmpty, sameValue } from '../fields/values';

const DECIMAL_KINDS: ReadonlySet<FieldKind> = new Set(['decimal', 'money', 'percentage']);
const TEXT_KINDS: ReadonlySet<FieldKind> = new Set(['text', 'textarea', 'email', 'phone', 'url']);

/**
 * ¿Son el mismo valor? Los decimales se comparan como cadenas decimales (`"10"` = `"10.00"`), los
 * números por valor, las fechas con hora por instante; el resto con la igualdad tolerante de los
 * campos (vacío = nulo = cadena en blanco = lista vacía).
 */
export function valuesEqual(a: unknown, b: unknown, kind?: FieldKind): boolean {
  if (isEmpty(a) && isEmpty(b)) return true;
  if (isEmpty(a) || isEmpty(b)) return false;
  if (kind && DECIMAL_KINDS.has(kind)) {
    const x = toDecimalString(a);
    const y = toDecimalString(b);
    if (x !== undefined && y !== undefined) return compareDecimalStrings(x, y) === 0;
  }
  if (kind === 'number') {
    const x = Number(a);
    const y = Number(b);
    if (Number.isFinite(x) && Number.isFinite(y)) return x === y;
  }
  if (kind === 'datetime') {
    const x = Date.parse(String(a));
    const y = Date.parse(String(b));
    if (Number.isFinite(x) && Number.isFinite(y)) return x === y;
  }
  return sameValue(a, b);
}

/** Valor con el que «Supr» vacía una celda: cadena vacía en los textos, lista vacía, `false`, o nulo. */
export function emptyValueFor(kind: FieldKind | undefined): unknown {
  if (!kind) return null;
  if (TEXT_KINDS.has(kind)) return '';
  if (kind === 'multiselect') return [];
  if (kind === 'boolean') return false;
  return null;
}
