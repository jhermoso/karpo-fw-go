// Ayuda guiada (PLAN §4.8): las anclas del mapa (`map.layout`, `map.breadcrumbs`, `map.legend`,
// `map.canvas`) existen en la pantalla y tienen su texto traducido en los dos idiomas.
import { waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import en from '../locales/en/map.json';
import es from '../locales/es/map.json';
import { installDemoBackend, installGeoFiles, renderMapView, resetFakeMap } from './test-utils';

vi.mock('maplibre-gl', async () => {
  const { fakeMaplibre } = await import('./test-utils');
  return { ...fakeMaplibre, default: fakeMaplibre };
});

installDemoBackend();
installGeoFiles();
beforeEach(() => resetFakeMap());

type Help = Record<string, { title: string; body: string }>;

describe('anclas de la ayuda guiada del mapa', () => {
  it('con el mapa a la vista: layout, breadcrumbs, canvas y legend, con texto en es y en', async () => {
    const view = await renderMapView();
    await view.map();
    await waitFor(() => expect(view.container.querySelector('[data-kx-view="map"]')).toHaveAttribute('data-kx-ready', 'true'), { timeout: 4000 });
    const found = [...view.container.querySelectorAll('[data-kx-help]')].map((el) => el.getAttribute('data-kx-help') ?? '').sort();
    expect(found).toEqual(['map.breadcrumbs', 'map.canvas', 'map.layout', 'map.legend']);
    for (const anchor of found) {
      const name = anchor.slice('map.'.length);
      for (const dictionary of [es, en]) {
        const entry = (dictionary.help as Help)[name];
        expect(entry?.title, `${anchor} sin título`).toBeTruthy();
        expect(entry?.body, `${anchor} sin cuerpo`).toBeTruthy();
      }
    }
  });

  it('en el listado no hay lienzo: la ayuda omite map.canvas', async () => {
    const view = await renderMapView({ search: '?view=map&layout=list' });
    await waitFor(() => expect(view.container.querySelector('[data-kx-view="map"]')).toHaveAttribute('data-kx-ready', 'true'), { timeout: 4000 });
    const found = [...view.container.querySelectorAll('[data-kx-help]')].map((el) => el.getAttribute('data-kx-help')).sort();
    expect(found).toEqual(['map.breadcrumbs', 'map.layout', 'map.legend']);
  });
});
