// Textos del mapa (espacio `map`). Con el i18next de la app se traduce con él; sin proveedor se
// degrada a un i18next privado con los mismos ficheros (la vista sirve suelta en pruebas y demos).
import { createInstance, type i18n as I18nInstance } from 'i18next';
import { useCallback, useContext } from 'react';
import { I18nContext, getI18n, useTranslation } from 'react-i18next';
import en from '../locales/en/map.json';
import es from '../locales/es/map.json';

export type MapT = (key: string, vars?: Record<string, unknown>) => string;

const NS = 'map';
let fallback: I18nInstance | undefined;

function fallbackInstance(): I18nInstance {
  if (!fallback) {
    fallback = createInstance();
    void fallback.init({
      lng: (typeof document !== 'undefined' && document.documentElement.lang) || 'es',
      fallbackLng: 'es',
      supportedLngs: ['es', 'en'],
      nonExplicitSupportedLngs: true,
      resources: { es: { [NS]: es }, en: { [NS]: en } },
      defaultNS: NS,
      ns: [NS],
      interpolation: { escapeValue: false },
      initAsync: false,
    });
  }
  return fallback;
}

function bundled(key: string): string | undefined {
  let node: unknown = es;
  for (const part of key.split('.')) {
    if (node && typeof node === 'object' && part in node) node = (node as Record<string, unknown>)[part];
    else return undefined;
  }
  return typeof node === 'string' ? node : undefined;
}

/** Traductor del espacio `map` y el idioma activo; se repinta al cambiar de idioma. */
export function useMapT(): { t: MapT; locale: string } {
  const context = useContext(I18nContext);
  const i18n = context?.i18n ?? getI18n() ?? fallbackInstance();
  const { t } = useTranslation(NS, { i18n });
  const translate = useCallback<MapT>(
    (key, vars) => {
      const call = t as unknown as (key: string, options: Record<string, unknown>) => unknown;
      const out = call(key, { ...vars, ns: NS, defaultValue: bundled(key) ?? key });
      return typeof out === 'string' ? out : String(out);
    },
    [t],
  );
  return { t: translate, locale: i18n.resolvedLanguage ?? i18n.language ?? 'es' };
}

/**
 * Formateador de intervalos numéricos del idioma (`Intl.NumberFormat#formatRange`, M-12): el separador, el
 * orden y las cifras salen del idioma, también en los de derecha a izquierda. `null` si el motor no lo
 * tiene o no admite el idioma: quien llama usa entonces la cadena `legend.range`. (Cuando
 * `src/i18n` exporte `formatNumberRange`, este formateador se sustituye por él.)
 */
export function numberRangeFormatter(locale: string, options?: Intl.NumberFormatOptions): ((from: number, to: number) => string) | null {
  try {
    const format = new Intl.NumberFormat(locale, options);
    if (typeof format.formatRange !== 'function') return null;
    return (from, to) => format.formatRange(from, to);
  } catch {
    return null;
  }
}
