// Paleta de órdenes (Mod+K): cmdk para la lista y el teclado, fuse.js para la búsqueda difusa. Se
// alimenta de la configuración: ir a una entidad, crear, las órdenes de la selección, tema, modo e idioma.
import { useQueryClient } from '@tanstack/react-query';
import * as RadixDialog from '@radix-ui/react-dialog';
import { Command } from 'cmdk';
import type { TFunction } from 'i18next';
import { useContext, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';
import { useSession, useSessionActions } from '../auth';
import { humanize, evaluateRule, useText } from '../config';
import { HelpContext, queryKeys, type CommandDef, type EntityConfig, type Row, type ThemeMode } from '../contracts';
import { useCommandRunner } from '../detail/commands';
import { useLocale } from '../i18n';
import { useTheme } from '../theme';
import { Icon, ShortcutKeys } from '../ui';
import { useEntityLocation, useEntityNavigate, useManifest } from './hooks';
import { languageName } from './locale-name';
import { canWriteEntity, flattenMenu } from './menu';
import { filterPalette, groupPalette, type PaletteItem } from './palette-items';
import { useSelection } from './selection';
import { useVisibleMenu } from './Sidebar';

export interface PaletteHostProps {
  open: boolean;
  onOpenChange(open: boolean): void;
  onOpenShortcuts(): void;
}

/** La paleta con sus elementos y los diálogos de las órdenes que lance. */
export function PaletteHost({ open, onOpenChange, onOpenShortcuts }: PaletteHostProps) {
  const { t } = useTranslation('core');
  const manifest = useManifest();
  const session = useSession();
  const resolve = useText();
  const navigate = useNavigate();
  const entityNavigate = useEntityNavigate();
  const queryClient = useQueryClient();
  const theme = useTheme();
  const { locale, locales, setLocale } = useLocale();
  const { logout } = useSessionActions();
  const help = useContext(HelpContext);
  const visibleMenu = useVisibleMenu();
  const location = useEntityLocation();
  const currentConfig = location.entity ? manifest.entities[location.entity] : undefined;
  const selected = useSelection(currentConfig?.id);
  const runner = useCommandRunner(currentConfig, { navigate: entityNavigate });

  const items = useMemo<PaletteItem[]>(() => {
    const out: PaletteItem[] = [];
    const leaves = flattenMenu(visibleMenu, manifest, resolve);

    // Órdenes de la selección (o del registro abierto).
    if (currentConfig) {
      const rows: Row[] = [...selected];
      if (rows.length === 0 && location.id) {
        const record = queryClient.getQueryData<Row>(queryKeys.record(currentConfig.id, location.id));
        if (record) rows.push(record);
      }
      out.push(...selectionItems(currentConfig, rows, runner.run, resolve, t, session));
    }

    for (const leaf of leaves) {
      const config = manifest.entities[leaf.entity];
      if (!config) continue;
      const name = resolve(config.namePlural) || leaf.label;
      out.push({
        id: `go:${config.id}`,
        group: 'go',
        label: t('palette.go', { name: leaf.label }),
        hint: leaf.group || undefined,
        keywords: `${name} ${resolve(config.name)} ${leaf.keywords}`,
        icon: config.icon ?? leaf.icon,
        run: () => void navigate(`/${encodeURIComponent(config.id)}`),
      });
      if (config.editable && config.form && config.form.create && canWriteEntity(config, session)) {
        out.push({
          id: `create:${config.id}`,
          group: 'create',
          label: t('palette.create', { name: resolve(config.name) || name }),
          hint: name,
          keywords: `nuevo nueva crear alta new add ${name}`,
          icon: 'plus',
          run: () => entityNavigate({ entity: config.id, mode: 'edit' }),
        });
      }
    }

    out.push({
      id: 'general:help',
      group: 'general',
      label: t('palette.help'),
      keywords: 'ayuda recorrido guiado tour help',
      icon: 'help',
      run: () => help?.start(),
    });
    out.push({
      id: 'general:shortcuts',
      group: 'general',
      label: t('palette.shortcuts'),
      keywords: 'atajos teclado shortcuts keyboard',
      icon: 'keyboard',
      shortcut: 'Shift+?',
      run: onOpenShortcuts,
    });
    out.push({
      id: 'general:home',
      group: 'general',
      label: t('palette.home'),
      keywords: 'inicio home',
      icon: 'home',
      run: () => void navigate('/'),
    });
    out.push({
      id: 'general:password',
      group: 'general',
      label: t('user.changePassword'),
      keywords: 'contraseña clave password',
      icon: 'key',
      run: () => void navigate('/cambiar-clave'),
    });
    out.push({
      id: 'general:signout',
      group: 'general',
      label: t('user.signOut'),
      keywords: 'salir cerrar sesión logout sign out',
      icon: 'log-out',
      run: () => void logout().then(() => navigate('/login', { replace: true })),
    });

    for (const name of theme.available) {
      if (name === theme.info.name) continue;
      const label = t(`theme.names.${name}`, { defaultValue: humanize(name) });
      out.push({
        id: `theme:${name}`,
        group: 'theme',
        label: t('palette.theme', { name: label }),
        keywords: `tema theme apariencia ${label}`,
        icon: 'sparkles',
        run: () => void theme.setTheme(name).catch(() => undefined),
      });
    }
    if (theme.info.modes.length > 1) {
      for (const mode of theme.info.modes) {
        if (mode === theme.mode) continue;
        const label = t(`mode.${mode}`);
        out.push({
          id: `mode:${mode}`,
          group: 'mode',
          label: t('palette.mode', { name: label }),
          keywords: `modo mode ${label} oscuro claro dark light`,
          icon: mode === 'dark' ? 'moon' : 'sun',
          run: () => theme.setMode(mode as ThemeMode),
        });
      }
    }
    const offered = (manifest.locales.length > 0 ? manifest.locales : locales).filter((code) => locales.includes(code));
    if (offered.length > 1) {
      for (const code of offered) {
        if (code === locale) continue;
        const name = languageName(code);
        out.push({
          id: `locale:${code}`,
          group: 'locale',
          label: t('palette.locale', { name }),
          keywords: `idioma language ${code} ${name}`,
          icon: 'language',
          run: () => void setLocale(code),
        });
      }
    }
    return out;
  }, [
    currentConfig,
    help,
    locale,
    locales,
    location.id,
    logout,
    manifest,
    navigate,
    entityNavigate,
    onOpenShortcuts,
    queryClient,
    resolve,
    runner.run,
    selected,
    session,
    setLocale,
    t,
    theme,
    visibleMenu,
  ]);

  return (
    <>
      <CommandPalette open={open} onOpenChange={onOpenChange} items={items} />
      {runner.dialogs}
    </>
  );
}

function selectionItems(
  config: EntityConfig,
  rows: readonly Row[],
  run: (cmd: CommandDef, row: Row) => void,
  resolve: ReturnType<typeof useText>,
  t: TFunction<'core'>,
  session: ReturnType<typeof useSession>,
): PaletteItem[] {
  if (rows.length === 0) return [];
  const out: PaletteItem[] = [];
  const entityName = resolve(config.name);
  const hint = rows.length === 1 ? String(rows[0]?.[config.displayField] ?? rows[0]?.id ?? '') : t('palette.selected', { n: rows.length });
  for (const cmd of config.commands) {
    if (!cmd.surfaces.includes('row') && !cmd.surfaces.includes('detail-header')) continue;
    if ((cmd.kind === 'http' || cmd.kind === 'delete' || cmd.kind === 'edit') && !canWriteEntity(config, session)) continue;
    const applicable = rows.filter((row) => !cmd.visibleWhen || evaluateRule(cmd.visibleWhen, { session, row }));
    if (applicable.length === 0) continue;
    // Con confirmación o motivo sólo se atiende una fila; sin ellos, todas las elegidas.
    const interactive = Boolean(cmd.confirm || cmd.prompt || cmd.kind === 'view' || cmd.kind === 'edit');
    if (interactive && applicable.length > 1) continue;
    out.push({
      id: `selection:${cmd.id}`,
      group: 'selection',
      label: resolve(cmd.label),
      hint: `${entityName} · ${hint}`,
      keywords: `${cmd.id} ${entityName}`,
      icon: cmd.icon,
      run: () => {
        for (const row of applicable) run(cmd, row);
      },
    });
  }
  return out;
}

interface CommandPaletteProps {
  open: boolean;
  onOpenChange(open: boolean): void;
  items: readonly PaletteItem[];
}

export function CommandPalette({ open, onOpenChange, items }: CommandPaletteProps) {
  const { t } = useTranslation('core');
  const [query, setQuery] = useState('');
  const searching = query.trim() !== '';
  const sections = useMemo(() => groupPalette(filterPalette(items, query), searching), [items, query, searching]);

  function change(next: boolean): void {
    if (!next) setQuery('');
    onOpenChange(next);
  }

  return (
    <RadixDialog.Root open={open} onOpenChange={change}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay data-kx-ui="dialog-overlay" />
        <RadixDialog.Content data-kx-ui="palette" data-th-elevation="modal" aria-describedby={undefined}>
          <RadixDialog.Title data-kx-part="sr-only">{t('palette.title')}</RadixDialog.Title>
          <Command label={t('palette.title')} shouldFilter={false} loop>
            <div data-kx-part="palette-input">
              <Icon name="search" />
              <Command.Input
                value={query}
                onValueChange={setQuery}
                placeholder={t('palette.placeholder')}
                aria-label={t('palette.placeholder')}
              />
            </div>
            <Command.List data-kx-part="palette-list" aria-label={t('palette.title')}>
              <Command.Empty data-kx-part="palette-empty">{t('palette.empty', { query: query.trim() })}</Command.Empty>
              {sections.map((section) => (
                <Command.Group key={section.group} heading={t(`palette.groups.${section.group}`)}>
                  {section.items.map((item) => (
                    <Command.Item
                      key={item.id}
                      value={item.id}
                      onSelect={() => {
                        change(false);
                        item.run();
                      }}
                    >
                      {item.icon ? <Icon name={item.icon} /> : <span data-kx-part="icon-space" />}
                      <span data-kx-part="item-label">{item.label}</span>
                      {item.hint ? <span data-kx-part="item-hint">{item.hint}</span> : null}
                      {item.shortcut ? <ShortcutKeys keys={item.shortcut} /> : null}
                    </Command.Item>
                  ))}
                </Command.Group>
              ))}
            </Command.List>
          </Command>
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
