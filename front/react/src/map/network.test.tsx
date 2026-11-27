// Soberanía (PLAN §4.12): el mapa no pide nada fuera del origen. La prueba envuelve `fetch`, falla si
// alguna URL sale del origen y comprueba que ni el estilo ni el DOM referencian recursos de terceros:
// ni estilos remotos, ni teselas, ni glifos, ni sprites.
import { act, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { EntityConfig } from '../contracts';
import { collectUrls } from './style';
import { FakeMap, installDemoBackend, installGeoFiles, renderMapView, resetFakeMap } from './test-utils';

vi.mock('maplibre-gl', async () => {
  const { fakeMaplibre } = await import('./test-utils');
  return { ...fakeMaplibre, default: fakeMaplibre };
});

installDemoBackend();
installGeoFiles();

const ORIGIN = window.location.origin;
let requested: string[] = [];

beforeEach(() => {
  resetFakeMap();
  requested = [];
  const real = globalThis.fetch;
  vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    const url = new URL(input instanceof Request ? input.url : String(input), ORIGIN);
    requested.push(url.href);
    if (url.origin !== window.location.origin && url.origin !== 'http://localhost') {
      throw new Error(`Petición fuera del origen: ${url.href}`);
    }
    return real(input, init);
  });
});

afterEach(() => vi.restoreAllMocks());

function externalReferences(root: ParentNode): string[] {
  const out: string[] = [];
  for (const element of root.querySelectorAll('[src], [href], link[rel=stylesheet]')) {
    const value = element.getAttribute('src') ?? element.getAttribute('href') ?? '';
    if (/^(https?:)?\/\//i.test(value) && new URL(value, ORIGIN).origin !== ORIGIN) out.push(value);
  }
  return out;
}

const withSource = (source: string) => (config: EntityConfig): EntityConfig => ({
  ...config,
  views: config.views.map((view) =>
    view.kind === 'map' ? { ...view, levels: view.levels.map((level) => ({ ...level, source })) } : view,
  ),
});

describe('el mapa sólo habla con su origen', () => {
  for (const source of ['demo-geojson', 'demo-pmtiles'] as const) {
    it(`${source}: peticiones, estilo y DOM sin nada de terceros al recorrer los niveles`, async () => {
      FakeMap.defaultSourceFeatures = [
        {
          properties: { code: 'ES', name: 'Spain', level: 'country', parentCode: '', hasChildren: 'true' },
          geometry: { type: 'Polygon', coordinates: [[[-9, 36], [3, 36], [3, 44], [-9, 44], [-9, 36]]] },
        },
      ];
      const view = await renderMapView({ configure: withSource(source) });
      const map = await view.map();
      await waitFor(() => expect(view.container.querySelector('[data-kx-view="map"]')).toHaveAttribute('data-kx-ready', 'true'), { timeout: 4000 });

      act(() => map.click('ES'));
      await waitFor(() => expect(view.params().get('level')).toBe('country'));
      await view.user.click(screen.getByRole('button', { name: 'Ver como tabla' }));
      await view.user.click(screen.getByRole('button', { name: 'Ver como mapa' }));
      act(() => view.theme.fire());
      await new Promise((resolve) => setTimeout(resolve, 50));

      expect(requested.length).toBeGreaterThan(0);
      expect(requested.filter((url) => new URL(url).origin !== ORIGIN && new URL(url).origin !== 'http://localhost')).toEqual([]);
      for (const style of [...FakeMap.instances.flatMap((m) => [m.style, ...m.setStyleCalls])]) {
        const urls = collectUrls(style).map((url) => url.replace(/^pmtiles:\/\//, ''));
        expect(urls.filter((url) => new URL(url).origin !== ORIGIN)).toEqual([]);
        expect(style).not.toHaveProperty('sprite');
        expect(style).not.toHaveProperty('glyphs');
      }
      expect(externalReferences(document)).toEqual([]);
    });
  }
});
