// Las ayudas de las pruebas e2e y el script de capturas (no necesitan navegador): detección de peticiones
// fuera del origen, credenciales de la demo, y que las 41 capturas versionadas son las del PLAN §7 y la ronda 2.
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { describe, expect, it } from 'vitest';
import { ROOT, VIEWPORT, isExternalUrl, mobileOptions, readDemoCredentials, readNamespaces } from './lib/demo.mjs';

interface Scene {
  id: string;
  title: string;
  shows: string;
  path: string;
  ready: string;
}
interface Variant {
  id: string;
  label: string;
  options: {
    theme: string;
    mode: string;
    lng: string;
    dir?: string;
    viewport?: { width: number; height: number };
    isMobile?: boolean;
    hasTouch?: boolean;
  };
  detail: string;
  scenes?: readonly string[];
}
interface CapturasModule {
  SCENES: readonly Scene[];
  VARIANTS: readonly Variant[];
  VERSIONED: readonly string[];
  fileName(scene: Scene, variant: Variant): string;
  scenesOf(variant: Variant): readonly Scene[];
  viewportOf(variant: Variant): { width: number; height: number };
  expectedCaptures(): { scene: Scene; variant: Variant; file: string }[];
  renderReadme(options: { date: string; browser: string; version: string }): string;
}

// El módulo es JavaScript sin declaraciones: se carga por URL y se tipa aquí.
const capturas = (await import(/* @vite-ignore */ pathToFileURL(path.join(ROOT, 'scripts', 'capturas.mjs')).href)) as CapturasModule;
const OUT_DIR = path.join(ROOT, 'docs', 'capturas');

/** Escenas del PLAN §7, en su orden. */
const PLAN_SCENES = [
  'cuadricula-editable',
  'detalle-edicion',
  'organigrama-listado',
  'organigrama-diagrama',
  'mapa-mundo',
  'mapa-pais',
  'mapa-region',
  'mapa-ciudad',
  'ayuda-recorrido',
];

describe('registro de peticiones', () => {
  const origin = 'http://127.0.0.1:4300';

  it('lo del propio origen y lo que no es red no sale', () => {
    for (const url of [
      `${origin}/api/v1/products`,
      `${origin}/geo/world.geojson`,
      `${origin}/themes/modern-flat/index.css`,
      'data:image/png;base64,AAAA',
      'blob:http://127.0.0.1:4300/5c1f',
      'about:blank',
    ]) {
      expect(isExternalUrl(url, origin), url).toBe(false);
    }
  });

  it('otro origen, otro puerto, otro protocolo o un servicio de teselas sí salen', () => {
    for (const url of [
      'https://tile.openstreetmap.org/3/4/2.png',
      'https://fonts.googleapis.com/css2?family=Inter',
      'http://127.0.0.1:4301/api/v1/products',
      'http://localhost:4300/api/v1/products',
      'https://127.0.0.1:4300/api/v1/products',
      'ws://127.0.0.1:4300/socket',
      'no es una url',
    ]) {
      expect(isExternalUrl(url, origin), url).toBe(true);
    }
  });
});

describe('datos de la demo que usan las pruebas', () => {
  it('el usuario de la demo existe y los demás se piden por nombre', () => {
    const credentials = readDemoCredentials();
    expect(credentials.username).toBe('demo');
    expect(credentials.password.length).toBeGreaterThan(8);
    expect(readDemoCredentials('nuevo').username).toBe('nuevo');
    expect(() => readDemoCredentials('nadie')).toThrow(/nadie/);
  });

  it('los espacios de nombres de i18n salen de src/locales', () => {
    expect(readNamespaces()).toEqual(expect.arrayContaining(['core', 'grid', 'detail', 'tree', 'map', 'help', 'fields', 'ui', 'auth', 'errors']));
  });
});

describe('sesiones de teléfono (B-5)', () => {
  it('por omisión son 390×844, con agente de móvil y pantalla táctil', () => {
    expect(mobileOptions()).toEqual({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  });

  it('admiten otro tamaño y completan el que falta con el de por omisión', () => {
    expect(mobileOptions({ width: 360, height: 780 }).viewport).toEqual({ width: 360, height: 780 });
    expect(mobileOptions({ width: 360 }).viewport).toEqual({ width: 360, height: 844 });
    expect(mobileOptions({ height: 700 }).viewport).toEqual({ width: 390, height: 700 });
  });

  it('el escritorio por omisión sigue siendo 1440×900, sin pantalla táctil', () => {
    expect(VIEWPORT).toEqual({ width: 1440, height: 900 });
    expect(mobileOptions()).not.toHaveProperty('dir');
  });

  it('los tamaños de teléfono de las pruebas caben en el modo compacto del marco (40em con la letra de 14 px del tema)', () => {
    const compactPx = 40 * 14;
    for (const { width } of [mobileOptions({ width: 360 }).viewport, mobileOptions({ width: 390 }).viewport]) {
      expect(width).toBeLessThanOrEqual(compactPx);
    }
  });
});

describe('teclas de la cuadrícula en la ayuda (B-4)', () => {
  const read = (file: string) => JSON.parse(readFileSync(path.join(ROOT, 'src', 'locales', file), 'utf8')) as { shortcuts: Record<string, string>; help: { table: { body: string } } };

  it('Tab se documenta como salida de la lista y, al editar, como confirmar y pasar a la celda siguiente', () => {
    for (const lang of ['es', 'en']) {
      const grid = read(`${lang}/grid.json`);
      const next = grid.shortcuts.next ?? '';
      expect(next, lang).toMatch(/\(/);
      expect(next.length, lang).toBeGreaterThan(30);
      expect(grid.help.table.body, lang).toContain('Tab');
      expect(grid.help.table.body, lang).toMatch(/Shift\+Tab|Mayús\+Tab/);
    }
    expect(read('es/grid.json').shortcuts.next ?? '').toContain('Salir de la cuadrícula');
    expect(read('en/grid.json').shortcuts.next ?? '').toContain('Leave the grid');
  });
});

describe('capturas (PLAN §7 y ronda 2)', () => {
  it('son 41: las 9 escenas del plan en cuatro variantes de escritorio y 5 en móvil, sin repetidos', () => {
    expect(capturas.SCENES.map((scene) => scene.id)).toEqual(PLAN_SCENES);
    expect(capturas.VARIANTS.map((variant) => variant.id)).toEqual(['claro', 'oscuro', 'rtl', 'ingles', 'movil']);
    const names = capturas.expectedCaptures().map((capture) => capture.file);
    expect(names).toHaveLength(41);
    expect(new Set(names).size).toBe(41);
    expect(names[0]).toBe('cuadricula-editable-claro.png');
    expect(names.at(-1)).toBe('ayuda-recorrido-movil.png');
    expect(names.filter((name) => name.endsWith('-ingles.png'))).toHaveLength(9);
    expect(names.filter((name) => name.endsWith('-movil.png'))).toHaveLength(5);
  });

  it('las variantes: modern-flat claro/oscuro en es, claro en ar (árabe real, sin ?dir=), claro en en (LTR) y el teléfono en es', () => {
    const [light, dark, rtl, english, mobile] = capturas.VARIANTS;
    expect(light?.options).toEqual({ theme: 'modern-flat', mode: 'light', lng: 'es' });
    expect(dark?.options).toEqual({ theme: 'modern-flat', mode: 'dark', lng: 'es' });
    expect(rtl?.options).toEqual({ theme: 'modern-flat', mode: 'light', lng: 'ar', locale: 'ar' });
    expect(rtl?.options).not.toHaveProperty('dir');
    expect(english?.options).toEqual({ theme: 'modern-flat', mode: 'light', lng: 'en' });
    expect(mobile?.options).toMatchObject({
      theme: 'modern-flat',
      mode: 'light',
      lng: 'es',
      isMobile: true,
      hasTouch: true,
      viewport: { width: 390, height: 844 },
    });
    expect(mobile?.scenes).toEqual(['cuadricula-editable', 'detalle-edicion', 'organigrama-listado', 'mapa-mundo', 'ayuda-recorrido']);
  });

  it('cada escena apunta a una ruta de la demo con su indicador de «lista»', () => {
    for (const scene of capturas.SCENES) {
      expect(scene.path, scene.id).toMatch(/^\/(product|facility|position)/);
      expect(scene.ready, scene.id).toContain('data-kx-ready');
    }
    const maps = capturas.SCENES.filter((scene) => scene.id.startsWith('mapa-')).map((scene) => scene.path);
    expect(maps).toEqual([
      '/facility?view=map&level=world',
      '/facility?view=map&level=country&code=ES',
      '/facility?view=map&level=region&code=ES51',
      '/facility?view=map&level=city&code=ES300',
    ]);
  });

  it('el README lleva fecha, navegador, versión y una fila por escena con una columna por variante', () => {
    const readme = capturas.renderReadme({ date: '2026-11-27', browser: 'Microsoft Edge', version: '158.0.1' });
    expect(readme).toContain('**Fecha:** 2026-11-27');
    expect(readme).toContain('Microsoft Edge 158.0.1');
    for (const { file } of capturas.expectedCaptures()) expect(readme).toContain(file);
    for (const file of capturas.VERSIONED) expect(readme).toContain(`(${file})`);
    expect(readme).toContain('Total: 41 imágenes.');
  });

  it('las capturas versionadas existen y todas las que hay son PNG del tamaño de su variante', () => {
    for (const file of capturas.VERSIONED) expect(existsSync(path.join(OUT_DIR, file)), `falta ${file}`).toBe(true);
    // Las demás sólo existen tras `npm run capturas` (están en .gitignore).
    for (const { variant, file } of capturas.expectedCaptures()) {
      const full = path.join(OUT_DIR, file);
      if (!capturas.VERSIONED.includes(file) && !existsSync(full)) continue;
      expect(existsSync(full), `falta ${path.relative(ROOT, full)}`).toBe(true);
      expect(statSync(full).size, file).toBeGreaterThan(10_000);
      const header = readFileSync(full).subarray(0, 24);
      expect(header.subarray(1, 4).toString('latin1'), 'firma PNG').toBe('PNG');
      const { width, height } = capturas.viewportOf(variant);
      expect([header.readUInt32BE(16), header.readUInt32BE(20)], file).toEqual([width, height]);
    }
    // Ni una imagen de más: lo que hay en la carpeta es lo que genera el script.
    const onDisk = readdirSync(OUT_DIR).filter((name) => name.endsWith('.png'));
    const expected = capturas.expectedCaptures().map((capture) => capture.file);
    for (const name of onDisk) expect(expected, `sobra ${name}`).toContain(name);
  });

  it('el README de docs/capturas es el que genera el script (fecha, navegador, versión y todas las imágenes)', () => {
    const readme = readFileSync(path.join(OUT_DIR, 'README.md'), 'utf8');
    expect(readme).toMatch(/\*\*Fecha:\*\* \d{4}-\d{2}-\d{2}/);
    expect(readme).toMatch(/\*\*Navegador:\*\* (Microsoft Edge|Google Chrome) \d+\.\d+/);
    for (const { file } of capturas.expectedCaptures()) expect(readme).toContain(file);
    expect(readme).toContain('Total: 41 imágenes.');
  });
});
