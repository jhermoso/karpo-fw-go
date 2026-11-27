// ThemeProvider con los manifiestos REALES de demo/public/themes (generados por Theros Front).
import { act, render, screen } from '@testing-library/react';
import { useEffect } from 'react';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ThemeApi } from '../contracts';
import { EXPECTED_DICTIONARY_HASH } from './contract';
import { ThemeProvider, clampMode, useTheme } from './ThemeProvider';

const THEMES_DIR = path.resolve(__dirname, '../../demo/public/themes');

function manifestOf(name: string): unknown {
  return JSON.parse(readFileSync(path.join(THEMES_DIR, name, 'manifest.json'), 'utf8'));
}

/** Servidor de manifiestos: los reales, o los que la prueba sustituya. */
function fakeFetch(overrides: Record<string, unknown> = {}) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    const match = /\/([^/]+)\/manifest\.json$/.exec(url);
    const name = match?.[1] ?? '';
    const body = overrides[name] ?? manifestOf(name);
    return { ok: true, status: 200, json: async () => body } as Response;
  }) as unknown as typeof fetch;
}

let theme: ThemeApi;
function Probe() {
  const api = useTheme();
  useEffect(() => {
    theme = api;
  });
  return <output data-testid="state">{`${api.info.name}|${api.mode}|${api.info.modes.join(',')}`}</output>;
}

function links(): HTMLLinkElement[] {
  return Array.from(document.head.querySelectorAll<HTMLLinkElement>('link[data-kx-theme]'));
}

/** jsdom no descarga hojas: la prueba dispara el `load` que haría el navegador. */
async function finishLoading(link?: HTMLLinkElement) {
  await act(async () => {
    (link ?? links().at(-1))?.dispatchEvent(new Event('load'));
    await Promise.resolve();
  });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

async function mount(props: Parameters<typeof ThemeProvider>[0] = {}) {
  const result = render(
    <ThemeProvider fetcher={fakeFetch()} {...props}>
      <Probe />
    </ThemeProvider>,
  );
  await finishLoading();
  return result;
}

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-th-mode');
});

afterEach(() => {
  vi.restoreAllMocks();
  document.head.querySelectorAll('link[data-kx-theme]').forEach((l) => l.remove());
});

describe('ThemeProvider: hoja del tema', () => {
  it('inserta el link a {origen}{tema}/index.css con el origen configurado', async () => {
    await mount({ origin: 'https://static.example.test/temas', defaultTheme: 'classic-1995' });
    expect(links()).toHaveLength(1);
    expect(links()[0]?.getAttribute('href')).toBe('https://static.example.test/temas/classic-1995/index.css');
    expect(links()[0]?.rel).toBe('stylesheet');
  });

  it('usa /themes/ y modern-flat por defecto y retira el link al desmontar', async () => {
    const { unmount } = await mount();
    expect(links()[0]?.getAttribute('href')).toBe('/themes/modern-flat/index.css');
    unmount();
    expect(links()).toHaveLength(0);
  });

  it('no duplica el link con React StrictMode', async () => {
    const { StrictMode } = await import('react');
    render(
      <StrictMode>
        <ThemeProvider fetcher={fakeFetch()}>
          <Probe />
        </ThemeProvider>
      </StrictMode>,
    );
    await finishLoading();
    expect(links()).toHaveLength(1);
  });

  it('adopta un link que ya está en la página en lugar de duplicarlo', async () => {
    const existing = document.createElement('link');
    existing.rel = 'stylesheet';
    existing.setAttribute('data-kx-theme', 'modern-flat');
    existing.setAttribute('href', '/themes/modern-flat/index.css');
    document.head.appendChild(existing);
    const { unmount } = await mount();
    expect(links()).toEqual([existing]);
    unmount();
    expect(links()).toEqual([existing]); // no es nuestro: no se retira
  });

  it('declara la hoja como no lista hasta que carga y registra el error si falla', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    render(
      <ThemeProvider fetcher={fakeFetch()} defaultTheme="modern-flat">
        <Probe />
      </ThemeProvider>,
    );
    // provisional: sólo claro hasta conocer el manifiesto
    expect(screen.getByTestId('state').textContent).toBe('modern-flat|system|light');
    await act(async () => {
      links()[0]?.dispatchEvent(new Event('error'));
      await Promise.resolve();
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(error).toHaveBeenCalled();
  });
});

describe('ThemeProvider: modos según el manifiesto real', () => {
  it('classic-1995 sólo ofrece claro', async () => {
    await mount({ defaultTheme: 'classic-1995' });
    expect(theme.info.modes).toEqual(['light']);
    expect(theme.info.name).toBe('classic-1995');
    expect(theme.mode).toBe('light');
    expect(theme.info.iconStyle).toBe('outline');
  });

  it('classic-1995 ignora setMode("dark") y avisa', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    await mount({ defaultTheme: 'classic-1995' });
    act(() => theme.setMode('dark'));
    expect(theme.mode).toBe('light');
    expect(document.documentElement.getAttribute('data-th-mode')).toBe('light');
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('no tiene el modo «dark»'));
  });

  it('modern-flat ofrece claro, oscuro y sistema', async () => {
    await mount();
    expect(theme.info.modes).toEqual(['light', 'dark', 'system']);
    expect(theme.info.highContrast).toBe(true);
    expect(theme.info.dictionaryHash).toBe(EXPECTED_DICTIONARY_HASH);
  });

  it('pone data-th-mode en <html> con claro y oscuro y lo quita con sistema', async () => {
    await mount();
    const root = document.documentElement;
    expect(root.hasAttribute('data-th-mode')).toBe(false); // sistema: manda el SO
    act(() => theme.setMode('dark'));
    expect(root.getAttribute('data-th-mode')).toBe('dark');
    expect(theme.mode).toBe('dark');
    act(() => theme.setMode('light'));
    expect(root.getAttribute('data-th-mode')).toBe('light');
    act(() => theme.setMode('system'));
    expect(root.hasAttribute('data-th-mode')).toBe(false);
    expect(theme.mode).toBe('system');
  });

  it('conserva otros modos del atributo (alto contraste) al cambiar claro/oscuro', async () => {
    document.documentElement.setAttribute('data-th-mode', 'high-contrast');
    await mount();
    act(() => theme.setMode('dark'));
    expect(document.documentElement.getAttribute('data-th-mode')).toBe('dark high-contrast');
  });

  it('glossy-2003 mide el estilo de icono del manifiesto (relleno)', async () => {
    await mount({ defaultTheme: 'glossy-2003' });
    expect(theme.info.iconStyle).toBe('filled');
  });

  it('souq (árabe) informa su dirección y su idioma, pero el tema no cambia lang ni dir', async () => {
    await mount({ defaultTheme: 'souq' });
    expect((theme.info as { direction?: string }).direction).toBe('rtl');
    expect(document.documentElement.getAttribute('dir')).toBeNull();
  });

  it('clampMode elige el modo más cercano que el tema tiene', () => {
    expect(clampMode('dark', ['light'])).toBe('light');
    expect(clampMode('system', ['light'])).toBe('light');
    expect(clampMode('light', ['dark'])).toBe('dark');
    expect(clampMode('system', ['light', 'dark', 'system'])).toBe('system');
  });
});

describe('ThemeProvider: onChange', () => {
  it('avisa al cambiar de modo y deja de avisar tras darse de baja', async () => {
    await mount();
    const callback = vi.fn();
    const off = theme.onChange(callback);
    act(() => theme.setMode('dark'));
    expect(callback).toHaveBeenCalledTimes(1);
    act(() => theme.setMode('light'));
    expect(callback).toHaveBeenCalledTimes(2);
    off();
    act(() => theme.setMode('dark'));
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('avisa si otro código cambia data-th-mode en <html>', async () => {
    await mount();
    const callback = vi.fn();
    theme.onChange(callback);
    await act(async () => {
      document.documentElement.setAttribute('data-th-mode', 'dark');
      await Promise.resolve();
    });
    expect(callback).toHaveBeenCalled();
    expect(theme.mode).toBe('dark');
  });

  it('avisa cuando cambian prefers-color-scheme, prefers-contrast o forced-colors', async () => {
    const listeners: Record<string, () => void> = {};
    vi.spyOn(window, 'matchMedia').mockImplementation(
      (query: string) =>
        ({
          matches: false,
          media: query,
          addEventListener: (_: string, cb: () => void) => {
            listeners[query] = cb;
          },
          removeEventListener: () => undefined,
        }) as unknown as MediaQueryList,
    );
    await mount();
    const callback = vi.fn();
    theme.onChange(callback);
    expect(Object.keys(listeners).sort()).toEqual([
      '(forced-colors: active)',
      '(prefers-color-scheme: dark)',
      '(prefers-contrast: more)',
    ]);
    for (const query of Object.keys(listeners)) listeners[query]?.();
    expect(callback).toHaveBeenCalledTimes(3);
  });

  it('un suscriptor que falla no impide avisar a los demás', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    await mount();
    const second = vi.fn();
    theme.onChange(() => {
      throw new Error('boom');
    });
    theme.onChange(second);
    act(() => theme.setMode('dark'));
    expect(second).toHaveBeenCalled();
    expect(error).toHaveBeenCalled();
  });

  it('avisa al terminar de cargar el tema inicial', async () => {
    const callback = vi.fn();
    function Subscriber() {
      const api = useTheme();
      // se suscribe en el efecto, antes de que llegue el manifiesto
      return <button onClick={() => api.onChange(callback)}>subscribe</button>;
    }
    render(
      <ThemeProvider fetcher={fakeFetch()}>
        <Subscriber />
        <Probe />
      </ThemeProvider>,
    );
    act(() => screen.getByRole('button', { name: 'subscribe' }).click());
    await finishLoading();
    expect(callback).toHaveBeenCalled();
  });
});

describe('ThemeProvider: aviso de hash', () => {
  it('avisa en la consola si el hash del diccionario no es el esperado', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const manifest = manifestOf('modern-flat') as { dictionary: { hash: string } };
    await mount({
      fetcher: fakeFetch({ 'modern-flat': { ...manifest, dictionary: { version: 4, hash: 'sha256:otro' } } }),
    });
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('sha256:otro'));
    expect(warn).toHaveBeenCalledWith(expect.stringContaining(EXPECTED_DICTIONARY_HASH));
  });

  it('no avisa con los cuatro temas reales', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    for (const name of ['modern-flat', 'classic-1995', 'glossy-2003', 'souq']) {
      const { unmount } = await mount({ defaultTheme: name });
      unmount();
    }
    expect(warn).not.toHaveBeenCalled();
  });

  it('el hash esperado se puede cambiar (otra versión del diccionario)', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    await mount({ expectedHash: 'sha256:distinto' });
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('sha256:distinto'));
  });

  it('si el manifiesto no se puede leer, supone sólo claro y lo dice', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const failing = vi.fn(async () => ({ ok: false, status: 404, json: async () => ({}) }) as Response);
    await mount({ fetcher: failing as unknown as typeof fetch });
    expect(theme.info.modes).toEqual(['light']);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('no se pudo leer el manifiesto'), expect.anything());
  });
});

describe('ThemeProvider: cambio de tema y preferencias', () => {
  it('setTheme enlaza la hoja nueva, espera a que cargue y retira la anterior', async () => {
    await mount({ available: ['modern-flat', 'classic-1995'] });
    const first = links()[0];
    let resolved = false;
    const pending = theme.setTheme('classic-1995').then(() => {
      resolved = true;
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(links()).toHaveLength(2); // la vieja se mantiene mientras carga la nueva
    expect(resolved).toBe(false);
    const second = links()[1];
    expect(second?.getAttribute('href')).toBe('/themes/classic-1995/index.css');
    await finishLoading(second);
    await pending;
    expect(resolved).toBe(true);
    expect(links()).toEqual([second]);
    expect(first?.isConnected).toBe(false);
    expect(theme.info.name).toBe('classic-1995');
    expect(theme.info.modes).toEqual(['light']);
  });

  it('al cambiar a un tema sólo claro, el modo oscuro pedido se reduce a claro y se avisa', async () => {
    await mount({ available: ['modern-flat', 'classic-1995'] });
    act(() => theme.setMode('dark'));
    expect(document.documentElement.getAttribute('data-th-mode')).toBe('dark');
    const callback = vi.fn();
    theme.onChange(callback);
    const pending = theme.setTheme('classic-1995');
    await act(async () => {
      await Promise.resolve();
    });
    await finishLoading(links()[1]);
    await pending;
    expect(document.documentElement.getAttribute('data-th-mode')).toBe('light');
    expect(theme.mode).toBe('light');
    expect(callback).toHaveBeenCalled();
  });

  it('rechaza un tema que no está disponible', async () => {
    await mount();
    await expect(theme.setTheme('no-existe')).rejects.toThrow(/Tema desconocido/);
  });

  it('si la hoja nueva falla, rechaza, retira su link y conserva el tema actual', async () => {
    await mount({ available: ['modern-flat', 'classic-1995'] });
    const pending = theme.setTheme('classic-1995');
    const assertion = expect(pending).rejects.toThrow(/No se pudo cargar/);
    await act(async () => {
      await Promise.resolve();
    });
    await act(async () => {
      links()[1]?.dispatchEvent(new Event('error'));
      await Promise.resolve();
    });
    await assertion;
    expect(links()).toHaveLength(1);
    expect(theme.info.name).toBe('modern-flat');
  });

  it('guarda tema y modo en localStorage y los recupera', async () => {
    const first = await mount({ available: ['modern-flat', 'classic-1995'] });
    act(() => theme.setMode('dark'));
    const pending = theme.setTheme('modern-flat');
    await pending; // ya es el actual: no hace nada
    expect(localStorage.getItem('karpo.theme.mode')).toBe('dark');
    first.unmount();

    localStorage.setItem('karpo.theme', 'classic-1995');
    await mount({ available: ['modern-flat', 'classic-1995'] });
    expect(theme.info.name).toBe('classic-1995');
    // el modo oscuro guardado no existe en classic-1995: se reduce a claro
    expect(theme.mode).toBe('light');
  });

  it('ignora una preferencia guardada que ya no está disponible', async () => {
    localStorage.setItem('karpo.theme', 'desaparecido');
    localStorage.setItem('karpo.theme.mode', 'raro');
    await mount();
    expect(theme.info.name).toBe('modern-flat');
    expect(theme.mode).toBe('system');
  });

  it('funciona sin localStorage (acceso que lanza) y con la persistencia desactivada', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('bloqueado');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('bloqueado');
    });
    await mount();
    act(() => theme.setMode('dark'));
    expect(theme.mode).toBe('dark');
  });

  it('con storageKey=null no escribe preferencias', async () => {
    await mount({ storageKey: null });
    act(() => theme.setMode('dark'));
    expect(localStorage.length).toBe(0);
  });

  it('resolveColor de la API usa la raíz por defecto', async () => {
    await mount();
    const spy = vi.spyOn(window, 'getComputedStyle').mockReturnValue({ color: 'rgb(1, 2, 3)' } as CSSStyleDeclaration);
    expect(theme.resolveColor('--th-semantic-up-strong')).toBe('rgb(1, 2, 3)');
    spy.mockRestore();
  });
});

describe('useTheme sin proveedor', () => {
  it('devuelve una API inerte (sólo claro, iconos de contorno)', () => {
    render(<Probe />);
    expect(theme.info.modes).toEqual(['light']);
    expect(theme.info.iconStyle).toBe('outline');
    expect(() => theme.setMode('dark')).not.toThrow();
    expect(typeof theme.onChange(() => undefined)).toBe('function');
  });

  it('su setTheme avisa y no falla', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    render(<Probe />);
    await expect(theme.setTheme('x')).resolves.toBeUndefined();
    expect(warn).toHaveBeenCalled();
  });
});
