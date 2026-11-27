// Combinaciones de teclas del registro de atajos (contracts/shortcuts.ts): análisis, coincidencia
// con un KeyboardEvent, texto para mostrar y detección de conflictos. Sin React.
import type { ShortcutDef } from '../contracts';

export type Platform = 'mac' | 'other';

export interface ParsedKeys {
  /** `Mod`: Ctrl (Cmd en Mac). */
  mod: boolean;
  ctrl: boolean;
  meta: boolean;
  alt: boolean;
  shift: boolean;
  /** Tecla en minúsculas (`k`, `?`, `escape`, `arrowdown`, `space`). */
  key: string;
}

const KEY_ALIASES: Record<string, string> = {
  esc: 'escape',
  del: 'delete',
  return: 'enter',
  up: 'arrowup',
  down: 'arrowdown',
  left: 'arrowleft',
  right: 'arrowright',
  plus: '+',
  ' ': 'space',
  spacebar: 'space',
  pgup: 'pageup',
  pgdn: 'pagedown',
  pagedn: 'pagedown',
};

export function detectPlatform(): Platform {
  if (typeof navigator === 'undefined') return 'other';
  const nav = navigator as Navigator & { userAgentData?: { platform?: string } };
  const source = nav.userAgentData?.platform ?? nav.platform ?? '';
  return /mac|iphone|ipad|ipod/i.test(source) ? 'mac' : 'other';
}

/** Analiza `Mod+Shift+Z`, `Shift+?`, `Escape`, `Mod++`… */
export function parseKeys(keys: string): ParsedKeys {
  const normalized = keys.trim().replace(/\+\+$/, '+Plus');
  const tokens = normalized
    .split('+')
    .map((token) => token.trim())
    .filter(Boolean);
  const parsed: ParsedKeys = { mod: false, ctrl: false, meta: false, alt: false, shift: false, key: '' };
  for (const token of tokens) {
    const lower = token.toLowerCase();
    if (lower === 'mod') parsed.mod = true;
    else if (lower === 'ctrl' || lower === 'control') parsed.ctrl = true;
    else if (lower === 'meta' || lower === 'cmd' || lower === 'command') parsed.meta = true;
    else if (lower === 'alt' || lower === 'option') parsed.alt = true;
    else if (lower === 'shift') parsed.shift = true;
    else parsed.key = KEY_ALIASES[lower] ?? lower;
  }
  return parsed;
}

/** Un símbolo (`?`, `/`, `.`): el Shift que lo produce depende de la distribución del teclado. */
function isSymbolKey(key: string): boolean {
  return key.length === 1 && !/[a-z0-9]/i.test(key);
}

/** Forma canónica según la plataforma (`Mod` ya resuelto): sirve para comparar combinaciones. */
export function canonicalKeys(keys: string, platform: Platform): string {
  const parsed = parseKeys(keys);
  const ctrl = parsed.ctrl || (parsed.mod && platform !== 'mac');
  const meta = parsed.meta || (parsed.mod && platform === 'mac');
  return [
    ctrl ? 'ctrl' : '',
    meta ? 'meta' : '',
    parsed.alt ? 'alt' : '',
    parsed.shift && !isSymbolKey(parsed.key) ? 'shift' : '',
    parsed.key,
  ]
    .filter(Boolean)
    .join('+');
}

function eventKey(event: KeyboardEvent): string {
  const key = event.key.toLowerCase();
  return KEY_ALIASES[key] ?? key;
}

/** ¿Corresponde el evento a la combinación? */
export function matchesEvent(parsed: ParsedKeys, event: KeyboardEvent, platform: Platform): boolean {
  if (!parsed.key) return false;
  const wantCtrl = parsed.ctrl || (parsed.mod && platform !== 'mac');
  const wantMeta = parsed.meta || (parsed.mod && platform === 'mac');
  if (event.ctrlKey !== wantCtrl || event.metaKey !== wantMeta || event.altKey !== parsed.alt) return false;
  if (!isSymbolKey(parsed.key) && event.shiftKey !== parsed.shift) return false;
  if (eventKey(event) === parsed.key) return true;
  // Con Alt (Option en Mac) la tecla escrita cambia: se mira la tecla física de letras y cifras.
  if (/^[a-z]$/.test(parsed.key)) return event.code === `Key${parsed.key.toUpperCase()}`;
  if (/^[0-9]$/.test(parsed.key)) return event.code === `Digit${parsed.key}`;
  return false;
}

const NAMED_DISPLAY: Record<string, string> = {
  escape: 'Esc',
  enter: 'Enter',
  tab: 'Tab',
  space: 'Space',
  delete: 'Del',
  backspace: '⌫',
  arrowup: '↑',
  arrowdown: '↓',
  arrowleft: '←',
  arrowright: '→',
  pageup: 'PgUp',
  pagedown: 'PgDn',
  home: 'Home',
  end: 'End',
};

/** Piezas para mostrar: `['Ctrl', 'Shift', 'Z']` o `['⌘', '⇧', 'Z']` en Mac. */
export function displayKeys(keys: string, platform: Platform = detectPlatform()): string[] {
  const parsed = parseKeys(keys);
  const mac = platform === 'mac';
  const parts: string[] = [];
  if (parsed.ctrl || (parsed.mod && !mac)) parts.push(mac ? '⌃' : 'Ctrl');
  if (parsed.meta || (parsed.mod && mac)) parts.push(mac ? '⌘' : 'Meta');
  if (parsed.alt) parts.push(mac ? '⌥' : 'Alt');
  if (parsed.shift) parts.push(mac ? '⇧' : 'Shift');
  if (parsed.key) {
    parts.push(NAMED_DISPLAY[parsed.key] ?? (parsed.key.length === 1 ? parsed.key.toUpperCase() : parsed.key));
  }
  return parts;
}

export interface ShortcutConflict {
  /** `duplicate`: misma combinación y ámbito; `shadow`: un ámbito tapa a un atajo global. */
  kind: 'duplicate' | 'shadow';
  keys: string;
  scope: ShortcutDef['scope'];
  ids: string[];
}

/** Conflictos entre atajos registrados (misma combinación en el mismo ámbito, o que tape a un global). */
export function findShortcutConflicts(
  defs: readonly ShortcutDef[],
  platform: Platform = detectPlatform(),
): ShortcutConflict[] {
  const conflicts: ShortcutConflict[] = [];
  const byScope = new Map<string, ShortcutDef[]>();
  for (const def of defs) {
    const key = `${def.scope}|${canonicalKeys(def.keys, platform)}`;
    byScope.set(key, [...(byScope.get(key) ?? []), def]);
  }
  for (const group of byScope.values()) {
    const [first] = group;
    if (first && group.length > 1) {
      conflicts.push({ kind: 'duplicate', keys: first.keys, scope: first.scope, ids: group.map((d) => d.id) });
    }
  }
  const globals = defs.filter((def) => def.scope === 'global');
  for (const def of defs) {
    if (def.scope === 'global') continue;
    const canonical = canonicalKeys(def.keys, platform);
    const shadowed = globals.filter((g) => canonicalKeys(g.keys, platform) === canonical);
    if (shadowed.length > 0) {
      conflicts.push({
        kind: 'shadow',
        keys: def.keys,
        scope: def.scope,
        ids: [def.id, ...shadowed.map((g) => g.id)],
      });
    }
  }
  return conflicts;
}
