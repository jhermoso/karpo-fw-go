// I18nProvider: i18next para React y `lang`/`dir` de <html> desde el idioma activo (PLAN §4.6, D-R16).
//
//  - `?lng=en` fuerza el idioma y `?dir=rtl|ltr` fuerza la dirección sin cambiar de idioma: así las
//    pruebas RTL con es/en siguen siendo posibles; la demo trae además un idioma RTL real (D-R38).
//  - Idiomas por demanda (D-R37): sin instancia propia, el proveedor carga el idioma inicial y los espacios del
//    marco ANTES de crear la instancia y no pinta nada hasta entonces (así nunca se ve una clave cruda). Si
//    ya están en memoria (pruebas, segunda montura) la instancia nace síncrona, como siempre.
//  - Prioridad del idioma inicial: `?lng=` > prop `locale` > preferencia guardada > navegador > es.
//  - `dir` y `lang` no los pone el tema (A3 §3): los pone esto, desde el idioma, no desde el manifiesto.
import type { i18n as I18nInstance } from 'i18next';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useState,
  type PropsWithChildren,
} from 'react';
import { I18nextProvider } from 'react-i18next';
import { readStored, writeStored } from '../theme/storage';
import { setIsolateDirection } from './bidi';
import { bootNamespaces, loadLanguage, prepareLocale } from './bundles';
import { createI18n } from './createI18n';
import { createFormatters, type Formatters } from './formatters';
import { PSEUDO_LOCALE, availableLocales, directionFor, isFrameLoaded, pickLocale } from './locales';

export interface I18nProviderProps {
  /** Instancia ya creada (`createI18n`); si falta se crea una con los idiomas de `src/locales`. */
  i18n?: I18nInstance;
  /** Idioma inicial si no hay `?lng=`. */
  locale?: string;
  /** Idiomas admitidos (sólo al crear la instancia). */
  locales?: string[];
  /** Cadena de consulta (`?lng=en&dir=rtl`). Por defecto, la de la página. */
  search?: string;
  /** Clave de localStorage para el idioma elegido; `null` desactiva la persistencia. */
  storageKey?: string | null;
}

export interface LocaleApi {
  /** Idioma activo (`es`, `en`...). */
  locale: string;
  /** Dirección aplicada a <html>: la del idioma o la forzada por `?dir=`. */
  direction: 'ltr' | 'rtl';
  /** Idiomas que se pueden elegir. */
  locales: string[];
  setLocale(locale: string): Promise<void>;
}

const LocaleContext = createContext<LocaleApi | null>(null);

const DEFAULT_STORAGE_KEY = 'karpo.locale';

function navigatorLanguage(): string | undefined {
  return typeof navigator === 'undefined' ? undefined : navigator.language;
}

function readSearch(search: string | undefined): URLSearchParams {
  if (search !== undefined) return new URLSearchParams(search);
  return new URLSearchParams(typeof window === 'undefined' ? '' : window.location.search);
}

/** Idioma con el que arranca la instancia que crea el proveedor: `?lng=` > propiedad > guardado > navegador > es. */
function initialLocale({ search, locale, storageKey }: Pick<I18nProviderProps, 'search' | 'locale' | 'storageKey'>): string {
  const stored = storageKey === null ? null : readStored(storageKey ?? DEFAULT_STORAGE_KEY);
  return readSearch(search).get('lng') ?? locale ?? stored ?? navigatorLanguage() ?? 'es';
}

/** Idioma que acabará eligiendo `createI18n` para lo pedido (para saber qué ficheros hacen falta). */
function resolvedLocale(requested: string): string {
  return requested === PSEUDO_LOCALE ? PSEUDO_LOCALE : pickLocale(requested, availableLocales());
}

export function I18nProvider(props: PropsWithChildren<I18nProviderProps>) {
  const { children, i18n: provided, locales } = props;
  const [created, setCreated] = useState<I18nInstance | null>(() => {
    if (provided) return null;
    const requested = initialLocale(props);
    return isFrameLoaded(resolvedLocale(requested), bootNamespaces()) ? createI18n({ locale: requested, locales }) : null;
  });
  useEffect(() => {
    if (provided || created) return undefined;
    let cancelled = false;
    const requested = initialLocale(props);
    void prepareLocale(resolvedLocale(requested)).then(() => {
      if (!cancelled) setCreated(createI18n({ locale: requested, locales }));
    });
    return () => {
      cancelled = true;
    };
    // El idioma inicial se resuelve una sola vez, al montar.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const instance = provided ?? created;
  if (!instance) return null;
  return (
    <ReadyProvider {...props} instance={instance} adoptLng={Boolean(provided)}>
      {children}
    </ReadyProvider>
  );
}

function ReadyProvider({
  children,
  instance,
  adoptLng,
  search,
  storageKey = DEFAULT_STORAGE_KEY,
}: PropsWithChildren<I18nProviderProps & { instance: I18nInstance; adoptLng: boolean }>) {
  const params = useMemo(() => readSearch(search), [search]);
  const dirParam = params.get('dir');
  const forcedDirection = dirParam === 'rtl' || dirParam === 'ltr' ? dirParam : null;
  const lngParam = params.get('lng');
  const provided = adoptLng ? instance : undefined;

  const [language, setLanguage] = useState(() => instance.resolvedLanguage ?? instance.language);
  // Antes de pintar a los hijos: sus textos interpolados ya deben llevar los aislantes de `?dir=rtl` (M-12).
  setIsolateDirection(instance, forcedDirection);

  const supported = useMemo(() => {
    const list = (instance.options.supportedLngs || []).filter(
      (l) => l !== 'cimode' && l !== PSEUDO_LOCALE,
    );
    return list;
  }, [instance]);

  // Una instancia entregada desde fuera adopta ?lng= (la creada aquí ya nació con él).
  useLayoutEffect(() => {
    if (provided && lngParam) {
      const target = lngParam === PSEUDO_LOCALE ? PSEUDO_LOCALE : pickLocale(lngParam, supported);
      if (target !== provided.language) void provided.changeLanguage(target);
    }
  }, [provided, lngParam, supported]);

  // `lang` y `dir` de <html> siguen al idioma activo.
  useLayoutEffect(() => {
    const root = document.documentElement;
    const apply = (lng: string) => {
      setLanguage(lng);
      root.setAttribute('lang', lng);
      root.setAttribute('dir', forcedDirection ?? directionFor(lng));
    };
    const onChanged = (lng: string) => apply(lng);
    instance.on('languageChanged', onChanged);
    // La lectura inicial se hace en el propio evento para no llamar a setState en el cuerpo del efecto.
    root.setAttribute('lang', instance.language);
    root.setAttribute('dir', forcedDirection ?? directionFor(instance.language));
    return () => {
      instance.off('languageChanged', onChanged);
    };
  }, [instance, forcedDirection]);

  const setLocale = useCallback(
    async (next: string) => {
      const target = next === PSEUDO_LOCALE ? PSEUDO_LOCALE : pickLocale(next, supported);
      // Antes de cambiar, el idioma nuevo trae los espacios que la instancia ya tenía (D-R37).
      await loadLanguage(instance, target);
      await instance.changeLanguage(target);
      if (storageKey !== null && target !== PSEUDO_LOCALE) writeStored(storageKey, target);
    },
    [instance, supported, storageKey],
  );

  const api = useMemo<LocaleApi>(
    () => ({
      locale: language,
      direction: forcedDirection ?? directionFor(language),
      locales: supported,
      setLocale,
    }),
    [language, forcedDirection, supported, setLocale],
  );

  return (
    <I18nextProvider i18n={instance}>
      <LocaleContext.Provider value={api}>{children}</LocaleContext.Provider>
    </I18nextProvider>
  );
}

const FALLBACK_LOCALE_API: LocaleApi = {
  locale: 'es',
  direction: 'ltr',
  locales: ['es'],
  setLocale: () => Promise.resolve(),
};

/** Idioma activo, dirección y cambio de idioma. Sin proveedor devuelve `es`/`ltr`. */
export function useLocale(): LocaleApi {
  return useContext(LocaleContext) ?? FALLBACK_LOCALE_API;
}

/** Formateadores (números, importes, porcentajes y fechas) del idioma activo. */
export function useFormatters(): Formatters {
  const { locale } = useLocale();
  return useMemo(() => createFormatters(locale), [locale]);
}
