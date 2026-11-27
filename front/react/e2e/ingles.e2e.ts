// Inglés (B-2, ronda 2): con `lng=en` ninguna pantalla enseña un literal español de las 8 configuraciones de Go.
//
// Los literales se leen de `../angular/config/entities` (cabeceras, etiquetas, títulos, pistas, textos de
// verdadero y falso, valores de `badgeMap` y `enumValues`, además de marcadores, mensajes y nombres de la
// entidad). Para cada pantalla de `SCREENS` (más el alta, el detalle y el menú de acciones de cada entidad) se
// recoge el texto visible y los atributos accesibles y se falla si algún texto es uno de esos literales,
// salvo una lista explícita de los que son iguales en inglés.
//
// Coincidencia: un texto de la página (un nodo de texto o un atributo) es el literal entero (sin distinguir
// mayúsculas, acentos de caja ni signos finales) o, si el literal tiene tres palabras o más, lo contiene. Un
// literal corto («Sede», «Tipo») dentro de un dato en español («Recepción (Sede Central)») no es una fuga.
//
// Además, afirmaciones concretas: la leyenda del mapa no dice «instalaciones», las insignias del organigrama
// son Filled, Open, Planned o Closed y las cabeceras de `/product` están en inglés.
import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';
import type { Page } from 'playwright-core';
import { describe, expect, it } from 'vitest';
import { ROOT } from './lib/demo.mjs';
import { attr, poll, visible } from './lib/esperar';
import { pageTexts, SCREENS, TEXT_ATTRIBUTES } from './lib/pantallas';
import { e2eSuite } from './lib/suite';

const ENTITIES_DIR = path.resolve(ROOT, '..', 'angular', 'config', 'entities');

/** Claves de la configuración de Go cuyo valor es un texto que ve el usuario. */
const TEXT_KEYS = new Set([
  'header',
  'label',
  'title',
  'hint',
  'trueLabel',
  'falseLabel',
  'placeholder',
  'confirmMessage',
  'emptyMessage',
  'createTitle',
  'editTitle',
  'saveButtonText',
  'cancelButtonText',
  'tooltip',
]);

/** Literales de Go que son idénticos en inglés (o no son palabras): no cuentan como fuga. */
const SAME_IN_ENGLISH: readonly string[] = ['-', 'ISO', 'ISO-3', 'SEPA', 'IBAN', 'Party', 'Euro', 'No'];

/** Las entidades de Go y su ruta. */
const ENTITY_IDS = ['facility', 'work-center', 'product', 'supplier', 'carrier', 'asset', 'country', 'holiday'] as const;

/** Texto comparable: sin espacios de más ni signos finales (`:`, `*`, `…`), en minúsculas. */
function normalize(text: string): string {
  return text
    .normalize('NFC')
    .toLocaleLowerCase('es')
    .replace(/\s+/g, ' ')
    .replace(/[\s:*…]+$/u, '')
    .trim();
}

function words(text: string): number {
  return text.split(/\s+/).filter(Boolean).length;
}

/** Recorre una configuración y recoge cada literal español con la ruta donde está. */
function collect(node: unknown, trail: string[], file: string, into: Map<string, string[]>): void {
  const add = (literal: string, where: string[]) => {
    if (!/\p{L}/u.test(literal)) return;
    const list = into.get(literal) ?? [];
    list.push(`${file}:${where.join('.')}`);
    into.set(literal, list);
  };
  if (Array.isArray(node)) {
    for (const item of node) {
      // `enumValues` y `options` también pueden ser listas de textos sueltos.
      if (typeof item === 'string' && ['enumValues', 'options'].includes(trail.at(-1) ?? '')) add(item, trail);
      else collect(item, trail, file, into);
    }
    return;
  }
  if (!node || typeof node !== 'object') return;
  for (const [key, value] of Object.entries(node)) {
    if (typeof value === 'string' && (TEXT_KEYS.has(key) || (trail.length === 0 && (key === 'name' || key === 'namePlural')))) {
      add(value, [...trail, key]);
    } else {
      collect(value, [...trail, key], file, into);
    }
  }
}

/** Todos los literales españoles de las 8 configuraciones de Go. */
function goLiterals(): Map<string, string[]> {
  const into = new Map<string, string[]>();
  for (const folder of readdirSync(ENTITIES_DIR)) {
    for (const file of readdirSync(path.join(ENTITIES_DIR, folder))) {
      if (!file.endsWith('.config.json')) continue;
      collect(JSON.parse(readFileSync(path.join(ENTITIES_DIR, folder, file), 'utf8')), [], file, into);
    }
  }
  return into;
}

const LITERALS = goLiterals();
const SAME = new Set(SAME_IN_ENGLISH.map(normalize));
/** Los que se vigilan: literal normalizado -> el literal tal como está en la configuración. */
const WATCHED = new Map<string, string>();
for (const literal of LITERALS.keys()) if (!SAME.has(normalize(literal))) WATCHED.set(normalize(literal), literal);

/** ¿Es el texto de la página uno de los literales vigilados? Devuelve el literal o `null`. */
function leakOf(unit: string): string | null {
  const text = normalize(unit);
  if (text === '') return null;
  const whole = WATCHED.get(text);
  if (whole) return whole;
  for (const [normalized, literal] of WATCHED) if (words(normalized) >= 3 && text.includes(normalized)) return literal;
  return null;
}

/** Los textos de la página: un elemento por nodo de texto visible y por atributo accesible. */
async function pageUnits(page: Page): Promise<string[]> {
  const units = await page.evaluate((attributes) => {
    const out: string[] = [document.title];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const parent = node.parentElement;
      if (!parent || ['SCRIPT', 'STYLE', 'NOSCRIPT', 'OPTION'].includes(parent.tagName)) continue;
      if (typeof parent.checkVisibility === 'function' && !parent.checkVisibility()) continue;
      const text = node.textContent?.trim();
      if (text) out.push(text);
    }
    for (const el of document.querySelectorAll(attributes.map((name) => `[${name}]`).join(', '))) {
      for (const name of attributes) {
        const value = el.getAttribute(name);
        if (value) out.push(value);
      }
    }
    return out;
  }, TEXT_ATTRIBUTES);
  return units;
}

/** Fugas en la página tal como está: `«literal» en «texto» (config)`. */
async function leaksOn(page: Page, where: string): Promise<string[]> {
  const found = new Set<string>();
  for (const unit of await pageUnits(page)) {
    const literal = leakOf(unit);
    if (literal) found.add(`${where}: «${literal}» en «${unit.slice(0, 80)}» (${LITERALS.get(literal)?.[0] ?? '?'})`);
  }
  return [...found];
}

const settle = (page: Page) => page.waitForLoadState('networkidle').catch(() => undefined);

describe('los literales de Go', () => {
  it('se leen de las 8 configuraciones', () => {
    expect(LITERALS.size).toBeGreaterThan(150);
    for (const known of ['Estado', 'Nombre', 'Instalaciones', 'Código de IVA', 'Activa', 'Guardar', 'Mercaderías (600)', 'Historial de bloqueos']) {
      expect(LITERALS.has(known), known).toBe(true);
    }
  });

  it('la lista de iguales en inglés sólo nombra literales que existen (no se pudre)', () => {
    for (const same of SAME_IN_ENGLISH) {
      if (!/\p{L}/u.test(same)) continue; // un guion no es una palabra: ni siquiera se recoge
      expect(LITERALS.has(same), `«${same}» ya no está en ninguna configuración: quítalo de SAME_IN_ENGLISH`).toBe(true);
    }
  });

  it('el detector reconoce un literal entero, uno largo dentro de otra frase y deja pasar un dato que sólo lo contiene', () => {
    expect(leakOf('Estado')).toBe('Estado');
    expect(leakOf('  Nombre…  ')).toMatch(/^Nombre/);
    expect(leakOf('Tipo:')).toBe('Tipo');
    expect(leakOf('Antes: Datos del producto, después')).toBe('Datos del producto');
    expect(leakOf('Recepción (Sede Central)')).toBeNull();
    expect(leakOf('Status')).toBeNull();
    expect(leakOf('No')).toBeNull();
    expect(leakOf('ISO')).toBeNull();
  });
});

e2eSuite('inglés: ningún literal español de Go', ({ open }) => {
  it('ninguna pantalla enseña un literal español con lng=en', async () => {
    const { page, go } = await open({ lng: 'en', path: '/', ready: false });
    const problems: string[] = [];
    for (const screen of SCREENS) {
      await go(screen.path, screen.ready);
      await settle(page);
      problems.push(...(await leaksOn(page, screen.path)));
    }
    expect(problems, 'literales españoles con lng=en').toEqual([]);
  });

  for (const id of ENTITY_IDS) {
    it(`${id}: ni el alta, ni el detalle en edición, ni el menú de acciones, ni los filtros`, async () => {
      const { page, go } = await open({ lng: 'en', path: `/${id}`, ready: '[data-kx-ready="true"]' });
      await settle(page);
      const problems: string[] = [...(await leaksOn(page, `/${id}`))];

      // Menú de acciones de la primera fila.
      const actions = page.locator('[role="row"][data-kx-row]').first().locator('[data-kx-col="__actions"] button').last();
      if (await actions.count()) {
        await actions.click().catch(() => undefined);
        problems.push(...(await leaksOn(page, `/${id} (acciones de fila)`)));
        await page.keyboard.press('Escape');
      }

      // Detalle en edición con doble clic sobre la primera celda de datos.
      const cell = page.locator('[role="row"][data-kx-row]').first().locator('[data-kx-col]:not([data-kx-col^="__"])').first();
      if (await cell.count()) {
        await cell.dblclick();
        const sheet = page.locator('section[data-th="sheet"]');
        await visible(sheet).toBe(true);
        await settle(page);
        problems.push(...(await leaksOn(page, `/${id} (detalle)`)));
        await page.keyboard.press('Escape');
        await page.keyboard.press('Escape');
      }

      // Formulario de alta (las entidades de sólo lectura lo rechazan: entonces no hay nada que ver).
      await go(`/${id}/nuevo`, false);
      await settle(page);
      problems.push(...(await leaksOn(page, `/${id}/nuevo`)));

      expect(problems, `literales españoles con lng=en en ${id}`).toEqual([]);
    });
  }

  it('control: con lng=es la misma lectura sí ve los literales (el detector no es ciego)', async () => {
    const { page } = await open({ lng: 'es', path: '/asset', ready: '[data-kx-ready="true"]' });
    await settle(page);
    const found = await leaksOn(page, '/asset');
    expect(found.some((line) => line.includes('«Estado»'))).toBe(true);
    expect(found.length).toBeGreaterThan(3);
  });

  it('control: un literal español que se cuela en la página en inglés se detecta', async () => {
    const { page } = await open({ lng: 'en', path: '/product', ready: '[data-kx-ready="true"]' });
    await page.evaluate(() => {
      const leak = document.createElement('p');
      leak.textContent = 'Datos del producto';
      document.body.append(leak);
      document.querySelector('button')?.setAttribute('aria-label', 'Retirar');
    });
    const found = (await leaksOn(page, '/product')).join('\n');
    expect(found).toContain('«Datos del producto»');
    expect(found).toContain('«Retirar»');
  });

  it('la leyenda del mapa no dice «instalaciones» y nombra la entidad en inglés', async () => {
    const { page } = await open({ lng: 'en', path: '/facility?view=map&level=world', ready: 'section[data-kx-view="map"][data-kx-ready="true"]' });
    const legend = page.locator('[data-kx-part="legend"]');
    await visible(legend).toBe(true);
    const text = (await legend.innerText()).replace(/\s+/g, ' ').trim();
    expect(text).not.toMatch(/instalaci[oó]n|instalaciones/i);
    expect(text).not.toMatch(/delimitaci[oó]n/i);
    expect(text).toMatch(/^Facilities by boundary/);
    expect(text).toContain('No data');
    // El nombre accesible de la figura y de su lista también.
    const names = await legend.evaluate((el) => [el.getAttribute('aria-labelledby'), ...[...el.querySelectorAll('[aria-label]')].map((e) => e.getAttribute('aria-label'))]);
    expect(names.join(' ')).not.toMatch(/instalaci/i);
  });

  it('el recuento de la cabecera y el control de vista están en inglés', async () => {
    const { page } = await open({ lng: 'en', path: '/facility', ready: '[data-kx-ready="true"]' });
    const header = page.locator('[data-kx-part="entity-header"]').first();
    const count = header.locator('[data-kx-part="entity-count"]');
    await poll(async () => (await count.innerText()).replace(/\s+/g, ' ').trim()).toMatch(/^\d+ (of [\d,.]+ )?facilities$/);
    expect(await header.locator('[data-kx-part="entity-title"]').innerText()).toBe('Facilities');
    expect((await header.innerText()).replace(/\s+/g, ' ')).not.toMatch(/instalaci/i);
    const group = page.getByRole('radiogroup', { name: 'View' });
    await visible(group).toBe(true);
    for (const name of ['Table', 'Tree', 'Diagram', 'Map']) await visible(group.getByRole('radio', { name })).toBe(true);
    await visible(page.getByRole('button', { name: 'New' })).toBe(true);
  });

  for (const layout of ['list', 'diagram'] as const) {
    it(`las insignias del organigrama (${layout}) son Filled, Open, Planned o Closed`, async () => {
      const { page } = await open({
        lng: 'en',
        path: `/position?view=tree&layout=${layout}`,
        ready: 'section[data-kx-view="tree"][data-kx-ready="true"]',
      });
      const badges = page.locator('section[data-kx-view="tree"] [data-kx-part="badge"]');
      await poll(() => badges.count()).toBeGreaterThan(3);
      // En el listado sólo se ven las ramas desplegadas; se abren para ver más insignias.
      if (layout === 'list') {
        for (const item of await page.getByRole('treeitem').all()) {
          if ((await item.getAttribute('aria-expanded')) === 'false') {
            await item.focus();
            await page.keyboard.press('ArrowRight');
          }
        }
      } else {
        // En el diagrama, las ramas plegadas se abren con su botón «Expand the branch of …».
        const fold = page.getByRole('button', { name: /^Expand the branch of/ });
        // `dispatchEvent`: con las tarjetas de R3-4 los hijos de la última fila quedan bajo el minimapa y un clic con puntero lo toca a él.
        for (let pass = 0; pass < 30 && (await fold.count()) > 0; pass += 1) await fold.first().dispatchEvent('click');
      }
      const texts = (await badges.allInnerTexts()).map((text) => text.trim()).filter(Boolean);
      expect(texts.length).toBeGreaterThan(3);
      const allowed = new Set(['Filled', 'Open', 'Planned', 'Closed']);
      expect(texts.filter((text) => !allowed.has(text)), 'insignias fuera de Filled|Open|Planned|Closed').toEqual([]);
      expect(new Set(texts).size, 'al menos dos estados distintos a la vista').toBeGreaterThanOrEqual(2);
      for (const spanish of ['Cubierto', 'Abierto', 'Planificado', 'Cerrado']) expect(texts).not.toContain(spanish);
    });
  }

  it('las cabeceras de /product están en inglés', async () => {
    const { page } = await open({ lng: 'en', path: '/product', ready: '[data-kx-ready="true"]' });
    const headers = page.getByRole('columnheader');
    await poll(() => headers.count()).toBeGreaterThan(4);
    const names = (await headers.allInnerTexts()).map((text) => text.replace(/\s+/g, ' ').trim()).filter(Boolean);
    expect(names).toEqual(expect.arrayContaining(['Reference', 'Name', 'Kind', 'Unit', 'Base price', 'Discontinued since']));
    for (const spanish of ['Referencia', 'Nombre', 'Naturaleza', 'Unidad', 'Precio base', 'Retirado desde']) {
      expect(names.join('|')).not.toContain(spanish);
    }
    await attr(page.locator('html'), 'lang').toBe('en');
  });

  it('los textos del detalle de un producto (sección, etiquetas, pistas y opciones) están en inglés', async () => {
    const { page } = await open({ lng: 'en', path: '/product/nuevo', ready: 'section[data-th="sheet"]' });
    const sheet = page.locator('section[data-th="sheet"]');
    await visible(sheet).toBe(true);
    const text = (await sheet.innerText()).replace(/\s+/g, ' ');
    for (const english of ['Product details', 'Reference', 'Base price', 'VAT code', 'For sale']) expect(text).toContain(english);
    for (const spanish of ['Datos del producto', 'Referencia', 'Precio base', 'Código de IVA', 'A la venta']) expect(text).not.toContain(spanish);
    expect(await pageTexts(page)).not.toHaveLength(0);
  });
});
