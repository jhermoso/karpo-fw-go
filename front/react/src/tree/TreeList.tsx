import { observeElementRect, useVirtualizer } from '@tanstack/react-virtual';
import { useCallback, useEffect, useLayoutEffect, useRef, type KeyboardEvent, type MouseEvent } from 'react';
import type { EntityConfig, FieldDef, Row } from '../contracts';
import { useText } from '../config';
import { FieldView } from '../fields';
import { Icon, Spinner } from '../ui';
import { isTypeAheadKey, treeKeyAction, typeAheadTarget } from './keyboard';
import { labelText, type NodeFieldDefs } from './fields';
import { useTreeT } from './i18n';
import { NodeText } from './Highlighted';
import type { Descendants, TreeModel, VisibleItem } from './model';

/** Con más filas visibles que esto el listado se virtualiza (con menos, el DOM completo es más barato). */
export const VIRTUALIZE_THRESHOLD = 300;
const TYPE_AHEAD_RESET_MS = 600;
const ESTIMATED_ROW = 36;
const FALLBACK_RECT = { width: 800, height: 600 } as const;

export interface TreeListProps {
  config: EntityConfig;
  model: TreeModel;
  items: readonly VisibleItem[];
  defs: NodeFieldDefs;
  counts: ReadonlyMap<string, Descendants>;
  selectedId: string | null;
  /** Elemento con el foco del teclado (`tabindex="0"`). */
  currentId: string | null;
  matches: ReadonlySet<string>;
  query: string;
  branchLoading: ReadonlySet<string>;
  direction: 'ltr' | 'rtl';
  locale: string;
  /** Pide enseñar un elemento (búsqueda, cambio de disposición): el contador cambia en cada petición. */
  reveal: { id: string; seq: number } | null;
  onCurrent(id: string): void;
  onSelect(id: string): void;
  onToggle(id: string, open: boolean): void;
  onExpandSiblings(id: string): void;
  /** Abre el registro en edición (doble clic o Intro). */
  onOpen(id: string): void;
}

function FieldCell({ row, def }: { row: Row; def: FieldDef }) {
  return <FieldView value={row[def.field]} def={def} row={row} />;
}

export function TreeList(props: TreeListProps) {
  const { t } = useTreeT();
  const text = useText();
  const { items, model, defs, counts, selectedId, currentId, matches, query, direction, locale, onCurrent } = props;
  const treeRef = useRef<HTMLDivElement>(null);
  const typed = useRef<{ text: string; timer: ReturnType<typeof setTimeout> | undefined }>({ text: '', timer: undefined });
  // El foco real sólo se mueve al elemento cuando lo ordenó el teclado, no al montar ni al hacer clic.
  const pendingFocus = useRef<string | null>(null);

  const virtual = items.length > VIRTUALIZE_THRESHOLD;
  // eslint-disable-next-line react-hooks/incompatible-library -- el virtualizador se usa sólo dentro de este componente
  const virtualizer = useVirtualizer({
    count: virtual ? items.length : 0,
    getScrollElement: () => treeRef.current,
    estimateSize: () => ESTIMATED_ROW,
    // Hasta que se mida el contenedor (primer pintado) y si no tiene tamaño (jsdom), una ventana razonable.
    initialRect: FALLBACK_RECT,
    observeElementRect: (instance, report) =>
      observeElementRect(instance, (rect) => report(rect.height > 0 ? rect : FALLBACK_RECT)),
    // Una fila sin altura medible (jsdom) cuenta como la estimada.
    measureElement: (element) => {
      const height = element.getBoundingClientRect().height;
      return height > 0 ? height : ESTIMATED_ROW;
    },
    overscan: 12,
  });

  useEffect(() => {
    const state = typed.current;
    return () => clearTimeout(state.timer);
  }, []);

  const focusItem = useCallback(
    (id: string) => {
      pendingFocus.current = id;
      onCurrent(id);
      if (virtual) {
        const index = items.findIndex((item) => item.id === id);
        if (index >= 0) virtualizer.scrollToIndex(index);
      }
    },
    [onCurrent, virtual, items, virtualizer],
  );

  // Enseñar un elemento (búsqueda o vuelta desde el diagrama): se desplaza hasta él sin quitar el foco.
  const reveal = props.reveal;
  const lastReveal = useRef(0);
  useEffect(() => {
    if (!reveal || reveal.seq === lastReveal.current) return;
    lastReveal.current = reveal.seq;
    if (virtual) {
      const index = items.findIndex((item) => item.id === reveal.id);
      if (index >= 0) virtualizer.scrollToIndex(index);
      return;
    }
    treeRef.current
      ?.querySelector<HTMLElement>(`[data-kx-id="${CSS.escape(reveal.id)}"]`)
      ?.scrollIntoView({ block: 'nearest' });
  }, [reveal, virtual, items, virtualizer]);

  // Tras pintar, el elemento ordenado recibe el foco real del navegador (si ya existe en el DOM).
  useLayoutEffect(() => {
    const id = pendingFocus.current;
    if (id === null) return;
    const element = treeRef.current?.querySelector<HTMLElement>(`[data-kx-id="${CSS.escape(id)}"]`);
    if (element) {
      element.focus();
      pendingFocus.current = null;
    }
  });

  const labelOf = useCallback(
    (id: string) => {
      const node = model.nodes.get(id);
      return node ? labelText(node.row, defs, locale, text) : '';
    },
    [model, defs, locale, text],
  );

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target instanceof HTMLElement && event.target.closest('input, textarea, select')) return;
    const action = treeKeyAction(items, currentId, event, { rtl: direction === 'rtl' });
    if (action) {
      event.preventDefault();
      switch (action.type) {
        case 'focus':
          focusItem(action.id);
          break;
        case 'expand':
          props.onToggle(action.id, true);
          break;
        case 'collapse':
          props.onToggle(action.id, false);
          break;
        case 'expand-siblings':
          props.onExpandSiblings(action.id);
          break;
        case 'select':
          props.onSelect(action.id);
          break;
        case 'activate':
          props.onOpen(action.id);
          break;
      }
      return;
    }
    if (isTypeAheadKey(event)) {
      const state = typed.current;
      clearTimeout(state.timer);
      state.text += event.key;
      state.timer = setTimeout(() => {
        state.text = '';
      }, TYPE_AHEAD_RESET_MS);
      const target = typeAheadTarget(items, currentId, state.text, labelOf);
      if (target) {
        event.preventDefault();
        focusItem(target);
      }
    }
  };

  const renderItem = (item: VisibleItem, index: number, start: number | undefined) => {
    const node = model.nodes.get(item.id);
    if (!node) return null;
    const count = counts.get(item.id);
    const selected = item.id === selectedId;
    const loading = props.branchLoading.has(item.id);
    return (
      <div
        key={item.id}
        ref={virtual ? virtualizer.measureElement : undefined}
        data-index={virtual ? index : undefined}
        role="treeitem"
        data-kx-id={item.id}
        data-kx-part="item"
        data-kx-help={index === 0 ? 'tree.node' : undefined}
        data-kx-match={matches.has(item.id) ? 'true' : undefined}
        aria-level={item.level}
        aria-setsize={item.setSize}
        aria-posinset={item.posInSet}
        aria-expanded={item.expandable ? item.expanded : undefined}
        aria-selected={selected}
        aria-busy={loading || undefined}
        tabIndex={item.id === currentId ? 0 : -1}
        style={start === undefined ? { '--kx-level': item.level } : { '--kx-level': item.level, transform: `translateY(${start}px)` }}
        onClick={() => {
          onCurrent(item.id);
          props.onSelect(item.id);
        }}
        onDoubleClick={(event: MouseEvent) => {
          event.preventDefault();
          props.onOpen(item.id);
        }}
        onFocus={(event) => {
          if (event.target === event.currentTarget) onCurrent(item.id);
        }}
      >
        <span
          data-kx-part="toggle"
          aria-hidden="true"
          data-kx-leaf={item.expandable ? undefined : 'true'}
          onClick={(event) => {
            event.stopPropagation();
            onCurrent(item.id);
            if (item.expandable) props.onToggle(item.id, !item.expanded);
          }}
          onDoubleClick={(event) => event.stopPropagation()}
        >
          {item.expandable ? <Icon name={item.expanded ? 'chevron-down' : 'chevron-right'} /> : null}
        </span>
        <span data-kx-part="label">
          <NodeText row={node.row} def={defs.label} locale={locale} resolve={text} query={query} plain />
        </span>
        {defs.subtitle ? (
          <span data-kx-part="subtitle">
            <NodeText row={node.row} def={defs.subtitle} locale={locale} resolve={text} query={query} />
          </span>
        ) : null}
        {defs.badge ? (
          <span data-kx-part="badge">
            <FieldCell row={node.row} def={defs.badge} />
          </span>
        ) : null}
        {loading ? <Spinner label={t('list.loadingBranch')} /> : null}
        {count && count.count > 0 ? (
          <span data-kx-part="count" data-kx-partial={count.partial ? 'true' : undefined}>
            <span aria-hidden="true">
              <bdi>{count.partial ? `≥${count.count}` : count.count}</bdi>
            </span>
            <span data-kx-part="sr-only">
              {t(count.partial ? 'list.descendantsPartial' : 'list.descendants', { count: count.count })}
            </span>
          </span>
        ) : null}
      </div>
    );
  };

  return (
    <div
      ref={treeRef}
      role="tree"
      aria-label={t('list.label', { name: text(props.config.namePlural) || text(props.config.name) })}
      aria-multiselectable="false"
      data-kx-part="tree"
      data-kx-virtual={virtual ? 'true' : undefined}
      onKeyDown={onKeyDown}
    >
      {virtual ? (
        <div role="none" data-kx-part="spacer" style={{ height: virtualizer.getTotalSize() }}>
          {virtualizer.getVirtualItems().map((v) => renderItem(items[v.index]!, v.index, v.start))}
        </div>
      ) : (
        items.map((item, index) => renderItem(item, index, undefined))
      )}
    </div>
  );
}
