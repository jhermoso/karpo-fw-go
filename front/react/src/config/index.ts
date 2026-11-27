// Motor de configuración (T1): carga, valida y normaliza el JSON de Angular al modelo interno.
export {
  loadManifest,
  loadManifestWithReport,
  loadLegacy,
  loadLegacyWithReport,
  loadDiscovered,
  loadDiscoveredWithReport,
  resolveRelative,
  type DiscoverOptions,
  type LegacyOptions,
  type LoadOptions,
  type LoadReport,
} from './manifest';
export {
  normalizeEntityConfig,
  normalizeRule,
  matchOption,
  optionKey,
  parseEndpoint,
  toneFromColor,
  toneFromHash,
  type NormalizeOptions,
  type ParsedEndpoint,
} from './normalize';
export {
  evaluateRule,
  hasPermission,
  looseEquals,
  permissionMatches,
  type RuleContext,
} from './rules';
export {
  clearConfigDictionaries,
  getConfigDictionary,
  humanize,
  registerConfigDictionary,
  resolveText,
  toText,
  translateWith,
  useText,
  type ConfigTranslate,
} from './text';
export { mergeOverlay } from './merge';
export { entityIconWarnings, menuIconWarnings, unknownIconMessage } from './icons';
export { COMPAT, checkKeys, keyStatus, renderCompatTable, type KeyStatus, type ScopeTable } from './compat';
export { KNOWN_SERVER_SORT, knownServerSort } from './sort';
export { validateEntityShape } from './schema';
export {
  ConfigError,
  asExt,
  type ColumnDefExt,
  type DataSourceExt,
  type EntityConfigExt,
  type FieldDefExt,
  type FilterDefExt,
  type GridOptions,
  type SectionDefExt,
} from './types';
