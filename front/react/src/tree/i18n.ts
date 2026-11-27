// Textos del organigrama (espacio `tree`). Con el i18next de la app se traduce con él; sin proveedor
// se degrada a un i18next privado con los mismos ficheros, para que la vista sirva suelta (pruebas,
// demostraciones). Cada clave lleva además el castellano como valor por defecto.
import { createInstance, type i18n as I18nInstance } from 'i18next';
import { useCallback, useContext } from 'react';
import { I18nContext, getI18n, useTranslation } from 'react-i18next';
import en from '../locales/en/tree.json';
import es from '../locales/es/tree.json';

export type TreeT = (key: string, vars?: Record<string, unknown>) => string;

const NS = 'tree';
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

/** Traductor del espacio `tree` y el idioma activo; se repinta al cambiar de idioma. */
export function useTreeT(): { t: TreeT; locale: string } {
  const context = useContext(I18nContext);
  const i18n = context?.i18n ?? getI18n() ?? fallbackInstance();
  const { t } = useTranslation(NS, { i18n });
  const translate = useCallback<TreeT>(
    (key, vars) => {
      const call = t as unknown as (key: string, options: Record<string, unknown>) => unknown;
      const out = call(key, { ...vars, ns: NS, defaultValue: bundled(key) ?? key });
      return typeof out === 'string' ? out : String(out);
    },
    [t],
  );
  return { t: translate, locale: i18n.resolvedLanguage ?? i18n.language ?? 'es' };
}
