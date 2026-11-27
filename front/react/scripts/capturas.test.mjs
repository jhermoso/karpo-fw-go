// Pruebas de `npm run capturas` sin navegador: las variantes, las escenas de cada una, los nombres de los ficheros y el
// README. Cuenta las 41 imágenes que pide la ronda 2 (27 de siempre, 9 en inglés y 5 de móvil).
import { describe, expect, it } from 'vitest';
import { MOBILE_SCENES, SCENES, VARIANTS, expectedCaptures, fileName, isRtl, renderReadme, scenesOf, viewportOf } from './capturas.mjs';

describe('capturas', () => {
  it('son 41: 9 escenas en 4 variantes de escritorio y 5 en móvil', () => {
    expect(SCENES).toHaveLength(9);
    expect(VARIANTS.map((variant) => variant.id)).toEqual(['claro', 'oscuro', 'rtl', 'ingles', 'movil']);
    expect(expectedCaptures()).toHaveLength(41);
    expect(new Set(expectedCaptures().map((capture) => capture.file)).size).toBe(41);
  });

  it('la variante rtl es árabe real (lng=ar, sin ?dir=), clara, 1440x900 y con las nueve escenas (D-R38)', () => {
    const rtl = VARIANTS.find((variant) => variant.id === 'rtl');
    expect(rtl?.options).toEqual({ theme: 'modern-flat', mode: 'light', lng: 'ar', locale: 'ar' });
    expect(rtl?.options).not.toHaveProperty('dir');
    expect(viewportOf(rtl)).toEqual({ width: 1440, height: 900 });
    expect(scenesOf(rtl)).toHaveLength(9);
    expect(isRtl(rtl)).toBe(true);
    expect(rtl?.detail).toMatch(/sin revisión nativa/);
    // Las demás son LTR; `?dir=rtl` con otro idioma también cuenta como RTL.
    for (const variant of VARIANTS.filter((candidate) => candidate.id !== 'rtl')) expect(isRtl(variant), variant.id).toBe(false);
    expect(isRtl({ options: { lng: 'en', dir: 'rtl' } })).toBe(true);
  });

  it('la escena mapa-region enseña varias delimitaciones del nivel región (Cataluña, ES51), no una sola que se salta', () => {
    const region = SCENES.find((scene) => scene.id === 'mapa-region');
    // `level=region&code=ES` no existe en la demo (no hay region-ES.geojson: «No hay geometría para ES»); ES30 (Madrid)
    // tiene una sola provincia y el nivel se salta.
    expect(region?.path).toBe('/facility?view=map&level=region&code=ES51');
    expect(region?.shows).toMatch(/cuatro delimitaciones/);
  });

  it('la variante en inglés es LTR, clara, 1440x900 y con las nueve escenas', () => {
    const ingles = VARIANTS.find((variant) => variant.id === 'ingles');
    expect(ingles?.options).toEqual({ theme: 'modern-flat', mode: 'light', lng: 'en' });
    expect(ingles?.options).not.toHaveProperty('dir');
    expect(viewportOf(ingles)).toEqual({ width: 1440, height: 900 });
    expect(scenesOf(ingles)).toHaveLength(9);
  });

  it('la variante de móvil es 390x844 con isMobile y hasTouch, en español y claro, con cinco escenas', () => {
    const movil = VARIANTS.find((variant) => variant.id === 'movil');
    expect(movil?.options).toMatchObject({ lng: 'es', mode: 'light', isMobile: true, hasTouch: true });
    expect(viewportOf(movil)).toEqual({ width: 390, height: 844 });
    expect(scenesOf(movil).map((scene) => scene.id)).toEqual([
      'cuadricula-editable',
      'detalle-edicion',
      'organigrama-listado',
      'mapa-mundo',
      'ayuda-recorrido',
    ]);
    expect(MOBILE_SCENES).toHaveLength(5);
    // Todas existen entre las escenas.
    for (const id of MOBILE_SCENES) expect(SCENES.some((scene) => scene.id === id)).toBe(true);
  });

  it('los ficheros se llaman <escena>-<variante>.png', () => {
    expect(fileName(SCENES[0], VARIANTS[3])).toBe('cuadricula-editable-ingles.png');
    expect(fileName(SCENES[8], VARIANTS[4])).toBe('ayuda-recorrido-movil.png');
  });

  it('el README lista las variantes, marca con un guion las escenas que no tiene el móvil y cuenta 41', () => {
    const readme = renderReadme({ date: '2026-11-27', browser: 'Microsoft Edge', version: '1' });
    expect(readme).toContain('Total: 41 imágenes.');
    expect(readme).toContain('`ingles` (1440×900, 9 escenas)');
    expect(readme).toContain('`rtl` (1440×900, 9 escenas): modern-flat, modo claro, árabe');
    expect(readme).toContain('traducción de demostración **sin revisión nativa**');
    expect(readme).toContain('`movil` (390×844, 5 escenas)');
    expect(readme).toContain('(mapa-mundo-movil.png)');
    const pais = readme.split('\n').find((line) => line.startsWith('| Mapa · país'));
    expect(pais).toMatch(/\| — \|$/);
    expect(pais).toContain('mapa-pais-ingles.png');
  });
});
