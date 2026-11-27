// Marco de la aplicación autenticada: menú lateral, barra superior, contenido, paleta y atajos globales.
import { Suspense, useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Outlet } from 'react-router';
import { useText } from '../config';
import { readStored, writeStored } from '../theme/storage';
import { Action, Feedback, useShortcut } from '../ui';
import { ShellSizeContext, useEntityLocation, useManifest, useShellSize } from './hooks';
import { PaletteHostLazy, preloadPalette } from './lazy';
import { ShortcutsPanel } from './ShortcutsPanel';
import { Sidebar } from './Sidebar';
import { TopBar } from './TopBar';
import { useSessionActions, useSessionState } from '../auth';
import './shell.css';

const COLLAPSED_KEY = 'karpo.nav.collapsed';

export function Shell() {
  const { t } = useTranslation('core');
  const manifest = useManifest();
  const resolve = useText();
  const { entity } = useEntityLocation();
  const { contextError } = useSessionState();
  const { reloadContext } = useSessionActions();
  const [collapsed, setCollapsed] = useState(() => readStored(COLLAPSED_KEY) === '1');
  const [navOpen, setNavOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  // La paleta (cmdk y fuse.js) se descarga la primera vez que se pide y, una vez montada, se queda: las órdenes
  // que lanza pueden abrir diálogos de confirmación que han de sobrevivir al cierre de la paleta.
  const [paletteLoaded, setPaletteLoaded] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const size = useShellSize(root);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const main = useRef<HTMLElement>(null);

  const openPalette = useCallback(() => {
    preloadPalette();
    setShortcutsOpen(false);
    setPaletteLoaded(true);
    setPaletteOpen(true);
  }, []);
  const openShortcuts = useCallback(() => {
    setPaletteOpen(false);
    setShortcutsOpen(true);
  }, []);

  useShortcut({ id: 'shell.palette', scope: 'global', keys: 'Mod+K', labelKey: 'core:shortcuts.palette', run: openPalette });
  useShortcut({ id: 'shell.shortcuts', scope: 'global', keys: 'Shift+?', labelKey: 'core:shortcuts.panel', run: openShortcuts });
  useShortcut({
    id: 'shell.sidebar',
    scope: 'global',
    keys: 'Mod+B',
    labelKey: 'core:shortcuts.sidebar',
    run: () => toggleCollapsed(),
  });

  function toggleCollapsed(): void {
    setCollapsed((current) => {
      writeStored(COLLAPSED_KEY, current ? '0' : '1');
      return !current;
    });
  }

  // El título de la pestaña del navegador sigue a la pantalla.
  const config = entity ? manifest.entities[entity] : undefined;
  const heading = config ? resolve(config.namePlural) : '';
  useEffect(() => {
    document.title = heading ? `${heading} · ${t('app.name')}` : t('app.name');
  }, [heading, t]);

  return (
    <ShellSizeContext.Provider value={size}>
      <div ref={root} data-kx-shell data-kx-nav-open={navOpen ? '' : undefined} data-kx-compact={size.compact ? '' : undefined}>
        <button type="button" data-kx-part="skip" onClick={() => main.current?.focus()}>
          {t('app.skip')}
        </button>
        <Sidebar
          collapsed={collapsed}
          onToggleCollapsed={toggleCollapsed}
          open={navOpen}
          onClose={() => setNavOpen(false)}
        />
        {navOpen ? <div data-kx-part="nav-scrim" aria-hidden="true" onClick={() => setNavOpen(false)} /> : null}
        <div data-kx-part="column">
          <TopBar
            navOpen={navOpen}
            onOpenNav={() => setNavOpen(true)}
            onOpenPalette={openPalette}
            onOpenShortcuts={openShortcuts}
          />
          {contextError ? (
            <Feedback tone="warning" role="status" data-kx-part="context-error">
              <span data-kx-part="text">{t('app.contextError')}</span>
              <Action kind="secondary" icon="refresh" onClick={() => void reloadContext()}>
                {t('app.retry')}
              </Action>
            </Feedback>
          ) : null}
          <main id="kx-main" ref={main} tabIndex={-1} data-kx-part="main">
            <Outlet />
          </main>
        </div>
        {paletteLoaded ? (
          <Suspense fallback={null}>
            <PaletteHostLazy open={paletteOpen} onOpenChange={setPaletteOpen} onOpenShortcuts={openShortcuts} />
          </Suspense>
        ) : null}
        <ShortcutsPanel open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
      </div>
    </ShellSizeContext.Provider>
  );
}
