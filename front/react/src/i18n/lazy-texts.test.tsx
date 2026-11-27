// Ninguna pantalla enseña una clave cruda de i18n al entrar por URL directa (D-R37, M-11): con los idiomas por
// demanda, `core`, `ui`, `auth`, `errors`, `fields` y `help` se cargan al arrancar y `grid`, `detail`, `tree` y
// `map` con su vista (`shell/lazy.ts`, `shell/views.tsx`). La caché de recursos se vacía antes de cada prueba
// (en setup.ts se carga entera), de modo que sólo se ve lo que la aplicación pide por sí misma.
//
// Las vistas de la cuadrícula, el organigrama y el mapa se sustituyen por dobles que pintan un texto de SU
// espacio de nombres SIN valor por omisión (las reales llevan el castellano como reserva y ocultarían el
// fallo); la ficha de detalle es la real.
import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setMockOptions } from '../../demo/mocks/node';
import type { Row } from '../contracts';
import { API_BASE, demoManifest, facilityRows, installMockBackend, seededSession, testQueryClient } from '../detail/test-utils';
import { KarpoApp } from '../shell/KarpoApp';
import { preloadAllLocales, resetLoadedLocales } from './locales';

vi.mock('react-resizable-panels', async () => {
  const { createElement } = await import('react');
  return {
    Group: (props: { children?: unknown }) => createElement('div', { 'data-kx-test': 'group' }, props.children as never),
    Panel: (props: { children?: unknown; id?: string }) => createElement('div', { 'data-kx-test': 'panel', 'data-panel-id': props.id }, props.children as never),
    Separator: (props: Record<string, unknown>) => createElement('div', { role: 'separator', tabIndex: -1, 'aria-label': props['aria-label'] as string }),
    useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChange: () => undefined, onLayoutChanged: () => undefined }),
  };
});

vi.mock('../grid', async () => {
  const { createElement } = await import('react');
  const { useTranslation } = await import('react-i18next');
  return {
    GridView: function GridText() {
      const { t } = useTranslation('grid');
      return createElement('p', { 'data-testid': 'view-text' }, t('toolbar.add'));
    },
  };
});
vi.mock('../tree', async () => {
  const { createElement } = await import('react');
  const { useTranslation } = await import('react-i18next');
  return {
    TreeView: function TreeText() {
      const { t } = useTranslation('tree');
      return createElement('p', { 'data-testid': 'view-text' }, t('loading'));
    },
  };
});
vi.mock('../map', async () => {
  const { createElement } = await import('react');
  const { useTranslation } = await import('react-i18next');
  return {
    MapView: function MapText() {
      const { t } = useTranslation('map');
      return createElement('p', { 'data-testid': 'view-text' }, t('loading'));
    },
  };
});

installMockBackend();

/** Una clave de i18n sin traducir es una sola «palabra» con puntos: `header.creating`, `batch.save`. */
const RAW_KEY = /^(?:[a-z][A-Za-z]*:)?[a-z][A-Za-z]*(?:\.[A-Za-z]+)+$/;

/** Textos y atributos visibles del documento que parecen una clave cruda. */
function rawKeysIn(root: ParentNode): string[] {
  const found: string[] = [];
  const walker = document.createTreeWalker(root as Node, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = (node.textContent ?? '').trim();
    if (RAW_KEY.test(text)) found.push(text);
  }
  for (const element of (root as Element).querySelectorAll?.('[aria-label],[title],[placeholder]') ?? []) {
    for (const attribute of ['aria-label', 'title', 'placeholder']) {
      const value = (element.getAttribute(attribute) ?? '').trim();
      if (RAW_KEY.test(value)) found.push(`${attribute}=${value}`);
    }
  }
  return found;
}

describe('rawKeysIn (el detector de la prueba)', () => {
  it('encuentra claves crudas y deja pasar texto, números y nombres con punto', () => {
    const host = document.createElement('div');
    host.innerHTML = '<p>header.creating</p><button aria-label="detail:save.saved">x</button><p>Guardar</p><p>v1.2</p><p>Dr. Pérez</p><p>3.5</p>';
    expect(rawKeysIn(host)).toEqual(['header.creating', 'aria-label=detail:save.saved']);
  });
});

beforeEach(() => {
  resetLoadedLocales();
});

afterEach(async () => {
  await preloadAllLocales();
});

async function openApp(initialPath: string, locale: 'es' | 'en'): Promise<void> {
  setMockOptions({ requireAuth: false });
  const manifest = await demoManifest();
  render(
    <KarpoApp
      manifest={manifest}
      router="memory"
      initialPath={initialPath}
      apiBaseUrl={API_BASE}
      sessionStorage={seededSession()}
      queryClient={testQueryClient()}
      locale={locale}
      fetch={(async () => new Response('no encontrado', { status: 404 })) as typeof fetch}
    />,
  );
}

const ENGLISH_VIEW_TEXT: Record<string, string> = {
  '/product': 'New row',
  '/facility?view=tree': 'Loading the org chart',
  '/facility?view=map': 'Loading the map',
};

describe.each(Object.entries(ENGLISH_VIEW_TEXT))('entrando por URL directa en %s (en inglés)', (url, text) => {
  it('la vista llega con sus textos y la pantalla no enseña ninguna clave cruda', async () => {
    await openApp(url, 'en');
    expect(await screen.findByTestId('view-text')).toHaveTextContent(text);
    // El marco también está traducido (idioma activo, sin esperar a otro idioma).
    expect(await screen.findByRole('navigation', { name: 'Main menu' })).toBeInTheDocument();
    expect(rawKeysIn(document.body)).toEqual([]);
    expect(document.documentElement).toHaveAttribute('lang', 'en');
  });
});

describe('en castellano y con la ficha de detalle (otro espacio por demanda)', () => {
  it('/product en es: la vista y el marco, sin claves', async () => {
    await openApp('/product', 'es');
    expect(await screen.findByTestId('view-text')).toHaveTextContent('Nueva fila');
    expect(rawKeysIn(document.body)).toEqual([]);
  });

  it('la ficha de un registro (/facility/<id>) trae su espacio `detail` antes de montarse', async () => {
    const record = facilityRows()[0] as Row;
    await openApp(`/facility/${record.id}`, 'en');
    expect(await screen.findByRole('button', { name: /Edit/ })).toBeInTheDocument();
    await waitFor(() => expect(rawKeysIn(document.body)).toEqual([]));
  });
});
