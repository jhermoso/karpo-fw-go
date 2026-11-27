// Textos de la cuadrícula (espacio `grid`) y de los errores del servidor (espacio `errors`).
// Igual que `ui/i18n.ts`: con el i18next de la app se traduce con él; sin proveedor, con un i18next
// privado que lleva los mismos ficheros y el castellano como valor por defecto de cada clave.
import { createInstance, type i18n as I18nInstance } from 'i18next';
import { useCallback, useContext } from 'react';
import { I18nContext, getI18n, useTranslation } from 'react-i18next';
import enErrors from '../locales/en/errors.json';
import enGrid from '../locales/en/grid.json';
import esErrors from '../locales/es/errors.json';
import esGrid from '../locales/es/grid.json';

export type GridTranslate = (key: string, vars?: Record<string, unknown>) => string;

const RESOURCES = {
  es: { grid: esGrid, errors: esErrors },
  en: { grid: enGrid, errors: enErrors },
} as const;

let fallback: I18nInstance | undefined;

function fallbackInstance(): I18nInstance {
  if (!fallback) {
    fallback = createInstance();
    const lng = typeof document === 'undefined' ? 'es' : document.documentElement.lang || 'es';
    void fallback.init({
      lng,
      fallbackLng: 'es',
      supportedLngs: ['es', 'en'],
      nonExplicitSupportedLngs: true,
      resources: RESOURCES,
      defaultNS: 'grid',
      ns: ['grid', 'errors'],
      interpolation: { escapeValue: false },
      initAsync: false,
    });
  }
  return fallback;
}

function lookupBundled(key: string): string | undefined {
  let node: unknown = RESOURCES.es.grid;
  for (const part of key.split('.')) {
    if (node && typeof node === 'object' && part in node) node = (node as Record<string, unknown>)[part];
    else return undefined;
  }
  return typeof node === 'string' ? node : undefined;
}

const PLURALS = new Intl.PluralRules('es');

/** Plantilla castellana de una clave; con `count`, su forma plural (`clave_one`, `clave_other`). */
function bundled(key: string, count?: unknown): string | undefined {
  const direct = lookupBundled(key);
  if (direct !== undefined || typeof count !== 'number') return direct;
  return lookupBundled(`${key}_${PLURALS.select(count)}`) ?? lookupBundled(`${key}_other`);
}

export interface GridI18n {
  /** Texto del espacio `grid`; admite `{{n}}` y `count` para plurales. */
  t: GridTranslate;
  /** Texto de un código de error del servidor (`errors:<código>`), o `fallback` si no hay traducción. */
  tError(code: string, fallbackText?: string): string;
  /** Etiqueta BCP 47 del idioma activo. */
  locale: string;
  i18n: I18nInstance;
}

export function useGridI18n(): GridI18n {
  const context = useContext(I18nContext);
  const i18n = context?.i18n ?? getI18n() ?? fallbackInstance();
  const { t } = useTranslation('grid', { i18n });
  const translate = useCallback<GridTranslate>(
    (key, vars) => {
      const call = t as unknown as (key: string, options: Record<string, unknown>) => unknown;
      const out = call(key, { ...vars, ns: 'grid', defaultValue: bundled(key, vars?.count) ?? key });
      return typeof out === 'string' ? out : String(out);
    },
    [t],
  );
  const tError = useCallback(
    (code: string, fallbackText?: string) => {
      const options = { ns: 'errors' } as const;
      if (i18n.exists(code, options)) return String(i18n.t(code, options));
      const bundle = fallbackInstance();
      if (bundle.exists(code, options)) return String(bundle.t(code, options));
      return fallbackText ?? code;
    },
    [i18n],
  );
  return { t: translate, tError, locale: i18n.resolvedLanguage ?? i18n.language ?? 'es', i18n };
}
