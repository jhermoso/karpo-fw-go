// Números y decimales por idioma, sin pasar por coma flotante: un importe o decimal que llega como
// cadena (`"1200.10"`) se muestra, se edita y vuelve al servidor como cadena (`"1200.10"`).
// La forma canónica es `-?\d+(\.\d+)?` (punto decimal, sin separador de miles).

export interface NumberLocale {
  decimal: string;
  group: string;
  /** Mínimo de cifras enteras para agrupar: 1 = desde 4 cifras (`1,234`); 2 = desde 5 (`12.345`, `1234`). */
  minGrouping: 1 | 2;
  /** Texto entre la cifra y el `%` (`es`: espacio duro). */
  percentSpace: string;
}

const NBSP = '\u00a0';

const TABLE: Record<string, NumberLocale> = {
  es: { decimal: ',', group: '.', minGrouping: 2, percentSpace: NBSP },
  en: { decimal: '.', group: ',', minGrouping: 1, percentSpace: '' },
};

const derived = new Map<string, NumberLocale>();

/** Separadores del idioma: tabla propia para `es` y `en`; Intl para el resto de etiquetas. */
export function numberLocale(locale: string): NumberLocale {
  const tag = locale.replace('_', '-');
  const exact = TABLE[tag.toLowerCase()];
  if (exact) return exact;
  let spec = derived.get(tag);
  if (!spec) {
    try {
      const format = new Intl.NumberFormat(tag);
      const parts = format.formatToParts(1234567.5);
      const decimal = parts.find((part) => part.type === 'decimal')?.value ?? '.';
      const group = parts.find((part) => part.type === 'group')?.value ?? ',';
      const percent = new Intl.NumberFormat(tag, { style: 'percent' }).formatToParts(0.5);
      const literal = percent.find((part) => part.type === 'literal')?.value ?? '';
      spec = {
        decimal,
        group,
        minGrouping: format.format(1234).includes(group) ? 1 : 2,
        percentSpace: literal,
      };
    } catch {
      spec = TABLE.en as NumberLocale;
    }
    derived.set(tag, spec);
  }
  return spec;
}

const CANONICAL = /^-?\d+(\.\d+)?$/;

/** ¿Es una cadena decimal canónica (`-12.50`)? */
export function isDecimalString(value: unknown): value is string {
  return typeof value === 'string' && CANONICAL.test(value);
}

/** Convierte un `number` a cadena decimal sin notación científica. */
export function numberToDecimalString(value: number): string {
  if (!Number.isFinite(value)) return '';
  const plain = String(value);
  if (!/e/i.test(plain)) return plain;
  return value.toLocaleString('en-US', { useGrouping: false, maximumFractionDigits: 20 });
}

/** Cadena decimal canónica de un valor (`number` o cadena canónica); `undefined` si no lo es. */
export function toDecimalString(value: unknown): string | undefined {
  if (typeof value === 'number') {
    const text = numberToDecimalString(value);
    return CANONICAL.test(text) ? text : undefined;
  }
  if (typeof value === 'string') {
    const text = value.trim();
    return CANONICAL.test(text) ? text : undefined;
  }
  return undefined;
}

function countOf(text: string, char: string): number {
  return text.split(char).length - 1;
}

function validGrouping(integer: string, separator: string): boolean {
  const parts = integer.split(separator);
  const [first, ...rest] = parts;
  return /^\d{1,3}$/.test(first ?? '') && rest.every((part) => /^\d{3}$/.test(part));
}

export type DecimalParse = { ok: true; value: string | null } | { ok: false };

/**
 * Analiza un texto escrito por una persona y devuelve la cadena decimal canónica:
 * `'1.234,56'` en `es` y `'1,234.56'` en `en` dan `'1234.56'`. Tolera el separador del otro idioma
 * cuando no hay ambigüedad, símbolos de moneda/porcentaje y espacios. Vacío da `null`.
 */
export function parseDecimalText(text: string, locale: string): DecimalParse {
  const spec = numberLocale(locale);
  let body = text
    .trim()
    .replace(/[\s\u00a0\u202f'\u2019]/g, '')
    .replace(/[€$£¥%]/g, '')
    .replace(/^(eur|usd|gbp)|(eur|usd|gbp)$/i, '');
  if (body === '') return { ok: true, value: null };

  let negative = false;
  const sign = body[0];
  if (sign === '-' || sign === '+' || sign === '−') {
    negative = sign !== '+';
    body = body.slice(1);
  }
  if (!/^[0-9.,]+$/.test(body) || !/\d/.test(body)) return { ok: false };

  const lastDot = body.lastIndexOf('.');
  const lastComma = body.lastIndexOf(',');
  let integer: string;
  let fraction = '';

  if (lastDot >= 0 && lastComma >= 0) {
    // Los dos separadores: el último es el decimal y el otro agrupa.
    const decimalSep = lastDot > lastComma ? '.' : ',';
    const groupSep = decimalSep === '.' ? ',' : '.';
    if (countOf(body, decimalSep) !== 1) return { ok: false };
    const [intPart = '', fracPart = ''] = body.split(decimalSep);
    if (!validGrouping(intPart, groupSep)) return { ok: false };
    integer = intPart.split(groupSep).join('');
    fraction = fracPart;
  } else if (lastDot >= 0 || lastComma >= 0) {
    const sep = lastDot >= 0 ? '.' : ',';
    if (countOf(body, sep) > 1) {
      // Repetido: sólo puede ser agrupación de miles.
      if (!validGrouping(body, sep)) return { ok: false };
      integer = body.split(sep).join('');
    } else {
      const [intPart = '', fracPart = ''] = body.split(sep);
      const looksGrouped = /^[1-9]\d{0,2}$/.test(intPart) && /^\d{3}$/.test(fracPart);
      if (sep !== spec.decimal && looksGrouped) {
        integer = intPart + fracPart; // separador de miles del idioma (`1.234` en es)
      } else {
        integer = intPart === '' ? '0' : intPart;
        fraction = fracPart;
      }
    }
  } else {
    integer = body;
  }

  integer = integer.replace(/^0+(?=\d)/, '');
  const allZero = /^0*$/.test(integer + fraction);
  const canonical = `${negative && !allZero ? '-' : ''}${integer}${fraction ? `.${fraction}` : ''}`;
  return CANONICAL.test(canonical) ? { ok: true, value: canonical } : { ok: false };
}

export interface DecimalFormatOptions {
  /** Decimales mínimos (se rellena con ceros). */
  minFraction?: number;
  /** Decimales exactos: redondea (mitad hacia fuera) si hay más. Sin esto nunca se redondea. */
  fraction?: number;
  /** Agrupar miles (por defecto sí). */
  grouping?: boolean;
  /** Cifras enteras mínimas para agrupar; por defecto, la regla del idioma. */
  minGrouping?: 1 | 2;
}

/** Redondeo decimal de una cadena canónica (mitad hacia fuera), sin coma flotante. */
export function roundDecimalString(value: string, digits: number): string {
  const match = /^(-?)(\d+)(?:\.(\d+))?$/.exec(value);
  if (!match) return value;
  const [, sign = '', integer = '0', fraction = ''] = match;
  if (fraction.length <= digits) return value;
  const kept = BigInt(integer + fraction.slice(0, digits));
  const roundUp = Number(fraction[digits]) >= 5;
  const scaled = (roundUp ? kept + 1n : kept).toString().padStart(digits + 1, '0');
  const cut = scaled.length - digits;
  const result = digits > 0 ? `${scaled.slice(0, cut)}.${scaled.slice(cut)}` : scaled;
  return /^0*(\.0*)?$/.test(result) ? result : `${sign}${result}`;
}

function groupInteger(integer: string, spec: NumberLocale, minGrouping: 1 | 2): string {
  if (integer.length < 3 + minGrouping) return integer;
  return integer.replace(/\B(?=(\d{3})+$)/g, spec.group);
}

/** Texto para mostrar de una cadena decimal canónica en el idioma indicado. */
export function formatDecimalString(
  value: string,
  locale: string,
  options: DecimalFormatOptions = {},
): string {
  if (!CANONICAL.test(value)) return value;
  const spec = numberLocale(locale);
  let text = options.fraction !== undefined ? roundDecimalString(value, options.fraction) : value;
  const negative = text.startsWith('-');
  if (negative) text = text.slice(1);
  const [integer = '0', fractionRaw = ''] = text.split('.');
  const minFraction = Math.max(options.fraction ?? 0, options.minFraction ?? 0);
  const fraction = fractionRaw.padEnd(minFraction, '0');
  const grouped = options.grouping === false ? integer : groupInteger(integer, spec, options.minGrouping ?? spec.minGrouping);
  return `${negative ? '-' : ''}${grouped}${fraction ? spec.decimal + fraction : ''}`;
}

/** Texto para editar: separador decimal del idioma, sin agrupar ni rellenar. */
export function editDecimalString(value: string, locale: string): string {
  if (!CANONICAL.test(value)) return value;
  return value.replace('.', numberLocale(locale).decimal);
}

/** Compara dos cadenas decimales canónicas: negativo, 0 o positivo (exacto, sin flotantes). */
export function compareDecimalStrings(a: string, b: string): number {
  const parse = (text: string) => {
    const negative = text.startsWith('-');
    const [integer = '0', fraction = ''] = (negative ? text.slice(1) : text).split('.');
    return { negative, integer: integer.replace(/^0+(?=\d)/, ''), fraction: fraction.replace(/0+$/, '') };
  };
  const left = parse(a);
  const right = parse(b);
  const leftZero = left.integer === '0' && left.fraction === '';
  const rightZero = right.integer === '0' && right.fraction === '';
  if (leftZero && rightZero) return 0;
  if (left.negative !== right.negative) return left.negative ? -1 : 1;
  let order: number;
  if (left.integer.length !== right.integer.length) {
    order = left.integer.length < right.integer.length ? -1 : 1;
  } else if (left.integer !== right.integer) {
    order = left.integer < right.integer ? -1 : 1;
  } else {
    const width = Math.max(left.fraction.length, right.fraction.length);
    const l = left.fraction.padEnd(width, '0');
    const r = right.fraction.padEnd(width, '0');
    order = l === r ? 0 : l < r ? -1 : 1;
  }
  return left.negative ? -order : order;
}
