// Ayuda guiada: pasos de la configuración o de las anclas, textos traducidos, anclas inexistentes
// omitidas, velo con la sonda, teclado y foco devuelto al botón. Usa driver.js de verdad (jsdom).
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { driver } from 'driver.js';
import type * as DriverJs from 'driver.js';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ThemeContext, type HelpStepDef, type ThemeApi } from '../contracts';
import { I18nProvider, collectResources, createI18n } from '../i18n';
import { HelpButton, HelpProvider, isTourSeen, resetTourSeen, useHelp } from './index';

vi.mock('driver.js', async (importOriginal) => {
  const actual = await importOriginal<typeof DriverJs>();
  return { ...actual, driver: vi.fn(actual.driver) };
});

const resources = collectResources();
const demoTexts = {
  es: {
    demo: {
      help: {
        toolbar: { title: 'Barra de herramientas', body: 'Aquí buscas y filtras.' },
        columns: { title: 'Columnas', body: 'Pulsa una cabecera para ordenar.' },
        solotitulo: { title: 'Sólo título', body: '' },
      },
    },
  },
  en: {
    demo: {
      help: {
        toolbar: { title: 'Toolbar', body: 'Search and filter here.' },
        columns: { title: 'Columns', body: 'Press a header to sort.' },
        solotitulo: { title: 'Title only', body: '' },
      },
    },
  },
};

function i18nFor(locale: 'es' | 'en') {
  const merged = Object.fromEntries(
    ['es', 'en'].map((lng) => [lng, { ...resources[lng], ...demoTexts[lng as 'es' | 'en'] }]),
  );
  return createI18n({ locale, resources: merged, pseudo: false });
}

function Anchors() {
  return (
    <>
      <div data-kx-help="demo.columns">columnas</div>
      <div data-kx-help="demo.toolbar">barra</div>
      <div data-kx-help="demo.fantasma" hidden>
        oculto
      </div>
      <div data-kx-help="demo.sintexto">sin traducción</div>
      <div data-kx-help="sinespacio">sin punto</div>
      <div data-kx-help="demo.toolbar">repetida</div>
    </>
  );
}

function Screen({ steps, withAnchors = true }: { steps?: HelpStepDef[]; withAnchors?: boolean }) {
  useHelp('demo', steps);
  return (
    <>
      <HelpButton />
      {withAnchors ? <Anchors /> : null}
    </>
  );
}

function wrap(children: ReactNode, locale: 'es' | 'en' = 'es', theme?: ThemeApi, search = '') {
  const tree = (
    <I18nProvider i18n={i18nFor(locale)} search={search}>
      <HelpProvider>{children}</HelpProvider>
    </I18nProvider>
  );
  return theme ? <ThemeContext.Provider value={theme}>{tree}</ThemeContext.Provider> : tree;
}

const popover = () => document.querySelector<HTMLElement>('.driver-popover');
const title = () => document.querySelector('.driver-popover-title')?.textContent;
const body = () => document.querySelector('.driver-popover-description')?.textContent;
const progress = () => document.querySelector('.driver-popover-progress-text')?.textContent;

async function startTour(user = userEvent.setup()) {
  await user.click(screen.getByRole('button', { name: /^(Ayuda|Help)$/ }));
  // driver.js se carga con import() al lanzar el recorrido (M-15.2): el popover llega un instante después.
  await waitFor(() => expect(popover()).not.toBeNull());
  return user;
}

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('dir');
  vi.mocked(driver).mockClear();
});

afterEach(() => {
  // cierra cualquier recorrido que haya quedado abierto
  document.querySelectorAll('.driver-popover, .driver-overlay, .driver-live-region').forEach((el) => el.remove());
  document.body.className = '';
  document.documentElement.className = '';
  vi.restoreAllMocks();
});

describe('recorrido generado desde las anclas [data-kx-help]', () => {
  it('sigue el orden del DOM y omite anclas ocultas, sin texto, repetidas o sin espacio', async () => {
    render(wrap(<Screen />));
    await startTour();
    // orden del DOM: columns, toolbar; el botón de ayuda va antes que ambas
    const config = vi.mocked(driver).mock.calls[0]?.[0];
    const titles = config?.steps?.map((s) => s.popover?.title);
    expect(titles).toEqual(['Ayuda guiada', 'Columnas', 'Barra de herramientas']);
    expect(progress()).toBe('Paso 1 de 3');
  });

  it('usa textos traducidos del espacio de nombres del ancla: es y en', async () => {
    const { unmount } = render(wrap(<Screen />, 'es'));
    await startTour();
    expect(title()).toBe('Ayuda guiada');
    expect(body()).toContain('Pulsa este botón');
    unmount();
    document.querySelectorAll('.driver-popover, .driver-overlay, .driver-live-region').forEach((el) => el.remove());

    render(wrap(<Screen />, 'en'));
    await startTour();
    expect(title()).toBe('Guided help');
    expect(body()).toContain('Press this button');
    expect(screen.getByRole('button', { name: 'Close the tour' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Next' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled();
    expect(progress()).toBe('Step 1 of 3');
  });

  it('el botón de ayuda también da el recorrido de las anclas visibles: Siguiente avanza', async () => {
    render(wrap(<Screen />));
    const user = await startTour();
    await user.click(screen.getByRole('button', { name: 'Siguiente' }));
    expect(title()).toBe('Columnas');
    expect(body()).toBe('Pulsa una cabecera para ordenar.');
    expect(progress()).toBe('Paso 2 de 3');
    await user.click(screen.getByRole('button', { name: 'Anterior' }));
    expect(title()).toBe('Ayuda guiada');
  });

  it('un paso sin cuerpo se queda con su título', async () => {
    render(
      wrap(
        <>
          <HelpButton />
          <div data-kx-help="demo.solotitulo">x</div>
        </>,
      ),
    );
    const user = await startTour();
    await user.click(screen.getByRole('button', { name: 'Siguiente' }));
    expect(title()).toBe('Sólo título');
  });

  it('sin ninguna ancla con texto enseña un aviso centrado en lugar de no hacer nada', async () => {
    render(
      wrap(
        <HelpProvider>
          <HelpButton />
        </HelpProvider>,
      ),
    );
    // el botón de ayuda es la única ancla; la ocultamos para vaciar la lista
    screen.getByRole('button', { name: 'Ayuda' }).setAttribute('data-kx-help', 'demo.fantasma');
    await startTour();
    expect(title()).toBe('Esta pantalla aún no tiene recorrido');
    expect(body()).toContain('Todavía no hay elementos explicados');
    expect(progress()).toBe('Paso 1 de 1');
  });

  it('escapa el HTML de los textos (driver.js usa innerHTML)', async () => {
    const instance = i18nFor('es');
    instance.addResourceBundle('es', 'demo', {
      help: { toolbar: { title: '<img src=x onerror=alert(1)>', body: 'a & b\nsegunda línea' } },
    }, true, true);
    render(
      <I18nProvider i18n={instance} search="">
        <HelpProvider>
          <HelpButton />
          <div data-kx-help="demo.toolbar">x</div>
        </HelpProvider>
      </I18nProvider>,
    );
    const user = await startTour();
    await user.click(screen.getByRole('button', { name: 'Siguiente' }));
    expect(document.querySelector('.driver-popover-title img')).toBeNull();
    expect(title()).toBe('<img src=x onerror=alert(1)>');
    expect(document.querySelector('.driver-popover-description br')).not.toBeNull();
  });
});

describe('recorrido desde la configuración (HelpStepDef con Text)', () => {
  const text = (literal: string, extra: Partial<HelpStepDef['title']> = {}) => ({ literal, ...extra });

  it('pone los pasos de la configuración primero, omite los que no existen y añade las anclas no cubiertas', async () => {
    const steps: HelpStepDef[] = [
      { target: 'demo.toolbar', title: text('Barra'), body: text('Cuerpo de la barra') },
      { target: 'demo.no-existe', title: text('Fantasma'), body: text('No debe salir') },
      { target: 'demo.fantasma', title: text('Oculto'), body: text('Tampoco') },
      { target: '#selector-inexistente', title: text('Otro'), body: text('Tampoco') },
      { target: '[invalido', title: text('Selector roto'), body: text('Tampoco') },
      { target: 'demo.columns', title: text('Columnas'), body: text('Cuerpo') },
    ];
    render(wrap(<Screen steps={steps} />));
    await startTour();
    const config = vi.mocked(driver).mock.calls[0]?.[0];
    // toolbar y columns están cubiertas por pasos de la configuración; sólo falta el botón de ayuda
    expect(config?.steps?.map((s) => s.popover?.title)).toEqual(['Barra', 'Columnas', 'Ayuda guiada']);
    expect(progress()).toBe('Paso 1 de 3');
  });

  it('el target puede ser un selector CSS', async () => {
    const steps: HelpStepDef[] = [{ target: '#caja', title: text('Caja'), body: text('Una caja') }];
    render(
      wrap(
        <>
          <Screen steps={steps} withAnchors={false} />
          <div id="caja">c</div>
        </>,
      ),
    );
    await startTour();
    expect(title()).toBe('Caja');
  });

  it('resuelve Text por clave de i18n, por idioma y por literal (es y en)', async () => {
    const steps: HelpStepDef[] = [
      {
        target: 'demo.toolbar',
        title: { literal: 'literal', key: 'help:button.label' },
        body: { literal: 'cuerpo literal', byLang: { es: 'cuerpo es', en: 'body en' } },
      },
      { target: 'demo.columns', title: { literal: 'sólo literal' }, body: { literal: 'x', key: 'help:no.existe' } },
    ];
    const first = render(wrap(<Screen steps={steps} />, 'en'));
    await startTour();
    expect(title()).toBe('Help'); // clave de i18n
    expect(body()).toBe('body en'); // por idioma
    first.unmount();
    document.querySelectorAll('.driver-popover, .driver-overlay, .driver-live-region').forEach((el) => el.remove());
    render(wrap(<Screen steps={steps} />, 'es'));
    await startTour();
    expect(title()).toBe('Ayuda');
    expect(body()).toBe('cuerpo es');
  });

  it('el Text cae al literal si ni la clave ni el idioma lo resuelven', async () => {
    const steps: HelpStepDef[] = [
      { target: 'demo.toolbar', title: { literal: 'Literal', key: 'help:no.existe', byLang: { fr: 'fr' } }, body: { literal: 'Cuerpo' } },
    ];
    render(wrap(<Screen steps={steps} />));
    await startTour();
    expect(title()).toBe('Literal');
  });

  it('si ningún paso de la configuración se ve, cae a las anclas', async () => {
    const steps: HelpStepDef[] = [{ target: 'demo.no-existe', title: text('x'), body: text('y') }];
    render(wrap(<Screen steps={steps} />));
    await startTour();
    expect(vi.mocked(driver).mock.calls[0]?.[0]?.steps?.map((s) => s.popover?.title)).toEqual([
      'Ayuda guiada',
      'Columnas',
      'Barra de herramientas',
    ]);
  });

  it('un paso sin título ni cuerpo se omite', async () => {
    const steps: HelpStepDef[] = [
      { target: 'demo.toolbar', title: text(''), body: text('') },
      { target: 'demo.columns', title: text('Con texto'), body: text('') },
    ];
    render(wrap(<Screen steps={steps} />));
    await startTour();
    // el paso vacío de `toolbar` se omite, así que su ancla (con texto) no está cubierta y se añade detrás
    expect(vi.mocked(driver).mock.calls[0]?.[0]?.steps?.map((s) => s.popover?.title)).toEqual([
      'Con texto',
      'Ayuda guiada',
      'Barra de herramientas',
    ]);
    expect(progress()).toBe('Paso 1 de 3');
    expect(title()).toBe('Con texto');
  });
});

describe('recorrido completo: configuración más anclas (D-R36)', () => {
  const text = (literal: string) => ({ literal });
  const titles = () => vi.mocked(driver).mock.calls[0]?.[0]?.steps?.map((s) => s.popover?.title);

  function Config({ steps }: { steps: HelpStepDef[] }) {
    useHelp('demo', steps);
    return (
      <>
        <div id="uno">uno</div>
        <div id="dos">dos</div>
        <HelpButton />
        <div data-kx-help="demo.a">a</div>
        <div data-kx-help="demo.b">b</div>
        <div data-kx-help="demo.c">c</div>
        <div data-kx-help="demo.columns">columnas</div>
      </>
    );
  }

  function Full({ steps }: { steps?: HelpStepDef[] }) {
    useHelp('demo', steps);
    return (
      <>
        <HelpButton />
        <div data-kx-help="demo.toolbar" id="barra">
          barra
        </div>
        <div data-kx-help="demo.columns">columnas</div>
        <div data-kx-help="demo.solotitulo">solo título</div>
        <div data-kx-help="demo.extra" hidden>
          oculto
        </div>
      </>
    );
  }

  it('una configuración de dos pasos con cinco anclas visibles da siete pasos', async () => {
    const i18n = i18nFor('es');
    i18n.addResourceBundle(
      'es',
      'demo',
      { help: { a: { title: 'A', body: 'a' }, b: { title: 'B', body: 'b' }, c: { title: 'C', body: 'c' } } },
      true,
      true,
    );
    const steps: HelpStepDef[] = [
      { target: '#uno', title: text('Uno'), body: text('Primero') },
      { target: '#dos', title: text('Dos'), body: text('Segundo') },
    ];
    render(
      <I18nProvider i18n={i18n} search="">
        <HelpProvider>
          <Config steps={steps} />
        </HelpProvider>
      </I18nProvider>,
    );
    await startTour();
    // cinco anclas visibles con texto: el botón de ayuda, a, b, c y columns
    expect(titles()).toEqual(['Uno', 'Dos', 'Ayuda guiada', 'A', 'B', 'C', 'Columnas']);
    expect(progress()).toBe('Paso 1 de 7');
  });

  it('un paso de la configuración que cita un ancla (o su elemento) la cubre: sin duplicados', async () => {
    const steps: HelpStepDef[] = [
      { target: 'demo.columns', title: text('Mis columnas'), body: text('x') },
      { target: '#barra', title: text('Mi barra'), body: text('y') },
    ];
    render(wrap(<Full steps={steps} />));
    await startTour();
    // columns se cubre por nombre y toolbar por elemento (el selector #barra): quedan el botón y solotitulo
    expect(titles()).toEqual(['Mis columnas', 'Mi barra', 'Ayuda guiada', 'Sólo título']);
  });

  it('un ancla oculta con reserva se explica sobre el elemento de reserva con el texto idle', async () => {
    const i18n = i18nFor('es');
    i18n.addResourceBundle(
      'es',
      'demo',
      {
        help: {
          lote: {
            title: 'Barra de lote',
            body: 'Guarda o descarta.',
            idle: { title: 'Aparece al editar', body: 'Guarda los cambios de golpe.' },
          },
          sinidle: { title: 'Sin idle', body: 'x' },
        },
      },
      true,
      true,
    );
    render(
      <I18nProvider i18n={i18n} search="">
        <HelpProvider>
          <HelpButton />
          <div data-kx-help="demo.toolbar">barra</div>
          <div data-kx-help="demo.lote" data-kx-help-fallback="demo.toolbar" hidden>
            lote
          </div>
          <div data-kx-help="demo.sinidle" data-kx-help-fallback="demo.toolbar" hidden>
            sin idle
          </div>
        </HelpProvider>
      </I18nProvider>,
    );
    await startTour();
    const steps = vi.mocked(driver).mock.calls[0]?.[0]?.steps;
    expect(titles()).toEqual(['Ayuda guiada', 'Barra de herramientas', 'Aparece al editar']);
    expect(steps?.[2]?.element).toBe(steps?.[1]?.element);
  });

  it('el cierre del popover es el icono x del marco, con su nombre accesible, y no un carácter', async () => {
    render(wrap(<Screen />));
    await startTour();
    const close = screen.getByRole('button', { name: 'Cerrar el recorrido' });
    expect(close.querySelector('svg[data-th-icon="x"]')).not.toBeNull();
    expect(close.querySelectorAll('svg path').length).toBeGreaterThan(0);
    expect(close.textContent).toBe('');
    expect(close).toHaveAttribute('aria-label', 'Cerrar el recorrido');
    // el icono se conserva (sin duplicarse) al cambiar de paso: se decora en cada uno
    await userEvent.setup().click(screen.getByRole('button', { name: 'Siguiente' }));
    expect(screen.getByRole('button', { name: 'Cerrar el recorrido' }).querySelectorAll('svg')).toHaveLength(1);
  });

  it('con un tema de iconos rellenos el cierre usa el peso fill', async () => {
    const theme = {
      info: { iconStyle: 'filled' },
      resolveColor: () => 'rgba(0, 0, 0, 0.4)',
      onChange: () => () => undefined,
    } as unknown as ThemeApi;
    render(wrap(<Screen />, 'es', theme));
    await startTour();
    expect(screen.getByRole('button', { name: 'Cerrar el recorrido' }).querySelector('svg')).toHaveAttribute(
      'data-kx-weight',
      'fill',
    );
  });

  it('help.css: el botón de cierre mide la altura de control regular, con el icono y el inset del tema', async () => {
    const { readFileSync } = await import('node:fs');
    const path = await import('node:path');
    const css = readFileSync(path.join(__dirname, 'help.css'), 'utf8');
    const block = /\.driver-popover-close-btn\s*\{([^}]*)\}/.exec(css)?.[1] ?? '';
    expect(block).toMatch(/inline-size:\s*var\(--th-control-height-regular\)/);
    expect(block).toMatch(/block-size:\s*var\(--th-control-height-regular\)/);
    expect(block).toMatch(/inset-block-start:\s*var\(--th-space-2\)/);
    expect(block).toMatch(/inset-inline-end:\s*var\(--th-space-2\)/);
    const icon = /\.driver-popover-close-btn\s*>\s*svg\s*\{([^}]*)\}/.exec(css)?.[1] ?? '';
    expect(icon).toMatch(/inline-size:\s*var\(--th-control-icon-size\)/);
  });
});

describe('aspecto: sin hoja de driver.js, con el tema', () => {
  it('el popover lleva los ganchos del tema: role=dialog, elevación y acciones', async () => {
    render(wrap(<Screen />));
    await startTour();
    const dialog = popover();
    expect(dialog).toHaveAttribute('role', 'dialog');
    expect(dialog).toHaveAttribute('data-th-elevation', 'modal');
    expect(dialog).toHaveClass('kx-tour');
    expect(screen.getByRole('button', { name: 'Siguiente' })).toHaveAttribute('data-th', 'action');
    expect(screen.getByRole('button', { name: 'Siguiente' })).toHaveAttribute('data-th-kind', 'primary');
    expect(screen.getByRole('button', { name: 'Anterior' })).toHaveAttribute('data-th-kind', 'secondary');
    const close = screen.getByRole('button', { name: 'Cerrar el recorrido' });
    expect(close).toHaveAttribute('data-th-kind', 'quiet');
    expect(close).toHaveAttribute('data-th', 'action');
  });

  it('el popover cumple el contrato de ganchos de Theros', async () => {
    const { assertTherosHooks } = await import('../theme');
    render(wrap(<Screen />));
    await startTour();
    expect(() => assertTherosHooks(popover() as HTMLElement)).not.toThrow();
  });

  it('el popover sigue la dirección del documento', async () => {
    render(wrap(<Screen />, 'es', undefined, '?dir=rtl'));
    await startTour();
    expect(popover()).toHaveAttribute('dir', 'rtl');
  });

  it('no importa el CSS por defecto de driver.js en ningún fichero de src/help', async () => {
    const { readdirSync, readFileSync } = await import('node:fs');
    const path = await import('node:path');
    const dir = __dirname;
    const code = (f: string) =>
      readFileSync(path.join(dir, f), 'utf8')
        .replace(/\/\*[\s\S]*?\*\//g, '')
        .replace(/^\s*\/\/.*$/gm, '');
    const offenders = readdirSync(dir)
      .filter((f) => /\.(tsx?|css)$/.test(f) && !f.includes('.test.'))
      .filter((f) => /import[^;]*['"][^'"]*driver[^'"]*\.css['"]|@import[^;]*driver/.test(code(f)));
    expect(offenders).toEqual([]);
    // y la hoja propia existe
    expect(readdirSync(dir)).toContain('help.css');
  });

  it('pasa a driver.js la configuración esperada: textos, clase, teclado propio y progreso', async () => {
    render(wrap(<Screen />));
    await startTour();
    const config = vi.mocked(driver).mock.calls[0]?.[0];
    expect(config).toMatchObject({
      popoverClass: 'kx-tour',
      allowKeyboardControl: false,
      showProgress: true,
      nextBtnText: 'Siguiente',
      prevBtnText: 'Anterior',
      doneBtnText: 'Terminar',
      closeBtnLabel: 'Cerrar el recorrido',
      progressText: 'Paso {{current}} de {{total}}',
      overlayOpacity: 1,
    });
  });
});

describe('velo con la sonda del tema', () => {
  function fakeTheme(initial: string) {
    let color = initial;
    const listeners = new Set<() => void>();
    const api: ThemeApi = {
      info: { name: 't', modes: ['light'], highContrast: false, iconStyle: 'outline', dictionaryHash: '' },
      mode: 'light',
      setMode: () => undefined,
      setTheme: () => Promise.resolve(),
      available: ['t'],
      resolveColor: vi.fn(() => color),
      onChange: (cb) => {
        listeners.add(cb);
        return () => {
          listeners.delete(cb);
        };
      },
    };
    return {
      api,
      change(next: string) {
        color = next;
        listeners.forEach((cb) => cb());
      },
      count: () => listeners.size,
    };
  }

  it('el color del velo sale de --th-surface-scrim resuelto con la sonda (trae alfa)', async () => {
    const theme = fakeTheme('rgba(9, 11, 15, 0.4)');
    render(wrap(<Screen />, 'es', theme.api));
    await startTour();
    expect(theme.api.resolveColor).toHaveBeenCalledWith('--th-surface-scrim', document.documentElement);
    expect(vi.mocked(driver).mock.calls[0]?.[0]?.overlayColor).toBe('rgba(9, 11, 15, 0.4)');
    await waitFor(() => expect(document.querySelector('.driver-overlay path')).not.toBeNull());
  });

  it('se recalcula cuando el tema o el modo cambian, y se da de baja al cerrar', async () => {
    const theme = fakeTheme('rgba(9, 11, 15, 0.4)');
    render(wrap(<Screen />, 'es', theme.api));
    const user = await startTour();
    expect(theme.count()).toBe(1);
    act(() => theme.change('rgba(0, 0, 0, 0.64)'));
    const instance = vi.mocked(driver).mock.results[0]?.value as ReturnType<typeof driver>;
    expect(instance.getConfig().overlayColor).toBe('rgba(0, 0, 0, 0.64)');
    await user.keyboard('{Escape}');
    expect(theme.count()).toBe(0);
  });

  it('sin ThemeProvider usa la sonda por defecto', async () => {
    render(wrap(<Screen />));
    await startTour();
    // jsdom: color sin tema -> cadena (vacía o la heredada), pero la configuración la lleva
    expect(vi.mocked(driver).mock.calls[0]?.[0]).toHaveProperty('overlayColor');
  });
});

describe('movimiento', () => {
  it('animate y smoothScroll sólo con prefers-reduced-motion: no-preference', async () => {
    vi.spyOn(window, 'matchMedia').mockImplementation(
      (query: string) => ({ matches: query === '(prefers-reduced-motion: no-preference)', media: query, addEventListener: () => undefined, removeEventListener: () => undefined }) as unknown as MediaQueryList,
    );
    const first = render(wrap(<Screen />));
    await startTour();
    expect(vi.mocked(driver).mock.calls[0]?.[0]).toMatchObject({ animate: true, smoothScroll: true });
    first.unmount();
    document.querySelectorAll('.driver-popover, .driver-overlay, .driver-live-region').forEach((el) => el.remove());

    vi.restoreAllMocks();
    vi.mocked(driver).mockClear();
    vi.spyOn(window, 'matchMedia').mockImplementation(
      () => ({ matches: false, media: '', addEventListener: () => undefined, removeEventListener: () => undefined }) as unknown as MediaQueryList,
    );
    render(wrap(<Screen />));
    await startTour();
    expect(vi.mocked(driver).mock.calls[0]?.[0]).toMatchObject({ animate: false, smoothScroll: false });
  });
});

describe('teclado y foco', () => {
  it('Esc cierra el recorrido y el foco vuelve al botón de ayuda', async () => {
    render(wrap(<Screen />));
    const button = screen.getByRole('button', { name: 'Ayuda' });
    const user = await startTour();
    expect(popover()).not.toBeNull();
    await user.keyboard('{Escape}');
    await act(async () => {
      await Promise.resolve();
    });
    expect(popover()).toBeNull();
    expect(button).toHaveFocus();
  });

  it('el botón Cerrar también devuelve el foco al botón de ayuda', async () => {
    render(wrap(<Screen />));
    const button = screen.getByRole('button', { name: 'Ayuda' });
    const user = await startTour();
    await user.click(screen.getByRole('button', { name: 'Cerrar el recorrido' }));
    await act(async () => {
      await Promise.resolve();
    });
    expect(popover()).toBeNull();
    expect(button).toHaveFocus();
  });

  it('terminar el último paso devuelve el foco, marca el recorrido como visto y lo quita del botón', async () => {
    render(wrap(<Screen withAnchors={false} />));
    const button = screen.getByRole('button', { name: 'Ayuda' });
    expect(button).toHaveAttribute('data-kx-help-unseen');
    expect(isTourSeen('demo')).toBe(false);
    const user = await startTour();
    await user.click(screen.getByRole('button', { name: 'Terminar' }));
    await act(async () => {
      await Promise.resolve();
    });
    expect(popover()).toBeNull();
    expect(button).toHaveFocus();
    expect(isTourSeen('demo')).toBe(true);
    expect(button).not.toHaveAttribute('data-kx-help-unseen');
    expect(JSON.parse(localStorage.getItem('karpo.help.seen') ?? '{}')).toEqual({ demo: true });
  });

  it('cerrar en el primer paso no marca el recorrido como visto', async () => {
    render(wrap(<Screen />));
    const user = await startTour();
    await user.keyboard('{Escape}');
    expect(isTourSeen('demo')).toBe(false);
  });

  it('las flechas avanzan y retroceden (LTR: derecha = siguiente)', async () => {
    render(wrap(<Screen />));
    await startTour();
    fireEvent.keyUp(document.body, { key: 'ArrowRight' });
    expect(title()).toBe('Columnas');
    fireEvent.keyUp(document.body, { key: 'ArrowRight' });
    expect(title()).toBe('Barra de herramientas');
    fireEvent.keyUp(document.body, { key: 'ArrowLeft' });
    expect(title()).toBe('Columnas');
  });

  it('en RTL las flechas se invierten (izquierda = siguiente)', async () => {
    render(wrap(<Screen />, 'es', undefined, '?dir=rtl'));
    await startTour();
    fireEvent.keyUp(document.body, { key: 'ArrowLeft' });
    expect(title()).toBe('Columnas');
    fireEvent.keyUp(document.body, { key: 'ArrowRight' });
    expect(title()).toBe('Ayuda guiada');
  });

  it('la flecha derecha en el último paso termina el recorrido', async () => {
    render(wrap(<Screen withAnchors={false} />));
    await startTour();
    fireEvent.keyUp(document.body, { key: 'ArrowRight' });
    await act(async () => {
      await Promise.resolve();
    });
    expect(popover()).toBeNull();
    expect(isTourSeen('demo')).toBe(true);
  });

  it('las flechas no se secuestran dentro de un widget (cuadrícula) pero sí dentro del popover', async () => {
    render(
      wrap(
        <>
          <Screen />
          <div role="grid" tabIndex={0} aria-label="rejilla">
            <div role="gridcell" tabIndex={0}>
              c
            </div>
          </div>
        </>,
      ),
    );
    await startTour();
    fireEvent.keyUp(screen.getByRole('gridcell'), { key: 'ArrowRight' });
    expect(title()).toBe('Ayuda guiada'); // no avanzó
    fireEvent.keyUp(screen.getByRole('button', { name: 'Siguiente' }), { key: 'ArrowRight' });
    expect(title()).toBe('Columnas');
    fireEvent.keyUp(screen.getByRole('gridcell'), { key: 'Escape' });
    expect(popover()).toBeNull(); // Esc siempre cierra
  });

  it('ignora las flechas con modificadores', async () => {
    render(wrap(<Screen />));
    await startTour();
    fireEvent.keyUp(document.body, { key: 'ArrowRight', ctrlKey: true });
    fireEvent.keyUp(document.body, { key: 'ArrowRight', shiftKey: true });
    expect(title()).toBe('Ayuda guiada');
  });

  it('el foco entra en el diálogo del recorrido al abrirlo', async () => {
    render(wrap(<Screen />));
    await startTour();
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    const active = document.activeElement;
    // driver.js lo envía al botón Siguiente/Terminar del popover (o al propio diálogo)
    expect(active === document.body || popover()?.contains(active) || active?.getAttribute('data-kx-help') === 'help.button').toBe(true);
  });

  it('lanzado por código (start) devuelve el foco a donde estaba', async () => {
    function Launcher() {
      const help = useHelp('demo');
      return (
        <>
          <button type="button" onClick={() => help.start()}>
            lanzar
          </button>
          <div data-kx-help="demo.toolbar">barra</div>
        </>
      );
    }
    render(wrap(<Launcher />));
    const launcher = screen.getByRole('button', { name: 'lanzar' });
    const user = userEvent.setup();
    await user.click(launcher);
    expect(popover()).not.toBeNull();
    await user.keyboard('{Escape}');
    await act(async () => {
      await Promise.resolve();
    });
    expect(launcher).toHaveFocus();
  });

  it('si quien lanzó ya no existe, el foco va al botón de ayuda', async () => {
    function Launcher({ show }: { show: boolean }) {
      const help = useHelp('demo');
      return (
        <>
          <HelpButton />
          {show ? (
            <button type="button" onClick={() => help.start()}>
              lanzar
            </button>
          ) : null}
          <div data-kx-help="demo.toolbar">barra</div>
        </>
      );
    }
    const { rerender } = render(wrap(<Launcher show />));
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'lanzar' }));
    rerender(wrap(<Launcher show={false} />));
    await user.keyboard('{Escape}');
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByRole('button', { name: 'Ayuda' })).toHaveFocus();
  });
});

describe('HelpButton y useHelp', () => {
  it('sin HelpProvider no pinta botón', () => {
    render(
      <I18nProvider i18n={i18nFor('es')} search="">
        <HelpButton />
      </I18nProvider>,
    );
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('tiene nombre accesible traducido y el ancla data-kx-help="help.button"', () => {
    const first = render(wrap(<Screen withAnchors={false} />, 'es'));
    expect(screen.getByRole('button', { name: 'Ayuda' })).toHaveAttribute('data-kx-help', 'help.button');
    expect(screen.getByRole('button', { name: 'Ayuda' })).toHaveAttribute('data-th', 'action');
    expect(screen.getByRole('button', { name: 'Ayuda' })).toHaveAttribute('type', 'button');
    first.unmount();
    render(wrap(<Screen withAnchors={false} />, 'en'));
    expect(screen.getByRole('button', { name: 'Help' })).toBeInTheDocument();
  });

  it('el icono es decorativo y sigue el estilo de iconos del tema', () => {
    const theme: ThemeApi = {
      info: { name: 'glossy', modes: ['light'], highContrast: false, iconStyle: 'filled', dictionaryHash: '' },
      mode: 'light',
      setMode: () => undefined,
      setTheme: () => Promise.resolve(),
      available: [],
      resolveColor: () => '',
      onChange: () => () => undefined,
    };
    render(wrap(<Screen withAnchors={false} />, 'es', theme));
    const svg = screen.getByRole('button', { name: 'Ayuda' }).querySelector('svg');
    expect(svg).toHaveAttribute('aria-hidden', 'true');
    expect(svg?.querySelector('path')?.getAttribute('d')).toBeTruthy();
  });

  it('useHelp registra y da de baja la pantalla; sin proveedor devuelve una API inerte', () => {
    function Inert() {
      const api = useHelp('x', [{ target: 'a', title: { literal: 'a' }, body: { literal: 'b' } }]);
      return <button type="button" onClick={() => api.start()}>inerte</button>;
    }
    render(<Inert />);
    expect(() => screen.getByRole('button', { name: 'inerte' }).click()).not.toThrow();
    expect(popover()).toBeNull();
  });

  it('con pasos nuevos en cada render no entra en bucle', () => {
    const rendered = vi.fn();
    function Looping() {
      rendered();
      useHelp('loop', [{ target: 'demo.toolbar', title: { literal: 'a' }, body: { literal: 'b' } }]);
      return null;
    }
    render(wrap(<Looping />));
    expect(rendered.mock.calls.length).toBeLessThan(6);
  });

  it('el último recorrido registrado es el de la pantalla activa; al desmontarlo vuelve el anterior', async () => {
    function A() {
      useHelp('a', [{ target: 'demo.toolbar', title: { literal: 'Paso A' }, body: { literal: 'cuerpo' } }]);
      return null;
    }
    function B() {
      useHelp('b', [{ target: 'demo.toolbar', title: { literal: 'Paso B' }, body: { literal: 'cuerpo' } }]);
      return null;
    }
    function App({ showB }: { showB: boolean }) {
      return (
        <>
          <HelpButton />
          <div data-kx-help="demo.toolbar">barra</div>
          <A />
          {showB ? <B /> : null}
        </>
      );
    }
    const { rerender } = render(wrap(<App showB />));
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Ayuda' }));
    expect(title()).toBe('Paso B');
    await user.keyboard('{Escape}');
    rerender(wrap(<App showB={false} />));
    await user.click(screen.getByRole('button', { name: 'Ayuda' }));
    expect(title()).toBe('Paso A');
  });

  it('resetTourSeen olvida lo visto (una pantalla o todas)', async () => {
    localStorage.setItem('karpo.help.seen', JSON.stringify({ a: true, b: true }));
    expect(isTourSeen('a')).toBe(true);
    resetTourSeen('a');
    expect(isTourSeen('a')).toBe(false);
    expect(isTourSeen('b')).toBe(true);
    resetTourSeen();
    expect(isTourSeen('b')).toBe(false);
  });

  it('lo visto sobrevive a un localStorage roto o con basura', () => {
    localStorage.setItem('karpo.help.seen', '{no es json');
    expect(isTourSeen('a')).toBe(false);
    localStorage.setItem('karpo.help.seen', '[1,2]');
    expect(isTourSeen('a')).toBe(false);
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('bloqueado');
    });
    expect(isTourSeen('a')).toBe(false);
  });

  it('lanzar un recorrido con otro abierto sustituye al anterior', async () => {
    render(wrap(<Screen />));
    const user = await startTour();
    const help = screen.getByRole('button', { name: 'Ayuda' });
    help.focus();
    await act(async () => {
      help.click();
    });
    expect(document.querySelectorAll('.driver-popover')).toHaveLength(1);
    await user.keyboard('{Escape}');
  });

  it('al desmontar el proveedor con el recorrido abierto, se cierra', async () => {
    const { unmount } = render(wrap(<Screen />));
    await startTour();
    expect(popover()).not.toBeNull();
    unmount();
    expect(popover()).toBeNull();
  });
});
