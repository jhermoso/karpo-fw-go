// Ayuda guiada (PLAN §4.8): cada ancla `data-kx-help="tree.<nombre>"` de la vista tiene su texto
// traducido (`tree:help.<nombre>.title|body`) en los dos idiomas.
import { screen, waitFor, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { HelpButton, HelpProvider } from '../help';
import { describe, expect, it } from 'vitest';
import en from '../locales/en/tree.json';
import es from '../locales/es/tree.json';
import { installDemoBackend, renderEntityView } from './test-utils';
import { TreeView } from './TreeView';

installDemoBackend();

function anchors(root: ParentNode): string[] {
  return [...root.querySelectorAll('[data-kx-help]')].map((el) => el.getAttribute('data-kx-help') ?? '');
}

type Help = Record<string, { title: string; body: string }>;

describe('anclas de la ayuda guiada del organigrama', () => {
  for (const layout of ['list', 'diagram'] as const) {
    it(`en ${layout}: search y node${layout === 'diagram' ? ', listlink y zoom' : ''}, con texto en es y en`, async () => {
      const view = await renderEntityView(TreeView, { entity: 'facility', search: `?view=tree&layout=${layout}` });
      await waitFor(() => expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-ready', 'true'), { timeout: 5000 });
      // La disposición la cambia la cabecera de la entidad (M-6): la vista ya no lleva su propio interruptor.
      expect(screen.queryByRole('group', { name: /Disposición/ })).toBeNull();
      const found = anchors(view.container).sort();
      expect(found).toEqual(layout === 'list' ? ['tree.node', 'tree.search'] : ['tree.listlink', 'tree.node', 'tree.search', 'tree.zoom']);
      // Cada ancla es única (el recorrido toma el primer elemento visible de cada una).
      expect(new Set(found).size).toBe(found.length);
      for (const anchor of found) {
        const name = anchor.slice('tree.'.length);
        for (const dictionary of [es, en]) {
          const entry = (dictionary.help as Help)[name];
          expect(entry?.title, `${anchor} sin título`).toBeTruthy();
          expect(entry?.body, `${anchor} sin cuerpo`).toBeTruthy();
        }
      }
    });
  }
});

describe('recorrido guiado con las anclas del organigrama', () => {
  it('el botón de ayuda recorre las anclas visibles con sus textos traducidos', async () => {
    const wrap = (children: ReactElement) => (
      <HelpProvider>
        <HelpButton />
        {children}
      </HelpProvider>
    );
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree', wrap });
    await waitFor(() => expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-ready', 'true'));
    await view.user.click(screen.getByRole('button', { name: /Ayuda/ }));
    const popover = () => document.querySelector<HTMLElement>('.driver-popover');
    await waitFor(() => expect(popover()).not.toBeNull());
    // Paso 1: el propio botón de ayuda; después, las dos anclas del listado en el orden del DOM.
    expect(popover()).toHaveTextContent('Paso 1 de 3');
    const next = () => view.user.click(within(popover()!).getByRole('button', { name: 'Siguiente' }));
    await next();
    await waitFor(() => expect(popover()).toHaveTextContent('Buscar en el organigrama'));
    await next();
    await waitFor(() => expect(popover()).toHaveTextContent('Elementos del organigrama'));
  });
});
