// Textos de las primitivas y de los campos (espacios `ui` y `fields`).
// Con el i18next de la app (T2) se traduce con él; sin proveedor se degrada a un i18next privado
// con los mismos ficheros `src/locales/{es,en}/{ui,fields}.json`, y cada clave lleva además el
// castellano como valor por defecto. Así los componentes sirven sueltos (pruebas, demos).
import { createInstance, type i18n as I18nInstance } from 'i18next';
import { useCallback, useContext } from 'react';
import { I18nContext, getI18n, useTranslation } from 'react-i18next';
import enFields from '../locales/en/fields.json';
import enUi from '../locales/en/ui.json';
import esFields from '../locales/es/fields.json';
import esUi from '../locales/es/ui.json';

export type TextNamespace = 'ui' | 'fields';
export type Translate = (key: string, vars?: Record<string, unknown>) => string;

const RESOURCES = {
  es: { ui: esUi, fields: esFields },
  en: { ui: enUi, fields: enFields },
} as const;

let fallback: I18nInstance | undefined;

function documentLanguage(): string {
  if (typeof document === 'undefined') return 'es';
  return document.documentElement.lang || 'es';
}

function fallbackInstance(): I18nInstance {
  if (!fallback) {
    fallback = createInstance();
    void fallback.init({
      lng: documentLanguage(),
      fallbackLng: 'es',
      supportedLngs: ['es', 'en'],
      nonExplicitSupportedLngs: true,
      resources: RESOURCES,
      defaultNS: 'ui',
      ns: ['ui', 'fields'],
      interpolation: { escapeValue: false },
      initAsync: false,
    });
  }
  return fallback;
}

/** Plantilla castellana de una clave (valor por defecto cuando falta en el i18next de la app). */
function bundledTemplate(ns: TextNamespace, key: string): string | undefined {
  let node: unknown = RESOURCES.es[ns];
  for (const part of key.split('.')) {
    if (node && typeof node === 'object' && part in node) {
      node = (node as Record<string, unknown>)[part];
    } else {
      return undefined;
    }
  }
  return typeof node === 'string' ? node : undefined;
}

/** Reduce una etiqueta de idioma a la que usan las tablas de formato (`es`, `en`, `es-MX`…). */
export function normalizeLocale(tag: string | undefined): string {
  if (!tag) return 'es';
  const clean = tag.replace('_', '-');
  return clean.length > 0 ? clean : 'es';
}

export interface UiI18n {
  t: Translate;
  /** Etiqueta BCP 47 del idioma activo. */
  locale: string;
  i18n: I18nInstance;
}

/** Traductor del espacio indicado y el idioma activo; se repinta al cambiar de idioma. */
export function useUiI18n(ns: TextNamespace): UiI18n {
  const context = useContext(I18nContext);
  const i18n = context?.i18n ?? getI18n() ?? fallbackInstance();
  const { t } = useTranslation(ns, { i18n });
  const translate = useCallback<Translate>(
    (key, vars) => {
      const call = t as unknown as (key: string, options: Record<string, unknown>) => unknown;
      const out = call(key, { ...vars, ns, defaultValue: bundledTemplate(ns, key) ?? key });
      return typeof out === 'string' ? out : String(out);
    },
    [t, ns],
  );
  return { t: translate, locale: normalizeLocale(i18n.resolvedLanguage ?? i18n.language), i18n };
}

/** Sólo el traductor. */
export function useT(ns: TextNamespace): Translate {
  return useUiI18n(ns).t;
}

/** Sólo el idioma activo. */
export function useLocale(): string {
  return useUiI18n('ui').locale;
}
