// Reglas de visibilidad (`visibleWhen`) y comparación de permisos.
// `permission` es fail-closed (sin sesión o sin permiso, oculto); el resto, fail-open: lo que no se
// puede evaluar (expresión ilegible, campo sin datos) deja visible, como Angular.
import type { Session, VisibilityRule } from '../contracts';

export interface RuleContext {
  session?: Pick<Session, 'can'>;
  row?: Record<string, unknown>;
  form?: Record<string, unknown>;
}

/**
 * ¿Un permiso concedido (con comodines) cubre el exigido? Se compara por segmentos
 * (`Contexto.Recurso.Acción`); `*` cubre un segmento y, al final, el resto.
 */
export function permissionMatches(granted: string, required: string): boolean {
  const g = granted.split('.');
  const r = required.split('.');
  for (let i = 0; i < g.length; i += 1) {
    const gs = g[i]!;
    if (gs === '*') {
      if (i >= r.length) return false;
      if (i === g.length - 1) return true;
      continue;
    }
    if (i >= r.length) return false;
    if (gs.toLowerCase() !== r[i]!.toLowerCase()) return false;
  }
  return g.length === r.length;
}

export function hasPermission(granted: readonly string[], required: string): boolean {
  return granted.some((permission) => permissionMatches(permission, required));
}

type Tri = boolean | 'unknown';

function readPath(
  source: Record<string, unknown> | undefined,
  path: string,
): { found: boolean; value?: unknown } {
  if (!source) return { found: false };
  let current: unknown = source;
  for (const part of path.split('.')) {
    if (typeof current !== 'object' || current === null || !(part in current)) {
      return { found: false };
    }
    current = (current as Record<string, unknown>)[part];
  }
  return { found: true, value: current };
}

function readField(ctx: RuleContext, field: string): { found: boolean; value?: unknown } {
  const fromForm = readPath(ctx.form, field);
  if (fromForm.found) return fromForm;
  return readPath(ctx.row, field);
}

function norm(value: unknown): string {
  if (value === null || value === undefined) return '';
  return String(value).trim().toLowerCase();
}

/** Igualdad tolerante: `true` == "true", 1 == "1", mayúsculas indiferentes. */
export function looseEquals(a: unknown, b: unknown): boolean {
  return norm(a) === norm(b);
}

function parseLiteral(token: string): unknown {
  const t = token.trim();
  if ((t.startsWith("'") && t.endsWith("'")) || (t.startsWith('"') && t.endsWith('"'))) {
    return t.slice(1, -1);
  }
  if (t === 'null') return null;
  return t;
}

const EXPR = /^\s*([A-Za-z_$][\w$.]*)\s*(==|!=)\s*(.+?)\s*$/;

function evalExpr(expr: string, ctx: RuleContext): Tri {
  const m = EXPR.exec(expr);
  if (!m) return 'unknown';
  const [, field, op, rawValue] = m as unknown as [string, string, string, string];
  const read = readField(ctx, field);
  if (!read.found) return 'unknown';
  const equal = looseEquals(read.value, parseLiteral(rawValue));
  return op === '==' ? equal : !equal;
}

function evalRule(rule: VisibilityRule, ctx: RuleContext): Tri {
  const results: Tri[] = [];

  if (rule.permission !== undefined) {
    const granted = ctx.session?.can(rule.permission) === true;
    if (!granted) return false; // fail-closed
  }
  if (rule.field !== undefined) {
    const read = readField(ctx, rule.field);
    if (!read.found) results.push('unknown');
    else if (Object.prototype.hasOwnProperty.call(rule, 'equals')) {
      results.push(looseEquals(read.value, rule.equals));
    } else {
      const v = read.value;
      results.push(!(v === null || v === undefined || v === '' || v === false || v === 0));
    }
  }
  if (rule.expr !== undefined) results.push(evalExpr(rule.expr, ctx));
  if (rule.allOf) {
    const all = rule.allOf.map((r) => evalRule(r, ctx));
    if (all.includes(false)) return false;
    results.push(all.includes('unknown') ? 'unknown' : true);
  }
  if (rule.anyOf && rule.anyOf.length > 0) {
    const any = rule.anyOf.map((r) => evalRule(r, ctx));
    if (any.includes(true)) results.push(true);
    else results.push(any.includes('unknown') ? 'unknown' : false);
  }
  if (rule.not) {
    const inner = evalRule(rule.not, ctx);
    results.push(inner === 'unknown' ? 'unknown' : !inner);
  }

  if (results.includes(false)) return false;
  if (results.includes('unknown')) return 'unknown';
  return true;
}

/** Evalúa una regla de visibilidad. Sin regla: visible. Lo ilegible: visible (salvo `permission`). */
export function evaluateRule(rule: VisibilityRule | undefined, ctx: RuleContext = {}): boolean {
  if (!rule) return true;
  return evalRule(rule, ctx) !== false;
}
