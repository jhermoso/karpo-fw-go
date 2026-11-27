// Formatos con Intl (PLAN §4.6): números, importes, porcentajes y fechas según el idioma activo.
//
//  - Los importes de Go son CADENAS decimales ("1234567890123456.78"): no caben en un double sin perder
//    cifras. Se formatean como texto: Intl.NumberFormat acepta cadenas decimales exactas (ES2023) y,
//    donde no lo hace, se agrupan los dígitos a mano con los símbolos del idioma. El valor que viaja y
//    se guarda sigue siendo la cadena; aquí sólo se muestra.
//  - Las fechas de Go son `YYYY-MM-DD` sin hora: se interpretan como fecha de calendario (UTC) y se
//    muestran en UTC, para que nadie vea el día anterior por su zona horaria.
//  - Las cifras salen SIEMPRE en dígitos latinos (`numberingSystem: 'latn'`, D-R38), también en árabe: una cifra
//    en dígitos arábigo-índicos no casa con el resto de la interfaz ni con lo que se teclea. Quien quiera otro
//    sistema numérico lo pide en `options.numberingSystem`.

export interface Formatters {
  /** Idioma de Intl con el que se formatea (el pseudoidioma `xx` usa el de referencia). */
  readonly locale: string;
  number(value: number | string, options?: Intl.NumberFormatOptions): string;
  /** Importe en `currency` (código ISO 4217). Con cadena decimal no pierde cifras. */
  money(value: number | string, currency: string, options?: Intl.NumberFormatOptions): string;
  /** `12.5` → «12,5 %» (el valor ya es un porcentaje, no una fracción). */
  percent(value: number | string, options?: Intl.NumberFormatOptions): string;
  /** `YYYY-MM-DD` (sin zona), instante ISO, `Date` o milisegundos. */
  date(value: Date | string | number, options?: Intl.DateTimeFormatOptions): string;
  dateTime(value: Date | string | number, options?: Intl.DateTimeFormatOptions): string;
  /** Rango de números con las convenciones del idioma (`4–12`, `~4` si coinciden): `Intl.NumberFormat#formatRange`. */
  range(from: number | string, to: number | string, options?: Intl.NumberFormatOptions): string;
}

const DECIMAL = /^([+-]?)(\d+)(?:\.(\d+))?$/;
const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})$/;

/** Sistema numérico por omisión de todos los idiomas (D-R38). */
export const DEFAULT_NUMBERING_SYSTEM = 'latn';

/** Locale de Intl para un idioma de la aplicación (`xx` es el pseudoidioma de desarrollo). */
export function intlLocale(lng: string): string {
  return lng === 'xx' || lng === '' ? 'es' : lng;
}

let stringPrecision: boolean | undefined;
/** ¿El motor de Intl formatea cadenas decimales sin pasarlas por un double? (NumberFormat v3). */
function supportsExactDecimalStrings(): boolean {
  if (stringPrecision === undefined) {
    try {
      const out = new Intl.NumberFormat('en', { useGrouping: false }).format('9007199254740993' as unknown as number);
      stringPrecision = out === '9007199254740993';
    } catch {
      stringPrecision = false;
    }
  }
  return stringPrecision;
}

/** Redondea una parte fraccionaria decimal a `digits` cifras (mitad hacia arriba); devuelve acarreo. */
function roundFraction(fraction: string, digits: number): { fraction: string; carry: boolean } {
  if (fraction.length <= digits) return { fraction: fraction.padEnd(digits, '0'), carry: false };
  const kept = fraction.slice(0, digits).split('').map(Number);
  let carry = Number(fraction[digits]) >= 5;
  for (let i = kept.length - 1; i >= 0 && carry; i -= 1) {
    const next = (kept[i] ?? 0) + 1;
    kept[i] = next % 10;
    carry = next === 10;
  }
  return { fraction: kept.join(''), carry };
}

function incrementInteger(integer: string): string {
  const digits = integer.split('').map(Number);
  let carry = true;
  for (let i = digits.length - 1; i >= 0 && carry; i -= 1) {
    const next = (digits[i] ?? 0) + 1;
    digits[i] = next % 10;
    carry = next === 10;
  }
  return (carry ? '1' : '') + digits.join('');
}

/** Agrupa a mano las cifras enteras con el símbolo y el mínimo de agrupación del idioma. */
function groupInteger(integer: string, locale: string): string {
  const plain = new Intl.NumberFormat(locale, { numberingSystem: DEFAULT_NUMBERING_SYSTEM });
  const parts = plain.formatToParts(1000000.5);
  const group = parts.find((p) => p.type === 'group')?.value;
  if (!group) return integer;
  // es, pl, pt-PT... no agrupan los números de 4 cifras (minimumGroupingDigits = 2).
  const minimum = plain.formatToParts(1000).some((p) => p.type === 'group') ? 1 : 2;
  if (integer.length < 3 + minimum) return integer;
  return integer.replace(/\B(?=(\d{3})+(?!\d))/g, group);
}

/**
 * Formatea una cadena decimal con un `Intl.NumberFormat` ya construido. Si el motor no entiende
 * cadenas exactas (o se fuerza `manual`), sustituye las cifras de la plantilla por las de la cadena.
 */
export function formatDecimalString(
  value: string,
  format: Intl.NumberFormat,
  options: { manual?: boolean } = {},
): string {
  const match = DECIMAL.exec(value.trim());
  if (!match) return format.format(Number(value));
  if (!options.manual && supportsExactDecimalStrings()) {
    return format.format(value.trim() as unknown as number);
  }
  const [, sign = '', integerRaw = '0', fractionRaw = ''] = match;
  const resolved = format.resolvedOptions();
  const min = resolved.minimumFractionDigits ?? 0;
  const max = resolved.maximumFractionDigits ?? 3;
  let integer = integerRaw.replace(/^0+(?=\d)/, '');
  const rounded = roundFraction(fractionRaw, max);
  let fraction = rounded.fraction;
  if (rounded.carry) integer = incrementInteger(integer);
  // Quita ceros finales de más allá del mínimo.
  while (fraction.length > min && fraction.endsWith('0')) fraction = fraction.slice(0, -1);
  const negative = sign === '-' && /[1-9]/.test(integer + fraction);
  const parts = format.formatToParts(negative ? -1 : 1);
  const grouped = resolved.useGrouping === false ? integer : groupInteger(integer, resolved.locale);
  const templateHasFraction = parts.some((p) => p.type === 'fraction');
  const decimalSymbol =
    new Intl.NumberFormat(resolved.locale, { minimumFractionDigits: 1, numberingSystem: resolved.numberingSystem })
      .formatToParts(1.5)
      .find((p) => p.type === 'decimal')?.value ?? '.';
  let out = '';
  let wroteInteger = false;
  for (const part of parts) {
    if (part.type === 'integer') {
      if (wroteInteger) continue;
      wroteInteger = true;
      out += grouped;
      // La plantilla (el valor 1) no tiene parte fraccionaria si el formato admite 0 decimales.
      if (!templateHasFraction && fraction.length > 0) out += decimalSymbol + fraction;
    } else if (part.type === 'group') {
      continue;
    } else if (part.type === 'decimal') {
      if (fraction.length > 0) out += part.value;
    } else if (part.type === 'fraction') {
      out += fraction;
    } else {
      out += part.value;
    }
  }
  return out;
}

/** Interpreta `YYYY-MM-DD` como fecha de calendario; devuelve `null` si no lo es. */
function parseDateOnly(value: string): Date | null {
  const match = DATE_ONLY.exec(value);
  if (!match) return null;
  const [, y, m, d] = match.map(Number) as [number, number, number, number];
  const date = new Date(Date.UTC(y, m - 1, d));
  return date.getUTCFullYear() === y && date.getUTCMonth() === m - 1 && date.getUTCDate() === d ? date : null;
}

const numberCache = new Map<string, Intl.NumberFormat>();
const dateCache = new Map<string, Intl.DateTimeFormat>();

function numberFormat(locale: string, options: Intl.NumberFormatOptions): Intl.NumberFormat {
  const withSystem: Intl.NumberFormatOptions = { numberingSystem: DEFAULT_NUMBERING_SYSTEM, ...options };
  const key = `${locale}|${JSON.stringify(withSystem)}`;
  let format = numberCache.get(key);
  if (!format) {
    format = new Intl.NumberFormat(locale, withSystem);
    numberCache.set(key, format);
  }
  return format;
}

function dateFormat(locale: string, options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const withSystem: Intl.DateTimeFormatOptions = { numberingSystem: DEFAULT_NUMBERING_SYSTEM, ...options };
  const key = `${locale}|${JSON.stringify(withSystem)}`;
  let format = dateCache.get(key);
  if (!format) {
    format = new Intl.DateTimeFormat(locale, withSystem);
    dateCache.set(key, format);
  }
  return format;
}

/**
 * Rango de números (`legend.range` del mapa, intervalos): `Intl.NumberFormat#formatRange` (M-12). Con cadenas
 * decimales no pierde cifras. En un motor sin `formatRange` (Edge < 106) se unen los extremos con un
 * guion corto y no se pierde el rango.
 */
export function formatNumberRange(
  locale: string,
  from: number | bigint | string,
  to: number | bigint | string,
  options: Intl.NumberFormatOptions = {},
): string {
  const format = numberFormat(intlLocale(locale), options);
  const withRange = format as Intl.NumberFormat & {
    formatRange?: (a: number | bigint | string, b: number | bigint | string) => string;
  };
  if (typeof withRange.formatRange === 'function') return withRange.formatRange(from, to);
  const single = (value: number | bigint | string): string =>
    typeof value === 'string' ? formatDecimalString(value, format) : format.format(value);
  return `${single(from)}–${single(to)}`;
}

function formatNumberLike(value: number | string, format: Intl.NumberFormat): string {
  return typeof value === 'string' ? formatDecimalString(value, format) : format.format(value);
}

function toDate(value: Date | string | number): { date: Date; calendarOnly: boolean } {
  if (typeof value === 'string') {
    const day = parseDateOnly(value);
    if (day) return { date: day, calendarOnly: true };
    // `2026-02-30` tiene forma de fecha pero no existe: Date la corregiría a marzo; aquí es inválida.
    if (DATE_ONLY.test(value)) return { date: new Date(Number.NaN), calendarOnly: true };
  }
  return { date: value instanceof Date ? value : new Date(value), calendarOnly: false };
}

/** Formateadores de un idioma. Sin estado: se pueden crear donde haga falta (también fuera de React). */
export function createFormatters(lng: string): Formatters {
  const locale = intlLocale(lng);
  return {
    locale,
    number: (value, options) => formatNumberLike(value, numberFormat(locale, options ?? {})),
    range: (from, to, options) => formatNumberRange(locale, from, to, options),
    money: (value, currency, options) =>
      formatNumberLike(value, numberFormat(locale, { style: 'currency', currency, ...options })),
    percent: (value, options) =>
      formatNumberLike(value, numberFormat(locale, { style: 'unit', unit: 'percent', maximumFractionDigits: 2, ...options })),
    date: (value, options) => {
      const { date, calendarOnly } = toDate(value);
      if (Number.isNaN(date.getTime())) return '';
      return dateFormat(locale, {
        dateStyle: 'medium',
        ...options,
        ...(calendarOnly ? { timeZone: 'UTC' } : {}),
      }).format(date);
    },
    dateTime: (value, options) => {
      const { date, calendarOnly } = toDate(value);
      if (Number.isNaN(date.getTime())) return '';
      return dateFormat(locale, {
        dateStyle: 'medium',
        timeStyle: 'short',
        ...options,
        ...(calendarOnly ? { timeZone: 'UTC' } : {}),
      }).format(date);
    },
  };
}
