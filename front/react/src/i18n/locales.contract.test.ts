// Contrato de i18n (PLAN §6): recorre TODO src/locales y falla si es y en no tienen exactamente las
// mismas claves o si falta la pareja de un fichero. Lo escriben todas las tareas (un fichero por idioma
// y espacio): esta prueba es la que les impide dejar un texto sin traducir.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { compareLocales, flattenLocale, placeholders, type LocaleFiles } from './localeParity';

const LOCALES_DIR = path.resolve(__dirname, '../locales');

function loadLocaleFiles(): LocaleFiles {
  const files: LocaleFiles = {};
  for (const lng of readdirSync(LOCALES_DIR)) {
    const dir = path.join(LOCALES_DIR, lng);
    if (!statSync(dir).isDirectory()) continue;
    files[lng] = {};
    for (const file of readdirSync(dir)) {
      if (!file.endsWith('.json')) continue;
      files[lng][file.replace(/\.json$/, '')] = JSON.parse(readFileSync(path.join(dir, file), 'utf8'));
    }
  }
  return files;
}

describe('src/locales (todo el árbol)', () => {
  const files = loadLocaleFiles();

  it('existen es (referencia) y en, y tienen al menos el espacio help de T2', () => {
    expect(Object.keys(files)).toEqual(expect.arrayContaining(['es', 'en']));
    expect(Object.keys(files['es'] ?? {})).toContain('help');
    expect(Object.keys(files['en'] ?? {})).toContain('help');
  });

  it('es y en tienen los mismos ficheros y exactamente las mismas claves, sin textos vacíos', () => {
    const issues = compareLocales(files);
    expect(issues.map((i) => `${i.file}: ${i.detail}`)).toEqual([]);
  });

  it('el árabe (D-R38) tiene los 10 espacios, todas las claves de es (con las categorías plurales propias) y los mismos marcadores', () => {
    expect(Object.keys(files['ar'] ?? {}).sort()).toEqual(Object.keys(files['es'] ?? {}).sort());
    expect(Object.keys(files['ar'] ?? {})).toHaveLength(10);
    const issues = compareLocales(files, 'es', ['es', 'en', 'ar']);
    expect(issues.map((i) => `${i.file}: ${i.detail}`)).toEqual([]);
  });

  it('cada clave plural del árabe trae las categorías one, two, few, many y other', () => {
    const ar = flattenLocale(files['ar'] ?? {});
    const stems = new Set<string>();
    for (const [ns, content] of Object.entries(files['es'] ?? {})) {
      for (const key of flattenLocale(content).keys()) if (key.endsWith('_other')) stems.add(`${ns}:${key.slice(0, -'_other'.length)}`);
    }
    expect(stems.size).toBeGreaterThan(20);
    const missing: string[] = [];
    for (const stem of stems) {
      const [ns = '', key = ''] = stem.split(':');
      const own = flattenLocale((files['ar'] ?? {})[ns] ?? {});
      for (const category of ['one', 'two', 'few', 'many', 'other']) {
        if (!own.has(`${key}_${category}`)) missing.push(`ar/${ns}.json: ${key}_${category}`);
      }
    }
    expect(missing).toEqual([]);
    expect(ar.size).toBeGreaterThan(500);
  });

  it('cada fichero es un objeto JSON con claves de texto (sin números ni null sueltos)', () => {
    for (const [lng, namespaces] of Object.entries(files)) {
      for (const [ns, content] of Object.entries(namespaces)) {
        expect(typeof content, `${lng}/${ns}`).toBe('object');
        for (const [key, text] of flattenLocale(content)) {
          expect(typeof text, `${lng}/${ns}:${key}`).toBe('string');
        }
      }
    }
  });
});

describe('compareLocales detecta lo que debe', () => {
  const base: LocaleFiles = {
    es: { grid: { title: 'Cuadrícula', actions: { save: 'Guardar {{count}}' } } },
    en: { grid: { title: 'Grid', actions: { save: 'Save {{count}}' } } },
  };

  it('pasa con ficheros iguales', () => {
    expect(compareLocales(base)).toEqual([]);
  });

  it('falla si se añade una clave sólo en es', () => {
    const files: LocaleFiles = {
      ...base,
      es: { grid: { title: 'Cuadrícula', actions: { save: 'Guardar {{count}}', undo: 'Deshacer' } } },
    };
    const issues = compareLocales(files);
    expect(issues).toEqual([
      expect.objectContaining({ file: 'en/grid.json', kind: 'missing-key', key: 'actions.undo' }),
    ]);
  });

  it('falla si se añade una clave sólo en en', () => {
    const files: LocaleFiles = {
      ...base,
      en: { grid: { title: 'Grid', actions: { save: 'Save {{count}}', undo: 'Undo' } } },
    };
    expect(compareLocales(files)).toEqual([
      expect.objectContaining({ file: 'en/grid.json', kind: 'extra-key', key: 'actions.undo' }),
    ]);
  });

  it('falla si falta la pareja de un fichero (en cualquiera de los dos sentidos)', () => {
    expect(compareLocales({ ...base, es: { ...base['es'], help: { a: 'b' } } })).toEqual([
      expect.objectContaining({ file: 'en/help.json', kind: 'missing-file' }),
    ]);
    expect(compareLocales({ ...base, en: { ...base['en'], help: { a: 'b' } } })).toEqual([
      expect.objectContaining({ file: 'en/help.json', kind: 'missing-file' }),
    ]);
  });

  it('falla si falta el idioma en entero', () => {
    expect(compareLocales({ es: base['es'] ?? {} })).toEqual([
      expect.objectContaining({ file: 'en/', kind: 'missing-file' }),
    ]);
  });

  it('falla con marcadores {{x}} distintos o con textos vacíos', () => {
    const files: LocaleFiles = {
      ...base,
      en: { grid: { title: ' ', actions: { save: 'Save {{n}}' } } },
    };
    const kinds = compareLocales(files).map((i) => i.kind);
    expect(kinds).toEqual(expect.arrayContaining(['empty', 'placeholders']));
  });

  it('otro idioma puede ir incompleto pero no tener claves que es no tiene', () => {
    const partial: LocaleFiles = { ...base, ar: { grid: { title: 'شبكة' } } };
    expect(compareLocales(partial)).toEqual([]);
    const extra: LocaleFiles = { ...base, ar: { grid: { title: 'شبكة', raro: 'x' } } };
    expect(compareLocales(extra)).toEqual([expect.objectContaining({ file: 'ar/grid.json', kind: 'extra-key' })]);
  });

  it('el árabe puede traer las categorías plurales que es no tiene, con los marcadores de _other', () => {
    const arabic: Record<string, string> = { rows_one: 'صف واحد', rows_two: 'صفان', rows_few: '{{count}} صفوف', rows_many: '{{count}} صفًا', rows_other: '{{count}} صف', who: 'مرحبًا {{name}}' };
    const files: LocaleFiles = {
      es: { n: { rows_one: '{{count}} fila', rows_other: '{{count}} filas', who: 'Hola {{name}}' } },
      en: { n: { rows_one: '{{count}} row', rows_other: '{{count}} rows', who: 'Hi {{name}}' } },
      ar: { n: arabic },
    };
    expect(compareLocales(files, 'es', ['es', 'en', 'ar'])).toEqual([]);
    // Una categoría plural de más no excusa un marcador ajeno ni una clave que no es plural.
    const badPlaceholder: LocaleFiles = { ...files, ar: { n: { ...arabic, rows_few: '{{n}} صفوف' } } };
    expect(compareLocales(badPlaceholder, 'es', ['es', 'en', 'ar'])).toEqual([expect.objectContaining({ kind: 'placeholders', key: 'rows_few' })]);
    const notPlural: LocaleFiles = { ...files, ar: { n: { ...arabic, who_few: 'x' } } };
    expect(compareLocales(notPlural, 'es', ['es', 'en', 'ar'])).toEqual([expect.objectContaining({ kind: 'extra-key', key: 'who_few' })]);
    // Falta una clave en el árabe requerido.
    const missing: LocaleFiles = { ...files, ar: { n: { rows_other: '{{count}} صف', who: 'مرحبًا {{name}}' } } };
    expect(compareLocales(missing, 'es', ['es', 'en', 'ar'])).toEqual([expect.objectContaining({ kind: 'missing-key', key: 'rows_one' })]);
    // El número puede ir con palabras en una clave plural, pero no en una que no lo es.
    const noCount: LocaleFiles = { ...files, ar: { n: { ...arabic, who: 'مرحبًا' } } };
    expect(compareLocales(noCount, 'es', ['es', 'en', 'ar'])).toEqual([expect.objectContaining({ kind: 'placeholders', key: 'who' })]);
  });

  it('aplana árboles y matrices y lee los marcadores', () => {
    expect([...flattenLocale({ a: { b: 'x', c: ['y', 'z'] } }).keys()]).toEqual(['a.b', 'a.c[0]', 'a.c[1]']);
    expect(placeholders('{{b}} y {{ a }} y {{n, number}}')).toEqual(['a', 'b', 'n']);
  });
});
