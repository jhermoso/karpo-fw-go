import { describe, expect, it } from 'vitest';
import type { ShortcutDef } from '../contracts';
import {
  canonicalKeys,
  detectPlatform,
  displayKeys,
  findShortcutConflicts,
  matchesEvent,
  parseKeys,
} from './shortcut-keys';

function event(init: Partial<KeyboardEvent> & { key: string }): KeyboardEvent {
  return { code: '', ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...init } as KeyboardEvent;
}

const def = (id: string, scope: ShortcutDef['scope'], keys: string): ShortcutDef => ({
  id,
  scope,
  keys,
  labelKey: id,
  run: () => undefined,
});

describe('combinaciones de teclas', () => {
  it('analiza modificadores, alias y la tecla +', () => {
    expect(parseKeys('Mod+Shift+Z')).toEqual({ mod: true, ctrl: false, meta: false, alt: false, shift: true, key: 'z' });
    expect(parseKeys('Ctrl+Alt+Del').key).toBe('delete');
    expect(parseKeys('Cmd+Option+Up')).toMatchObject({ meta: true, alt: true, key: 'arrowup' });
    expect(parseKeys('Mod++')).toMatchObject({ mod: true, key: '+' });
    expect(parseKeys('Esc').key).toBe('escape');
    expect(parseKeys('Shift+?').key).toBe('?');
    expect(parseKeys('PgDn').key).toBe('pagedown');
  });

  it("canonicalKeys resuelve Mod según la plataforma e ignora Shift en símbolos", () => {
    expect(canonicalKeys('Mod+K', 'other')).toBe('ctrl+k');
    expect(canonicalKeys('Mod+K', 'mac')).toBe('meta+k');
    expect(canonicalKeys('Ctrl+K', 'other')).toBe(canonicalKeys('Mod+K', 'other'));
    expect(canonicalKeys('Shift+?', 'other')).toBe('?');
    expect(canonicalKeys('Shift+Z', 'other')).toBe('shift+z');
  });

  it('matchesEvent exige los mismos modificadores', () => {
    const palette = parseKeys('Mod+K');
    expect(matchesEvent(palette, event({ key: 'k', ctrlKey: true }), 'other')).toBe(true);
    expect(matchesEvent(palette, event({ key: 'K', ctrlKey: true }), 'other')).toBe(true); // con bloqueo de mayúsculas
    expect(matchesEvent(palette, event({ key: 'K', ctrlKey: true, shiftKey: true }), 'other')).toBe(false);
    expect(matchesEvent(palette, event({ key: 'k' }), 'other')).toBe(false);
    expect(matchesEvent(palette, event({ key: 'k', metaKey: true }), 'other')).toBe(false);
    expect(matchesEvent(palette, event({ key: 'k', metaKey: true }), 'mac')).toBe(true);
    expect(matchesEvent(palette, event({ key: 'k', ctrlKey: true, altKey: true }), 'other')).toBe(false);
    expect(matchesEvent(parseKeys(''), event({ key: 'k' }), 'other')).toBe(false);
  });

  it('en símbolos Shift no cuenta y con Alt se mira la tecla física', () => {
    expect(matchesEvent(parseKeys('Shift+?'), event({ key: '?' }), 'other')).toBe(true);
    expect(matchesEvent(parseKeys('Shift+?'), event({ key: '?', shiftKey: true }), 'other')).toBe(true);
    expect(matchesEvent(parseKeys('/'), event({ key: '/', shiftKey: true }), 'other')).toBe(true);
    expect(matchesEvent(parseKeys('Alt+K'), event({ key: '˚', altKey: true, code: 'KeyK' }), 'other')).toBe(true);
    expect(matchesEvent(parseKeys('Alt+1'), event({ key: '¡', altKey: true, code: 'Digit1' }), 'other')).toBe(true);
    expect(matchesEvent(parseKeys('Alt+K'), event({ key: 'j', altKey: true, code: 'KeyJ' }), 'other')).toBe(false);
    expect(matchesEvent(parseKeys('Alt+ArrowDown'), event({ key: 'ArrowDown', altKey: true }), 'other')).toBe(true);
    expect(matchesEvent(parseKeys('Space'), event({ key: ' ' }), 'other')).toBe(true);
  });

  it('displayKeys pinta por plataforma', () => {
    expect(displayKeys('Mod+Shift+Z', 'other')).toEqual(['Ctrl', 'Shift', 'Z']);
    expect(displayKeys('Mod+Shift+Z', 'mac')).toEqual(['⌘', '⇧', 'Z']);
    expect(displayKeys('Ctrl+Alt+Delete', 'other')).toEqual(['Ctrl', 'Alt', 'Del']);
    expect(displayKeys('Ctrl+Alt+ArrowUp', 'mac')).toEqual(['⌃', '⌥', '↑']);
    expect(displayKeys('Meta+F2', 'other')).toEqual(['Meta', 'f2']);
    expect(displayKeys('Escape')).toEqual(['Esc']);
    expect(['mac', 'other']).toContain(detectPlatform());
  });

  it('findShortcutConflicts: duplicados y sombras', () => {
    const defs = [
      def('a', 'global', 'Mod+K'),
      def('b', 'global', 'Ctrl+K'),
      def('c', 'grid', 'Mod+K'),
      def('d', 'grid', 'F2'),
      def('e', 'tree', 'F2'),
    ];
    expect(findShortcutConflicts(defs, 'other')).toEqual([
      { kind: 'duplicate', keys: 'Mod+K', scope: 'global', ids: ['a', 'b'] },
      { kind: 'shadow', keys: 'Mod+K', scope: 'grid', ids: ['c', 'a', 'b'] },
    ]);
    // En Mac, Ctrl+K y Mod+K son combinaciones distintas.
    expect(findShortcutConflicts(defs, 'mac')).toEqual([
      { kind: 'shadow', keys: 'Mod+K', scope: 'grid', ids: ['c', 'a'] },
    ]);
    expect(findShortcutConflicts([])).toEqual([]);
  });
});
