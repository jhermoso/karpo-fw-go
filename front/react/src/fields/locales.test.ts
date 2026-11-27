import { describe, expect, it } from 'vitest';
import enFields from '../locales/en/fields.json';
import enUi from '../locales/en/ui.json';
import esFields from '../locales/es/fields.json';
import esUi from '../locales/es/ui.json';

function keysOf(node: unknown, prefix = ''): string[] {
  if (node === null || typeof node !== 'object') return [prefix];
  return Object.entries(node).flatMap(([key, child]) => keysOf(child, prefix ? `${prefix}.${key}` : key));
}

function placeholders(text: string): string[] {
  return [...text.matchAll(/{{\s*(\w+)\s*}}/g)].map((match) => match[1] as string).sort();
}

function flatten(node: unknown, prefix = ''): Record<string, string> {
  if (node === null || typeof node !== 'object') return { [prefix]: String(node) };
  return Object.entries(node).reduce<Record<string, string>>(
    (acc, [key, child]) => ({ ...acc, ...flatten(child, prefix ? `${prefix}.${key}` : key) }),
    {},
  );
}

describe.each([
  ['fields', esFields, enFields],
  ['ui', esUi, enUi],
])('locales %s', (_name, es, en) => {
  it('es y en tienen exactamente las mismas claves', () => {
    expect(keysOf(en).sort()).toEqual(keysOf(es).sort());
  });

  it('ninguna cadena está vacía y las variables coinciden entre idiomas', () => {
    const esFlat = flatten(es);
    const enFlat = flatten(en);
    for (const [key, text] of Object.entries(esFlat)) {
      expect(text.trim(), `es ${key}`).not.toBe('');
      expect((enFlat[key] ?? '').trim(), `en ${key}`).not.toBe('');
      expect(placeholders(enFlat[key] ?? ''), key).toEqual(placeholders(text));
    }
  });
});

describe('mensajes de validación', () => {
  it('hay un texto por cada código de validate', () => {
    for (const code of ['required', 'maxLength', 'minLength', 'min', 'max', 'pattern', 'format']) {
      expect(esFields.errors).toHaveProperty(code);
      expect(enFields.errors).toHaveProperty(code);
    }
  });
});
