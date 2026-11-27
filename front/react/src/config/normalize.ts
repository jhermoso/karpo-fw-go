// Normalizador: JSON de Angular (más su superposición) -> EntityConfig (PLAN §4.3, A1 §5, §12.1).
// Reglas: groups = sections; field = dataField; checkbox/switch = boolean; badgeMap + enumValues ->
// options con claves en cadena; colores con nombre -> tono; hex ignorado; editType -> saveMode;
// rowActions + commands -> CommandDef; sortable -> serverSortField sólo si el servidor lo admite;
// hijos con {id} sin ?foreignKey=; claves desconocidas o muertas -> ConfigWarning.
import type {
  ChildGridDef,
  CommandDef,
  ConfigWarning,
  FieldKind,
  HelpStepDef,
  LookupDef,
  MapLevelDef,
  OptionDef,
  Text,
  Tone,
  ViewDef,
  VisibilityRule,
} from '../contracts';
import { checkKeys } from './compat';
import { entityIconWarnings } from './icons';
import { mergeOverlay } from './merge';
import { validateEntityShape } from './schema';
import { knownServerSort } from './sort';
import { humanize, toText } from './text';
import {
  ConfigError,
  type ColumnDefExt,
  type EntityConfigExt,
  type FieldDefExt,
  type FilterDefExt,
  type SectionDefExt,
} from './types';

type Obj = Record<string, unknown>;

// ---------------------------------------------------------------------------------------------
// Ayudas de lectura defensiva
// ---------------------------------------------------------------------------------------------

export function isObj(value: unknown): value is Obj {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function asStr(value: unknown): string | undefined {
  if (typeof value === 'string') return value.trim() === '' ? undefined : value;
  if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  return undefined;
}

function asNum(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim() !== '' && Number.isFinite(Number(value))) {
    return Number(value);
  }
  return undefined;
}

function asBool(value: unknown): boolean | undefined {
  if (typeof value === 'boolean') return value;
  if (value === 'true') return true;
  if (value === 'false') return false;
  return undefined;
}

function asArr(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function strArr(value: unknown): string[] {
  return asArr(value).filter((v): v is string => typeof v === 'string');
}

function clamp(n: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, n));
}

interface Ctx {
  warnings: ConfigWarning[];
  id: string;
  versioned: boolean;
}

function warn(ctx: Ctx, path: string, message: string, severity: 'info' | 'warn' = 'warn'): void {
  ctx.warnings.push({ path, message, severity });
}

/** Revisa las claves de un objeto contra la tabla de compatibilidad y anota los avisos. */
function scan(ctx: Ctx, scope: string, obj: Obj, path: string): void {
  for (const w of checkKeys(scope, obj)) {
    warn(ctx, path ? `${path}.${w.key}` : w.key, w.message, w.severity);
  }
}

/** Texto de una propiedad (con alias) y su `<prop>Key`. */
function textOf(obj: Obj, props: readonly string[]): Text | undefined {
  for (const prop of props) {
    const value = obj[prop];
    const key = obj[`${prop}Key`];
    const text = toText(value, key);
    if (text) return text;
  }
  return undefined;
}

// ---------------------------------------------------------------------------------------------
// Tonos y colores
// ---------------------------------------------------------------------------------------------

const TONE_NAMES: Record<string, Tone> = {
  success: 'success',
  green: 'success',
  error: 'error',
  danger: 'error',
  red: 'error',
  warning: 'warning',
  orange: 'warning',
  yellow: 'warning',
  amber: 'warning',
  info: 'info',
  blue: 'info',
  cyan: 'info',
  teal: 'info',
  neutral: 'neutral',
  gray: 'neutral',
  grey: 'neutral',
  secondary: 'neutral',
  default: 'neutral',
  accent: 'accent',
  primary: 'accent',
  purple: 'accent',
  violet: 'accent',
  indigo: 'accent',
  brand: 'accent',
};

const HASH_TONES: Tone[] = ['accent', 'info', 'success', 'warning'];

/** Tono de un color con nombre. `hex` = hexadecimal (se ignora); `unknown` = nombre sin equivalente. */
export function toneFromColor(color: unknown): { tone?: Tone; ignored?: 'hex' | 'unknown' } {
  if (typeof color !== 'string' || color.trim() === '') return {};
  const c = color.trim().toLowerCase();
  if (/^#[0-9a-f]{3,8}$/.test(c) || /^(rgb|hsl)a?\(/.test(c)) return { ignored: 'hex' };
  const tone = TONE_NAMES[c];
  return tone ? { tone } : { ignored: 'unknown' };
}

/** Tono estable por hash (FNV-1a) del identificador de la entidad. */
export function toneFromHash(id: string): Tone {
  let hash = 0x811c9dc5;
  for (let i = 0; i < id.length; i += 1) {
    hash ^= id.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return HASH_TONES[hash % HASH_TONES.length]!;
}

function toneOf(ctx: Ctx, raw: Obj, path: string): Tone | undefined {
  const explicit = asStr(raw.tone);
  if (explicit && TONE_NAMES[explicit.toLowerCase()]) return TONE_NAMES[explicit.toLowerCase()];
  const result = toneFromColor(raw.color);
  if (result.ignored === 'hex') {
    warn(ctx, `${path}.color`, 'color hexadecimal ignorado: el color sale de los tonos del tema', 'info');
  } else if (result.ignored === 'unknown') {
    warn(ctx, `${path}.color`, `color \`${String(raw.color)}\` sin equivalente: se ignora`, 'info');
  }
  return result.tone;
}

// ---------------------------------------------------------------------------------------------
// Reglas de visibilidad
// ---------------------------------------------------------------------------------------------

const LEGACY_RULE_KEYS = [
  'hasRole',
  'capability',
  'sector',
  'userRole',
  'requiredModule',
  'featureFlag',
  'roles',
  'accessMode',
];

/** Normaliza una regla: cadena = `{expr}`; las cláusulas legadas se descartan con aviso. */
export function normalizeRule(
  raw: unknown,
  warnings: ConfigWarning[],
  path: string,
): VisibilityRule | undefined {
  if (raw === undefined || raw === null || raw === '') return undefined;
  if (typeof raw === 'string') return { expr: raw };
  if (Array.isArray(raw)) {
    const items = raw
      .map((r, i) => normalizeRule(r, warnings, `${path}[${i}]`))
      .filter((r): r is VisibilityRule => r !== undefined);
    return items.length > 0 ? { allOf: items } : undefined;
  }
  if (!isObj(raw)) {
    warnings.push({ path, message: 'regla de visibilidad ilegible: se ignora (visible)', severity: 'warn' });
    return undefined;
  }
  const out: VisibilityRule = {};
  for (const w of checkKeys('visibleWhen', raw)) {
    warnings.push({
      path: `${path}.${w.key}`,
      message: LEGACY_RULE_KEYS.includes(w.key)
        ? `cláusula legada \`${w.key}\` sin equivalente: se descarta`
        : w.message,
      severity: w.severity,
    });
  }
  const permission = raw.permission;
  if (typeof permission === 'string' && permission !== '') out.permission = permission;
  else if (Array.isArray(permission)) {
    const perms = strArr(permission).map((p) => ({ permission: p }) as VisibilityRule);
    if (perms.length > 0) out.anyOf = [...(out.anyOf ?? []), ...perms];
  }
  const expr = asStr(raw.expr);
  if (expr) out.expr = expr;
  const field = asStr(raw.field);
  if (field) {
    out.field = field;
    if (Object.prototype.hasOwnProperty.call(raw, 'equals')) out.equals = raw.equals;
  }
  for (const key of ['allOf', 'anyOf'] as const) {
    if (Array.isArray(raw[key])) {
      const items = (raw[key] as unknown[])
        .map((r, i) => normalizeRule(r, warnings, `${path}.${key}[${i}]`))
        .filter((r): r is VisibilityRule => r !== undefined);
      if (items.length > 0) out[key] = [...(out[key] ?? []), ...items];
    }
  }
  if (raw.not !== undefined) {
    const not = normalizeRule(raw.not, warnings, `${path}.not`);
    if (not) out.not = not;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

// ---------------------------------------------------------------------------------------------
// Opciones, tipos y claves ajenas
// ---------------------------------------------------------------------------------------------

/** Clave tolerante de un valor: `true` y `"true"` (y `"True"`) son lo mismo. */
export function optionKey(value: unknown): string {
  return value === null || value === undefined ? '' : String(value).trim().toLowerCase();
}

/** Busca la opción de un valor con comparación tolerante (`true` casa con `"true"`). */
export function matchOption<T extends { value: string }>(
  options: readonly T[] | undefined,
  value: unknown,
): T | undefined {
  if (!options || value === undefined || value === null) return undefined;
  const wanted = optionKey(value);
  return options.find((o) => optionKey(o.value) === wanted);
}

function optionFrom(
  item: unknown,
  ctx: Ctx,
  path: string,
  scope: 'option' | 'enumValue',
  forcedValue?: string,
): OptionDef | undefined {
  if (typeof item === 'string' || typeof item === 'number' || typeof item === 'boolean') {
    const value = forcedValue ?? String(item);
    return { value, label: { literal: forcedValue !== undefined ? String(item) : value } };
  }
  if (!isObj(item)) {
    warn(ctx, path, 'opción ilegible: se omite');
    return undefined;
  }
  scan(ctx, scope, item, path);
  const rawValue = forcedValue ?? item.value ?? item.id ?? item.key;
  if (rawValue === undefined || rawValue === null) {
    warn(ctx, path, 'opción sin `value`: se omite');
    return undefined;
  }
  const value = String(rawValue);
  const label = textOf(item, ['label', 'text', 'name', 'header']) ?? { literal: value };
  const option: OptionDef = { value, label };
  const tone = toneOf(ctx, item, path);
  if (tone) option.tone = tone;
  const icon = asStr(item.icon);
  if (icon) option.icon = icon;
  return option;
}

/** `badgeMap` + `enumValues` + `options` -> opciones con valor en cadena (las últimas mandan). */
function buildOptions(raw: Obj, ctx: Ctx, path: string): OptionDef[] | undefined {
  const merged = new Map<string, OptionDef>();
  const put = (option: OptionDef | undefined) => {
    if (option) merged.set(optionKey(option.value), option);
  };
  if (Array.isArray(raw.options)) {
    raw.options.forEach((item, i) => put(optionFrom(item, ctx, `${path}.options[${i}]`, 'option')));
  }
  if (isObj(raw.badgeMap)) {
    for (const [value, entry] of Object.entries(raw.badgeMap)) {
      put(optionFrom(entry, ctx, `${path}.badgeMap.${value}`, 'enumValue', value));
    }
  }
  if (Array.isArray(raw.enumValues)) {
    raw.enumValues.forEach((item, i) =>
      put(optionFrom(item, ctx, `${path}.enumValues[${i}]`, 'enumValue')),
    );
  }
  return merged.size > 0 ? [...merged.values()] : undefined;
}

function simpleRecord(value: unknown): Record<string, string | number | boolean> | undefined {
  if (!isObj(value)) return undefined;
  const out: Record<string, string | number | boolean> = {};
  for (const [k, v] of Object.entries(value)) {
    if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') out[k] = v;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function buildLookup(raw: Obj): LookupDef | undefined {
  const nested = isObj(raw.lookup) ? raw.lookup : undefined;
  const entity = asStr(raw.lookupEntity) ?? asStr(nested?.entity);
  if (!entity) return undefined;
  const lookup: LookupDef = {
    entity,
    valueField: asStr(raw.lookupValueField) ?? asStr(nested?.valueField) ?? 'id',
    displayField: asStr(raw.lookupDisplayField) ?? asStr(nested?.displayField) ?? 'name',
  };
  const filter = simpleRecord(raw.lookupFilter) ?? simpleRecord(nested?.filter);
  if (filter) lookup.filter = filter;
  return lookup;
}

type FieldScope = 'column' | 'form' | 'detail' | 'child';

const KIND_BY_TYPE: Record<string, FieldKind> = {
  text: 'text',
  string: 'text',
  guid: 'text',
  textarea: 'textarea',
  number: 'number',
  int: 'number',
  integer: 'number',
  decimal: 'decimal',
  currency: 'money',
  money: 'money',
  percentage: 'percentage',
  percent: 'percentage',
  date: 'date',
  datetime: 'datetime',
  'date-time': 'datetime',
  boolean: 'boolean',
  bool: 'boolean',
  checkbox: 'boolean',
  switch: 'boolean',
  select: 'select',
  radio: 'select',
  'multi-select': 'multiselect',
  multiselect: 'multiselect',
  badge: 'badge',
  lookup: 'lookup',
  'guid-select': 'lookup',
  autocomplete: 'lookup',
  email: 'email',
  phone: 'phone',
  tel: 'phone',
  url: 'url',
  link: 'url',
};

/** Tipos que Angular conoce pero que React no tiene como tipo de campo: degradan a `text`. */
const DEGRADED_TYPES = new Set([
  'image',
  'icon',
  'progress',
  'trend',
  'rating',
  'sparkline',
  'array',
  'object',
  'tag',
  'avatar',
  'password',
  'file',
  'color',
  'rich-text',
  'richtext',
  'time',
  'json',
]);

function kindOf(
  type: unknown,
  scope: FieldScope,
  ctx: Ctx,
  path: string,
): FieldKind {
  const t = typeof type === 'string' ? type.trim().toLowerCase() : '';
  if (t === '') return 'text';
  if (t === 'enum') return scope === 'column' || scope === 'child' ? 'badge' : 'select';
  const kind = KIND_BY_TYPE[t];
  if (kind) return kind;
  if (DEGRADED_TYPES.has(t)) {
    warn(ctx, `${path}.type`, `tipo \`${t}\` sin equivalente en React: se usa \`text\``);
  } else {
    warn(ctx, `${path}.type`, `tipo \`${t}\` desconocido: se usa \`text\``);
  }
  return 'text';
}

function compilePattern(pattern: string | undefined, ctx: Ctx, path: string): string | undefined {
  if (!pattern) return undefined;
  try {
    new RegExp(pattern);
    return pattern;
  } catch {
    warn(ctx, `${path}.pattern`, `expresión regular inválida \`${pattern}\`: se ignora`);
    return undefined;
  }
}

function applyValidators(raw: Obj, def: FieldDefExt): void {
  for (const item of asArr(raw.validators)) {
    if (!isObj(item)) continue;
    const type = asStr(item.type);
    const value = item.value;
    switch (type) {
      case 'required':
        def.required = true;
        break;
      case 'min':
        if (def.min === undefined && asNum(value) !== undefined) def.min = asNum(value);
        break;
      case 'max':
        if (def.max === undefined && asNum(value) !== undefined) def.max = asNum(value);
        break;
      case 'minLength':
        if (def.minLength === undefined && asNum(value) !== undefined) def.minLength = asNum(value);
        break;
      case 'maxLength':
        if (def.maxLength === undefined && asNum(value) !== undefined) def.maxLength = asNum(value);
        break;
      case 'pattern':
        if (def.pattern === undefined && typeof value === 'string') def.pattern = value;
        break;
      default:
        break;
    }
  }
}

function buildField(
  raw: unknown,
  scope: FieldScope,
  ctx: Ctx,
  path: string,
): FieldDefExt | undefined {
  if (!isObj(raw)) {
    warn(ctx, path, 'campo ilegible: se omite');
    return undefined;
  }
  const scopeName =
    scope === 'column' ? 'column' : scope === 'form' ? 'formField' : scope === 'detail' ? 'detailField' : 'childColumn';
  scan(ctx, scopeName, raw, path);
  const field = asStr(raw.field) ?? asStr(raw.dataField);
  if (!field) {
    warn(ctx, path, 'campo sin `field`: se omite');
    return undefined;
  }
  if (raw.hidden === true) return undefined;

  const options = buildOptions(raw, ctx, path);
  const lookup = buildLookup(raw);
  let kind = kindOf(raw.type, scope, ctx, path);
  if (lookup && (kind === 'text' || kind === 'select' || kind === 'lookup')) {
    kind = 'lookup';
  } else if (kind === 'lookup' && !lookup) {
    kind = options && options.length > 0 ? 'select' : 'text';
    warn(ctx, `${path}.type`, `tipo \`${String(raw.type)}\` sin \`lookupEntity\`: se usa \`${kind}\``);
  }

  const editableFlag = asBool(raw.editable);
  const readonly = raw.readonly === true || raw.disabled === true;
  let editable: boolean;
  switch (scope) {
    case 'child':
      editable = false;
      break;
    case 'column':
      editable = editableFlag === true && !readonly; // se resuelve después con el formulario
      break;
    default:
      editable = editableFlag !== false && !readonly;
  }

  const label = textOf(raw, ['label', 'header']) ?? { literal: humanize(field) };
  const def: FieldDefExt = {
    field,
    label,
    kind,
    required: raw.required === true,
    editable,
  };
  if (raw.immutable === true || raw.readonlyOnEdit === true || raw.createOnly === true) {
    def.immutable = true;
  }
  const min = asNum(raw.min);
  const max = asNum(raw.max);
  const minLength = asNum(raw.minLength);
  const maxLength = asNum(raw.maxLength);
  if (min !== undefined) def.min = min;
  if (max !== undefined) def.max = max;
  if (minLength !== undefined) def.minLength = minLength;
  if (maxLength !== undefined) def.maxLength = maxLength;
  const pattern = compilePattern(asStr(raw.pattern), ctx, path);
  if (pattern) def.pattern = pattern;
  applyValidators(raw, def);
  if (options) def.options = options;
  if (lookup) def.lookup = lookup;
  const hint = textOf(raw, ['hint', 'tooltip']);
  if (hint) def.hint = hint;
  const placeholder = textOf(raw, ['placeholder']);
  if (placeholder) def.placeholder = placeholder;
  const width = asNum(raw.width);
  if (scope !== 'column' && scope !== 'child' && width !== undefined) {
    def.width = clamp(Math.round(width), 1, 12);
  }
  const format = asStr(raw.format);
  if (format) def.format = format;
  const emptyValue = asStr(raw.emptyValue);
  if (emptyValue !== undefined) def.emptyValue = emptyValue;
  const currency = asStr(raw.currency) ?? asStr(raw.currencyCode);
  if (currency) def.currency = currency;
  const trueLabel = textOf(raw, ['trueLabel']);
  if (trueLabel) def.trueLabel = trueLabel;
  const falseLabel = textOf(raw, ['falseLabel']);
  if (falseLabel) def.falseLabel = falseLabel;
  const visibleWhen = normalizeRule(raw.visibleWhen, ctx.warnings, `${path}.visibleWhen`);
  if (visibleWhen) def.visibleWhen = visibleWhen;
  const defaultValue = raw.defaultValue !== undefined ? raw.defaultValue : raw.default;
  if (defaultValue !== undefined) def.defaultValue = defaultValue;
  return def;
}

// ---------------------------------------------------------------------------------------------
// Columnas, secciones, filtros
// ---------------------------------------------------------------------------------------------

function buildColumns(
  rawColumns: unknown[],
  scope: 'column' | 'child',
  ctx: Ctx,
  path: string,
): ColumnDefExt[] {
  const out: ColumnDefExt[] = [];
  rawColumns.forEach((raw, i) => {
    const here = `${path}[${i}]`;
    if (isObj(raw) && asStr(raw.type)?.toLowerCase() === 'actions') {
      warn(ctx, `${here}.type`, 'columna `actions`: las acciones salen de `rowActions`', 'info');
      return;
    }
    const def = buildField(raw, scope, ctx, here);
    if (!def || !isObj(raw)) return;
    const column: ColumnDefExt = { ...def, sortable: scope === 'column' && raw.sortable === true };
    const widthPx = asNum(raw.width);
    if (widthPx !== undefined && widthPx > 0) column.widthPx = widthPx;
    if (raw.visible === false) column.hidden = true;
    const sticky = raw.sticky ?? raw.pinned;
    if (sticky === true || sticky === 'left' || sticky === 'start') column.pinned = 'start';
    else if (sticky === 'right' || sticky === 'end') column.pinned = 'end';
    if (raw.priority !== undefined && raw.priority !== null) {
      const priority = asNum(raw.priority);
      if (priority !== undefined && Number.isInteger(priority)) column.priority = priority;
      else warn(ctx, `${here}.priority`, 'prioridad de columna no entera: se ignora');
    }
    out.push(column);
  });
  return out;
}

function buildSections(
  rawSections: unknown,
  scope: 'form' | 'detail',
  ctx: Ctx,
  path: string,
): SectionDefExt[] {
  const out: SectionDefExt[] = [];
  asArr(rawSections).forEach((raw, i) => {
    const here = `${path}[${i}]`;
    if (!isObj(raw)) {
      warn(ctx, here, 'sección ilegible: se omite');
      return;
    }
    scan(ctx, 'section', raw, here);
    if (raw.apiEndpoint !== undefined) {
      warn(ctx, `${here}.apiEndpoint`, 'sección con endpoint propio no soportada: se usa el registro de la entidad');
    }
    const fields = asArr(raw.fields)
      .map((f, j) => buildField(f, scope, ctx, `${here}.fields[${j}]`))
      .filter((f): f is FieldDefExt => f !== undefined);
    const section: SectionDefExt = {
      id: asStr(raw.id) ?? `section-${i + 1}`,
      title: textOf(raw, ['title', 'label']) ?? { literal: '' },
      columns: clamp(Math.round(asNum(raw.columns) ?? 1), 1, 4),
      collapsible: raw.collapsible === true,
      collapsed: raw.collapsed === true,
      fields,
    };
    const icon = asStr(raw.icon);
    if (icon) section.icon = icon;
    const description = textOf(raw, ['description']);
    if (description) section.description = description;
    const visibleWhen = normalizeRule(raw.visibleWhen, ctx.warnings, `${here}.visibleWhen`);
    if (visibleWhen) section.visibleWhen = visibleWhen;
    out.push(section);
  });
  return out;
}

const FILTER_KINDS: Record<string, FilterDefExt['kind']> = {
  text: 'text',
  select: 'select',
  'multi-select': 'multiselect',
  multiselect: 'multiselect',
  date: 'date',
  number: 'number',
  boolean: 'boolean',
  checkbox: 'boolean',
  switch: 'boolean',
};

function buildFilters(rawFilters: unknown[], ctx: Ctx): FilterDefExt[] {
  const out: FilterDefExt[] = [];
  rawFilters.forEach((raw, i) => {
    const path = `filters[${i}]`;
    if (!isObj(raw)) {
      warn(ctx, path, 'filtro ilegible: se omite');
      return;
    }
    scan(ctx, 'filter', raw, path);
    const field = asStr(raw.field) ?? asStr(raw.serverField);
    if (!field) {
      warn(ctx, path, 'filtro sin `field`: se omite');
      return;
    }
    const options = buildOptions(raw, ctx, path);
    const type = (asStr(raw.type) ?? 'text').toLowerCase();
    let kind = FILTER_KINDS[type];
    if (!kind) {
      const degraded: FilterDefExt['kind'] | undefined =
        type === 'date-range'
          ? 'date'
          : type === 'number-range'
            ? 'number'
            : type === 'autocomplete'
              ? options
                ? 'select'
                : 'text'
              : undefined;
      if (degraded) {
        kind = degraded;
        warn(ctx, `${path}.type`, `tipo de filtro \`${type}\` degradado a \`${kind}\``);
      } else {
        kind = 'text';
        warn(ctx, `${path}.type`, `tipo de filtro \`${type}\` desconocido: se usa \`text\``);
      }
    }
    const quickSearch = field === '_search' || field === 'search';
    const filter: FilterDefExt = {
      field,
      serverField: asStr(raw.serverField) ?? (quickSearch ? 'search' : field),
      label: textOf(raw, ['label']) ?? { literal: humanize(field.replace(/^_/, '')) },
      kind,
      quickSearch,
    };
    if (options) filter.options = options;
    if (raw.defaultValue !== undefined) filter.defaultValue = raw.defaultValue;
    const hint = textOf(raw, ['hint']);
    if (hint) filter.hint = hint;
    const placeholder = textOf(raw, ['placeholder']);
    if (placeholder) filter.placeholder = placeholder;
    const width = asNum(raw.width);
    if (width !== undefined) filter.width = clamp(Math.round(width), 1, 12);
    const icon = asStr(raw.icon);
    if (icon) filter.icon = icon;
    const visibleWhen = normalizeRule(raw.visibleWhen, ctx.warnings, `${path}.visibleWhen`);
    if (visibleWhen) filter.visibleWhen = visibleWhen;
    out.push(filter);
  });
  return out;
}

// ---------------------------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------------------------

export interface ParsedEndpoint {
  endpoint: string;
  versioned: boolean;
}

/** `/products` -> con versión; `~/x`, `/api/x` y `https://…` -> sin versión; `/api/v1/x` -> `/x`. */
export function parseEndpoint(raw: string): ParsedEndpoint {
  let ep = raw.trim();
  if (/^https?:\/\//i.test(ep)) return { endpoint: ep, versioned: false };
  if (ep.startsWith('~/')) return { endpoint: ep.slice(1), versioned: false };
  if (!ep.startsWith('/')) ep = `/${ep}`;
  const versioned = /^\/api\/v\d+(\/.*)?$/.exec(ep);
  if (versioned) return { endpoint: versioned[1] ?? '/', versioned: true };
  const bare = /^\/api(\/.*)$/.exec(ep);
  if (bare) return { endpoint: bare[1]!, versioned: false };
  return { endpoint: ep, versioned: true };
}

function buildChildGrids(
  rawGrids: unknown[],
  parentVersioned: boolean,
  ctx: Ctx,
  path: string,
): ChildGridDef[] {
  const out: ChildGridDef[] = [];
  rawGrids.forEach((raw, i) => {
    const here = `${path}[${i}]`;
    if (!isObj(raw)) {
      warn(ctx, here, 'hijo ilegible: se omite');
      return;
    }
    scan(ctx, 'childGrid', raw, here);
    let apiEndpoint = asStr(raw.apiEndpoint);
    if (!apiEndpoint) {
      const inline = asStr(raw.dataField) ?? asStr(raw.field);
      const entityId = asStr(raw.entityId);
      if (inline && !entityId) {
        warn(ctx, here, 'hijo con datos en línea del registro padre no soportado: se descarta');
        return;
      }
      if (entityId) {
        apiEndpoint = `/${entityId}s`;
        warn(ctx, `${here}.entityId`, `hijo sin \`apiEndpoint\`: se usa \`${apiEndpoint}\``, 'info');
      } else {
        warn(ctx, here, 'hijo sin `apiEndpoint`: se descarta');
        return;
      }
    }
    const parsed = parseEndpoint(apiEndpoint);
    let endpoint = parsed.endpoint;
    const foreignKey = asStr(raw.foreignKey);
    if (foreignKey && !/\{id\}/.test(endpoint)) {
      endpoint += `${endpoint.includes('?') ? '&' : '?'}${foreignKey}={id}`;
    }
    // Un hijo de una entidad sin versión, o con ruta sin versión, se marca con `~` (sin versión).
    if (/^https?:\/\//i.test(endpoint)) {
      // absoluta: tal cual
    } else if (!parentVersioned || !parsed.versioned) {
      endpoint = `~${endpoint}`;
    }
    const columns = buildColumns(asArr(raw.columns), 'child', ctx, `${here}.columns`);
    const grid: ChildGridDef = {
      id: asStr(raw.id) ?? `child-${i + 1}`,
      title: textOf(raw, ['title', 'label']) ?? { literal: humanize(asStr(raw.id) ?? 'child') },
      endpoint,
      columns,
    };
    const emptyMessage = textOf(raw, ['emptyMessage']);
    if (emptyMessage) grid.emptyMessage = emptyMessage;
    out.push(grid);
  });
  return out;
}

// ---------------------------------------------------------------------------------------------
// Órdenes
// ---------------------------------------------------------------------------------------------

const DEFAULT_CONFIRM: Text = { literal: '¿Confirmar la operación?', key: 'errors:confirm.default' };

const BUILTIN: Record<string, { kind: 'view' | 'edit' | 'delete'; label: Text; icon: string; tone?: Tone; surfaces: CommandDef['surfaces'] }> = {
  view: {
    kind: 'view',
    label: { literal: 'Ver detalle', key: 'errors:command.view' },
    icon: 'view',
    surfaces: ['row'],
  },
  edit: {
    kind: 'edit',
    label: { literal: 'Editar', key: 'errors:command.edit' },
    icon: 'edit',
    surfaces: ['row', 'detail-header'],
  },
  delete: {
    kind: 'delete',
    label: { literal: 'Eliminar', key: 'errors:command.delete' },
    icon: 'delete',
    tone: 'error',
    surfaces: ['row', 'detail-header'],
  },
};

const SURFACES = new Set(['row', 'detail-header', 'toolbar']);

function confirmText(raw: Obj): Text | undefined {
  const confirm = raw.confirm;
  const message = textOf(raw, ['confirmMessage']);
  if (confirm === true) return message ?? DEFAULT_CONFIRM;
  if (typeof confirm === 'string' || isObj(confirm)) {
    return toText(confirm, raw.confirmKey) ?? message;
  }
  if (message && confirm !== false) return message;
  return undefined;
}

function buildExplicitCommand(raw: unknown, ctx: Ctx, path: string): CommandDef | undefined {
  if (!isObj(raw)) {
    warn(ctx, path, 'orden ilegible: se omite');
    return undefined;
  }
  scan(ctx, 'command', raw, path);
  const id = asStr(raw.id);
  if (!id) {
    warn(ctx, path, 'orden sin `id`: se omite');
    return undefined;
  }
  const builtin = BUILTIN[id];
  const endpointRaw = asStr(raw.endpoint);
  const method = (asStr(raw.method) ?? 'POST').toUpperCase();
  const surfaces = strArr(raw.surfaces).filter((s): s is CommandDef['surfaces'][number] =>
    SURFACES.has(s),
  );
  const cmd: CommandDef = {
    id,
    label: textOf(raw, ['label']) ?? builtin?.label ?? { literal: humanize(id) },
    surfaces: surfaces.length > 0 ? surfaces : (builtin?.surfaces ?? ['row', 'detail-header']),
    kind: builtin && !endpointRaw ? builtin.kind : 'http',
  };
  const icon = asStr(raw.icon) ?? builtin?.icon;
  if (icon) cmd.icon = icon;
  const tone = toneOf(ctx, raw, path) ?? builtin?.tone;
  if (tone) cmd.tone = tone;
  if (cmd.kind === 'http') {
    if (method === 'POST' || method === 'PUT' || method === 'DELETE') cmd.method = method;
    else {
      warn(ctx, `${path}.method`, `método \`${method}\` no soportado: se usa POST`);
      cmd.method = 'POST';
    }
    if (endpointRaw) cmd.endpoint = endpointRaw;
    cmd.sendVersion = raw.sendVersion === true;
  }
  const confirm = confirmText(raw);
  if (confirm) cmd.confirm = confirm;
  if (isObj(raw.prompt)) {
    scan(ctx, 'prompt', raw.prompt, `${path}.prompt`);
    const field = asStr(raw.prompt.field);
    if (field) {
      cmd.prompt = {
        field,
        label: textOf(raw.prompt, ['label']) ?? { literal: humanize(field) },
        required: raw.prompt.required === true,
      };
      const maxLength = asNum(raw.prompt.maxLength);
      if (maxLength !== undefined) cmd.prompt.maxLength = maxLength;
    } else {
      warn(ctx, `${path}.prompt`, '`prompt` sin `field`: se ignora');
    }
  }
  const success = textOf(raw, ['successMessage']);
  if (success) cmd.successMessage = success;
  const accepted = textOf(raw, ['acceptedMessage']);
  if (accepted) cmd.acceptedMessage = accepted;
  const visibleWhen = normalizeRule(raw.visibleWhen, ctx.warnings, `${path}.visibleWhen`);
  if (visibleWhen) cmd.visibleWhen = visibleWhen;
  return cmd;
}

function buildCommands(raw: Obj, editable: boolean, ctx: Ctx): CommandDef[] {
  const commands = new Map<string, CommandDef>();
  asArr(raw.commands).forEach((item, i) => {
    const cmd = buildExplicitCommand(item, ctx, `commands[${i}]`);
    if (cmd) commands.set(cmd.id, cmd);
  });

  const grid = isObj(raw.grid) ? raw.grid : {};
  asArr(grid.rowActions).forEach((item, i) => {
    const path = `grid.rowActions[${i}]`;
    if (!isObj(item)) {
      warn(ctx, path, 'acción ilegible: se omite');
      return;
    }
    scan(ctx, 'rowAction', item, path);
    const action = asStr(item.action);
    if (!action) {
      warn(ctx, path, 'acción sin `action`: se omite');
      return;
    }
    const existing = commands.get(action);
    if (existing) {
      if (!existing.surfaces.includes('row')) existing.surfaces = [...existing.surfaces, 'row'];
      return;
    }
    const builtin = BUILTIN[action];
    if (!builtin) {
      warn(ctx, `${path}.action`, `acción \`${action}\` sin orden registrada: se descarta`);
      return;
    }
    if (!editable && builtin.kind !== 'view') {
      warn(ctx, path, `acción \`${action}\` en una entidad de sólo lectura: se descarta`, 'info');
      return;
    }
    const cmd = buildExplicitCommand(
      {
        id: action,
        label: item.label,
        labelKey: item.labelKey,
        icon: item.icon,
        color: item.color,
        confirm: item.confirm,
        confirmMessage: item.confirmMessage,
        confirmMessageKey: item.confirmMessageKey,
        visibleWhen: item.visibleWhen,
      },
      { ...ctx, warnings: [] },
      path,
    );
    if (cmd) {
      // Se repite la normalización con el contexto real para conservar los avisos del color y la regla.
      toneOf(ctx, item, path);
      normalizeRule(item.visibleWhen, ctx.warnings, `${path}.visibleWhen`);
      commands.set(cmd.id, cmd);
    }
  });

  if (!editable) {
    for (const [id, cmd] of commands) {
      if (cmd.kind === 'edit' || cmd.kind === 'delete') commands.delete(id);
    }
  }
  return [...commands.values()];
}

// ---------------------------------------------------------------------------------------------
// Vistas y ayuda
// ---------------------------------------------------------------------------------------------

const MAP_LEVEL_IDS = new Set(['world', 'country', 'region', 'province', 'city']);

function buildMapLevels(raw: unknown[], ctx: Ctx, path: string): MapLevelDef[] {
  const levels: MapLevelDef[] = [];
  raw.forEach((item, i) => {
    const here = `${path}[${i}]`;
    if (!isObj(item)) {
      warn(ctx, here, 'nivel ilegible: se omite');
      return;
    }
    scan(ctx, 'mapLevel', item, here);
    const id = asStr(item.id);
    const source = asStr(item.source);
    if (!id || !MAP_LEVEL_IDS.has(id) || !source) {
      warn(ctx, here, 'nivel sin `id` válido o sin `source`: se omite');
      return;
    }
    levels.push({
      id: id as MapLevelDef['id'],
      label: textOf(item, ['label']) ?? { literal: humanize(id) },
      boundaryLevel: asStr(item.boundaryLevel) ?? id,
      codeProperty: asStr(item.codeProperty) ?? 'code',
      source,
      ...(item.defaultCode === null ? { defaultCode: null } : asStr(item.defaultCode) ? { defaultCode: asStr(item.defaultCode) } : {}),
    });
  });
  return levels;
}

function buildViews(raw: Obj, displayField: string, ctx: Ctx): { views: ViewDef[]; defaultView: 'grid' | 'tree' | 'map' } {
  const views: ViewDef[] = [{ kind: 'grid' }];

  for (const view of asArr(raw.availableViews)) {
    if (typeof view !== 'string') continue;
    if (view === 'grid' || view === 'detail') continue;
    // `tree` y `map` valen si `views[]` (o `hierarchy`, para `tree`) declara esa vista: es lo que la define.
    const declared =
      asArr(raw.views).some((v) => isObj(v) && v.kind === view) ||
      (view === 'tree' && isObj(raw.hierarchy) && raw.hierarchy.type === 'tree');
    if ((view === 'tree' || view === 'map') && declared) continue;
    warn(ctx, 'availableViews', `vista \`${view}\` no soportada: se ignora`);
  }

  asArr(raw.views).forEach((item, i) => {
    const path = `views[${i}]`;
    if (!isObj(item)) {
      warn(ctx, path, 'vista ilegible: se omite');
      return;
    }
    const kind = asStr(item.kind);
    if (kind === 'grid') return;
    if (kind === 'tree') {
      scan(ctx, 'viewTree', item, path);
      const parentField = asStr(item.parentField);
      if (!parentField) {
        warn(ctx, path, 'vista `tree` sin `parentField`: se omite');
        return;
      }
      const source = isObj(item.source) ? item.source : { type: 'flat' };
      const view: ViewDef = {
        kind: 'tree',
        parentField,
        labelField: asStr(item.labelField) ?? displayField,
        source: { type: 'flat' },
      };
      const subtitleField = asStr(item.subtitleField);
      if (subtitleField) view.subtitleField = subtitleField;
      const badgeField = asStr(item.badgeField);
      if (badgeField) view.badgeField = badgeField;
      if (source.type === 'chart') {
        const endpoint = asStr(source.endpoint);
        if (endpoint) {
          view.source = {
            type: 'chart',
            endpoint,
            rootsFrom: 'list',
            depth: clamp(Math.round(asNum(source.depth) ?? 3), 1, 20),
            childrenField: asStr(source.childrenField) ?? 'reports',
            nodeField: asStr(source.nodeField) ?? 'position',
          };
        } else {
          warn(ctx, `${path}.source`, 'fuente `chart` sin `endpoint`: se usa `flat`');
        }
      }
      views.push(view);
      return;
    }
    if (kind === 'map') {
      scan(ctx, 'viewMap', item, path);
      const boundaryField = asStr(item.boundaryField);
      const levels = buildMapLevels(asArr(item.levels), ctx, `${path}.levels`);
      if (!boundaryField || levels.length === 0) {
        warn(ctx, path, 'vista `map` sin `boundaryField` o sin niveles válidos: se omite');
        return;
      }
      const view: ViewDef = {
        kind: 'map',
        boundaryField,
        levels,
        aggregate: item.aggregate === 'sum' ? 'sum' : 'count',
        // Q-10: un nivel con una sola delimitación se salta salvo que la configuración diga `false`.
        skipSingle: asBool(item.skipSingle) ?? true,
      };
      const valueField = asStr(item.valueField);
      if (valueField) view.valueField = valueField;
      views.push(view);
      return;
    }
    warn(ctx, `${path}.kind`, `vista \`${String(kind)}\` desconocida: se omite`);
  });

  // `hierarchy` (declarada y muerta en Angular) -> organigrama si no se declaró uno.
  if (isObj(raw.hierarchy)) {
    scan(ctx, 'hierarchy', raw.hierarchy, 'hierarchy');
    const parentField = asStr(raw.hierarchy.parentField);
    if (raw.hierarchy.type === 'tree' && parentField && !views.some((v) => v.kind === 'tree')) {
      views.push({ kind: 'tree', parentField, labelField: displayField, source: { type: 'flat' } });
    }
  }

  const wanted = asStr(raw.defaultView);
  let defaultView: 'grid' | 'tree' | 'map' = 'grid';
  if (wanted === 'tree' || wanted === 'map') {
    if (views.some((v) => v.kind === wanted)) defaultView = wanted;
    else warn(ctx, 'defaultView', `\`defaultView: ${wanted}\` sin esa vista: se usa \`grid\``);
  } else if (wanted && wanted !== 'grid' && wanted !== 'detail') {
    warn(ctx, 'defaultView', `\`defaultView: ${wanted}\` no soportada: se usa \`grid\``, 'info');
  }
  return { views, defaultView };
}

function buildHelp(raw: unknown, ctx: Ctx): HelpStepDef[] {
  const list = Array.isArray(raw) ? raw : isObj(raw) ? asArr(raw.steps) : [];
  const out: HelpStepDef[] = [];
  list.forEach((item, i) => {
    const path = `help[${i}]`;
    if (!isObj(item)) {
      warn(ctx, path, 'paso de ayuda ilegible: se omite');
      return;
    }
    scan(ctx, 'helpStep', item, path);
    const target = asStr(item.target);
    if (!target) {
      warn(ctx, path, 'paso de ayuda sin `target`: se omite');
      return;
    }
    out.push({
      target,
      title: textOf(item, ['title']) ?? { literal: '' },
      body: textOf(item, ['body', 'description', 'text']) ?? { literal: '' },
    });
  });
  return out;
}

// ---------------------------------------------------------------------------------------------
// Herencia de pistas del formulario al detalle (M-8)
// ---------------------------------------------------------------------------------------------

/** Lo que un campo del detalle hereda del campo homónimo de `form` cuando no lo trae. */
const INHERITED_PROPS = ['hint', 'placeholder', 'maxLength', 'help'] as const;

/**
 * Las configuraciones de Go ponen `hint` sólo en `form`, y el detalle (lo que se ve al leer y al editar)
 * no las lleva. El detalle hereda `hint`, `placeholder`, `maxLength` y `help` del campo de `form` con el
 * mismo `field` si no trae los suyos, y avisa con `info`. (Ninguna clave del JSON da hoy `help` a un
 * campo: se hereda si el campo normalizado lo trae.)
 */
function inheritFromForm(detail: SectionDefExt[], form: SectionDefExt[], ctx: Ctx): void {
  const byField = new Map<string, FieldDefExt>();
  for (const section of form) for (const field of section.fields) if (!byField.has(field.field)) byField.set(field.field, field);
  detail.forEach((section, i) => {
    section.fields.forEach((field, j) => {
      const source = byField.get(field.field) as unknown as Record<string, unknown> | undefined;
      if (!source) return;
      const target = field as unknown as Record<string, unknown>;
      const inherited: string[] = [];
      for (const prop of INHERITED_PROPS) {
        const value = source[prop];
        if (value === undefined || target[prop] !== undefined) continue;
        target[prop] = typeof value === 'object' && value !== null ? structuredClone(value) : value;
        inherited.push(prop);
      }
      if (inherited.length > 0) {
        const list = inherited.map((prop) => `\`${prop}\``).join(', ');
        warn(ctx, `detail.sections[${i}].fields[${j}]`, `el campo \`${field.field}\` hereda ${list} de \`form\``, 'info');
      }
    });
  });
}

// ---------------------------------------------------------------------------------------------
// Entidad completa
// ---------------------------------------------------------------------------------------------

export interface NormalizeOptions {
  /** Identificador a usar si el JSON no lo trae (o el que manda el manifiesto). */
  id?: string;
}

function generateSectionFromColumns(columns: ColumnDefExt[], editable: boolean): SectionDefExt[] {
  const fields: FieldDefExt[] = columns
    .filter((c) => c.kind !== 'badge')
    .map((c) => {
      const {
        widthPx: _widthPx,
        sortable: _sortable,
        serverSortField: _serverSortField,
        pinned: _pinned,
        hidden: _hidden,
        priority: _priority,
        ...field
      } = c;
      return { ...field, editable: editable && c.editable };
    });
  if (fields.length === 0) return [];
  return [
    {
      id: 'main',
      title: { literal: '' },
      columns: 2,
      collapsible: false,
      collapsed: false,
      fields,
    },
  ];
}

/** Normaliza el JSON de una entidad (más su superposición) al modelo interno. */
export function normalizeEntityConfig(
  rawInput: unknown,
  overlay?: unknown,
  options: NormalizeOptions = {},
): EntityConfigExt {
  if (!isObj(rawInput)) {
    throw new ConfigError('La configuración de la entidad no es un objeto JSON');
  }
  let raw: Obj = overlay !== undefined ? (mergeOverlay(rawInput, overlay) as Obj) : rawInput;
  if (options.id !== undefined && asStr(raw.id) !== options.id) {
    raw = { ...raw, id: options.id };
  }

  const shape = validateEntityShape(raw);
  if (shape.fatal.length > 0) {
    throw new ConfigError(
      `Configuración de entidad inválida (${shape.fatal.map((f) => f.message).join('; ')})`,
      shape.fatal,
    );
  }
  const id = String(raw.id);
  const apiEndpointRaw = String(raw.apiEndpoint);
  const parsedEndpoint = parseEndpoint(apiEndpointRaw);
  const versioned = raw.apiVersioned === false ? false : parsedEndpoint.versioned;
  const ctx: Ctx = { warnings: [...shape.warnings], id, versioned };

  scan(ctx, 'root', raw, '');
  if (options.id !== undefined && options.id !== rawInput.id && rawInput.id !== undefined) {
    warn(ctx, 'id', `el \`id\` del manifiesto (\`${options.id}\`) manda sobre el del JSON (\`${String(rawInput.id)}\`)`, 'info');
  }

  // Raíz
  const editable = raw.editable !== false;
  const name = textOf(raw, ['name']) ?? { literal: humanize(id) };
  const namePlural = textOf(raw, ['namePlural']) ?? name;
  const description = textOf(raw, ['description']);
  const icon = asStr(raw.icon);
  const toneResult = toneFromColor(raw.color);
  let tone = toneResult.tone;
  if (!tone) {
    if (toneResult.ignored === 'hex') {
      warn(ctx, 'color', 'color hexadecimal ignorado: el tono sale de un hash estable del `id`', 'info');
    } else if (toneResult.ignored === 'unknown') {
      warn(ctx, 'color', `color \`${String(raw.color)}\` sin equivalente: tono por hash del \`id\``, 'info');
    }
    tone = toneFromHash(id);
  }

  // dataSource
  const rawDs = isObj(raw.dataSource) ? raw.dataSource : {};
  scan(ctx, 'dataSource', rawDs, 'dataSource');
  if (isObj(rawDs.serverFiltering)) scan(ctx, 'serverFiltering', rawDs.serverFiltering, 'dataSource.serverFiltering');
  if (isObj(rawDs.pairSearch)) scan(ctx, 'pairSearch', rawDs.pairSearch, 'dataSource.pairSearch');
  const dsType = asStr(rawDs.type);
  if (dsType && dsType !== 'single') {
    warn(ctx, 'dataSource.type', `\`${dsType}\` no soportado: se trata como \`single\``);
  }
  const listStyle: 'maintenance' | 'number-size' =
    rawDs.listStyle === 'number-size' || rawDs.listStyle === 'maintenance'
      ? (rawDs.listStyle as 'maintenance' | 'number-size')
      : versioned
        ? 'maintenance'
        : 'number-size';
  const listMethod: 'search' | 'get' =
    rawDs.listMethod === 'get' || rawDs.listMethod === 'search'
      ? (rawDs.listMethod as 'get' | 'search')
      : listStyle === 'number-size'
        ? 'get'
        : 'search';
  const serverFiltering = isObj(rawDs.serverFiltering) ? rawDs.serverFiltering : {};
  const companyFilterField = asStr(serverFiltering.companyFilterField);
  const baseFilter = isObj(rawDs.baseFilter) ? { ...rawDs.baseFilter } : {};

  // Rejilla
  const rawGrid = isObj(raw.grid) ? raw.grid : {};
  scan(ctx, 'grid', rawGrid, 'grid');
  const columns = buildColumns(asArr(rawGrid.columns), 'column', ctx, 'grid.columns');

  // Ordenación en servidor
  const sortMap: Record<string, string> = {};
  if (isObj(raw.serverSortFields)) {
    for (const [k, v] of Object.entries(raw.serverSortFields)) if (typeof v === 'string') sortMap[k] = v;
  } else if (Array.isArray(raw.serverSortFields)) {
    // Lista blanca: cada campo se ordena en el servidor con su propio nombre (así la escribe la demo).
    for (const field of strArr(raw.serverSortFields)) sortMap[field] = field;
  }
  if (isObj(rawDs.pairSearch) && isObj(rawDs.pairSearch.sortFields)) {
    for (const [k, v] of Object.entries(rawDs.pairSearch.sortFields)) {
      if (typeof v === 'string' && !(k in sortMap)) sortMap[k] = v;
    }
  }
  const whitelist = versioned ? knownServerSort(parsedEndpoint.endpoint) : [];
  const clientOnly: string[] = [];
  for (const column of columns) {
    if (!column.sortable) continue;
    const server = sortMap[column.field] ?? (whitelist.includes(column.field) ? column.field : undefined);
    if (server) column.serverSortField = server;
    else clientOnly.push(column.field);
  }
  if (clientOnly.length > 0) {
    warn(
      ctx,
      'grid.columns',
      `columnas ordenables sólo en cliente (el servidor no declara su orden): ${clientOnly.join(', ')}`,
      'info',
    );
  }

  // Formulario
  let form: EntityConfigExt['form'] = null;
  const rawForm = raw.form;
  if (isObj(rawForm)) {
    scan(ctx, 'form', rawForm, 'form');
    if (rawForm.sections !== undefined && rawForm.groups !== undefined) {
      warn(ctx, 'form.groups', '`sections` y `groups` a la vez: manda `sections`');
    }
    let sections = buildSections(rawForm.sections ?? rawForm.groups, 'form', ctx, rawForm.sections !== undefined ? 'form.sections' : 'form.groups');
    if (sections.length === 0 && rawForm.generated === true) {
      sections = generateSectionFromColumns(columns, true).map((s) => ({
        ...s,
        fields: s.fields.map((f) => ({ ...f, editable: true })),
      }));
    }
    form = {
      sections,
      create: editable && rawForm.create !== false && sections.length > 0,
      edit: editable && rawForm.edit !== false && sections.length > 0,
    };
    const createTitle = textOf(rawForm, ['createTitle']);
    if (createTitle) form.createTitle = createTitle;
    const editTitle = textOf(rawForm, ['editTitle']);
    if (editTitle) form.editTitle = editTitle;
  } else if (rawForm !== undefined && rawForm !== null) {
    warn(ctx, 'form', 'formulario ilegible: se ignora');
  }

  // Detalle
  const rawDetail = isObj(raw.detail) ? { ...raw.detail } : {};
  scan(ctx, 'detail', rawDetail, 'detail');
  let detailSections = buildSections(rawDetail.sections, 'detail', ctx, 'detail.sections');
  const childRaw: unknown[] = [...asArr(rawDetail.childGrids)];
  if (Array.isArray(rawDetail.tabs)) {
    rawDetail.tabs.forEach((tab, i) => {
      const path = `detail.tabs[${i}]`;
      if (!isObj(tab)) return;
      scan(ctx, 'tab', tab, path);
      if (tab.component !== undefined) {
        warn(ctx, `${path}.component`, 'componente a medida de una pestaña no soportado: se omite');
      }
      detailSections = [...detailSections, ...buildSections(tab.sections, 'detail', ctx, `${path}.sections`)];
      if (isObj(tab.childGrid)) childRaw.push(tab.childGrid);
    });
  }
  const childGrids = buildChildGrids(childRaw, versioned, ctx, 'detail.childGrids');
  let detailGenerated = false;
  if (detailSections.length === 0) {
    const fromForm = form?.sections.length
      ? form.sections.map((s) => ({ ...s, fields: s.fields.map((f) => ({ ...f })) }))
      : generateSectionFromColumns(columns, editable);
    if (fromForm.length > 0) {
      warn(ctx, 'detail.sections', 'detalle sin secciones: se genera con los campos del formulario o de las columnas', 'info');
      detailSections = fromForm;
      detailGenerated = true;
    }
  }

  if (form && form.sections.length > 0) inheritFromForm(detailSections, form.sections, ctx);

  // Edición de las columnas: sale del formulario (o del detalle) salvo que la columna lo diga.
  const formFields = new Map<string, FieldDefExt>();
  const detailFields = new Map<string, FieldDefExt>();
  for (const s of detailSections) for (const f of s.fields) detailFields.set(f.field, f);
  if (form?.edit) for (const s of form.sections) for (const f of s.fields) formFields.set(f.field, f);
  const rawColumns = asArr(rawGrid.columns);
  columns.forEach((column) => {
    const source = rawColumns.find(
      (c) => isObj(c) && (asStr(c.field) ?? asStr(c.dataField)) === column.field,
    );
    const explicit = isObj(source) ? asBool(source.editable) : undefined;
    if (!editable) {
      column.editable = false;
      return;
    }
    if (explicit !== undefined) {
      column.editable = explicit && !(isObj(source) && (source.readonly === true || source.disabled === true));
      return;
    }
    const def = formFields.get(column.field) ?? detailFields.get(column.field);
    column.editable = !!def && def.editable && !def.immutable && column.kind !== 'badge';
  });

  // Filtros
  const filters = buildFilters(asArr(raw.filters), ctx);

  // Órdenes, vistas, ayuda
  const commands = buildCommands(raw, editable, ctx);
  const displayField =
    asStr(raw.displayField) ??
    columns.find((c) => c.kind === 'text')?.field ??
    asStr(raw.primaryKey) ??
    'id';
  const { views, defaultView } = buildViews(raw, displayField, ctx);
  const help = buildHelp(raw.help, ctx);

  // editType -> saveMode
  const editType = asStr(raw.editType);
  let saveMode: EntityConfigExt['saveMode'] = 'optimistic';
  if (editType === 'party') {
    saveMode = 'form';
    warn(ctx, 'editType', '`party` (POST /parties/rename y /toggle) no se soporta: se guarda por formulario con PUT');
  } else if (editType && editType !== 'direct') {
    warn(ctx, 'editType', `\`${editType}\` desconocido: se usa \`direct\``);
  }
  const wantedSave = asStr(raw.saveMode);
  if (wantedSave === 'optimistic' || wantedSave === 'batch' || wantedSave === 'form') saveMode = wantedSave;
  else if (wantedSave) warn(ctx, 'saveMode', `\`${wantedSave}\` desconocido: se usa \`${saveMode}\``);

  const presentationRaw = asStr(raw.presentation);
  const presentation: 'panel' | 'page' = presentationRaw === 'page' ? 'page' : 'panel';
  if (presentationRaw && presentationRaw !== 'panel' && presentationRaw !== 'page') {
    warn(ctx, 'presentation', `\`${presentationRaw}\` desconocida: se usa \`panel\``);
  }

  const gridOptions: EntityConfigExt['grid'] = {
    pageSize: clamp(Math.round(asNum(rawGrid.pageSize) ?? 25), 1, 1000),
    pageSizeOptions: asArr(rawGrid.pageSizeOptions)
      .map(asNum)
      .filter((n): n is number => n !== undefined && n > 0 && n <= 1000)
      .map(Math.round),
    selectable: rawGrid.selectable !== false,
    selectionMode: rawGrid.selectionMode === 'single' ? 'single' : 'multiple',
  };
  if (gridOptions.pageSizeOptions.length === 0) gridOptions.pageSizeOptions = [10, 25, 50, 100];
  if (isObj(rawGrid.defaultSort)) {
    const field = asStr(rawGrid.defaultSort.field);
    if (field) {
      gridOptions.defaultSort = {
        field,
        direction: String(rawGrid.defaultSort.direction).toLowerCase() === 'desc' ? 'desc' : 'asc',
      };
    }
  }

  const config: EntityConfigExt = {
    id,
    name,
    namePlural,
    endpoint: parsedEndpoint.endpoint,
    apiVersioned: versioned,
    primaryKey: asStr(raw.primaryKey) ?? 'id',
    displayField,
    editable,
    saveMode,
    presentation,
    dataSource: {
      pagination: rawDs.paginationStrategy === 'server' ? 'server' : 'client',
      listStyle,
      listMethod,
      baseFilter,
    },
    grid: gridOptions,
    searchable: raw.searchable === true,
    searchFields: strArr(raw.searchFields),
    companyScoped: raw.companyScoped !== false,
    columns,
    filters,
    form,
    detail: { sections: detailSections, childGrids },
    views,
    defaultView,
    commands,
    help,
    warnings: ctx.warnings,
  };
  if (rawDs.paginationStrategy !== undefined && rawDs.paginationStrategy !== 'server' && rawDs.paginationStrategy !== 'client') {
    warn(ctx, 'dataSource.paginationStrategy', `\`${String(rawDs.paginationStrategy)}\` desconocida: se usa \`client\``);
  }
  if (description) config.description = description;
  if (icon) config.icon = icon;
  config.tone = tone;
  if (companyFilterField) config.dataSource.companyFilterField = companyFilterField;
  const aggregate = asStr(raw.aggregateEndpoint);
  if (aggregate) config.aggregateEndpoint = parseEndpoint(aggregate).endpoint;
  if (isObj(raw.permissions)) {
    const read = asStr(raw.permissions.read);
    const write = asStr(raw.permissions.write);
    if (read || write) config.permissions = { ...(read ? { read } : {}), ...(write ? { write } : {}) };
  }
  if (isObj(raw.emptyState) || asStr(rawGrid.emptyMessage) || isObj(rawGrid.emptyMessage)) {
    const es = isObj(raw.emptyState) ? raw.emptyState : {};
    if (isObj(raw.emptyState)) scan(ctx, 'emptyState', raw.emptyState, 'emptyState');
    const emptyState: NonNullable<EntityConfigExt['emptyState']> = {};
    const eIcon = asStr(es.icon);
    if (eIcon) emptyState.icon = eIcon;
    const title = textOf(es, ['title']);
    if (title) emptyState.title = title;
    const message = textOf(es, ['message']) ?? toText(rawGrid.emptyMessage, rawGrid.emptyMessageKey);
    if (message) emptyState.message = message;
    config.emptyState = emptyState;
  }
  // B-1: avisos de icono sobre el modelo ya normalizado (al final, para ver también lo heredado).
  // Un detalle generado repite los iconos del formulario o de las columnas: no se avisa dos veces.
  ctx.warnings.push(
    ...entityIconWarnings(config).filter((w) => !(detailGenerated && w.path.startsWith('detail.sections'))),
  );
  return config;
}

