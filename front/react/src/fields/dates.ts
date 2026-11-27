// Fechas y fechas con hora por idioma. Hacia el servidor: `YYYY-MM-DD` (fecha) e ISO 8601 en UTC
// (fecha con hora). Una fecha con hora sin zona se toma como hora local de pared; con `Z` o
// desfase, se muestra en la hora local de la persona.

export interface DateLocale {
  order: 'dmy' | 'mdy' | 'ymd';
  separator: string;
  /** Hora de 12 h con AM/PM. */
  hour12: boolean;
}

const TABLE: Record<string, DateLocale> = {
  es: { order: 'dmy', separator: '/', hour12: false },
  en: { order: 'mdy', separator: '/', hour12: true },
};

const derived = new Map<string, DateLocale>();

export function dateLocale(locale: string): DateLocale {
  const tag = locale.replace('_', '-');
  const exact = TABLE[tag.toLowerCase()];
  if (exact) return exact;
  let spec = derived.get(tag);
  if (!spec) {
    try {
      const format = new Intl.DateTimeFormat(tag, { year: 'numeric', month: '2-digit', day: '2-digit' });
      const parts = format.formatToParts(new Date(2001, 10, 22));
      const order = parts
        .filter((part) => part.type === 'day' || part.type === 'month' || part.type === 'year')
        .map((part) => part.type[0])
        .join('');
      const separator = parts.find((part) => part.type === 'literal')?.value.trim() || '/';
      const hour12 = new Intl.DateTimeFormat(tag, { hour: 'numeric' }).resolvedOptions().hour12 === true;
      spec = {
        order: order === 'dmy' || order === 'mdy' || order === 'ymd' ? order : 'dmy',
        separator,
        hour12,
      };
    } catch {
      spec = TABLE.es as DateLocale;
    }
    derived.set(tag, spec);
  }
  return spec;
}

export interface DateParts {
  year: number;
  month: number;
  day: number;
}

export interface DateTimeParts extends DateParts {
  hour: number;
  minute: number;
  second: number;
}

const pad = (n: number, width = 2) => String(n).padStart(width, '0');

/** ¿Existe ese día en el calendario? */
export function isRealDate({ year, month, day }: DateParts): boolean {
  if (year < 1 || year > 9999 || month < 1 || month > 12 || day < 1) return false;
  const date = new Date(Date.UTC(2000, month - 1, day));
  date.setUTCFullYear(year);
  return date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day;
}

export function toIsoDate({ year, month, day }: DateParts): string {
  return `${pad(year, 4)}-${pad(month)}-${pad(day)}`;
}

const ISO_DATE = /^(\d{4})-(\d{2})-(\d{2})/;

/** Partes de una fecha `YYYY-MM-DD` (o el comienzo de un ISO con hora); `undefined` si no es real. */
export function dateParts(value: unknown): DateParts | undefined {
  if (value instanceof Date) {
    if (Number.isNaN(value.getTime())) return undefined;
    return { year: value.getFullYear(), month: value.getMonth() + 1, day: value.getDate() };
  }
  if (typeof value !== 'string') return undefined;
  const match = ISO_DATE.exec(value.trim());
  if (!match) return undefined;
  const parts = { year: Number(match[1]), month: Number(match[2]), day: Number(match[3]) };
  return isRealDate(parts) ? parts : undefined;
}

export function formatDateParts(parts: DateParts, locale: string): string {
  const spec = dateLocale(locale);
  const d = pad(parts.day);
  const m = pad(parts.month);
  const y = pad(parts.year, 4);
  const sequence = spec.order === 'dmy' ? [d, m, y] : spec.order === 'mdy' ? [m, d, y] : [y, m, d];
  return sequence.join(spec.separator);
}

/** `YYYY-MM-DD` a texto del idioma; `undefined` si no es una fecha. */
export function formatIsoDate(value: unknown, locale: string): string | undefined {
  const parts = dateParts(value);
  return parts ? formatDateParts(parts, locale) : undefined;
}

export type DateParse = { ok: true; value: DateParts } | { ok: false };

/** Analiza `14/03/2025`, `14-3-25`, `14.03.2025`, `2025-03-14` o `14032025` según el idioma. */
export function parseDateText(text: string, locale: string): DateParse {
  const clean = text.trim();
  const iso = /^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})$/.exec(clean);
  let numbers: [number, number, number] | undefined;
  let order = dateLocale(locale).order;
  if (iso) {
    numbers = [Number(iso[1]), Number(iso[2]), Number(iso[3])];
    order = 'ymd';
  } else {
    const tokens = clean.split(/[/.\-\s]+/).filter(Boolean);
    if (tokens.length === 3 && tokens.every((token) => /^\d{1,4}$/.test(token))) {
      numbers = [Number(tokens[0]), Number(tokens[1]), Number(tokens[2])];
    } else if (tokens.length === 1 && /^\d{8}$/.test(clean)) {
      const compact = clean;
      numbers =
        order === 'ymd'
          ? [Number(compact.slice(0, 4)), Number(compact.slice(4, 6)), Number(compact.slice(6, 8))]
          : [Number(compact.slice(0, 2)), Number(compact.slice(2, 4)), Number(compact.slice(4, 8))];
    }
  }
  if (!numbers) return { ok: false };
  const [a, b, c] = numbers;
  const raw =
    order === 'dmy'
      ? { day: a, month: b, year: c }
      : order === 'mdy'
        ? { month: a, day: b, year: c }
        : { year: a, month: b, day: c };
  // Año de dos cifras: 00-69 → 2000-2069, 70-99 → 1970-1999.
  const year = raw.year < 100 ? (raw.year < 70 ? 2000 + raw.year : 1900 + raw.year) : raw.year;
  const parts = { year, month: raw.month, day: raw.day };
  return isRealDate(parts) ? { ok: true, value: parts } : { ok: false };
}

const ISO_DATE_TIME =
  /^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?\s*(Z|[+-]\d{2}:?\d{2})?$/i;

/** Partes (en hora local de pared) de un ISO con hora; `undefined` si no es válido. */
export function dateTimeParts(value: unknown): DateTimeParts | undefined {
  if (value instanceof Date) {
    if (Number.isNaN(value.getTime())) return undefined;
    return {
      year: value.getFullYear(),
      month: value.getMonth() + 1,
      day: value.getDate(),
      hour: value.getHours(),
      minute: value.getMinutes(),
      second: value.getSeconds(),
    };
  }
  if (typeof value !== 'string') return undefined;
  const text = value.trim();
  const match = ISO_DATE_TIME.exec(text);
  if (!match) {
    // Sólo fecha: medianoche.
    const day = dateParts(text);
    return day && /^\d{4}-\d{2}-\d{2}$/.test(text) ? { ...day, hour: 0, minute: 0, second: 0 } : undefined;
  }
  const parts: DateTimeParts = {
    year: Number(match[1]),
    month: Number(match[2]),
    day: Number(match[3]),
    hour: Number(match[4]),
    minute: Number(match[5]),
    second: Number(match[6] ?? 0),
  };
  if (!isRealDate(parts) || parts.hour > 23 || parts.minute > 59 || parts.second > 59) return undefined;
  const zone = match[7];
  if (zone) {
    // Con zona (`Z` o desfase): se muestra en la hora local de la persona.
    const offset = zone.toUpperCase() === 'Z' ? 'Z' : zone.replace(/^([+-]\d{2})(\d{2})$/, '$1:$2');
    const iso = `${match[1]}-${match[2]}-${match[3]}T${match[4]}:${match[5]}:${pad(parts.second)}${offset}`;
    return dateTimeParts(new Date(iso));
  }
  return parts;
}

export function formatTimeParts(parts: DateTimeParts, locale: string): string {
  const spec = dateLocale(locale);
  if (!spec.hour12) return `${pad(parts.hour)}:${pad(parts.minute)}`;
  const suffix = parts.hour >= 12 ? 'PM' : 'AM';
  const hour = parts.hour % 12 === 0 ? 12 : parts.hour % 12;
  return `${hour}:${pad(parts.minute)} ${suffix}`;
}

export function formatDateTimeParts(parts: DateTimeParts, locale: string): string {
  return `${formatDateParts(parts, locale)} ${formatTimeParts(parts, locale)}`;
}

export type DateTimeParse = { ok: true; value: DateTimeParts } | { ok: false };

/** Analiza `14/03/2025 10:30`, `03/14/2025 10:30 PM`, `2025-03-14T10:30` o sólo la fecha. */
export function parseDateTimeText(text: string, locale: string): DateTimeParse {
  const clean = text.trim();
  const fromIso = dateTimeParts(clean);
  if (fromIso && ISO_DATE_TIME.test(clean)) return { ok: true, value: fromIso };
  const split = /^(\S+)(?:\s+(.+))?$/.exec(clean);
  if (!split) return { ok: false };
  const date = parseDateText(split[1] ?? '', locale);
  if (!date.ok) return { ok: false };
  let hour = 0;
  let minute = 0;
  let second = 0;
  const timeText = split[2]?.trim();
  if (timeText) {
    const time = /^(\d{1,2})[:.](\d{2})(?::(\d{2}))?\s*([ap])?\.?m?\.?$/i.exec(timeText);
    if (!time) return { ok: false };
    hour = Number(time[1]);
    minute = Number(time[2]);
    second = Number(time[3] ?? 0);
    const meridiem = time[4]?.toLowerCase();
    if (meridiem) {
      if (hour < 1 || hour > 12) return { ok: false };
      hour = (hour % 12) + (meridiem === 'p' ? 12 : 0);
    }
    if (hour > 23 || minute > 59 || second > 59) return { ok: false };
  }
  return { ok: true, value: { ...date.value, hour, minute, second } };
}

/** ISO 8601 en UTC de unas partes en hora local (lo que viaja al servidor). */
export function toIsoDateTime(parts: DateTimeParts): string {
  const date = new Date(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, parts.second);
  return date.toISOString();
}
