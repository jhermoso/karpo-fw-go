// i18n y RTL (T2): i18next con espacios de nombres por módulo, dirección por idioma y formatos Intl.
export { createI18n, type CreateI18nOptions } from './createI18n';
export { FSI, PDI, isolate, isolateDirection, setIsolateDirection } from './bidi';
export { ensureNamespaces, loadLanguage, prepareLocale } from './bundles';
export {
  I18nProvider,
  useFormatters,
  useLocale,
  type I18nProviderProps,
  type LocaleApi,
} from './I18nProvider';
export {
  DEFAULT_NUMBERING_SYSTEM,
  createFormatters,
  formatDecimalString,
  formatNumberRange,
  intlLocale,
  type Formatters,
} from './formatters';
export {
  REFERENCE_LOCALE,
  PSEUDO_LOCALE,
  FRAME_NAMESPACES,
  availableLocales,
  baseLanguage,
  collectResources,
  directionFor,
  listLocales,
  pickLocale,
  preloadAllLocales,
  pseudoLocalize,
} from './locales';
