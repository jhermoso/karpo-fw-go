// Descubrimiento de idiomas, dirección y pseudolocalización (sin DOM).
import { afterEach, describe, expect, it } from 'vitest';
import {
  buildPseudoResources,
  collectResources,
  directionFor,
  listLocales,
  pickLocale,
  preloadAllLocales,
  pseudoLocalize,
  resetLoadedLocales,
} from './locales';

describe('directionFor', () => {
  it('ar, he, fa y ur van de derecha a izquierda', () => {
    for (const lng of ['ar', 'he', 'fa', 'ur']) expect(directionFor(lng), lng).toBe('rtl');
  });

  it('el árabe de Egipto y el hebreo en mayúsculas también', () => {
    expect(directionFor('ar-EG')).toBe('rtl');
    expect(directionFor('HE')).toBe('rtl');
    expect(directionFor('fa_IR')).toBe('rtl');
  });

  it('es, en y el resto van de izquierda a derecha', () => {
    for (const lng of ['es', 'en', 'en-GB', 'de', 'zh', 'xx', '']) expect(directionFor(lng), lng).toBe('ltr');
  });
});

describe('pickLocale', () => {
  it('elige el idioma admitido más cercano', () => {
    expect(pickLocale('en-GB', ['es', 'en'])).toBe('en');
    expect(pickLocale('es_ES', ['es', 'en'])).toBe('es');
    expect(pickLocale('EN', ['es', 'en'])).toBe('en');
  });

  it('cae al idioma de referencia si no hay nada parecido', () => {
    expect(pickLocale('ja', ['es', 'en'])).toBe('es');
    expect(pickLocale(null, ['en', 'es'])).toBe('es');
    expect(pickLocale(undefined, ['en'])).toBe('en');
  });
});

describe('collectResources', () => {
  it('agrupa ruta/idioma/espacio y descarta lo que no es un fichero de idioma', () => {
    const resources = collectResources({
      '../locales/es/grid.json': { a: 'x' },
      '../locales/en/grid.json': { a: 'y' },
      '..\\locales\\es\\ui.json': { b: 'z' },
      '../locales/es/README.md': 'no',
      '../otra/cosa.json': { c: 1 },
    });
    expect(resources).toEqual({ es: { grid: { a: 'x' }, ui: { b: 'z' } }, en: { grid: { a: 'y' } } });
  });

  it('sin argumentos devuelve lo ya cargado: nada hasta pedirlo, y luego todos los ficheros reales (help incluido)', async () => {
    resetLoadedLocales();
    expect(collectResources()).toEqual({});
    await preloadAllLocales();
    const real = collectResources();
    expect(Object.keys(real)).toEqual(expect.arrayContaining(['es', 'en']));
    expect(real['es']?.['help']).toBeDefined();
    expect(real['en']?.['help']).toBeDefined();
  });

  it('listLocales pone el idioma de referencia primero y no cuenta xx', () => {
    expect(listLocales({ en: {}, ar: {}, xx: {}, es: {} })).toEqual(['es', 'ar', 'en']);
  });
});

afterEach(async () => {
  await preloadAllLocales();
});

describe('pseudolocalización', () => {
  it('acentúa y encierra en corchetes', () => {
    const out = pseudoLocalize('Guardar');
    expect(out.startsWith('[')).toBe(true);
    expect(out.endsWith(']')).toBe(true);
    expect(out).not.toContain('Guardar');
  });

  it('conserva intactos los marcadores {{x}} y $t(x)', () => {
    expect(pseudoLocalize('Hola {{name}}, tienes {{count}} avisos')).toMatch(/\{\{name\}\}.*\{\{count\}\}/);
    expect(pseudoLocalize('ver $t(otra.clave) ahora')).toContain('$t(otra.clave)');
  });

  it('pseudolocaliza todo el árbol del idioma de referencia', () => {
    const pseudo = buildPseudoResources({
      es: { help: { button: { label: 'Ayuda' } }, ui: { n: 'x' } },
      en: { help: { button: { label: 'Help' } } },
    }) as Record<string, Record<string, unknown>>;
    expect(JSON.stringify(pseudo)).toContain('[');
    expect(Object.keys(pseudo)).toEqual(['help', 'ui']);
  });
});
