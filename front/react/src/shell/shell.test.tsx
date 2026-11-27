// KarpoApp con router de memoria contra el backend falso de la demo y el manifiesto real. Las vistas de la
// cuadrícula, el organigrama y el mapa se sustituyen por dobles (las escriben otras tareas en paralelo).
import { QueryClient } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent, { type UserEvent } from '@testing-library/user-event';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import credentials from '../../demo/data/credenciales-demo.json';
import { mock, server, setMockOptions } from '../../demo/mocks/node';
import { facilityRows, API_BASE, MemoryStorage, demoManifest, installMockBackend, testQueryClient } from '../detail/test-utils';
import type { AppManifest, Row } from '../contracts';
import { assertTherosHooks } from '../theme';
import { KarpoApp } from './KarpoApp';
import { GridDouble, MapDouble, TreeDouble, gridMounts } from './test-doubles';

// En jsdom no hay geometría y el separador de paneles captura cualquier clic: se sustituye por contenedores simples.
vi.mock('react-resizable-panels', async () => {
  const { createElement } = await import('react');
  return {
    Group: (props: { children?: unknown }) => createElement('div', { 'data-kx-test': 'group' }, props.children as never),
    Panel: (props: { children?: unknown; id?: string; defaultSize?: string; minSize?: string }) =>
      createElement(
        'div',
        { 'data-kx-test': 'panel', 'data-panel-id': props.id, 'data-default-size': props.defaultSize, 'data-min-size': props.minSize },
        props.children as never,
      ),
    Separator: (props: Record<string, unknown>) => createElement('div', { role: 'separator', tabIndex: -1, 'aria-label': props['aria-label'] as string }),
    useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChange: () => undefined, onLayoutChanged: () => undefined }),
  };
});
vi.mock('../grid', () => ({ GridView: GridDouble }));
vi.mock('../tree', () => ({ TreeView: TreeDouble }));
vi.mock('../map', () => ({ MapView: MapDouble }));

installMockBackend();

const THEMES_DIR = path.resolve(import.meta.dirname, '../../demo/public/themes');

/** `fetch` de los manifiestos de tema: los reales generados por Theros Front. */
const themeFetch = (async (input: RequestInfo | URL) => {
  const url = new URL(String(input), 'http://localhost');
  const match = /\/themes\/([^/]+)\/manifest\.json$/.exec(url.pathname);
  if (!match) return new Response('no encontrado', { status: 404 });
  try {
    const body = readFileSync(path.join(THEMES_DIR, match[1] as string, 'manifest.json'), 'utf8');
    return new Response(body, { status: 200, headers: { 'content-type': 'application/json' } });
  } catch {
    return new Response('no encontrado', { status: 404 });
  }
}) as typeof fetch;

type CredentialName = 'demo' | 'lectura' | 'nuevo';

function credentialsOf(name: CredentialName): { username: string; password: string } {
  const user = credentials.users.find((u) => u.username === name);
  if (!user) throw new Error(`Sin credenciales de demostración para ${name}`);
  return { username: user.username, password: user.password };
}

interface AppOptions {
  initialPath?: string;
  theme?: string;
  manifest?: (base: AppManifest) => AppManifest;
}

async function renderApp(options: AppOptions = {}): Promise<{ user: UserEvent; manifest: AppManifest }> {
  setMockOptions({ requireAuth: true });
  const base = await demoManifest();
  const manifest = options.manifest ? options.manifest(base) : base;
  const withTheme = options.theme ? { ...manifest, theme: { ...manifest.theme, default: options.theme } } : manifest;
  const user = userEvent.setup();
  render(
    <KarpoApp
      manifest={withTheme}
      router="memory"
      initialPath={options.initialPath ?? '/'}
      apiBaseUrl={API_BASE}
      fetch={themeFetch}
      sessionStorage={new MemoryStorage()}
      queryClient={testQueryClient()}
      locale="es"
    />,
  );
  return { user, manifest: withTheme };
}

async function signIn(user: UserEvent, name: CredentialName = 'demo'): Promise<void> {
  const { username, password } = credentialsOf(name);
  await user.type(await screen.findByRole('textbox', { name: /Usuario/ }), username);
  await user.type(screen.getByLabelText(/Contraseña/), password);
  await user.click(screen.getByRole('button', { name: 'Entrar' }));
}

function menuLinks(): string[] {
  const nav = screen.getByRole('navigation', { name: 'Menú principal' });
  return within(nav)
    .getAllByRole('link')
    .map((link) => link.textContent?.trim() ?? '');
}

beforeEach(() => {
  gridMounts.count = 0;
});

describe('KarpoApp · entrada y rutas', () => {
  it('sin sesión pide entrar y, tras entrar, vuelve a la lista (login → menú → lista)', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    expect(await screen.findByRole('heading', { level: 1, name: 'Iniciar sesión' })).toBeInTheDocument();
    await signIn(user);
    expect(await screen.findByRole('heading', { name: 'Cuadrícula de facility' })).toBeInTheDocument();
    const current = screen.getByRole('link', { name: 'Instalaciones' });
    expect(current).toHaveAttribute('aria-current', 'page');
    expect(current).toHaveAttribute('data-th', 'nav-item');
    expect(screen.getByRole('banner')).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1, name: 'Instalaciones' })).toBeInTheDocument();
  });

  it('con router memory e initialPath /facility/<id>?mode=edit la ficha se abre en edición con el foco en el primer campo', async () => {
    const record = facilityRows()[0] as Row;
    const { user } = await renderApp({ initialPath: `/facility/${record.id}?mode=edit` });
    await signIn(user);
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    await waitFor(() => expect(name).toHaveFocus());
    expect(name).toHaveValue(String(record.name));
    // La lista sigue a la vista junto al panel.
    expect(screen.getByRole('heading', { name: 'Cuadrícula de facility' })).toBeInTheDocument();
  });

  it('el doble clic de una fila abre el detalle en edición sin desmontar la lista', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    const rows = await within(await screen.findByRole('region', { name: 'Doble de la cuadrícula de facility' })).findAllByRole('listitem');
    await user.dblClick(within(rows[0] as HTMLElement).getByRole('button'));
    await screen.findByRole('textbox', { name: 'Nombre' });
    expect(screen.getByRole('heading', { name: 'Cuadrícula de facility' })).toBeInTheDocument();
    expect(gridMounts.count).toBe(1);
    // Cerrar el detalle vuelve a la lista (y la lista sigue montada).
    await user.click(screen.getByRole('button', { name: 'Cerrar el detalle' }));
    await waitFor(() => expect(screen.queryByRole('textbox', { name: 'Nombre' })).not.toBeInTheDocument());
    expect(gridMounts.count).toBe(1);
  });

  it('el alta (/:entity/nuevo) abre el formulario del alta', async () => {
    const { user } = await renderApp({ initialPath: '/facility/nuevo' });
    await signIn(user);
    expect(await screen.findByRole('heading', { level: 2, name: 'Nueva instalación' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Crear' })).toBeInTheDocument();
  });

  it('un usuario que debe cambiar la contraseña sólo ve esa pantalla', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user, 'nuevo');
    expect(await screen.findByRole('heading', { level: 1, name: 'Cambiar la contraseña' })).toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: 'Menú principal' })).not.toBeInTheDocument();
  });

  it('una dirección que no existe enseña la página «no encontrada» y /403 la de sin acceso', async () => {
    const { user } = await renderApp({ initialPath: '/facility/uno/dos/tres' });
    await signIn(user);
    expect(await screen.findByRole('heading', { level: 1, name: 'No se encuentra la página' })).toBeInTheDocument();
  });

  it('una entidad desconocida es «no encontrada»', async () => {
    const { user } = await renderApp({ initialPath: '/no-existe' });
    await signIn(user);
    expect(await screen.findByRole('heading', { level: 1, name: 'No se encuentra la página' })).toBeInTheDocument();
  });

  it('la página 403 explica que no hay acceso y ofrece volver al inicio', async () => {
    const { user } = await renderApp({ initialPath: '/403' });
    await signIn(user);
    expect(await screen.findByRole('heading', { level: 1, name: 'No tienes acceso' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Volver al inicio' })).toHaveAttribute('href', '/');
  });
});

describe('KarpoApp · menú y permisos', () => {
  it('el menú oculta las entidades sin permiso de lectura según /api/auth/context', async () => {
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user, 'lectura');
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    const links = menuLinks();
    expect(links).toEqual(expect.arrayContaining(['Instalaciones', 'Productos', 'Países', 'Delimitaciones', 'Festivos']));
    for (const hidden of ['Proveedores', 'Transportistas', 'Activos', 'Centros de trabajo', 'Puestos y organigrama']) {
      expect(links).not.toContain(hidden);
    }
  });

  it('el administrador ve todas las pantallas del menú', async () => {
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user, 'demo');
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    expect(menuLinks()).toHaveLength(10);
  });

  it('entrar por la dirección de una entidad prohibida enseña «no tienes acceso»', async () => {
    const { user } = await renderApp({ initialPath: '/supplier' });
    await signIn(user, 'lectura');
    expect(await screen.findByRole('heading', { level: 1, name: 'No tienes acceso' })).toBeInTheDocument();
    expect(screen.getByText('Tu usuario no tiene permiso para ver «Proveedores».')).toBeInTheDocument();
  });

  it('el buscador del menú filtra con tolerancia a erratas y Enter abre la primera coincidencia', async () => {
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user);
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    const search = screen.getByRole('searchbox', { name: 'Buscar en el menú' });
    await user.type(search, 'provedor');
    expect(await screen.findByText('Resultados: 1')).toBeInTheDocument();
    const found = menuLinks();
    expect(found).toHaveLength(1);
    expect(found[0]).toMatch(/^Proveedores/);
    await user.keyboard('{Enter}');
    expect(await screen.findByRole('heading', { name: 'Cuadrícula de supplier' })).toBeInTheDocument();
    expect(search).toHaveValue('');
  });

  it('un 403 al leer la lista enseña el aviso con la opción de reintentar', async () => {
    server.use(
      http.post('*/api/v1/facilities/search', () =>
        HttpResponse.json({ status: 403, title: 'Forbidden' }, { status: 403, headers: { 'Content-Type': 'application/problem+json' } }),
      ),
    );
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('No tienes permiso para ver esta lista.');
    expect(within(alert).getByRole('button', { name: 'Reintentar' })).toBeInTheDocument();
  });
});

describe('KarpoApp · un solo h1 por pantalla (M-1, D-R32)', () => {
  const firstId = (): string => (facilityRows()[0] as Row).id;
  const ROUTES: { name: string; path: () => string; heading: string }[] = [
    { name: 'una entidad', path: () => '/facility', heading: 'Instalaciones' },
    { name: 'el inicio', path: () => '/', heading: 'Inicio' },
    { name: 'la página 403', path: () => '/403', heading: 'No tienes acceso' },
    { name: 'una dirección que no existe', path: () => '/no-existe', heading: 'No se encuentra la página' },
    { name: 'la ficha como página', path: () => `/facility/${firstId()}?page=1`, heading: String((facilityRows()[0] as Row).name) },
    { name: 'la ficha en el panel', path: () => `/facility/${firstId()}`, heading: 'Instalaciones' },
  ];

  it.each(ROUTES)('en $name hay exactamente un h1 y la barra superior no lleva ningún encabezado', async ({ path, heading }) => {
    const { user } = await renderApp({ initialPath: path() });
    await signIn(user);
    expect(await screen.findByRole('heading', { level: 1, name: heading })).toBeInTheDocument();
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    const topbar = document.querySelector<HTMLElement>('[data-kx-part="topbar"]') as HTMLElement;
    expect(within(topbar).queryAllByRole('heading')).toHaveLength(0);
  });

  it('la ficha como página usa h1 y en el panel h2 con el mismo id y aria-labelledby', async () => {
    const record = facilityRows()[0] as Row;
    const page = await renderApp({ initialPath: `/facility/${record.id}?page=1` });
    await signIn(page.user);
    const asPage = await screen.findByRole('heading', { level: 1, name: String(record.name) });
    expect(document.querySelector('section[data-th="sheet"]')).toHaveAttribute('aria-labelledby', asPage.id);
  });

  it('en el panel la cabecera de la entidad es el h1 y la ficha su h2', async () => {
    const record = facilityRows()[0] as Row;
    const { user } = await renderApp({ initialPath: `/facility/${record.id}` });
    await signIn(user);
    const title = await screen.findByRole('heading', { level: 2, name: String(record.name) });
    expect(document.querySelector('section[data-th="sheet"]')).toHaveAttribute('aria-labelledby', title.id);
    expect(screen.getByRole('heading', { level: 1, name: 'Instalaciones' })).toHaveAttribute('data-kx-part', 'entity-title');
  });

  it('la miga muestra la sección del menú y la entidad, con el separador espejado en RTL', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    await screen.findByRole('heading', { level: 1, name: 'Instalaciones' });
    const crumbs = within(document.querySelector<HTMLElement>('[data-kx-part="topbar"]') as HTMLElement).getByRole('navigation', { name: 'Ruta' });
    const items = within(crumbs).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent(/Organización/i);
    expect(items[1]).toHaveTextContent('Instalaciones');
    expect(items[1]).toHaveAttribute('aria-current', 'page');
    expect(items[1]?.querySelector('svg[data-th-icon]')).toHaveAttribute('data-kx-mirror', 'true');
    expect(crumbs).not.toHaveAttribute('aria-hidden');
  });

  it('sin entidad la barra enseña el nombre de la aplicación en un párrafo', async () => {
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user);
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    const banner = document.querySelector<HTMLElement>('[data-kx-part="topbar"]') as HTMLElement;
    expect(within(banner).queryByRole('navigation', { name: 'Ruta' })).not.toBeInTheDocument();
    const crumb = banner.querySelector('[data-kx-part="crumbs"]') as HTMLElement;
    expect(crumb.localName).toBe('p');
    expect(crumb).toHaveTextContent('Karpo');
  });

  it('con el detalle abierto la lista ocupa el 60 % por omisión y nunca baja del 40 %', async () => {
    const record = facilityRows()[0] as Row;
    const { user } = await renderApp({ initialPath: `/facility/${record.id}` });
    await signIn(user);
    await screen.findByRole('heading', { level: 2, name: String(record.name) });
    const list = document.querySelector('[data-panel-id="list"]') as HTMLElement;
    const detail = document.querySelector('[data-panel-id="detail"]') as HTMLElement;
    expect(list).toHaveAttribute('data-default-size', '60');
    expect(list).toHaveAttribute('data-min-size', '40');
    expect(detail).toHaveAttribute('data-default-size', '40');
  });

  it('sin detalle la lista ocupa todo el ancho', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    await screen.findByRole('heading', { level: 1, name: 'Instalaciones' });
    expect(document.querySelector('[data-panel-id="list"]')).toHaveAttribute('data-default-size', '100');
    expect(document.querySelector('[data-panel-id="detail"]')).toBeNull();
  });
});

describe('KarpoApp · barra superior', () => {
  it('la barra superior ya no lleva el selector de vista: vive en la cabecera de la entidad', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    await screen.findByRole('radiogroup', { name: 'Vista' });
    const banner = screen.getByRole('banner');
    expect(within(banner).queryByRole('radiogroup')).not.toBeInTheDocument();
    expect(within(banner).queryByRole('group', { name: 'Vista' })).not.toBeInTheDocument();
    expect(screen.getAllByRole('radiogroup')).toHaveLength(1);
  });

  it('una entidad con una sola vista no enseña el control de vista', async () => {
    const { user } = await renderApp({ initialPath: '/product' });
    await signIn(user);
    await screen.findByRole('heading', { name: 'Cuadrícula de product' });
    expect(screen.queryByRole('radiogroup', { name: 'Vista' })).not.toBeInTheDocument();
  });

  it('el selector de modo ofrece claro, oscuro y sistema con modern-flat', async () => {
    const { user } = await renderApp({ initialPath: '/', theme: 'modern-flat' });
    await signIn(user);
    await finishThemeLoad();
    await user.click(await screen.findByRole('button', { name: 'Modo de color' }));
    const menu = await screen.findByRole('menu');
    expect(within(menu).getAllByRole('menuitemradio').map((i) => i.textContent)).toEqual(['Claro', 'Oscuro', 'Según el sistema']);
    await user.click(within(menu).getByRole('menuitemradio', { name: 'Oscuro' }));
    await waitFor(() => expect(document.documentElement.getAttribute('data-th-mode')).toContain('dark'));
  });

  it('el selector de modo no ofrece oscuro con classic-1995', async () => {
    const { user } = await renderApp({ initialPath: '/', theme: 'classic-1995' });
    await signIn(user);
    await finishThemeLoad();
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    expect(screen.queryByRole('button', { name: 'Modo de color' })).not.toBeInTheDocument();
    expect(screen.queryByText('Oscuro')).not.toBeInTheDocument();
  });

  it('el menú de idioma cambia los textos y el atributo lang', async () => {
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user);
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    await user.click(screen.getByRole('button', { name: 'Idioma' }));
    await user.click(await screen.findByRole('menuitemradio', { name: 'English' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Home' })).toBeInTheDocument();
    expect(document.documentElement).toHaveAttribute('lang', 'en');
  });

  it('cambiar de idioma caduca las consultas de entidades para que el servidor las traduzca de nuevo', async () => {
    const invalidate = vi.spyOn(QueryClient.prototype, 'invalidateQueries');
    try {
      const { user } = await renderApp({ initialPath: '/' });
      await signIn(user);
      await screen.findByRole('heading', { level: 1, name: 'Inicio' });
      expect(invalidate).not.toHaveBeenCalledWith({ queryKey: ['entity'] });
      await user.click(screen.getByRole('button', { name: 'Idioma' }));
      await user.click(await screen.findByRole('menuitemradio', { name: 'English' }));
      await screen.findByRole('heading', { level: 1, name: 'Home' });
      await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ['entity'] }));
    } finally {
      invalidate.mockRestore();
    }
  });

  it('el menú de usuario cierra la sesión y vuelve a la entrada', async () => {
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user);
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    await user.click(screen.getByRole('button', { name: /Menú de/ }));
    await user.click(await screen.findByRole('menuitem', { name: 'Cerrar sesión' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Iniciar sesión' })).toBeInTheDocument();
  });
});

describe('KarpoApp · cabecera de la entidad', () => {
  it('el control de vista tiene una opción por vista y disposición, conserva la elección en la URL y se maneja con flechas', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    const group = await screen.findByRole('radiogroup', { name: 'Vista' });
    const radios = within(group).getAllByRole('radio');
    expect(radios.map((radio) => radio.textContent)).toEqual(['Tabla', 'Árbol', 'Diagrama', 'Mapa']);
    expect(within(group).getByRole('radio', { name: 'Tabla' })).toHaveAttribute('aria-checked', 'true');
    // Una sola parada de tabulación: sólo la opción elegida es tabulable.
    expect(radios.map((radio) => radio.tabIndex)).toEqual([0, -1, -1, -1]);

    await user.click(within(group).getByRole('radio', { name: 'Mapa' }));
    expect(await screen.findByRole('heading', { name: 'Mapa de facility' })).toBeInTheDocument();
    expect(screen.getByText('vista=map')).toBeInTheDocument();
    expect(within(group).getByRole('radio', { name: 'Mapa' })).toHaveAttribute('aria-checked', 'true');

    // El árbol y el diagrama son la misma vista con distinta disposición (`layout` en la URL).
    await user.click(within(group).getByRole('radio', { name: 'Diagrama' }));
    expect(await screen.findByRole('heading', { name: 'Organigrama de facility' })).toBeInTheDocument();
    expect(screen.getByText('layout=diagram')).toBeInTheDocument();
    await user.click(within(group).getByRole('radio', { name: 'Árbol' }));
    expect(await screen.findByText('layout=list')).toBeInTheDocument();
    expect(within(group).getByRole('radio', { name: 'Árbol' })).toHaveAttribute('aria-checked', 'true');

    // Flechas: eligen y mueven el foco; Inicio y Fin van a los extremos.
    within(group).getByRole('radio', { name: 'Árbol' }).focus();
    await user.keyboard('{ArrowRight}');
    expect(within(group).getByRole('radio', { name: 'Diagrama' })).toHaveFocus();
    expect(within(group).getByRole('radio', { name: 'Diagrama' })).toHaveAttribute('aria-checked', 'true');
    await user.keyboard('{End}');
    expect(within(group).getByRole('radio', { name: 'Mapa' })).toHaveFocus();
    await user.keyboard('{Home}');
    expect(within(group).getByRole('radio', { name: 'Tabla' })).toHaveFocus();
    expect(await screen.findByRole('heading', { name: 'Cuadrícula de facility' })).toBeInTheDocument();
  });

  it('enseña el recuento de lo que cuenta la vista, en plural y con el separador de miles del idioma', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    await screen.findByRole('heading', { name: 'Cuadrícula de facility' });
    const total = (mock.data.facilities as unknown as Row[]).length;
    const expected = new Intl.NumberFormat('es').format(total);
    const count = await waitFor(() => {
      const found = document.querySelector('[data-kx-part="entity-count"]');
      if (!found?.textContent) throw new Error('aún no hay recuento');
      return found;
    });
    expect(count.textContent).toBe(`${expected} instalaciones`);
  });

  it('«Nuevo» es la acción primaria y lleva al alta; el atajo N está documentado', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    await screen.findByRole('heading', { name: 'Cuadrícula de facility' });
    const create = await screen.findByRole('button', { name: 'Nuevo' });
    expect(create).toHaveAttribute('data-th-kind', 'primary');
    expect(create).toHaveAttribute('aria-keyshortcuts', 'N');
    await user.keyboard('?');
    const dialog = await screen.findByRole('dialog', { name: 'Atajos de teclado' });
    expect(within(dialog).getByText('Nuevo registro de la pantalla')).toBeInTheDocument();
    await user.keyboard('{Escape}');
    await user.click(create);
    expect(await screen.findByRole('heading', { level: 2, name: 'Nueva instalación' })).toBeInTheDocument();
  });

  it('el atajo N abre el alta cuando el foco no está en un campo de texto', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    await screen.findByRole('button', { name: 'Nuevo' });
    await user.keyboard('n');
    expect(await screen.findByRole('heading', { level: 2, name: 'Nueva instalación' })).toBeInTheDocument();
  });

  it('sin permiso de escritura no hay «Nuevo» ni atajo N', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user, 'lectura');
    await screen.findByRole('heading', { name: 'Cuadrícula de facility' });
    await screen.findByRole('radiogroup', { name: 'Vista' });
    expect(screen.queryByRole('button', { name: 'Nuevo' })).not.toBeInTheDocument();
    await user.keyboard('n');
    expect(screen.queryByRole('heading', { level: 2, name: 'Nueva instalación' })).not.toBeInTheDocument();
    await user.keyboard('?');
    const dialog = await screen.findByRole('dialog', { name: 'Atajos de teclado' });
    expect(within(dialog).queryByText('Nuevo registro de la pantalla')).not.toBeInTheDocument();
  });
});

describe('KarpoApp · marco compacto y estrecho', () => {
  /** jsdom no mide: el marco mide lo que se diga (el resto de elementos, nada). */
  function shellWidth(width: number): void {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      const w = this.hasAttribute('data-kx-shell') ? width : 0;
      return { x: 0, y: 0, top: 0, left: 0, right: w, bottom: 0, width: w, height: 0, toJSON: () => ({}) } as DOMRect;
    });
  }

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('a 360 px la barra superior se reduce a menú, miga, paleta, ayuda y «Más», sin teclas visibles', async () => {
    shellWidth(360);
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    const banner = await screen.findByRole('banner');
    await within(banner).findByRole('button', { name: 'Más' });
    // La barra no tiene encabezados: la miga compacta es sólo la entidad y no se anuncia (el `h1` de la cabecera ya lo dice).
    expect(within(banner).queryAllByRole('heading')).toHaveLength(0);
    const crumbs = banner.querySelector<HTMLElement>('[data-kx-part="crumbs"]') as HTMLElement;
    expect(crumbs).toHaveAttribute('aria-hidden', 'true');
    expect(crumbs).toHaveTextContent(/^Instalaciones$/);
    expect(within(banner).getByRole('button', { name: 'Buscar o ir a…' })).toBeInTheDocument();
    expect(within(banner).getByRole('button', { name: 'Ayuda' })).toBeInTheDocument();
    for (const gone of ['Tema', 'Modo de color', 'Idioma', /Menú de/]) {
      expect(within(banner).queryByRole('button', { name: gone })).not.toBeInTheDocument();
    }
    expect(document.querySelector('kbd')).toBeNull();
    expect(banner).toHaveAttribute('data-kx-compact');
  });

  it('«Más» se abre con el teclado, reúne tema, modo, idioma, atajos, clave y salida, y Esc devuelve el foco', async () => {
    shellWidth(360);
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user);
    const more = await screen.findByRole('button', { name: 'Más' });
    more.focus();
    await user.keyboard('{Enter}');
    const menu = await screen.findByRole('menu');
    expect(within(menu).getByRole('group', { name: 'Idioma' })).toBeInTheDocument();
    expect(within(menu).getByRole('menuitemradio', { name: 'English' })).toBeInTheDocument();
    expect(within(menu).getByRole('menuitem', { name: 'Atajos de teclado' })).toBeInTheDocument();
    expect(within(menu).getByRole('menuitem', { name: 'Cambiar la contraseña' })).toBeInTheDocument();
    expect(within(menu).getByRole('menuitem', { name: 'Cerrar sesión' })).toBeInTheDocument();
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('menu')).not.toBeInTheDocument());
    expect(more).toHaveFocus();
  });

  it('desde «Más» se cambia el idioma y se cierra la sesión', async () => {
    shellWidth(360);
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user);
    await user.click(await screen.findByRole('button', { name: 'Más' }));
    await user.click(await screen.findByRole('menuitemradio', { name: 'English' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Home' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'More' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Sign out' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in' })).toBeInTheDocument();
  });

  it('a 800 px la barra sigue completa pero el detalle ocupa la página entera (el menú ya es un cajón)', async () => {
    shellWidth(800);
    const record = facilityRows()[0] as Row;
    const { user } = await renderApp({ initialPath: `/facility/${record.id}` });
    await signIn(user);
    // Como página, la ficha es la dueña del `h1` (no hay cabecera de entidad).
    expect(await screen.findByRole('heading', { level: 1, name: String(record.name) })).toBeInTheDocument();
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    const banner = document.querySelector<HTMLElement>('[data-kx-part="topbar"]') as HTMLElement;
    expect(within(banner).getByRole('button', { name: 'Idioma' })).toBeInTheDocument();
    expect(within(banner).queryByRole('button', { name: 'Más' })).not.toBeInTheDocument();
    expect(document.querySelector('[data-kx-screen="entity"]')).toHaveAttribute('data-kx-layout', 'page');
    // Sin sitio para dos paneles: no hay lista ni botón de pasar de panel a página.
    expect(screen.queryByRole('heading', { name: 'Cuadrícula de facility' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /pantalla completa|página/i })).not.toBeInTheDocument();
  });

  it('con el marco ancho el detalle sigue junto a la lista', async () => {
    shellWidth(1400);
    const record = facilityRows()[0] as Row;
    const { user } = await renderApp({ initialPath: `/facility/${record.id}` });
    await signIn(user);
    expect(await screen.findByRole('heading', { level: 2, name: String(record.name) })).toBeInTheDocument();
    expect(document.querySelector('[data-kx-screen="entity"]')).toHaveAttribute('data-kx-layout', 'panel');
    expect(screen.getByRole('heading', { name: 'Cuadrícula de facility' })).toBeInTheDocument();
  });
});

describe('KarpoApp · paleta y atajos', () => {
  it('Ctrl+K abre la paleta y Enter navega a la entidad buscada', async () => {
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user);
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    await user.keyboard('{Control>}k{/Control}');
    const dialog = await screen.findByRole('dialog', { name: 'Paleta de órdenes' });
    const input = within(dialog).getByRole('combobox');
    await user.type(input, 'producto');
    expect(await within(dialog).findByRole('option', { name: /Ir a Productos/ })).toBeInTheDocument();
    await user.keyboard('{Enter}');
    expect(await screen.findByRole('heading', { name: 'Cuadrícula de product' })).toBeInTheDocument();
    expect(screen.queryByRole('dialog', { name: 'Paleta de órdenes' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Productos' })).toHaveAttribute('aria-current', 'page');
  });

  it('la paleta ofrece crear, cambiar de tema, de modo y de idioma, y órdenes de la selección', async () => {
    const carrier = (mock.data.carriers as unknown as Row[])[0] as Row;
    const { user } = await renderApp({ initialPath: '/carrier' });
    await signIn(user);
    const choice = await screen.findByRole('button', { name: new RegExp(String(carrier.name ?? carrier.id)) });
    await user.click(choice);
    await user.keyboard('{Control>}k{/Control}');
    const dialog = await screen.findByRole('dialog', { name: 'Paleta de órdenes' });
    const options = within(dialog)
      .getAllByRole('option')
      .map((o) => o.textContent ?? '');
    expect(options.some((text) => text.includes('Nuevo: Transportista'))).toBe(true);
    expect(options.some((text) => text.includes('Tema: Clásico 1995'))).toBe(true);
    expect(options.some((text) => text.includes('Idioma: English'))).toBe(true);
    expect(options.some((text) => text.includes('Modo: Oscuro'))).toBe(false); // el tema aún no está cargado: sólo claro
    expect(options.some((text) => /Bloquear|Desbloquear/.test(text))).toBe(true);
  });

  it('Shift+? abre el panel de atajos con los registrados, agrupados por ámbito', async () => {
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user);
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    await user.keyboard('?');
    const dialog = await screen.findByRole('dialog', { name: 'Atajos de teclado' });
    expect(within(dialog).getByText('Abrir la paleta de órdenes')).toBeInTheDocument();
    expect(within(dialog).getByText('Mostrar los atajos de teclado')).toBeInTheDocument();
    expect(within(dialog).getByRole('heading', { name: 'En toda la aplicación' })).toBeInTheDocument();
  });

  it('el panel de atajos incluye los de la ficha cuando está abierta', async () => {
    const record = facilityRows()[0] as Row;
    const { user } = await renderApp({ initialPath: `/facility/${record.id}` });
    await signIn(user);
    await screen.findByRole('heading', { level: 2, name: String(record.name) });
    await user.keyboard('?');
    const dialog = await screen.findByRole('dialog', { name: 'Atajos de teclado' });
    expect(within(dialog).getByText('Guardar la ficha')).toBeInTheDocument();
    expect(within(dialog).getByText('Ir al registro siguiente')).toBeInTheDocument();
    expect(within(dialog).getByRole('heading', { name: 'En la ficha' })).toBeInTheDocument();
  });
});

describe('KarpoApp · ayuda guiada y permisos de escritura', () => {
  it('el botón de ayuda lanza el recorrido por las anclas del marco con textos traducidos', async () => {
    const { user } = await renderApp({ initialPath: '/' });
    await signIn(user);
    await screen.findByRole('heading', { level: 1, name: 'Inicio' });
    await user.click(screen.getByRole('button', { name: 'Ayuda' }));
    await waitFor(() => expect(document.querySelector('.driver-popover-title')).not.toBeNull());
    expect(document.querySelector('.driver-popover-title')?.textContent).toBe('Menú');
    expect(document.querySelector('.driver-popover-description')?.textContent).toContain('pantallas a las que tienes acceso');
    // Las cinco anclas del marco existen y llevan el espacio de nombres core.
    const anchors = [...document.querySelectorAll('[data-kx-help^="core."]')].map((el) => el.getAttribute('data-kx-help'));
    expect(anchors).toEqual(expect.arrayContaining(['core.menu', 'core.search', 'core.palette', 'core.user']));
  });

  it('en una entidad con varias vistas el control de vista y «Nuevo» llevan su ancla de ayuda', async () => {
    const { user } = await renderApp({ initialPath: '/facility' });
    await signIn(user);
    await screen.findByRole('radiogroup', { name: 'Vista' });
    expect(document.querySelector('[data-kx-help="core.view-switch"]')).not.toBeNull();
    expect(document.querySelector('[data-kx-help="core.new"]')).not.toBeNull();
  });

  it('sin permiso de escritura, ?mode=edit abre la ficha en lectura y avisa del motivo', async () => {
    const record = facilityRows()[0] as Row;
    const { user } = await renderApp({ initialPath: `/facility/${record.id}?mode=edit` });
    await signIn(user, 'lectura');
    expect(await screen.findByRole('heading', { level: 2, name: String(record.name) })).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: 'Nombre' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Editar' })).toHaveAttribute('aria-disabled', 'true');
    expect(screen.getAllByText('No tienes permiso para modificar este registro.').length).toBeGreaterThan(0);
  });
});

describe('KarpoApp · cambios sin guardar y contrato', () => {
  it('salir de la ficha con cambios pregunta antes y permite seguir editando', async () => {
    const record = facilityRows()[0] as Row;
    const { user } = await renderApp({ initialPath: `/facility/${record.id}?mode=edit` });
    await signIn(user);
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    await user.type(name, ' modificado');
    await user.click(screen.getByRole('link', { name: 'Productos' }));
    const dialog = await screen.findByRole('dialog', { name: 'Cambios sin guardar' });
    await user.click(within(dialog).getByRole('button', { name: 'Seguir editando' }));
    expect((screen.getByRole('textbox', { name: 'Nombre' }) as HTMLInputElement).value).toContain('modificado');
    await user.click(screen.getByRole('link', { name: 'Productos' }));
    const again = await screen.findByRole('dialog', { name: 'Cambios sin guardar' });
    await user.click(within(again).getByRole('button', { name: 'Descartar los cambios' }));
    expect(await screen.findByRole('heading', { name: 'Cuadrícula de product' })).toBeInTheDocument();
  });

  it('el DOM del marco y de la ficha cumple el contrato de ganchos de Theros', async () => {
    const record = facilityRows()[0] as Row;
    const { user } = await renderApp({ initialPath: `/facility/${record.id}?mode=edit` });
    await signIn(user);
    await screen.findByRole('textbox', { name: 'Nombre' });
    assertTherosHooks(document.body);
  });
});

/** jsdom no descarga hojas: la prueba dispara el `load` que haría el navegador y espera al manifiesto. */
async function finishThemeLoad(): Promise<void> {
  const link = await waitFor(() => {
    const found = document.head.querySelector<HTMLLinkElement>('link[data-kx-theme]');
    if (!found) throw new Error('el tema aún no está enlazado');
    return found;
  });
  link.dispatchEvent(new Event('load'));
  await waitFor(() => {
    const mode = document.documentElement.getAttribute('data-th-mode');
    void mode;
  });
  await new Promise((resolve) => setTimeout(resolve, 50));
}
