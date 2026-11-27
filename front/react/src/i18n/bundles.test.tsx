// Idiomas por demanda (D-R37, M-11): al arrancar sólo el idioma activo con los espacios del marco; el resto
// llega con su vista (`ensureNamespaces`) o con el idioma al que se cambia (`loadLanguage`). Aquí se vacía
// la caché de recursos (en setup.ts se carga entera) para medir el arranque real.
import { act, render, screen } from '@testing-library/react';
import { useTranslation } from 'react-i18next';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootNamespaces, ensureNamespaces, forgetRequestedNamespaces, loadLanguage, prepareLocale } from './bundles';
import { createI18n } from './createI18n';
import { I18nProvider, useLocale } from './I18nProvider';
import {
  FRAME_NAMESPACES,
  availableLocales,
  availableNamespaces,
  collectResources,
  isFrameLoaded,
  languagesNeeded,
  loadBundles,
  preloadAllLocales,
  resetLoadedLocales,
} from './locales';

beforeEach(() => {
  resetLoadedLocales();
  forgetRequestedNamespaces();
  localStorage.clear();
});

afterEach(async () => {
  // El resto de ficheros de pruebas da por hecho que todo está cargado (setup.ts).
  await preloadAllLocales();
});

const loadedNamespaces = (lng: string): string[] => Object.keys(collectResources()[lng] ?? {}).sort();

describe('descubrimiento sin cargar', () => {
  it('conoce los idiomas y espacios sin haberlos descargado', () => {
    expect(availableLocales()).toEqual(expect.arrayContaining(['es', 'en']));
    expect(availableLocales()[0]).toBe('es');
    expect(availableNamespaces()).toEqual(expect.arrayContaining([...FRAME_NAMESPACES, 'grid', 'detail', 'tree', 'map']));
    expect(collectResources()).toEqual({});
    expect(isFrameLoaded('en')).toBe(false);
  });

  it('los espacios del marco son el mínimo que pinta la primera pantalla; grid, detail, tree y map quedan fuera', () => {
    expect([...FRAME_NAMESPACES].sort()).toEqual(['auth', 'core', 'errors', 'fields', 'help', 'ui']);
    for (const ns of ['grid', 'detail', 'tree', 'map']) expect(FRAME_NAMESPACES).not.toContain(ns);
  });
});

describe('prepareLocale', () => {
  it('carga sólo el idioma activo y los espacios del marco', async () => {
    await prepareLocale('en');
    expect(loadedNamespaces('en')).toEqual([...FRAME_NAMESPACES].sort());
    expect(loadedNamespaces('es')).toEqual([]);
    expect(isFrameLoaded('en')).toBe(true);
    expect(isFrameLoaded('es')).toBe(false);
  });

  it('un idioma sin paridad garantizada trae también su reserva (ar → en; el resto, es)', async () => {
    expect(languagesNeeded('es')).toEqual(['es']);
    expect(languagesNeeded('en')).toEqual(['en']);
    expect(languagesNeeded('ar')).toEqual(['en', 'ar']);
    expect(languagesNeeded('ar-EG')).toEqual(['en', 'ar-EG']);
    expect(languagesNeeded('xx')).toEqual(['es', 'xx']);
    await prepareLocale('xx');
    expect(loadedNamespaces('es')).toEqual([...FRAME_NAMESPACES].sort());
    expect(loadedNamespaces('xx')).toEqual([...FRAME_NAMESPACES].sort());
    expect(collectResources().xx?.core).toBeDefined();
  });

  it('pedir dos veces lo mismo no repite la descarga ni falla con un espacio sin fichero', async () => {
    await Promise.all([prepareLocale('en'), prepareLocale('en')]);
    await loadBundles('en', ['no-existe']);
    expect(loadedNamespaces('en')).toEqual([...FRAME_NAMESPACES].sort());
  });
});

describe('instancia gestionada', () => {
  it('nace con el marco, no tiene todavía los espacios de las vistas y los recibe con ensureNamespaces', async () => {
    await prepareLocale('en');
    const instance = createI18n({ locale: 'en', pseudo: false });
    expect(instance.language).toBe('en');
    expect(instance.hasResourceBundle('en', 'core')).toBe(true);
    expect(instance.hasResourceBundle('en', 'grid')).toBe(false);
    // Sin su espacio, la clave sale cruda: es lo que ensureNamespaces evita antes de montar una vista.
    expect(instance.t('toolbar.add', { ns: 'grid' })).toBe('toolbar.add');

    await ensureNamespaces(['grid', 'tree']);
    expect(instance.hasResourceBundle('en', 'grid')).toBe(true);
    expect(instance.t('toolbar.add', { ns: 'grid' })).toBe('New row');
    expect(instance.exists('view.label', { ns: 'tree' })).toBe(true);
    // Un idioma que no es el activo no se descarga.
    expect(loadedNamespaces('es')).toEqual([]);
  });

  it('una instancia nueva nace con los espacios que alguna vista ya pidió (otra montura de la aplicación)', async () => {
    expect(bootNamespaces().sort()).toEqual([...FRAME_NAMESPACES].sort());
    await ensureNamespaces(['grid']);
    expect(bootNamespaces()).toContain('grid');
    await prepareLocale('es');
    expect(loadedNamespaces('es')).toContain('grid');
    const instance = createI18n({ locale: 'es', pseudo: false });
    expect(instance.hasResourceBundle('es', 'grid')).toBe(true);
    expect(instance.t('toolbar.add', { ns: 'grid' })).toBe('Nueva fila');
  });

  it('ensureNamespaces sin ninguna instancia no falla', async () => {
    await expect(ensureNamespaces(['grid'])).resolves.toBeUndefined();
  });

  it('el idioma de la lista no depende de lo cargado: ofrece es y en aunque sólo haya uno en memoria', async () => {
    await prepareLocale('en');
    const instance = createI18n({ locale: 'en', pseudo: false });
    expect(instance.options.supportedLngs).toEqual(expect.arrayContaining(['es', 'en']));
  });

  it('loadLanguage trae al idioma nuevo los espacios que la instancia ya tenía (también los de las vistas)', async () => {
    await prepareLocale('en');
    const instance = createI18n({ locale: 'en', pseudo: false });
    await ensureNamespaces(['detail']);
    expect(instance.hasResourceBundle('es', 'core')).toBe(false);

    await loadLanguage(instance, 'es');
    for (const ns of [...FRAME_NAMESPACES, 'detail']) expect(instance.hasResourceBundle('es', ns), ns).toBe(true);
    expect(instance.hasResourceBundle('es', 'grid')).toBe(false);
    await instance.changeLanguage('es');
    expect(instance.t('header.creating', { ns: 'detail' })).toBe('Alta');
  });

  it('una instancia con recursos propios no se toca', async () => {
    const own = createI18n({ locale: 'es', resources: { es: { core: { a: 'A' } } }, pseudo: false });
    await ensureNamespaces(['grid']);
    await loadLanguage(own, 'en');
    expect(own.hasResourceBundle('es', 'grid')).toBe(false);
    expect(own.hasResourceBundle('en', 'core')).toBe(false);
  });
});

function Probe() {
  const { t } = useTranslation('help');
  const { locale, setLocale } = useLocale();
  return (
    <>
      <output data-testid="text">{t('button.label')}</output>
      <output data-testid="locale">{locale}</output>
      <button type="button" onClick={() => void setLocale('es')}>
        cambiar
      </button>
    </>
  );
}

describe('I18nProvider con idiomas por demanda', () => {
  it('no pinta nada hasta tener el marco del idioma inicial y luego pinta ya traducido (nunca una clave)', async () => {
    const { container } = render(
      <I18nProvider locale="en" search="" storageKey={null}>
        <Probe />
      </I18nProvider>,
    );
    expect(container).toBeEmptyDOMElement();
    expect(await screen.findByTestId('text')).toHaveTextContent('Help');
    expect(screen.getByTestId('text').textContent).not.toBe('button.label');
    expect(document.documentElement).toHaveAttribute('lang', 'en');
    expect(loadedNamespaces('es')).toEqual([]);
  });

  it('con el marco ya cargado nace síncrono, como siempre', async () => {
    await prepareLocale('es');
    const { container } = render(
      <I18nProvider locale="es" search="" storageKey={null}>
        <Probe />
      </I18nProvider>,
    );
    expect(container).not.toBeEmptyDOMElement();
    expect(screen.getByTestId('text')).toHaveTextContent('Ayuda');
  });

  it('cambiar de idioma carga el nuevo antes de repintar: ni una clave cruda por el camino', async () => {
    render(
      <I18nProvider locale="en" search="" storageKey={null}>
        <Probe />
      </I18nProvider>,
    );
    await screen.findByTestId('text');
    expect(loadedNamespaces('es')).toEqual([]);
    const seen: string[] = [];
    const observer = new MutationObserver(() => seen.push(screen.getByTestId('text').textContent ?? ''));
    observer.observe(screen.getByTestId('text'), { childList: true, characterData: true, subtree: true });
    await act(async () => {
      screen.getByRole('button', { name: 'cambiar' }).click();
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    observer.disconnect();
    expect(screen.getByTestId('text')).toHaveTextContent('Ayuda');
    expect(seen).not.toContain('button.label');
    expect(loadedNamespaces('es')).toEqual(expect.arrayContaining([...FRAME_NAMESPACES]));
  });

  it('una instancia entregada desde fuera se pinta tal cual, sin esperar', () => {
    const own = createI18n({ locale: 'es', resources: { es: { help: { button: { label: 'Mía' } } } }, pseudo: false });
    const { container } = render(
      <I18nProvider i18n={own} search="" storageKey={null}>
        <Probe />
      </I18nProvider>,
    );
    expect(container).not.toBeEmptyDOMElement();
    expect(screen.getByTestId('text')).toHaveTextContent('Mía');
  });
});
