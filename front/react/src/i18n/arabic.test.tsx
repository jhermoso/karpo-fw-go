// Árabe de la demo (D-R38, M-10): idioma RTL real, con sus seis categorías plurales, reserva en inglés,
// aislamiento FSI/PDI de lo interpolado y cifras latinas. Sin revisión nativa (Q-5 sigue abierta).
import { afterEach, describe, expect, it, vi } from 'vitest';
import { clearConfigDictionaries, registerConfigDictionary, resolveText } from '../config/text';
import { FSI, PDI } from './bidi';
import { createI18n } from './createI18n';
import { createFormatters } from './formatters';
import {
  availableLocales,
  availableNamespaces,
  collectResources,
  directionFor,
  fallbackLanguages,
  languagesNeeded,
  reserveFor,
} from './locales';
import { languageName } from '../shell/locale-name';

const iso = (value: string | number) => `${FSI}${value}${PDI}`;

describe('idioma ar', () => {
  it('está entre los idiomas con fichero, con los mismos espacios de nombres que es', () => {
    expect(availableLocales()).toEqual(expect.arrayContaining(['es', 'en', 'ar']));
    const resources = collectResources();
    expect(Object.keys(resources['ar'] ?? {}).sort()).toEqual(availableNamespaces());
    expect(Object.keys(resources['ar'] ?? {}).sort()).toEqual(Object.keys(resources['es'] ?? {}).sort());
  });

  it('va de derecha a izquierda y se nombra en su propio idioma', () => {
    expect(directionFor('ar')).toBe('rtl');
    expect(directionFor('ar-EG')).toBe('rtl');
    expect(languageName('ar')).toBe('العربية');
    expect(languageName('es')).toBe('Español');
  });

  it('sin datos de idioma en el motor, el selector sigue mostrando «العربية» y no «ar»', () => {
    const spy = vi.spyOn(Intl, 'DisplayNames').mockImplementation(function () {
      throw new RangeError('sin datos');
    } as unknown as typeof Intl.DisplayNames);
    try {
      expect(languageName('ar')).toBe('العربية');
      expect(languageName('es')).toBe('Español');
      expect(languageName('fr')).toBe('Fr');
    } finally {
      spy.mockRestore();
    }
  });

  it('su reserva es el inglés (el resto cae al castellano) y por eso se cargan juntos', () => {
    expect(reserveFor('ar')).toBe('en');
    expect(reserveFor('ar-EG')).toBe('en');
    expect(reserveFor('en')).toBe('es');
    expect(fallbackLanguages()).toEqual({ default: ['es'], ar: ['en'] });
    expect(languagesNeeded('ar')).toEqual(['en', 'ar']);
  });

  it('lo que falta en ar sale en inglés, no en castellano', () => {
    const instance = createI18n({
      locale: 'ar',
      pseudo: false,
      resources: {
        es: { core: { only: 'sólo es', both: 'es' } },
        en: { core: { only: 'only en', both: 'en' } },
        ar: { core: { both: 'عربي' } },
      },
    });
    expect(instance.t('both', { ns: 'core' })).toBe('عربي');
    expect(instance.t('only', { ns: 'core' })).toBe('only en');
  });

  it('un idioma sin reserva propia sigue cayendo al castellano', () => {
    const instance = createI18n({
      locale: 'en',
      pseudo: false,
      resources: { es: { core: { only: 'sólo es' } }, en: { core: {} } },
    });
    expect(instance.t('only', { ns: 'core' })).toBe('sólo es');
  });
});

describe('textos reales en árabe', () => {
  const ar = () => createI18n({ locale: 'ar', pseudo: false, resources: collectResources() });

  it('usa las seis categorías plurales del árabe (cuántos cambios hay)', () => {
    const t = ar().getFixedT('ar', 'grid');
    expect(t('batch.count', { count: 1 })).toBe('تغيير واحد');
    expect(t('batch.count', { count: 2 })).toBe('تغييران');
    expect(t('batch.count', { count: 4 })).toBe(`${iso(4)} تغييرات`);
    expect(t('batch.count', { count: 14 })).toBe(`${iso(14)} تغييرًا`);
    expect(t('batch.count', { count: 100 })).toBe(`${iso(100)} تغيير`);
    expect(t('batch.count', { count: 0 })).toBe('لا تغييرات');
  });

  it('las cinco cadenas del jurado llevan sus cifras aisladas con FSI y PDI', () => {
    const instance = ar();
    // 1. Recuento de la cabecera.
    expect(instance.t('header.count', { ns: 'core', n: 14, name: 'منشأة' })).toBe(`${iso(14)} ${iso('منشأة')}`);
    // 2. «Paso N de M».
    expect(instance.t('tour.progress', { ns: 'help', current: 2, total: 8 })).toBe(`الخطوة ${iso(2)} من ${iso(8)}`);
    // 3. Campos marcados.
    expect(instance.t('save.fieldErrors', { ns: 'detail', n: 3 })).toBe(`راجع الحقول المحددة: ${iso(3)}.`);
    // 4. Límites del mundo (delimitaciones del nivel).
    expect(instance.t('status.level', { ns: 'map', level: 'المدينة', count: 14 })).toBe(
      `${iso('المدينة')}: ${iso(14)} حدًّا إداريًا.`,
    );
    // 5. Filas del pie.
    expect(instance.t('footer.count', { ns: 'grid', loaded: 50, total: 1204 })).toBe(`${iso(50)} من ${iso(1204)} صفًا`);
  });

  it('en LTR no se añade ningún aislante: sólo el árabe los pide', () => {
    const en = createI18n({ locale: 'en', pseudo: false, resources: collectResources() });
    expect(en.t('tour.progress', { ns: 'help', current: 2, total: 8 })).toBe('Step 2 of 8');
  });

  it('las cifras salen con dígitos latinos también en árabe', () => {
    const formatters = createFormatters('ar');
    expect(formatters.number(1234.5)).toMatch(/^[0-9][0-9,.]*$/u);
    expect(formatters.number(1234.5)).not.toMatch(/[٠-٩]/u);
    expect(formatters.date(new Date(Date.UTC(2026, 10, 27)))).not.toMatch(/[٠-٩]/u);
  });
});

describe('textos de la configuración en árabe (reserva en inglés)', () => {
  afterEach(() => clearConfigDictionaries());

  it('una clave sin traducir al árabe sale en inglés y no en el literal castellano', () => {
    registerConfigDictionary('es', { 'entities.asset.name': 'Activo', 'entities.facility.name': 'Instalación' });
    registerConfigDictionary('en', { 'entities.asset.name': 'Asset', 'entities.facility.name': 'Facility' });
    registerConfigDictionary('ar', { 'entities.facility.name': 'منشأة' });
    const asset = { literal: 'Activo', key: 'entities.asset.name' };
    const facility = { literal: 'Instalación', key: 'entities.facility.name' };
    const none = () => undefined;
    expect(resolveText(facility, 'ar', none)).toBe('منشأة');
    expect(resolveText(asset, 'ar', none)).toBe('Asset');
    expect(resolveText(asset, 'ar-EG', none)).toBe('Asset');
    // Sin la reserva, el resto de idiomas sigue cayendo al literal.
    expect(resolveText(asset, 'es', none)).toBe('Activo');
    expect(resolveText({ literal: 'Lit', key: 'no.existe' }, 'ar', none)).toBe('Lit');
  });
});
