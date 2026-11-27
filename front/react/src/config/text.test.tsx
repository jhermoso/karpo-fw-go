// Textos de la configuración: resolveText y useText con una instancia de i18next real.
import { act, render, screen } from '@testing-library/react';
import i18next from 'i18next';
import { I18nextProvider } from 'react-i18next';
import { afterEach, describe, expect, it } from 'vitest';
import {
  clearConfigDictionaries,
  getConfigDictionary,
  humanize,
  registerConfigDictionary,
  resolveText,
  toText,
  translateWith,
  useText,
} from './text';

afterEach(() => {
  clearConfigDictionaries();
});

describe('toText', () => {
  it('admite literal, clave y objeto por idioma', () => {
    expect(toText('Hola')).toEqual({ literal: 'Hola' });
    expect(toText('Hola', 'k.hola')).toEqual({ literal: 'Hola', key: 'k.hola' });
    expect(toText(undefined, 'k.solo')).toEqual({ literal: '', key: 'k.solo' });
    expect(toText({ es: 'Hola', en: 'Hello' })).toEqual({ literal: 'Hola', byLang: { es: 'Hola', en: 'Hello' } });
    expect(toText({ en: 'Hello', fr: 'Salut' })).toMatchObject({ literal: 'Hello' });
    expect(toText({ 'pt-BR': 'Olá' })).toEqual({ literal: 'Olá', byLang: { 'pt-BR': 'Olá' } });
    expect(toText(5)).toEqual({ literal: '5' });
    expect(toText(true, 'k')).toEqual({ literal: 'true', key: 'k' });
    expect(toText({ no: 1 })).toBeUndefined();
    expect(toText({ Nope: 'x' })).toBeUndefined();
    expect(toText(undefined)).toBeUndefined();
    expect(toText([])).toBeUndefined();
    expect(toText('x', '  ')).toEqual({ literal: 'x' });
  });

  it('humanize deja una etiqueta legible', () => {
    expect(humanize('unitPrice')).toBe('Unit price');
    expect(humanize('work-center')).toBe('Work center');
    expect(humanize('snake_case_name')).toBe('Snake case name');
  });
});

describe('resolveText', () => {
  const text = { literal: 'Literal', key: 'k.text', byLang: { es: 'Por idioma ES', en: 'By lang EN' } };

  it('orden: clave traducida, diccionario de la configuración, objeto por idioma, literal', () => {
    expect(resolveText(text, 'es', (key) => (key === 'k.text' ? 'Traducida' : undefined))).toBe('Traducida');
    registerConfigDictionary('es', { k: { text: 'Del diccionario' } });
    expect(resolveText(text, 'es', () => undefined)).toBe('Del diccionario');
    expect(resolveText(text, 'en', () => undefined)).toBe('By lang EN');
    expect(resolveText(text, 'en-US', () => undefined)).toBe('By lang EN');
    expect(resolveText(text, 'fr', () => undefined)).toBe('Literal');
    expect(resolveText({ literal: 'Sólo literal' }, 'es')).toBe('Sólo literal');
    expect(resolveText({ literal: '', key: 'k.sin.nada' }, 'es', () => undefined)).toBe('k.sin.nada');
    expect(resolveText(undefined, 'es')).toBe('');
  });

  it('el diccionario admite claves planas o anidadas y se acumula por idioma', () => {
    registerConfigDictionary('es', { 'a.b': 'AB' });
    registerConfigDictionary('es', { c: { d: 'CD' }, ignorado: 3, lista: ['x'] });
    expect(getConfigDictionary('es')).toEqual({ 'a.b': 'AB', 'c.d': 'CD' });
    expect(getConfigDictionary('xx')).toEqual({});
    registerConfigDictionary('es', 'no es un objeto');
    expect(getConfigDictionary('es')).toEqual({ 'a.b': 'AB', 'c.d': 'CD' });
  });

  it('sin traductor usa la instancia global de i18next si está inicializada', async () => {
    const t0 = { literal: 'Lit', key: 'global.k' };
    expect(resolveText(t0, 'es')).toBe('Lit');
    await i18next.init({
      lng: 'es',
      resources: { es: { translation: { 'global.k': 'Global ES' } }, en: { translation: { 'global.k': 'Global EN' } } },
    });
    try {
      expect(resolveText(t0, 'es')).toBe('Global ES');
      expect(resolveText(t0, 'en')).toBe('Global EN');
    } finally {
      i18next.isInitialized = false;
    }
  });
});

describe('translateWith', () => {
  it('busca la clave tal cual y en el espacio `config`', async () => {
    const i18n = i18next.createInstance();
    await i18n.init({
      lng: 'es',
      ns: ['translation', 'config', 'auth'],
      resources: {
        es: { translation: { directa: 'Directa' }, config: { 'solo.config': 'En config' }, auth: { title: 'Acceso' } },
      },
    });
    expect(translateWith(i18n, 'directa', 'es')).toBe('Directa');
    expect(translateWith(i18n, 'solo.config', 'es')).toBe('En config');
    expect(translateWith(i18n, 'auth:title', 'es')).toBe('Acceso');
    expect(translateWith(i18n, 'config:solo.config', 'es')).toBe('En config');
    expect(translateWith(i18n, 'no.existe', 'es')).toBeUndefined();
    expect(translateWith(undefined, 'x', 'es')).toBeUndefined();
    expect(translateWith({}, 'x', 'es')).toBeUndefined();
  });
});

describe('useText', () => {
  it('resuelve con el idioma activo y se actualiza al cambiarlo', async () => {
    const i18n = i18next.createInstance();
    await i18n.init({
      lng: 'es',
      fallbackLng: 'es',
      resources: {
        es: { translation: { 'menu.ops': 'Operaciones' } },
        en: { translation: { 'menu.ops': 'Operations' } },
      },
    });
    registerConfigDictionary('en', { 'only.dict': 'Only in the dictionary' });
    const label = { literal: 'Lit', key: 'menu.ops' };
    const dict = { literal: 'Lit', key: 'only.dict' };
    const byLang = { literal: 'ES', byLang: { es: 'ES', en: 'EN' } };
    function Probe() {
      const text = useText();
      return (
        <ul>
          <li>{text(label)}</li>
          <li>{text(dict)}</li>
          <li>{text(byLang)}</li>
          <li>{text(undefined) || 'vacío'}</li>
        </ul>
      );
    }
    render(
      <I18nextProvider i18n={i18n}>
        <Probe />
      </I18nextProvider>,
    );
    const items = () => screen.getAllByRole('listitem').map((li) => li.textContent);
    expect(items()).toEqual(['Operaciones', 'Lit', 'ES', 'vacío']);
    await act(async () => {
      await i18n.changeLanguage('en');
    });
    expect(items()).toEqual(['Operations', 'Only in the dictionary', 'EN', 'vacío']);
  });

  it('sin proveedor de i18n devuelve el literal', () => {
    function Probe() {
      const text = useText();
      return <p>{text({ literal: 'Sin i18n', key: 'x.y' })}</p>;
    }
    render(<Probe />);
    expect(screen.getByText('Sin i18n')).toBeInTheDocument();
  });
});
