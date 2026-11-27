// Contrato de la API pública: src/index.ts reexporta cada módulo con los nombres de PLAN §4.2.
import { describe, expect, it } from 'vitest';
import * as karpo from './index';

const PUBLIC_NAMES: Record<string, string[]> = {
  config: ['loadManifest', 'loadLegacy', 'normalizeEntityConfig', 'evaluateRule', 'useText', 'resolveText'],
  api: ['createHttpClient', 'createEntityApi', 'ApiError', 'useEntityList', 'useEntityRecord', 'useLookupLabel'],
  auth: ['SessionProvider', 'useSession', 'LoginPage', 'ChangePasswordPage', 'OrganizationScopePicker'],
  theme: ['ThemeProvider', 'useTheme', 'resolveThemeColor', 'assertTherosHooks'],
  i18n: ['createI18n', 'I18nProvider', 'directionFor', 'useFormatters', 'ensureNamespaces', 'FRAME_NAMESPACES'],
  help: ['HelpProvider', 'HelpButton', 'useHelp'],
  fields: ['fieldTypes', 'getFieldType', 'registerFieldType', 'FieldView', 'FieldEditor'],
  ui: [
    'Action',
    'Dialog',
    'ConfirmDialog',
    'PromptDialog',
    'Feedback',
    'ToastProvider',
    'useToast',
    'Badge',
    'Chip',
    'Kbd',
    'Skeleton',
    'Spinner',
    'Icon',
    'iconData',
    'Toolbar',
    'Field',
    'ShortcutProvider',
    'useShortcut',
    'useShortcutRegistry',
  ],
  grid: ['GridView'],
  shell: ['KarpoApp'],
  detail: ['DetailView'],
  tree: ['TreeView'],
  map: ['MapView'],
  contracts: [
    'queryKeys',
    'ManifestContext',
    'ApiContext',
    'SessionContext',
    'ThemeContext',
    'HelpContext',
    'viewChoices',
    'ICON_NAMES',
    'CORE_ICON_NAMES',
    'DIRECTIONAL_ICONS',
    'isIconName',
  ],
};

describe('API pública de @karpo/react', () => {
  for (const [module, names] of Object.entries(PUBLIC_NAMES)) {
    it(`${module} exporta ${names.length} nombre(s) del plan`, () => {
      const exported = karpo as unknown as Record<string, unknown>;
      for (const name of names) expect(exported[name], `${module}.${name}`).toBeDefined();
    });
  }

  it('los contextos y las claves son los de contracts, no copias', async () => {
    const contracts = await import('./contracts');
    expect(karpo.ThemeContext).toBe(contracts.ThemeContext);
    expect(karpo.queryKeys).toBe(contracts.queryKeys);
  });
});
