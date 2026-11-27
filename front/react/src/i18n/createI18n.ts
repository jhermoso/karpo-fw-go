// Arranque de i18next (PLAN §4.6): idiomas y espacios de nombres por módulo, pseudolocalización `xx` en
// desarrollo. La instancia nace ya inicializada (recursos en memoria), de modo que los componentes se pintan
// con su texto desde el primer fotograma, sin Suspense.
//
// Sin `resources`, nace con lo que ya se cargó de `src/locales` (D-R37: por demanda; `prepareLocale` carga el
// idioma y los espacios del marco antes, `ensureNamespaces` los de cada vista después) y queda «gestionada»:
// recibe los espacios que se pidan y los del idioma al que se cambie. Con `resources` propios no se toca.
import i18next, { type i18n as I18nInstance, type Resource } from 'i18next';
import { initReactI18next } from 'react-i18next';
import { isolate, isolateDirection } from './bidi';
import { manageInstance } from './bundles';
import {
  fallbackLanguages,
  PSEUDO_LOCALE,
  availableLocales,
  buildPseudoResources,
  collectResources,
  directionFor,
  listLocales,
  pickLocale,
} from './locales';

export interface CreateI18nOptions {
  /** Idioma inicial (`es`, `en-GB`...); se ajusta al admitido más cercano. */
  locale: string;
  /** Idiomas admitidos; por defecto, los que tienen ficheros en `src/locales` (estén cargados o no). */
  locales?: string[];
  /** Recursos propios (pruebas, aplicaciones que añaden espacios); por defecto, lo ya cargado de `src/locales`. */
  resources?: Resource;
  /** Añade el idioma de pseudolocalización `xx`. Por defecto, sólo en desarrollo. */
  pseudo?: boolean;
}

const isDevelopment = (): boolean => {
  try {
    return import.meta.env.DEV === true;
  } catch {
    return false;
  }
};

/** Crea una instancia de i18next independiente y ya inicializada. */
export function createI18n(options: CreateI18nOptions): I18nInstance {
  // Copia profunda: i18next muta los recursos (addResourceBundle) y no deben filtrarse a otras instancias.
  const ownResources = options.resources !== undefined;
  const resources: Resource = structuredClone(options.resources ?? collectResources());
  const pseudo = options.pseudo ?? isDevelopment();
  if (pseudo && !resources[PSEUDO_LOCALE]) resources[PSEUDO_LOCALE] = buildPseudoResources(resources);

  const supported = options.locales ?? (ownResources ? listLocales(resources) : availableLocales());
  const withPseudo = pseudo ? [...supported, PSEUDO_LOCALE] : supported;
  const lng = options.locale === PSEUDO_LOCALE && pseudo ? PSEUDO_LOCALE : pickLocale(options.locale, supported);

  const namespaces = [...new Set(Object.values(resources).flatMap((byNs) => Object.keys(byNs)))];
  const defaultNS = namespaces.includes('core') ? 'core' : (namespaces[0] ?? 'translation');

  const instance = i18next.createInstance();
  // Con dirección RTL (la del idioma o la forzada por `?dir=rtl`) cada valor interpolado va aislado con
  // FSI/PDI (M-12). `escape` sustituye al escape HTML por defecto: React ya escapa, así que no se pierde nada.
  const isolateValue = (value: string): string => {
    const direction = isolateDirection(instance) ?? directionFor(instance.resolvedLanguage ?? instance.language ?? lng);
    return direction === 'rtl' ? isolate(value) : value;
  };
  void instance.use(initReactI18next).init({
    lng,
    fallbackLng: fallbackLanguages(),
    supportedLngs: withPseudo,
    resources,
    ns: namespaces,
    defaultNS,
    initAsync: false,
    interpolation: { escapeValue: true, escape: isolateValue },
    react: { useSuspense: false },
  });
  if (!ownResources) manageInstance(instance);
  return instance;
}
