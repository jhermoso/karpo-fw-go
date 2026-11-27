// Temas de Theros Front: proveedor, sonda de color, capas CSS y contrato de ganchos (T2).
export {
  ThemeProvider,
  useTheme,
  clampMode,
  type KarpoThemeInfo,
  type ThemeProviderProps,
} from './ThemeProvider';
export { resolveThemeColor, resolveThemeLength, measureIconStyle } from './probe';
export {
  assertTherosHooks,
  findTherosHookViolations,
  TherosHookError,
  EXPECTED_DICTIONARY_HASH,
  THEROS_VALUES,
  THEROS_ROLES,
  type HookCheckOptions,
  type HookViolation,
} from './contract';
export { parseThemeManifest, loadThemeManifest, type ThemeManifestInfo } from './manifest';
