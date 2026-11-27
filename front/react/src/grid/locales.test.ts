// Textos de la cuadrícula: mismas claves en es y en, todas las que usa el código existen, y cada ancla de
// ayuda (`data-kx-help="grid.<nombre>"`) tiene título y cuerpo.
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import enGrid from '../locales/en/grid.json';
import esGrid from '../locales/es/grid.json';
import { GRID_SHORTCUTS } from './shortcuts';

function flatten(node: unknown, prefix = ''): Record<string, string> {
  if (node === null || typeof node !== 'object') return { [prefix]: String(node) };
  return Object.entries(node).reduce<Record<string, string>>(
    (acc, [key, child]) => ({ ...acc, ...flatten(child, prefix ? `${prefix}.${key}` : key) }),
    {},
  );
}

const placeholders = (text: string) => [...text.matchAll(/{{\s*(\w+)\s*}}/g)].map((m) => m[1] as string).sort();
const es = flatten(esGrid);
const en = flatten(enGrid);

describe('grid.json', () => {
  it('es y en tienen exactamente las mismas claves', () => {
    expect(Object.keys(en).sort()).toEqual(Object.keys(es).sort());
  });

  it('ninguna cadena está vacía y las variables coinciden entre idiomas', () => {
    for (const [key, text] of Object.entries(es)) {
      expect(text.trim(), `es ${key}`).not.toBe('');
      expect((en[key] ?? '').trim(), `en ${key}`).not.toBe('');
      expect(placeholders(en[key] ?? ''), key).toEqual(placeholders(text));
    }
  });

  it('todas las claves que usa el código existen (con su forma plural si la tiene)', () => {
    const dir = resolve(import.meta.dirname);
    const files = readdirSync(dir).filter((name) => /\.(ts|tsx)$/.test(name) && !name.includes('.test.') && name !== 'test-utils.tsx');
    const used = new Set<string>();
    for (const file of files) {
      const source = readFileSync(resolve(dir, file), 'utf8');
      for (const match of source.matchAll(/\bt\(\s*'([A-Za-z0-9_.-]+)'/g)) used.add(match[1] as string);
    }
    expect(used.size).toBeGreaterThan(50);
    for (const key of used) {
      const present = key in es || `${key}_one` in es || `${key}_other` in es;
      expect(present, `falta la clave grid:${key}`).toBe(true);
    }
    // las que se componen en tiempo de ejecución
    for (const kind of ['edit', 'clear', 'paste', 'fill', 'revert', 'revertRow', 'revertAll']) expect(es[`history.${kind}`], kind).toBeTruthy();
  });

  it('cada ancla de ayuda y cada atajo tienen su texto', () => {
    for (const anchor of ['toolbar', 'search', 'filters', 'columns', 'table', 'row-actions', 'batch-bar', 'undo']) {
      expect(es[`help.${anchor}.title`], anchor).toBeTruthy();
      expect(es[`help.${anchor}.body`], anchor).toBeTruthy();
    }
    // Texto de reserva de una ancla oculta (D-R36): la barra de lote se explica sobre la barra de la lista.
    expect(es['help.batch-bar.idle.title']).toBeTruthy();
    expect(es['help.batch-bar.idle.body']).toBeTruthy();
    for (const shortcut of GRID_SHORTCUTS) {
      expect(shortcut.labelKey.startsWith('grid:shortcuts.')).toBe(true);
      expect(es[shortcut.labelKey.replace('grid:', '')], shortcut.id).toBeTruthy();
    }
  });
});
