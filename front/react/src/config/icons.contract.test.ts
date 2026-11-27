// Contrato de iconos de la configuración (B-1, D-R20): ni el manifiesto de la demo, ni sus entidades, ni las
// 8 configuraciones de Go usan un nombre fuera de `ICON_NAMES`; y un icono desconocido produce un
// `ConfigWarning` `warn` con su ruta (en el menú y en la entidad).
import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { isIconName, type ConfigWarning } from '../contracts';
import { allReal } from './__fixtures__/real-configs';
import { DEMO_CONFIG_DIR, DEMO_CONFIG_BASE, demoDiskFetch, loadDemoReport } from './__fixtures__/demo-config';
import { entityIconWarnings, menuIconWarnings } from './icons';
import { loadManifestWithReport } from './manifest';
import { normalizeEntityConfig } from './normalize';

const iconWarnings = (list: ConfigWarning[]) => list.filter((w) => /^icono `.+` desconocido/.test(w.message));

/** Todo valor de una propiedad `icon` de un JSON de configuración, con su ruta. */
function rawIcons(value: unknown, path = ''): { path: string; icon: string }[] {
  if (Array.isArray(value)) return value.flatMap((item, i) => rawIcons(item, `${path}[${i}]`));
  if (typeof value !== 'object' || value === null) return [];
  return Object.entries(value).flatMap(([key, child]) => {
    const here = path ? `${path}.${key}` : key;
    return key === 'icon' && typeof child === 'string' ? [{ path: here, icon: child }] : rawIcons(child, here);
  });
}

describe('los iconos de la configuración de la demo y de Go son nombres del vocabulario', () => {
  it('manifest.json: todos los `icon` (menú y entidades) están en ICON_NAMES', () => {
    const manifest = JSON.parse(readFileSync(resolve(DEMO_CONFIG_DIR, 'manifest.json'), 'utf8')) as unknown;
    const icons = rawIcons(manifest);
    expect(icons.length).toBeGreaterThanOrEqual(15);
    for (const { path, icon } of icons) expect(isIconName(icon), `${path}: ${icon}`).toBe(true);
  });

  it('las 4 entidades propias de la demo y las 8 de Go no tienen un icono fuera de la lista', () => {
    const own = readdirSync(resolve(DEMO_CONFIG_DIR, 'entities')).map((f) =>
      JSON.parse(readFileSync(resolve(DEMO_CONFIG_DIR, 'entities', f), 'utf8')),
    );
    expect(own).toHaveLength(4);
    for (const json of [...own, ...allReal().map((r) => r.json)]) {
      for (const { path, icon } of rawIcons(json)) expect(isIconName(icon), `${json.id}.${path}: ${icon}`).toBe(true);
    }
  });

  it('cargar el manifiesto de la demo no produce ningún aviso de icono (menú ni entidades)', async () => {
    const { manifest, warnings } = await loadDemoReport();
    expect(Object.keys(manifest.entities)).toHaveLength(12);
    expect(manifest.menu.length).toBeGreaterThanOrEqual(3);
    expect(iconWarnings(warnings)).toEqual([]);
    for (const entity of Object.values(manifest.entities)) expect(iconWarnings(entity.warnings), entity.id).toEqual([]);
    expect(menuIconWarnings(manifest.menu, manifest.entities)).toEqual([]);
  });

  it('las 8 configuraciones de Go, normalizadas tal cual, no producen avisos de icono', () => {
    for (const { path, json } of allReal()) {
      const config = normalizeEntityConfig(json);
      expect(iconWarnings(config.warnings), path).toEqual([]);
      expect(entityIconWarnings(config), path).toEqual([]);
    }
  });
});

describe('un icono desconocido produce un aviso `warn` con ruta', () => {
  it('en un manifiesto con `no-such-icon` en el menú', async () => {
    const manifest = JSON.parse(readFileSync(resolve(DEMO_CONFIG_DIR, 'manifest.json'), 'utf8')) as {
      menu: { children?: { icon?: string }[] }[];
    };
    manifest.menu[2]!.children![1]!.icon = 'no-such-icon';
    const fetcher = (async (input: RequestInfo | URL) =>
      String(input) === `${DEMO_CONFIG_BASE}manifest.json`
        ? new Response(JSON.stringify(manifest), { status: 200 })
        : demoDiskFetch(input)) as typeof fetch;
    const { warnings } = await loadManifestWithReport(`${DEMO_CONFIG_BASE}manifest.json`, { fetch: fetcher, logWarnings: false });
    expect(iconWarnings(warnings)).toEqual([
      { path: 'menu[2].children[1].icon', message: 'icono `no-such-icon` desconocido: se usa un círculo', severity: 'warn' },
    ]);
  });

  it('en una entidad (la entidad, sus opciones y sus órdenes)', () => {
    const json = allReal().find((r) => r.json.id === 'carrier')!.json;
    const config = normalizeEntityConfig(json, { icon: 'no-such-icon', commands: [{ id: 'x', label: 'X', icon: 'also-missing', endpoint: '/x/{id}/x' }] });
    const found = iconWarnings(config.warnings);
    expect(found.map((w) => w.path)).toEqual(['commands[0].icon', 'icon']);
    expect(found.every((w) => w.severity === 'warn')).toBe(true);
  });
});
