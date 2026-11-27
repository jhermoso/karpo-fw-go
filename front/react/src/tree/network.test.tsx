// Soberanía (PLAN §4.12): el organigrama no pide nada fuera del origen. La prueba envuelve `fetch`,
// falla si alguna URL sale del origen y comprueba que el DOM no referencia recursos de terceros.
import { screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { installDemoBackend, renderEntityView } from './test-utils';
import { TreeView } from './TreeView';

installDemoBackend();

const ORIGIN = 'http://localhost';
let requested: string[] = [];

beforeEach(() => {
  requested = [];
  const real = globalThis.fetch;
  vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    const url = new URL(input instanceof Request ? input.url : String(input), ORIGIN);
    requested.push(url.href);
    if (url.origin !== ORIGIN) throw new Error(`Petición fuera del origen: ${url.href}`);
    return real(input, init);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

function externalReferences(root: ParentNode): string[] {
  const out: string[] = [];
  for (const element of root.querySelectorAll('[src], [href], link[rel=stylesheet]')) {
    const value = element.getAttribute('src') ?? element.getAttribute('href') ?? '';
    if (/^(https?:)?\/\//i.test(value) && new URL(value, ORIGIN).origin !== ORIGIN) out.push(value);
  }
  return out;
}

describe('el organigrama sólo habla con su origen', () => {
  for (const [entity, layout] of [
    ['facility', 'list'],
    ['facility', 'diagram'],
    ['position', 'list'],
    ['position', 'diagram'],
  ] as const) {
    it(`${entity} en ${layout}: todas las peticiones son del origen y no hay recursos de terceros`, async () => {
      const view = await renderEntityView(TreeView, { entity, search: `?view=tree&layout=${layout}` });
      await waitFor(() => expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-ready', 'true'));
      // Se interactúa con él: plegar, buscar y cambiar de disposición (como hace la cabecera de la entidad).
      await view.user.click(screen.getByRole('button', { name: 'Plegar todo' }));
      await view.user.type(screen.getByRole('searchbox'), 'a');
      view.goto(`/${entity}?view=tree&layout=${layout === 'list' ? 'diagram' : 'list'}`);
      await new Promise((resolve) => setTimeout(resolve, 50));

      expect(requested.length).toBeGreaterThan(0);
      expect(requested.filter((url) => new URL(url).origin !== ORIGIN)).toEqual([]);
      expect(externalReferences(document)).toEqual([]);
    });
  }
});
