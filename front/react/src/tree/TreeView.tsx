import { useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import type { EntityViewProps } from '../contracts';
import { useText } from '../config';
import { useLocale } from '../i18n';
import { useHelp } from '../help';
import { Action, Feedback, Icon, Skeleton, Toolbar } from '../ui';
import { searchText, nodeFieldDefs } from './fields';
import { useTreeT } from './i18n';
import {
  ancestorsOf,
  countDescendants,
  expandForMatches,
  expandableIds,
  findMatches,
  flattenVisible,
  siblingsOf,
  type TreeViewDef,
} from './model';
import { TreeDiagram } from './TreeDiagram';
import { TreeList } from './TreeList';
import { useTreeData } from './use-tree-data';

export type TreeLayout = 'list' | 'diagram';

export function treeLayoutOf(params: Record<string, string> | undefined): TreeLayout {
  return params?.layout === 'diagram' ? 'diagram' : 'list';
}

/**
 * Vista de organigrama de una entidad: listado `role=tree` o diagrama, con el estado compartido. La
 * disposición (`layout` en la URL) la elige el control de vista de la cabecera de la entidad (M-6); el
 * diagrama ofrece además «Ver como lista» como alternativa accesible.
 */
export function TreeView(props: EntityViewProps) {
  const view = props.config.views.find((v): v is TreeViewDef => v.kind === 'tree');
  if (!view) return <TreeMissing />;
  return <TreeViewInner {...props} view={view} />;
}

function TreeMissing() {
  const { t } = useTreeT();
  return <Feedback tone="warning">{t('missing')}</Feedback>;
}

function TreeViewInner({ config, route, navigate, onStatus, view }: EntityViewProps & { view: TreeViewDef }) {
  const { t, locale } = useTreeT();
  const text = useText();
  const { direction } = useLocale();
  const searchId = useId();
  const layout = treeLayoutOf(route.params);
  const data = useTreeData(config, view);
  const { model } = data;
  const defs = useMemo(() => nodeFieldDefs(config, view), [config, view]);
  const counts = useMemo(() => countDescendants(model), [model]);

  useHelp(`${config.id}:tree`, config.help.length > 0 ? config.help : undefined);

  // Plegado: `null` = el de partida (las raíces desplegadas). Al primer cambio se materializa.
  const [expandedState, setExpanded] = useState<ReadonlySet<string> | null>(null);
  const expanded = useMemo<ReadonlySet<string>>(() => expandedState ?? new Set(model.roots), [expandedState, model.roots]);
  const items = useMemo(() => flattenVisible(model, expanded), [model, expanded]);

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [currentState, setCurrentState] = useState<string | null>(null);
  const currentId = useMemo(() => {
    if (currentState !== null && items.some((item) => item.id === currentState)) return currentState;
    if (selectedId !== null && items.some((item) => item.id === selectedId)) return selectedId;
    return items[0]?.id ?? null;
  }, [currentState, selectedId, items]);

  const [query, setQuery] = useState('');
  const [matchIndex, setMatchIndex] = useState(0);
  const [reveal, setReveal] = useState<{ id: string; seq: number } | null>(null);
  const [diagramReady, setDiagramReady] = useState(false);

  const matches = useMemo(
    () => findMatches(model, query, (node) => searchText(node.row, defs, locale, text)),
    [model, query, defs, locale, text],
  );
  const matchSet = useMemo(() => new Set(matches), [matches]);

  const requestReveal = useCallback((id: string) => {
    setReveal((prev) => ({ id, seq: (prev?.seq ?? 0) + 1 }));
  }, []);

  // `?focus=<id>` (el enlace «Ver en el árbol» de la ficha): despliega los antecesores, selecciona el
  // elemento y lo centra. Se aplica una sola vez por valor, en cuanto el elemento está en el modelo; si
  // no llega (otra rama sin cargar o un id que no existe) la vista se queda como estaba.
  const focusId = route.params?.focus ?? '';
  const [handledFocus, setHandledFocus] = useState('');
  if (focusId !== '' && focusId !== handledFocus && !data.loading && model.nodes.has(focusId)) {
    setHandledFocus(focusId);
    setExpanded((prev) => expandForMatches(model, prev ?? new Set(model.roots), [focusId]));
    setSelectedId(focusId);
    setCurrentState(focusId);
    requestReveal(focusId);
  }

  // Al cambiar de disposición (desde la cabecera de la entidad o desde «Ver como lista») se enseña lo
  // elegido, para no perder el sitio. Se ajusta al renderizar, no en un efecto: es estado derivado de la URL.
  const [shownLayout, setShownLayout] = useState(layout);
  if (shownLayout !== layout) {
    setShownLayout(layout);
    if (selectedId !== null) requestReveal(selectedId);
  }

  const showList = useCallback(
    () => navigate({ ...route, view: 'tree', params: { ...route.params, layout: 'list' } }),
    [navigate, route],
  );

  const toggle = useCallback(
    (id: string, open: boolean) => {
      setExpanded((prev) => {
        const next = new Set(prev ?? model.roots);
        if (open) next.add(id);
        else next.delete(id);
        return next;
      });
      if (open && model.nodes.get(id)?.frontier) void data.loadBranch(id);
      // Si se pliega la rama que contiene el elemento actual, el actual pasa a ser la rama.
      if (!open && currentId !== null && ancestorsOf(model, currentId).includes(id)) setCurrentState(id);
    },
    // `data` cambia de identidad en cada render; sólo se usa `loadBranch`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [model, currentId, data.loadBranch],
  );

  const expandSiblings = useCallback(
    (id: string) => {
      const siblings = siblingsOf(model, id).filter((sibling) => {
        const node = model.nodes.get(sibling);
        return node !== undefined && (node.children.length > 0 || node.frontier);
      });
      setExpanded((prev) => {
        const next = new Set(prev ?? model.roots);
        for (const sibling of siblings) next.add(sibling);
        return next;
      });
      for (const sibling of siblings) if (model.nodes.get(sibling)?.frontier) void data.loadBranch(sibling);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [model, data.loadBranch],
  );

  const expandAll = () => setExpanded(new Set(expandableIds(model)));
  const collapseAll = () => {
    setExpanded(new Set());
    if (currentId !== null && model.nodes.get(currentId)?.parentId !== null) setCurrentState(model.roots[0] ?? null);
  };

  const open = useCallback(
    (id: string) => navigate({ entity: config.id, id, mode: 'edit' }),
    [navigate, config.id],
  );

  const select = useCallback((id: string) => setSelectedId(id), []);

  const onSearch = (value: string) => {
    setQuery(value);
    setMatchIndex(0);
    const found = findMatches(model, value, (node) => searchText(node.row, defs, locale, text));
    if (found.length > 0) {
      setExpanded((prev) => expandForMatches(model, prev ?? new Set(model.roots), found));
      requestReveal(found[0]!);
    }
  };

  const stepMatch = (delta: number) => {
    if (matches.length === 0) return;
    const next = (matchIndex + delta + matches.length) % matches.length;
    setMatchIndex(next);
    const id = matches[next]!;
    setExpanded((prev) => expandForMatches(model, prev ?? new Set(model.roots), [id]));
    setCurrentState(id);
    requestReveal(id);
  };

  const onSearchKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      stepMatch(event.shiftKey ? -1 : 1);
    } else if (event.key === 'Escape' && query !== '') {
      event.preventDefault();
      event.stopPropagation();
      onSearch('');
    }
  };

  // La cabecera de la entidad cuenta cuántos elementos hay y cuántos casan con la búsqueda (M-7).
  const statusOut = useRef(onStatus);
  useEffect(() => {
    statusOut.current = onStatus;
  });
  const reported = useRef('');
  const total = model.nodes.size;
  const matched = query.trim() === '' ? total : matches.length;
  const busy = data.loading || data.loadingMore;
  useEffect(() => {
    const key = `${total}|${matched}|${busy}`;
    if (key === reported.current) return;
    reported.current = key;
    statusOut.current?.({ total, matched, loading: busy });
  }, [total, matched, busy]);

  const ready =
    !data.loading && !data.error && (model.roots.length === 0 || layout === 'list' || diagramReady);
  const name = text(config.namePlural) || text(config.name);
  const status =
    query.trim() === ''
      ? ''
      : matches.length === 0
        ? t('search.none')
        : t('search.count', { current: matchIndex + 1, total: matches.length });

  return (
    <section
      data-kx-view="tree"
      data-kx-layout={layout}
      data-kx-ready={ready ? 'true' : undefined}
      aria-label={t('view.label', { name })}
      aria-busy={data.loading || undefined}
    >
      <Toolbar>
        <div role="search" aria-label={t('search.label')} data-kx-part="search" data-kx-help="tree.search">
          <label htmlFor={searchId} data-kx-part="sr-only">
            {t('search.label')}
          </label>
          <Icon name="search" />
          <input
            id={searchId}
            type="search"
            value={query}
            placeholder={t('search.placeholder')}
            autoComplete="off"
            onChange={(event) => onSearch(event.target.value)}
            onKeyDown={onSearchKey}
          />
          <Action
            kind="quiet"
            icon="chevron-up"
            aria-label={t('search.previous')}
            disabledReason={matches.length === 0 ? t('search.none') : undefined}
            onClick={() => stepMatch(-1)}
          />
          <Action
            kind="quiet"
            icon="chevron-down"
            aria-label={t('search.next')}
            disabledReason={matches.length === 0 ? t('search.none') : undefined}
            onClick={() => stepMatch(1)}
          />
          <span role="status" data-kx-part="search-status">
            {status}
          </span>
        </div>
        <div data-kx-part="folding">
          <Action kind="quiet" icon="expand" onClick={expandAll}>
            {t('toolbar.expandAll')}
          </Action>
          <Action kind="quiet" icon="collapse" onClick={collapseAll}>
            {t('toolbar.collapseAll')}
          </Action>
        </div>
      </Toolbar>

      <div data-kx-part="body">
        {data.error ? (
          <Feedback tone="error">
            <span>{t('error.load')}</span>
            <Action kind="secondary" icon="refresh" onClick={data.refetch}>
              {t('error.retry')}
            </Action>
          </Feedback>
        ) : data.loading ? (
          <Skeleton lines={6} label={t('loading')} />
        ) : model.roots.length === 0 ? (
          <Feedback tone="info">{t('empty')}</Feedback>
        ) : layout === 'list' ? (
          <TreeList
            config={config}
            model={model}
            items={items}
            defs={defs}
            counts={counts}
            selectedId={selectedId}
            currentId={currentId}
            matches={matchSet}
            query={query}
            branchLoading={data.branchLoading}
            direction={direction}
            locale={locale}
            reveal={reveal}
            onCurrent={setCurrentState}
            onSelect={select}
            onToggle={toggle}
            onExpandSiblings={expandSiblings}
            onOpen={open}
          />
        ) : (
          <TreeDiagram
            config={config}
            model={model}
            expanded={expanded}
            defs={defs}
            counts={counts}
            selectedId={selectedId}
            currentId={currentId}
            matches={matchSet}
            query={query}
            branchLoading={data.branchLoading}
            direction={direction}
            locale={locale}
            reveal={reveal}
            onCurrent={setCurrentState}
            onSelect={select}
            onToggle={toggle}
            onOpen={open}
            onShowList={showList}
            onReady={setDiagramReady}
          />
        )}
        {data.loadingMore ? (
          <p role="status" data-kx-part="more">
            {t('loadingMore')}
          </p>
        ) : null}
      </div>
    </section>
  );
}
