// Barra superior (`role=banner`): migas, paleta, ayuda, tema, modo, idioma y usuario. El selector de vista ya no
// está aquí: vive en la cabecera de la entidad (M-6). En un marco compacto (≤ 40em, B-5) la barra se reduce a
// menú, miga, paleta (sólo el icono), ayuda y un menú «Más» que reúne tema, modo, idioma, atajos, cambio de
// clave y salida; se pinta un árbol u otro (no se oculta con CSS) para que lo oculto no quede en el árbol de
// accesibilidad ni en el orden de tabulación.
import * as DropdownMenu from '@radix-ui/react-dropdown-menu';
import { Fragment, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';
import { useSession, useSessionActions, useSessionState } from '../auth';
import { humanize, useText } from '../config';
import type { ThemeMode } from '../contracts';
import { HelpButton } from '../help';
import { useLocale } from '../i18n';
import { useTheme } from '../theme';
import { Action, Icon, ShortcutKeys, useToast } from '../ui';
import { ChoiceGroup, ChoiceMenu, type ChoiceOption } from './ChoiceMenu';
import { useCompact, useEntityLocation, useManifest } from './hooks';
import { flattenMenu } from './menu';
import { preloadPalette } from './lazy';
import { languageName } from './locale-name';

export interface TopBarProps {
  onOpenNav(): void;
  navOpen: boolean;
  onOpenPalette(): void;
  onOpenShortcuts(): void;
}

export function TopBar({ onOpenNav, navOpen, onOpenPalette, onOpenShortcuts }: TopBarProps) {
  const { t } = useTranslation('core');
  const compact = useCompact();

  return (
    <header role="banner" data-kx-part="topbar" data-kx-compact={compact ? '' : undefined}>
      <Action
        kind="quiet"
        icon="list"
        data-kx-part="nav-toggle"
        aria-label={t('nav.open')}
        aria-expanded={navOpen}
        onClick={onOpenNav}
      />
      <Crumbs compact={compact} />
      <span data-kx-part="spacer" />
      {compact ? (
        <Action
          kind="secondary"
          icon="search"
          data-kx-help="core.palette"
          data-kx-part="palette-button"
          aria-label={t('palette.button')}
          title={t('palette.button')}
          aria-haspopup="dialog"
          aria-keyshortcuts="Control+K Meta+K"
          onClick={onOpenPalette}
          onPointerEnter={preloadPalette}
          onFocus={preloadPalette}
        />
      ) : (
        <Action
          kind="secondary"
          icon="search"
          data-kx-help="core.palette"
          data-kx-part="palette-button"
          aria-haspopup="dialog"
          aria-keyshortcuts="Control+K Meta+K"
          onClick={onOpenPalette}
          onPointerEnter={preloadPalette}
          onFocus={preloadPalette}
        >
          <span data-kx-part="palette-label">{t('palette.button')}</span>
          <ShortcutKeys keys="Mod+K" />
        </Action>
      )}
      <HelpButton />
      {compact ? (
        <MoreMenu onOpenShortcuts={onOpenShortcuts} />
      ) : (
        <>
          <ThemeMenu />
          <ModeMenu />
          <LocaleMenu />
          <UserMenu onOpenShortcuts={onOpenShortcuts} />
        </>
      )}
    </header>
  );
}

// ---------------------------------------------------------------------------------------------
// Migas (M-1, D-R32): la barra no lleva ningún encabezado; el `h1` de la pantalla lo pinta su contenido.
// ---------------------------------------------------------------------------------------------

/**
 * Ruta de la pantalla: sección del menú › entidad (sin entidad, el nombre de la aplicación). Es texto
 * secundario, no un encabezado. En el modo compacto la barra enseña sólo la última miga y la oculta al lector
 * de pantalla: el `h1` del contenido (oculto sólo a la vista) ya dice dónde se está.
 */
function Crumbs({ compact }: { compact: boolean }) {
  const { t } = useTranslation('core');
  const manifest = useManifest();
  const resolve = useText();
  const { entity } = useEntityLocation();
  const config = entity ? manifest.entities[entity] : undefined;
  const leaves = useMemo(() => flattenMenu(manifest.menu, manifest, resolve), [manifest, resolve]);

  if (!entity || !config) {
    return (
      <p data-kx-part="crumbs" data-kx-crumb="app" aria-hidden={compact ? true : undefined}>
        {t('app.name')}
      </p>
    );
  }
  const group = leaves.find((leaf) => leaf.entity === entity)?.group ?? '';
  const crumbs = [group, resolve(config.namePlural)].filter((text) => text !== '');
  const shown = compact ? crumbs.slice(-1) : crumbs;
  return (
    <nav data-kx-part="crumbs" aria-label={t('crumbs.label')} aria-hidden={compact ? true : undefined}>
      <ol>
        {shown.map((text, index) => {
          const last = index === shown.length - 1;
          return (
            <li key={`${index}:${text}`} data-kx-crumb={last ? 'current' : 'section'} aria-current={last ? 'page' : undefined}>
              {index > 0 ? <Icon name="caret-right" /> : null}
              <span>{text}</span>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

// ---------------------------------------------------------------------------------------------
// Elecciones de tema, modo e idioma (las usan el menú de cada una y el menú «Más»)
// ---------------------------------------------------------------------------------------------

interface Choice {
  label: string;
  icon: string;
  value: string;
  options: ChoiceOption[];
  onChange(value: string): void;
}

/** Temas disponibles; `null` con uno solo (un selector que no ofrece nada sobra). */
function useThemeChoice(): Choice | null {
  const { t } = useTranslation('core');
  const theme = useTheme();
  const toast = useToast();
  const options = useMemo<ChoiceOption[]>(
    () =>
      theme.available.map((name) => ({
        value: name,
        label: t(`theme.names.${name}`, { defaultValue: humanize(name) }),
      })),
    [theme.available, t],
  );
  if (options.length < 2) return null;
  return {
    label: t('theme.label'),
    icon: 'palette',
    value: theme.info.name,
    options,
    onChange: (name) => {
      theme.setTheme(name).catch(() => toast.show({ tone: 'error', message: t('theme.error', { name }) }));
    },
  };
}

const MODE_ICON: Record<ThemeMode, string> = { light: 'sun', dark: 'moon', system: 'monitor' };

/** Modos que el tema declara; `null` si no hay dos (un interruptor que no hace nada sobra). */
function useModeChoice(): Choice | null {
  const { t } = useTranslation('core');
  const theme = useTheme();
  const modes = theme.info.modes;
  if (modes.length < 2) return null;
  return {
    label: t('mode.label'),
    icon: MODE_ICON[theme.mode],
    value: theme.mode,
    options: modes.map((mode) => ({ value: mode, label: t(`mode.${mode}`) })),
    onChange: (mode) => theme.setMode(mode as ThemeMode),
  };
}

function useLocaleChoice(): Choice | null {
  const { t } = useTranslation('core');
  const manifest = useManifest();
  const { locale, locales, setLocale } = useLocale();
  const available = useMemo(() => {
    const offered = manifest.locales.length > 0 ? manifest.locales : locales;
    return offered.filter((code) => locales.includes(code));
  }, [manifest.locales, locales]);
  const options = useMemo<ChoiceOption[]>(() => available.map((code) => ({ value: code, label: languageName(code) })), [available]);
  if (options.length < 2) return null;
  const base = locale.split('-')[0] ?? locale;
  return {
    label: t('locale.label'),
    icon: 'language',
    value: available.includes(locale) ? locale : base,
    options,
    onChange: (code) => void setLocale(code),
  };
}

function ThemeMenu() {
  const choice = useThemeChoice();
  return choice ? <ChoiceMenu {...choice} /> : null;
}

/** Modo claro/oscuro/sistema: sólo los que el tema declara. El icono del modo «sistema» no es el del tema. */
function ModeMenu() {
  const choice = useModeChoice();
  return choice ? <ChoiceMenu {...choice} /> : null;
}

function LocaleMenu() {
  const choice = useLocaleChoice();
  return choice ? <ChoiceMenu {...choice} /> : null;
}

// ---------------------------------------------------------------------------------------------
// Usuario y menú «Más»
// ---------------------------------------------------------------------------------------------

/** Nombre del usuario, cambio de contraseña y salida: lo que comparten el menú de usuario y el «Más». */
function useUserActions(onOpenShortcuts: () => void) {
  const { t } = useTranslation('core');
  const session = useSession();
  const { username } = useSessionState();
  const { logout } = useSessionActions();
  const navigate = useNavigate();
  const name = session.context?.subjectName || username || t('user.anonymous');
  return {
    name,
    changePassword: () => void navigate('/cambiar-clave'),
    shortcuts: onOpenShortcuts,
    signOut: () => {
      void logout().then(() => navigate('/login', { replace: true }));
    },
  };
}

function UserItems({ actions }: { actions: ReturnType<typeof useUserActions> }) {
  const { t } = useTranslation('core');
  return (
    <>
      <DropdownMenu.Item data-kx-ui="menu-item" onSelect={actions.changePassword}>
        <span data-kx-part="check">
          <Icon name="key" />
        </span>
        {t('user.changePassword')}
      </DropdownMenu.Item>
      <DropdownMenu.Item data-kx-ui="menu-item" onSelect={actions.shortcuts}>
        <span data-kx-part="check">
          <Icon name="keyboard" />
        </span>
        {t('user.shortcuts')}
      </DropdownMenu.Item>
      <DropdownMenu.Separator data-kx-part="menu-separator" />
      <DropdownMenu.Item data-kx-ui="menu-item" onSelect={actions.signOut}>
        <span data-kx-part="check">
          <Icon name="log-out" />
        </span>
        {t('user.signOut')}
      </DropdownMenu.Item>
    </>
  );
}

function UserMenu({ onOpenShortcuts }: { onOpenShortcuts(): void }) {
  const { t } = useTranslation('core');
  const actions = useUserActions(onOpenShortcuts);
  const { name } = actions;
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <Action kind="quiet" icon="user" data-kx-help="core.user" data-kx-part="user" aria-label={t('user.menu', { name })}>
          <span data-kx-part="user-name">{name}</span>
        </Action>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content data-kx-ui="menu" data-th-elevation="overlay" align="end" sideOffset={4}>
          <DropdownMenu.Label data-kx-part="menu-label">{name}</DropdownMenu.Label>
          <UserItems actions={actions} />
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

/**
 * El menú «Más» del modo compacto: tema, modo e idioma como grupos de opciones, y debajo los atajos, el cambio
 * de clave y la salida. Es un menú de Radix: se abre con Intro, Espacio o la flecha abajo, se recorre con las
 * flechas y se cierra con Esc devolviendo el foco al botón.
 */
function MoreMenu({ onOpenShortcuts }: { onOpenShortcuts(): void }) {
  const { t } = useTranslation('core');
  const themeChoice = useThemeChoice();
  const modeChoice = useModeChoice();
  const localeChoice = useLocaleChoice();
  const actions = useUserActions(onOpenShortcuts);
  const choices = [themeChoice, modeChoice, localeChoice].filter((choice): choice is Choice => choice !== null);
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <Action
          kind="quiet"
          icon="dots-three"
          data-kx-part="more"
          data-kx-help="core.more"
          aria-label={t('more.label')}
          title={t('more.label')}
        />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content data-kx-ui="menu" data-kx-menu="more" data-th-elevation="overlay" align="end" sideOffset={4}>
          <DropdownMenu.Label data-kx-part="menu-label">{actions.name}</DropdownMenu.Label>
          {choices.map((choice) => (
            <Fragment key={choice.label}>
              <DropdownMenu.Separator data-kx-part="menu-separator" />
              <ChoiceGroup label={choice.label} value={choice.value} options={choice.options} onChange={choice.onChange} />
            </Fragment>
          ))}
          <DropdownMenu.Separator data-kx-part="menu-separator" />
          <UserItems actions={actions} />
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
