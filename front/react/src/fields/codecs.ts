// Códecs de los 16 tipos de campo: `format`, `parse`, `validate` y alineación, sin React.
// El registro (`registry.ts`) los une a su visor y su editor. Reglas (PLAN §4.8, A4 §5.1.1):
//   - decimal, money y percentage viajan como CADENA decimal canónica (`"1200.10"`), sin flotantes;
//   - number viaja como `number`;
//   - date viaja como `YYYY-MM-DD` y datetime como ISO 8601 en UTC;
//   - boolean tolera true/'true'/'sí'/'yes'/'1'; select, por valor o etiqueta;
//   - validate devuelve códigos: required, maxLength, minLength, min, max, pattern, format.
import type { FieldDef, FieldKind, FieldTypeDef, TextResolver } from '../contracts';
import {
  compareDecimalStrings,
  editDecimalString,
  formatDecimalString,
  numberLocale,
  numberToDecimalString,
  parseDecimalText,
  toDecimalString,
  type DecimalFormatOptions,
} from './decimal';
import {
  dateParts,
  dateTimeParts,
  formatDateParts,
  formatDateTimeParts,
  parseDateText,
  parseDateTimeText,
  toIsoDate,
  toIsoDateTime,
} from './dates';
import { cachedLookupId, cachedLookupLabel } from './lookup-cache';
import { findOption, foldText, isEmpty, optionLabel, resolveWith, toStringArray } from './values';

export type ParseResult = { ok: true; value: unknown } | { ok: false; code: string };

export interface FieldCodec {
  kind: FieldKind;
  align: FieldTypeDef['align'];
  /**
   * Texto para mostrar (vacío si no hay valor; el visor pinta `emptyValue`). `resolve` traduce las
   * etiquetas de la configuración (booleano, opciones); sin él se usa su literal o el objeto por idioma.
   */
  format(value: unknown, def: FieldDef, locale: string, resolve?: TextResolver): string;
  /** Texto para editar (por ejemplo, sin separador de miles). */
  editText(value: unknown, def: FieldDef, locale: string, resolve?: TextResolver): string;
  parse(text: string, def: FieldDef, locale: string, resolve?: TextResolver): ParseResult;
  validate(value: unknown, def: FieldDef): string[];
}

const OK_EMPTY: ParseResult = { ok: true, value: null };
const FORMAT_ERROR: ParseResult = { ok: false, code: 'format' };

// ---------------------------------------------------------------------------------------------
// Validación compartida

function requiredCodes(value: unknown, def: FieldDef): string[] | undefined {
  if (isEmpty(value)) return def.required ? ['required'] : [];
  return undefined;
}

/** `pattern` con la semántica de HTML: debe coincidir con todo el texto. */
function matchesPattern(text: string, pattern: string): boolean {
  try {
    return new RegExp(`^(?:${pattern})$`, 'u').test(text);
  } catch {
    return true; // un patrón inválido en la configuración no bloquea al usuario
  }
}

function lengthCodes(text: string, def: FieldDef): string[] {
  const codes: string[] = [];
  const length = Array.from(text).length;
  if (def.maxLength !== undefined && length > def.maxLength) codes.push('maxLength');
  if (def.minLength !== undefined && length < def.minLength) codes.push('minLength');
  if (def.pattern !== undefined && !matchesPattern(text, def.pattern)) codes.push('pattern');
  return codes;
}

function validateText(value: unknown, def: FieldDef, formatOk?: (text: string) => boolean): string[] {
  const early = requiredCodes(value, def);
  if (early) return early;
  const text = typeof value === 'number' ? String(value) : value;
  if (typeof text !== 'string') return ['format'];
  const codes = lengthCodes(text, def);
  if (formatOk && !formatOk(text.trim())) codes.push('format');
  return codes;
}

function validateNumeric(value: unknown, def: FieldDef): string[] {
  const early = requiredCodes(value, def);
  if (early) return early;
  const text = toDecimalString(value);
  if (text === undefined) return ['format'];
  const codes: string[] = [];
  if (def.min !== undefined && compareDecimalStrings(text, numberToDecimalString(def.min)) < 0) codes.push('min');
  if (def.max !== undefined && compareDecimalStrings(text, numberToDecimalString(def.max)) > 0) codes.push('max');
  return codes;
}

// ---------------------------------------------------------------------------------------------
// Formato numérico (`FieldDef.format`)

/**
 * `FieldDef.format` en campos numéricos: decimales (`0.00`, `n2`, `decimals:2`) y, en `money`, la
 * moneda (`EUR`, `currency:EUR`). Sin formato, `money` muestra al menos 2 decimales y nunca se
 * redondea un valor con más.
 */
export function fractionDigitsOf(def: FieldDef): number | undefined {
  const format = def.format?.trim();
  if (!format) return undefined;
  const zeros = /^#*[0#,]*\.(0+)$/.exec(format);
  if (zeros) return zeros[1]?.length;
  const named = /^(?:n|f|decimals?[:=])(\d)$/i.exec(format);
  if (named) return Number(named[1]);
  return undefined;
}

export function currencyOf(def: FieldDef): string | undefined {
  const format = def.format?.trim();
  if (!format) return undefined;
  const named = /^(?:currency|moneda)[:=]([A-Za-z]{3})$/i.exec(format);
  if (named) return named[1]?.toUpperCase();
  return /^[A-Z]{3}$/.test(format) ? format : undefined;
}

const currencyAffixes = new Map<string, { symbol: string; prefix: boolean; gap: string }>();

function currencyAffix(code: string, locale: string): { symbol: string; prefix: boolean; gap: string } {
  const cacheKey = `${locale}|${code}`;
  let affix = currencyAffixes.get(cacheKey);
  if (!affix) {
    try {
      const parts = new Intl.NumberFormat(locale, { style: 'currency', currency: code }).formatToParts(1234.5);
      const symbolIndex = parts.findIndex((part) => part.type === 'currency');
      const integerIndex = parts.findIndex((part) => part.type === 'integer');
      const literal = parts.find((part) => part.type === 'literal')?.value ?? '';
      affix = {
        symbol: parts[symbolIndex]?.value ?? code,
        prefix: symbolIndex < integerIndex,
        gap: literal,
      };
    } catch {
      affix = { symbol: code, prefix: false, gap: ' ' };
    }
    currencyAffixes.set(cacheKey, affix);
  }
  return affix;
}

function decimalOptions(def: FieldDef, kind: FieldKind): DecimalFormatOptions {
  const fraction = fractionDigitsOf(def);
  if (fraction !== undefined) return { fraction, minGrouping: kind === 'money' ? 1 : undefined };
  if (kind === 'money') return { minFraction: 2, minGrouping: 1 };
  if (kind === 'decimal') return { minGrouping: 1 };
  return {};
}

function formatNumeric(kind: FieldKind) {
  return (value: unknown, def: FieldDef, locale: string): string => {
    if (isEmpty(value)) return '';
    const text = toDecimalString(value);
    if (text === undefined) return String(value);
    let out = formatDecimalString(text, locale, decimalOptions(def, kind));
    if (kind === 'percentage') return `${out}${numberLocale(locale).percentSpace}%`;
    if (kind === 'money') {
      const code = currencyOf(def);
      if (code) {
        const affix = currencyAffix(code, locale);
        out = affix.prefix ? `${affix.symbol}${affix.gap}${out}` : `${out}${affix.gap}${affix.symbol}`;
      }
    }
    return out;
  };
}

function editNumeric(value: unknown, _def: FieldDef, locale: string): string {
  if (isEmpty(value)) return '';
  const text = toDecimalString(value);
  return text === undefined ? String(value) : editDecimalString(text, locale);
}

function numericCodec(kind: 'number' | 'decimal' | 'money' | 'percentage'): FieldCodec {
  return {
    kind,
    align: 'end',
    format: formatNumeric(kind),
    editText: editNumeric,
    parse(text, _def, locale) {
      const result = parseDecimalText(text, locale);
      if (!result.ok) return FORMAT_ERROR;
      if (result.value === null) return OK_EMPTY;
      return { ok: true, value: kind === 'number' ? Number(result.value) : result.value };
    },
    validate: validateNumeric,
  };
}

// ---------------------------------------------------------------------------------------------
// Texto

function stringCodec(
  kind: 'text' | 'textarea' | 'email' | 'phone' | 'url',
  formatOk?: (text: string) => boolean,
): FieldCodec {
  const asText = (value: unknown): string => (isEmpty(value) ? '' : String(value));
  return {
    kind,
    align: 'start',
    format: (value) => asText(value),
    editText: (value) => asText(value),
    parse(text) {
      if (kind === 'textarea') return { ok: true, value: text.replace(/\r\n?/g, '\n') };
      return { ok: true, value: text.trim() };
    },
    validate: (value, def) => validateText(value, def, formatOk),
  };
}

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

function phoneOk(text: string): boolean {
  const digits = text.replace(/\D/g, '');
  return /^\+?[\d\s().\-/]+(?:\s*(?:ext|x)\.?\s*\d+)?$/i.test(text) && digits.length >= 6 && digits.length <= 15;
}

const URL_PROTOCOLS = new Set(['http:', 'https:', 'ftp:', 'mailto:', 'tel:']);

export function isSafeUrl(text: string): boolean {
  try {
    return URL_PROTOCOLS.has(new URL(text).protocol);
  } catch {
    return false;
  }
}

// ---------------------------------------------------------------------------------------------
// Fecha y fecha con hora

const dateCodec: FieldCodec = {
  kind: 'date',
  align: 'start',
  format(value, _def, locale) {
    const parts = dateParts(value);
    return parts ? formatDateParts(parts, locale) : isEmpty(value) ? '' : String(value);
  },
  editText(value, def, locale) {
    return dateCodec.format(value, def, locale);
  },
  parse(text, _def, locale) {
    if (text.trim() === '') return OK_EMPTY;
    const result = parseDateText(text, locale);
    return result.ok ? { ok: true, value: toIsoDate(result.value) } : FORMAT_ERROR;
  },
  validate(value, def) {
    const early = requiredCodes(value, def);
    if (early) return early;
    return dateParts(value) ? [] : ['format'];
  },
};

const dateTimeCodec: FieldCodec = {
  kind: 'datetime',
  align: 'start',
  format(value, _def, locale) {
    const parts = dateTimeParts(value);
    return parts ? formatDateTimeParts(parts, locale) : isEmpty(value) ? '' : String(value);
  },
  editText(value, def, locale) {
    return dateTimeCodec.format(value, def, locale);
  },
  parse(text, _def, locale) {
    if (text.trim() === '') return OK_EMPTY;
    const result = parseDateTimeText(text, locale);
    return result.ok ? { ok: true, value: toIsoDateTime(result.value) } : FORMAT_ERROR;
  },
  validate(value, def) {
    const early = requiredCodes(value, def);
    if (early) return early;
    return dateTimeParts(value) ? [] : ['format'];
  },
};

// ---------------------------------------------------------------------------------------------
// Booleano

const TRUE_WORDS = new Set(['true', 't', '1', 'si', 'sí', 's', 'yes', 'y', 'x', 'on', 'verdadero', 'ok']);
const FALSE_WORDS = new Set(['false', 'f', '0', 'no', 'n', 'off', 'falso']);

/** `true`/`'true'`/`'sí'`/`'yes'`/`1`… → true; `false`/`'no'`/`0`… → false; otro valor → `undefined`. */
export function toBoolean(value: unknown): boolean | undefined {
  if (typeof value === 'boolean') return value;
  if (typeof value === 'number') return value === 1 ? true : value === 0 ? false : undefined;
  if (typeof value !== 'string') return undefined;
  const word = foldText(value);
  if (TRUE_WORDS.has(word) || TRUE_WORDS.has(value.trim().toLowerCase())) return true;
  if (FALSE_WORDS.has(word)) return false;
  return undefined;
}

export function booleanLabel(value: boolean, def: FieldDef, locale: string, resolve?: TextResolver): string {
  const custom = value ? def.trueLabel : def.falseLabel;
  const text = custom ? resolveWith(custom, locale, resolve) : '';
  if (text) return text;
  const english = locale.toLowerCase().startsWith('en');
  if (value) return english ? 'Yes' : 'Sí';
  return 'No';
}

const booleanCodec: FieldCodec = {
  kind: 'boolean',
  align: 'center',
  format(value, def, locale, resolve) {
    const flag = toBoolean(value);
    return flag === undefined ? '' : booleanLabel(flag, def, locale, resolve);
  },
  editText(value, def, locale, resolve) {
    return booleanCodec.format(value, def, locale, resolve);
  },
  parse(text, def, locale, resolve) {
    if (text.trim() === '') return OK_EMPTY;
    const flag = toBoolean(text);
    if (flag !== undefined) return { ok: true, value: flag };
    // También las etiquetas propias de la configuración (la traducida y la del idioma de referencia).
    for (const candidate of [true, false]) {
      const labels = [booleanLabel(candidate, def, locale, resolve), booleanLabel(candidate, def, locale)];
      if (labels.some((label) => foldText(label) === foldText(text))) return { ok: true, value: candidate };
    }
    return FORMAT_ERROR;
  },
  validate(value, def) {
    if (value === null || value === undefined || value === '') return def.required ? ['required'] : [];
    return toBoolean(value) === undefined ? ['format'] : [];
  },
};

// ---------------------------------------------------------------------------------------------
// Selección

function selectFormat(value: unknown, def: FieldDef, locale: string, resolve?: TextResolver): string {
  if (isEmpty(value)) return '';
  const option = findOption(def.options, value, locale, resolve);
  return option ? optionLabel(option, locale, resolve) : String(value);
}

function selectCodec(kind: 'select' | 'badge'): FieldCodec {
  return {
    kind,
    align: 'start',
    format: selectFormat,
    editText: selectFormat,
    parse(text, def, locale, resolve) {
      if (text.trim() === '') return OK_EMPTY;
      if (!def.options || def.options.length === 0) return { ok: true, value: text.trim() };
      const option = findOption(def.options, text, locale, resolve);
      return option ? { ok: true, value: option.value } : FORMAT_ERROR;
    },
    validate(value, def) {
      const early = requiredCodes(value, def);
      if (early) return early;
      if (def.options && def.options.length > 0 && !def.options.some((o) => o.value === String(value))) {
        return ['format'];
      }
      return [];
    },
  };
}

const multiselectCodec: FieldCodec = {
  kind: 'multiselect',
  align: 'start',
  format(value, def, locale, resolve) {
    return toStringArray(value)
      .map((item) => selectFormat(item, def, locale, resolve))
      .join(', ');
  },
  editText(value, def, locale, resolve) {
    return multiselectCodec.format(value, def, locale, resolve);
  },
  parse(text, def, locale, resolve) {
    if (text.trim() === '') return OK_EMPTY;
    const items = text
      .split(/[,;|\n]/)
      .map((part) => part.trim())
      .filter(Boolean);
    const values: string[] = [];
    for (const item of items) {
      if (!def.options || def.options.length === 0) {
        values.push(item);
        continue;
      }
      const option = findOption(def.options, item, locale, resolve);
      if (!option) return FORMAT_ERROR;
      if (!values.includes(option.value)) values.push(option.value);
    }
    return { ok: true, value: values };
  },
  validate(value, def) {
    const items = toStringArray(value);
    if (items.length === 0) return def.required ? ['required'] : [];
    const codes: string[] = [];
    if (def.options && def.options.length > 0 && items.some((i) => !def.options?.some((o) => o.value === i))) {
      codes.push('format');
    }
    if (def.min !== undefined && items.length < def.min) codes.push('min');
    if (def.max !== undefined && items.length > def.max) codes.push('max');
    return codes;
  },
};

// ---------------------------------------------------------------------------------------------
// Clave ajena

const lookupCodec: FieldCodec = {
  kind: 'lookup',
  align: 'start',
  /** Sin red: la etiqueta si ya se conoce; si no, el propio id. */
  format(value, def) {
    if (isEmpty(value)) return '';
    return (def.lookup ? cachedLookupLabel(def.lookup, value) : undefined) ?? String(value);
  },
  editText(value, def, locale) {
    return lookupCodec.format(value, def, locale);
  },
  parse(text, def) {
    const clean = text.trim();
    if (clean === '') return OK_EMPTY;
    // Una etiqueta ya conocida da su id; si no, se asume que es el id (el servidor lo comprobará).
    const id = def.lookup ? cachedLookupId(def.lookup, clean) : undefined;
    return { ok: true, value: id ?? clean };
  },
  validate(value, def) {
    const early = requiredCodes(value, def);
    if (early) return early;
    return typeof value === 'string' || typeof value === 'number' ? [] : ['format'];
  },
};

// ---------------------------------------------------------------------------------------------

export const codecs: Readonly<Record<FieldKind, FieldCodec>> = {
  text: stringCodec('text'),
  textarea: stringCodec('textarea'),
  number: numericCodec('number'),
  decimal: numericCodec('decimal'),
  money: numericCodec('money'),
  percentage: numericCodec('percentage'),
  date: dateCodec,
  datetime: dateTimeCodec,
  boolean: booleanCodec,
  select: selectCodec('select'),
  multiselect: multiselectCodec,
  badge: selectCodec('badge'),
  lookup: lookupCodec,
  email: stringCodec('email', (text) => text === '' || EMAIL.test(text)),
  phone: stringCodec('phone', (text) => text === '' || phoneOk(text)),
  url: stringCodec('url', (text) => text === '' || isSafeUrl(text)),
};

export function getCodec(kind: FieldKind): FieldCodec {
  return codecs[kind] ?? codecs.text;
}
