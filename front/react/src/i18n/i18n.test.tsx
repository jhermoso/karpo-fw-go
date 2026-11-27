// I18nProvider: lang y dir de <html>, ?dir= y ?lng=, cambio de idioma, preferencia guardada.
import { act, render, screen } from '@testing-library/react';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createI18n } from './createI18n';
import { I18nProvider, useFormatters, useLocale, type I18nProviderProps } from './I18nProvider';

let locale: ReturnType<typeof useLocale>;
function Probe() {
  const { t } = useTranslation('help');
  const api = useLocale();
  useEffect(() => {
    locale = api;
  });
  const { locale: current, direction } = api;
  const formatters = useFormatters();
  return (
    <>
      <output data-testid="text">{t('button.label')}</output>
      <output data-testid="locale">{`${current}|${direction}`}</output>
      <output data-testid="date">{formatters.date('2026-11-27', { dateStyle: 'long' })}</output>
    </>
  );
}

function mount(props: I18nProviderProps = {}) {
  return render(
    <I18nProvider {...props}>
      <Probe />
    </I18nProvider>,
  );
}

const html = () => document.documentElement;

beforeEach(() => {
  localStorage.clear();
  html().removeAttribute('lang');
  html().removeAttribute('dir');
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('createI18n', () => {
  it('nace inicializada, con es de referencia y en, y los espacios de src/locales', () => {
    const es = createI18n({ locale: 'es' });
    expect(es.isInitialized).toBe(true);
    expect(es.language).toBe('es');
    expect(es.t('button.label', { ns: 'help' })).toBe('Ayuda');
    const en = createI18n({ locale: 'en' });
    expect(en.t('button.label', { ns: 'help' })).toBe('Help');
    expect(en.hasResourceBundle('en', 'help')).toBe(true);
  });

  it('ajusta el idioma pedido al admitido más cercano y cae a es', () => {
    expect(createI18n({ locale: 'en-GB' }).language).toBe('en');
    expect(createI18n({ locale: 'ja' }).language).toBe('es');
  });

  it('una clave que falta en en cae al texto en es en lugar de enseñar la clave', () => {
    const instance = createI18n({
      locale: 'en',
      resources: { es: { core: { solo: 'Sólo es' } }, en: { core: {} } },
    });
    expect(instance.t('solo', { ns: 'core' })).toBe('Sólo es');
  });

  it('con pseudo activado ofrece xx: todo entre corchetes', () => {
    const instance = createI18n({ locale: 'xx', pseudo: true });
    expect(instance.language).toBe('xx');
    expect(instance.t('button.label', { ns: 'help' })).toMatch(/^\[.*\]$/);
    expect(instance.t('tour.progress', { ns: 'help', current: 2, total: 5 })).toMatch(/2.*5/);
  });

  it('sin pseudo, xx no existe y se pide es', () => {
    expect(createI18n({ locale: 'xx', pseudo: false }).language).toBe('es');
  });

  it('en desarrollo (pruebas) la pseudolocalización está activa por defecto', () => {
    expect(createI18n({ locale: 'xx' }).language).toBe('xx');
  });

  it('cada instancia es independiente', () => {
    const a = createI18n({ locale: 'es' });
    const b = createI18n({ locale: 'en' });
    expect(a.language).toBe('es');
    expect(b.language).toBe('en');
  });
});

describe('I18nProvider: lang y dir de <html>', () => {
  it('pone lang y dir del idioma por defecto (es, ltr)', () => {
    mount({ locale: 'es', search: '' });
    expect(html().getAttribute('lang')).toBe('es');
    expect(html().getAttribute('dir')).toBe('ltr');
    expect(screen.getByTestId('text').textContent).toBe('Ayuda');
  });

  it('?dir=rtl fuerza dir=rtl y deja el lang del idioma activo', () => {
    mount({ locale: 'en', search: '?dir=rtl' });
    expect(html().getAttribute('dir')).toBe('rtl');
    expect(html().getAttribute('lang')).toBe('en');
    expect(screen.getByTestId('text').textContent).toBe('Help');
    expect(screen.getByTestId('locale').textContent).toBe('en|rtl');
  });

  it('?dir=ltr no cambia nada en un idioma LTR y se ignoran valores raros', () => {
    mount({ locale: 'es', search: '?dir=ltr' });
    expect(html().getAttribute('dir')).toBe('ltr');
    document.documentElement.removeAttribute('dir');
    mount({ locale: 'es', search: '?dir=sideways' });
    expect(html().getAttribute('dir')).toBe('ltr');
  });

  it('?lng=en gana al idioma de la propiedad y a la preferencia guardada', () => {
    localStorage.setItem('karpo.locale', 'es');
    mount({ locale: 'es', search: '?lng=en' });
    expect(html().getAttribute('lang')).toBe('en');
    expect(screen.getByTestId('text').textContent).toBe('Help');
  });

  it('un idioma RTL real (ar) pone dir=rtl sin ?dir=', () => {
    const instance = createI18n({
      locale: 'ar',
      locales: ['es', 'ar'],
      resources: { es: { help: { button: { label: 'Ayuda' } } }, ar: { help: { button: { label: 'مساعدة' } } } },
    });
    mount({ i18n: instance, search: '' });
    expect(html().getAttribute('lang')).toBe('ar');
    expect(html().getAttribute('dir')).toBe('rtl');
    expect(screen.getByTestId('text').textContent).toBe('مساعدة');
  });

  it('con una instancia ajena, ?lng= la cambia de idioma', async () => {
    const instance = createI18n({ locale: 'es' });
    mount({ i18n: instance, search: '?lng=en' });
    await act(async () => {
      await Promise.resolve();
    });
    expect(instance.language).toBe('en');
    expect(html().getAttribute('lang')).toBe('en');
  });

  it('el idioma guardado se usa si no hay ?lng= ni propiedad', () => {
    localStorage.setItem('karpo.locale', 'en');
    mount({ search: '' });
    expect(html().getAttribute('lang')).toBe('en');
  });

  it('?lng=xx activa la pseudolocalización en desarrollo', () => {
    mount({ search: '?lng=xx' });
    expect(screen.getByTestId('text').textContent).toMatch(/^\[.*\]$/);
    expect(html().getAttribute('lang')).toBe('xx');
    expect(html().getAttribute('dir')).toBe('ltr');
  });

  it('cambiar de idioma actualiza textos, lang, dir, formatos y la preferencia', async () => {
    mount({ locale: 'es', search: '' });
    expect(screen.getByTestId('date').textContent).toContain('noviembre');
    await act(async () => {
      await locale.setLocale('en');
    });
    expect(screen.getByTestId('text').textContent).toBe('Help');
    expect(html().getAttribute('lang')).toBe('en');
    expect(screen.getByTestId('date').textContent).toContain('November');
    expect(localStorage.getItem('karpo.locale')).toBe('en');
  });

  it('la dirección forzada con ?dir= sobrevive al cambio de idioma', async () => {
    mount({ locale: 'es', search: '?dir=rtl' });
    await act(async () => {
      await locale.setLocale('en');
    });
    expect(html().getAttribute('dir')).toBe('rtl');
    expect(html().getAttribute('lang')).toBe('en');
  });

  it('setLocale con un idioma desconocido cae a es y xx no se guarda', async () => {
    mount({ locale: 'en', search: '' });
    await act(async () => {
      await locale.setLocale('ja');
    });
    expect(html().getAttribute('lang')).toBe('es');
    await act(async () => {
      await locale.setLocale('xx');
    });
    expect(html().getAttribute('lang')).toBe('xx');
    expect(localStorage.getItem('karpo.locale')).toBe('es');
  });

  it('ofrece la lista de idiomas sin xx', () => {
    mount({ locale: 'es', search: '' });
    expect(locale.locales).toEqual(expect.arrayContaining(['es', 'en']));
    expect(locale.locales).not.toContain('xx');
  });

  it('funciona sin localStorage', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('bloqueado');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('bloqueado');
    });
    mount({ locale: 'es', search: '' });
    await act(async () => {
      await locale.setLocale('en');
    });
    expect(html().getAttribute('lang')).toBe('en');
  });

  it('con storageKey=null no guarda', async () => {
    mount({ locale: 'es', search: '', storageKey: null });
    await act(async () => {
      await locale.setLocale('en');
    });
    expect(localStorage.length).toBe(0);
  });

  it('lee la consulta de la dirección de la página si no se le da una', () => {
    window.history.pushState({}, '', '/?dir=rtl&lng=en');
    try {
      mount();
      expect(html().getAttribute('dir')).toBe('rtl');
      expect(html().getAttribute('lang')).toBe('en');
    } finally {
      window.history.pushState({}, '', '/');
    }
  });
});

describe('useLocale / useFormatters sin proveedor', () => {
  it('devuelve es y ltr', () => {
    render(<Probe />);
    expect(screen.getByTestId('locale').textContent).toBe('es|ltr');
    expect(screen.getByTestId('date').textContent).toContain('noviembre');
  });
});
