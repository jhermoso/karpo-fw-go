// Menú lateral: `nav[data-th=nav]` con `a[data-th=nav-item][aria-current=page]` (A3 §5), filtrado por
// permisos, con buscador (fuse.js, diferido), grupos plegables y selector de ámbito de organizaciones.
import * as Popover from '@radix-ui/react-popover';
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink, useNavigate } from 'react-router';
import { OrganizationScopePicker, useSession, useSessionState } from '../auth';
import { useText } from '../config';
import type { MenuNode } from '../contracts';
import { readStored, writeStored } from '../theme/storage';
import { Action, Icon, Toolbar } from '../ui';
import { useManifest } from './hooks';
import { canReadEntity, filterMenu, flattenMenu, matchMenu, type MenuLeaf } from './menu';

/** Menú del manifiesto o, si no declara ninguno, todas las entidades que el usuario puede leer. */
export function useVisibleMenu(): MenuNode[] {
  const manifest = useManifest();
  const session = useSession();
  return useMemo(() => {
    if (manifest.menu.length > 0) return filterMenu(manifest.menu, manifest, session);
    return Object.values(manifest.entities)
      .filter((config) => canReadEntity(config, session))
      .map<MenuNode>((config) => ({
        id: `entity-${config.id}`,
        label: config.namePlural,
        icon: config.icon,
        entity: config.id,
      }));
  }, [manifest, session]);
}

type MenuSearch = (leaves: readonly MenuLeaf[], query: string) => MenuLeaf[];

/**
 * La búsqueda difusa (fuse.js) se descarga la primera vez que se escribe en el buscador; hasta entonces, y si
 * la descarga falla, vale la búsqueda por palabras de `matchMenu`.
 */
function useFuzzyMenuSearch(wanted: boolean): MenuSearch | undefined {
  const [search, setSearch] = useState<MenuSearch | undefined>(undefined);
  useEffect(() => {
    if (!wanted || search) return undefined;
    let cancelled = false;
    import('./menu-search')
      .then((module) => {
        if (!cancelled) setSearch(() => module.searchMenu);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [wanted, search]);
  return search;
}

const GROUPS_KEY = 'karpo.nav.closed';

function readClosed(): Record<string, boolean> {
  try {
    const raw = readStored(GROUPS_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : {};
    return typeof parsed === 'object' && parsed !== null ? (parsed as Record<string, boolean>) : {};
  } catch {
    return {};
  }
}

export interface SidebarProps {
  collapsed: boolean;
  onToggleCollapsed(): void;
  /** Cajón abierto en pantallas estrechas. */
  open: boolean;
  onClose(): void;
}

export function Sidebar({ collapsed, onToggleCollapsed, open, onClose }: SidebarProps) {
  const { t } = useTranslation('core');
  const manifest = useManifest();
  const resolve = useText();
  const navigate = useNavigate();
  const visible = useVisibleMenu();
  const [query, setQuery] = useState('');
  const [closed, setClosed] = useState<Record<string, boolean>>(readClosed);
  const resultsId = useId();
  const listRef = useRef<HTMLUListElement>(null);

  const leaves = useMemo(() => flattenMenu(visible, manifest, resolve), [visible, manifest, resolve]);
  const searching = query.trim() !== '';
  const fuzzy = useFuzzyMenuSearch(searching);
  const results = useMemo(
    () => (searching ? (fuzzy ?? matchMenu)(leaves, query) : []),
    [leaves, query, searching, fuzzy],
  );

  function toggleGroup(id: string): void {
    setClosed((current) => {
      const next = { ...current, [id]: !current[id] };
      writeStored(GROUPS_KEY, JSON.stringify(next));
      return next;
    });
  }

  function onSearchKey(event: KeyboardEvent<HTMLInputElement>): void {
    if (event.key === 'Escape' && query !== '') {
      event.preventDefault();
      event.stopPropagation();
      setQuery('');
    } else if (event.key === 'Enter' && results[0]) {
      event.preventDefault();
      void navigate(`/${encodeURIComponent(results[0].entity)}`);
      setQuery('');
      onClose();
    } else if (event.key === 'ArrowDown') {
      const first = listRef.current?.querySelector<HTMLElement>('a');
      if (first) {
        event.preventDefault();
        first.focus();
      }
    }
  }

  return (
    <nav
      data-th="nav"
      data-kx-help="core.menu"
      data-kx-part="sidebar"
      data-kx-collapsed={collapsed ? '' : undefined}
      data-kx-open={open ? '' : undefined}
      aria-label={t('nav.label')}
    >
      <div data-kx-part="nav-head">
        <span data-th="brand" data-kx-part="brand">
          <Icon name="layers" />
          <span data-kx-part="brand-name">{t('app.name')}</span>
        </span>
        <Action
          kind="quiet"
          icon="sidebar"
          data-kx-part="collapse"
          aria-label={collapsed ? t('nav.expand') : t('nav.collapse')}
          aria-pressed={collapsed}
          onClick={onToggleCollapsed}
        />
        <Action kind="quiet" icon="x" data-kx-part="close" aria-label={t('nav.close')} onClick={onClose} />
      </div>

      <Toolbar search label={t('nav.search.label')} data-kx-help="core.search" data-kx-part="nav-search">
        <Icon name="search" />
        <input
          type="search"
          value={query}
          placeholder={t('nav.search.placeholder')}
          aria-label={t('nav.search.label')}
          aria-controls={resultsId}
          autoComplete="off"
          spellCheck={false}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={onSearchKey}
        />
      </Toolbar>

      <ScopeButton />

      <div id={resultsId} data-kx-part="nav-body">
        {searching ? (
          <>
            <p role="status" data-kx-part="nav-status">
              {results.length === 0
                ? t('nav.search.empty', { query: query.trim() })
                : t('nav.search.results', { n: results.length })}
            </p>
            <ul ref={listRef} data-kx-part="nav-results">
              {results.map((leaf) => (
                <li key={leaf.id}>
                  <NavItem
                    leaf={leaf}
                    onNavigate={() => {
                      setQuery('');
                      onClose();
                    }}
                    showGroup
                  />
                </li>
              ))}
            </ul>
          </>
        ) : (
          <MenuLevel nodes={visible} closed={closed} onToggle={toggleGroup} collapsed={collapsed} onNavigate={onClose} />
        )}
      </div>
    </nav>
  );
}

function ScopeButton() {
  const { t } = useTranslation('core');
  const { scope } = useSessionState();
  const label = scope.length === 0 ? t('nav.scope.all') : t('nav.scope.some', { n: scope.length });
  return (
    <Popover.Root>
      <Popover.Trigger asChild>
        <Action
          kind="quiet"
          icon="office"
          data-kx-part="scope"
          title={t('nav.scope.title')}
          aria-label={`${t('nav.scope.title')}: ${label}`}
        >
          <span data-kx-part="scope-label">{label}</span>
        </Action>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content data-kx-ui="popover" data-th-elevation="overlay" align="start" sideOffset={4}>
          <OrganizationScopePicker />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

interface LevelProps {
  nodes: readonly MenuNode[];
  closed: Record<string, boolean>;
  onToggle(id: string): void;
  collapsed: boolean;
  onNavigate(): void;
}

function MenuLevel({ nodes, closed, onToggle, collapsed, onNavigate }: LevelProps) {
  const resolve = useText();
  const manifest = useManifest();
  return (
    <ul data-kx-part="nav-list">
      {nodes.map((node) => {
        const label = resolve(node.label);
        if (node.entity !== undefined && !node.children?.length) {
          const config = manifest.entities[node.entity];
          const leaf: MenuLeaf = {
            id: node.id,
            entity: node.entity,
            label: label || resolve(config?.namePlural) || node.entity,
            group: '',
            icon: node.icon ?? config?.icon,
            keywords: '',
          };
          return (
            <li key={node.id}>
              <NavItem leaf={leaf} onNavigate={onNavigate} />
            </li>
          );
        }
        const isClosed = closed[node.id] === true && !collapsed;
        return (
          <li key={node.id} data-kx-part="nav-group">
            <GroupToggle
              node={node}
              label={label}
              closed={isClosed}
              onToggle={() => onToggle(node.id)}
            />
            {isClosed ? null : (
              <MenuLevel
                nodes={node.children ?? []}
                closed={closed}
                onToggle={onToggle}
                collapsed={collapsed}
                onNavigate={onNavigate}
              />
            )}
          </li>
        );
      })}
    </ul>
  );
}

function GroupToggle({
  node,
  label,
  closed,
  onToggle,
}: {
  node: MenuNode;
  label: string;
  closed: boolean;
  onToggle(): void;
}) {
  return (
    <button type="button" data-kx-part="group-toggle" aria-expanded={!closed} onClick={onToggle} title={label}>
      {node.icon ? <Icon name={node.icon} /> : null}
      <span data-kx-part="group-label">{label}</span>
      <Icon name={closed ? 'chevron-right' : 'chevron-down'} />
    </button>
  );
}

function NavItem({
  leaf,
  onNavigate,
  showGroup,
}: {
  leaf: MenuLeaf;
  onNavigate(): void;
  showGroup?: boolean;
}) {
  return (
    <NavLink to={`/${encodeURIComponent(leaf.entity)}`} data-th="nav-item" title={leaf.label} onClick={onNavigate}>
      <Icon name={leaf.icon ?? 'folder'} />
      <span data-kx-part="item-label">{leaf.label}</span>
      {showGroup && leaf.group ? <span data-kx-part="item-group">{leaf.group}</span> : null}
    </NavLink>
  );
}
