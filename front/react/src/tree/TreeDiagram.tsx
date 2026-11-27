import {
  Handle,
  MiniMap,
  Panel,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useOnViewportChange,
  useReactFlow,
  type Edge,
  type Node,
  type NodeProps,
  type PanelPosition,
} from '@xyflow/react';
import {
  createContext,
  memo,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
} from 'react';
import { useText } from '../config';
import type { EntityConfig, FieldDef, Row, Text } from '../contracts';
import { FieldView } from '../fields';
import { resolveThemeLength, useTheme } from '../theme';
import { Action, Icon, Spinner } from '../ui';
import {
  FAR_ZOOM,
  FIT_MIN_ZOOM,
  MAX_ZOOM,
  MIN_ZOOM,
  farFontSize,
  fitModesDiffer,
  fitViewport,
  isFarZoom,
  type FitInput,
  type FitMode,
} from './fit';
import { diagramKeyAction } from './keyboard';
import { layoutTree } from './layout';
import { labelText, plainFieldText, type NodeFieldDefs } from './fields';
import { NodeText } from './Highlighted';
import { useTreeT, type TreeT } from './i18n';
import { cameraDuration } from './motion';
import { flattenVisible, hasChildren, type Descendants, type TreeModel } from './model';

/** Tamaño de respaldo de una tarjeta cuando no se puede medir (sin estilos cargados, jsdom). */
export const FALLBACK_CARD = { width: 240, height: 128 } as const;
const FALLBACK_GAP = { x: 24, y: 56 } as const;
/** Con más nodos que esto sólo se pintan los visibles en el encuadre. */
const CULL_THRESHOLD = 250;
/** Aire del encuadre: fracción de la menor dimensión del lienzo. */
const FIT_PADDING_RATIO = 0.08;
/** Tamaño del encuadre cuando no se puede medir (jsdom). */
const FALLBACK_VIEW = { width: 1024, height: 640 } as const;
/** En el zoom lejano el texto no crece más que una fracción del alto de la tarjeta (cabe el título y el contador). */
const FAR_FONT_CEILING = 2.4;

type Resolve = (text: Text | undefined) => string;

interface CardData extends Record<string, unknown> {
  nodeId: string;
}
type CardNodeType = Node<CardData, 'card'>;

interface DiagramContextValue {
  config: EntityConfig;
  model: TreeModel;
  expanded: ReadonlySet<string>;
  defs: NodeFieldDefs;
  counts: ReadonlyMap<string, Descendants>;
  selectedId: string | null;
  currentId: string | null;
  matches: ReadonlySet<string>;
  query: string;
  branchLoading: ReadonlySet<string>;
  locale: string;
  firstId: string | null;
  direction: 'ltr' | 'rtl';
  t: TreeT;
  text: Resolve;
  onCurrent(id: string): void;
  onSelect(id: string): void;
  onToggle(id: string, open: boolean): void;
  onOpen(id: string): void;
  onKey(event: KeyboardEvent<HTMLElement>, id: string): void;
}

const DiagramContext = createContext<DiagramContextValue | null>(null);

function FieldCell({ row, def }: { row: Row; def: FieldDef }) {
  return <FieldView value={row[def.field]} def={def} row={row} />;
}

/**
 * `title` con lo que el elemento muestra, al pasar el puntero. Para lo que no es texto plano (un `lookup` se resuelve
 * en pantalla, no aquí): el contenido de ahora, no el de cuando se pintó la tarjeta.
 */
function titleFromContent(event: MouseEvent<HTMLElement>): void {
  const element = event.currentTarget;
  const shown = element.textContent?.trim() ?? '';
  if (shown !== '' && element.getAttribute('title') !== shown) element.setAttribute('title', shown);
}

/** Contenido de una tarjeta; lo comparten el nodo real y la sonda de medida. */
function CardBody({ row, defs, locale, query, text }: { row: Row; defs: NodeFieldDefs; locale: string; query: string; text: Resolve }) {
  // Rótulo y subtítulo caben en dos líneas (R3-4, M-7.3); si aun así se recortan, el `title` lleva el texto entero.
  const subtitle = defs.subtitle ? plainFieldText(row, defs.subtitle, locale, text) : '';
  return (
    <>
      <span data-kx-part="label" title={labelText(row, defs, locale, text)}>
        <NodeText row={row} def={defs.label} locale={locale} resolve={text} query={query} plain />
      </span>
      {defs.subtitle ? (
        <span data-kx-part="subtitle" {...(subtitle !== '' ? { title: subtitle } : { onMouseEnter: titleFromContent })}>
          <NodeText row={row} def={defs.subtitle} locale={locale} resolve={text} query={query} />
        </span>
      ) : null}
      {defs.badge ? (
        <span data-kx-part="badge">
          <FieldCell row={row} def={defs.badge} />
        </span>
      ) : null}
    </>
  );
}

const CardNode = memo(function CardNode({ data }: NodeProps<CardNodeType>) {
  const ctx = useContext(DiagramContext);
  const countId = useId();
  const node = ctx?.model.nodes.get(data.nodeId);
  if (!ctx || !node) return null;
  const { t } = ctx;
  const id = node.id;
  const expandable = hasChildren(node);
  const open = expandable && ctx.expanded.has(id);
  const count = ctx.counts.get(id);
  const name = labelText(node.row, ctx.defs, ctx.locale, ctx.text);
  const loading = ctx.branchLoading.has(id);
  const counted = count !== undefined && count.count > 0;
  return (
    <div
      className="nopan"
      data-kx-part="card"
      dir={ctx.direction}
      data-kx-selected={id === ctx.selectedId ? 'true' : undefined}
      data-kx-match={ctx.matches.has(id) ? 'true' : undefined}
      data-kx-folded={expandable && !open ? 'true' : undefined}
      data-kx-expandable={expandable ? 'true' : undefined}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} aria-hidden="true" />
      <button
        type="button"
        data-kx-part="main"
        data-kx-id={id}
        data-kx-help={ctx.firstId === id ? 'tree.node' : undefined}
        aria-pressed={id === ctx.selectedId}
        tabIndex={id === ctx.currentId ? 0 : -1}
        onClick={() => {
          ctx.onCurrent(id);
          ctx.onSelect(id);
        }}
        onDoubleClick={() => ctx.onOpen(id)}
        onFocus={() => ctx.onCurrent(id)}
        onKeyDown={(event) => ctx.onKey(event, id)}
      >
        <CardBody row={node.row} defs={ctx.defs} locale={ctx.locale} query={ctx.query} text={ctx.text} />
      </button>
      {expandable ? (
        <button
          type="button"
          data-kx-part="fold"
          tabIndex={-1}
          aria-expanded={open}
          aria-label={t(open ? 'diagram.fold' : 'diagram.unfold', { name })}
          aria-describedby={counted ? countId : undefined}
          onClick={() => {
            ctx.onCurrent(id);
            ctx.onToggle(id, !open);
          }}
          onDoubleClick={(event) => event.stopPropagation()}
        >
          {loading ? <Spinner decorative /> : <Icon name={open ? 'chevron-up' : 'chevron-down'} />}
          {counted ? (
            <span
              id={countId}
              data-kx-part="count"
              data-kx-partial={count.partial ? 'true' : undefined}
              role="img"
              aria-label={t(count.partial ? 'list.descendantsPartial' : 'list.descendants', { count: count.count })}
            >
              <bdi>{count.partial ? `≥${count.count}` : count.count}</bdi>
            </span>
          ) : null}
        </button>
      ) : null}
      <Handle type="source" position={Position.Bottom} isConnectable={false} aria-hidden="true" />
    </div>
  );
});

const NODE_TYPES = { card: CardNode };
/** Sin el enlace de atribución (apunta a un sitio de terceros; el MIT de @xyflow lo permite, ver docs/notas/T7.md). */
const PRO_OPTIONS = { hideAttribution: true } as const;

/** Dirección de esta misma pantalla con otra disposición (para que «Ver como lista» sea un enlace de verdad). */
function layoutHref(layout: string): string {
  if (typeof window === 'undefined') return `?layout=${layout}`;
  const url = new URL(window.location.href);
  url.searchParams.set('layout', layout);
  return `${url.pathname}${url.search}`;
}

export interface TreeDiagramProps {
  config: EntityConfig;
  model: TreeModel;
  expanded: ReadonlySet<string>;
  defs: NodeFieldDefs;
  counts: ReadonlyMap<string, Descendants>;
  selectedId: string | null;
  currentId: string | null;
  matches: ReadonlySet<string>;
  query: string;
  branchLoading: ReadonlySet<string>;
  direction: 'ltr' | 'rtl';
  locale: string;
  /** Pide centrar un nodo (búsqueda): el contador cambia en cada petición. */
  reveal: { id: string; seq: number } | null;
  onCurrent(id: string): void;
  onSelect(id: string): void;
  onToggle(id: string, open: boolean): void;
  onOpen(id: string): void;
  /** «Ver como lista»: la alternativa accesible al diagrama (M-13). */
  onShowList(): void;
  /** `true` cuando la disposición y el encuadre han terminado. */
  onReady(ready: boolean): void;
}

export function TreeDiagram(props: TreeDiagramProps) {
  return (
    <ReactFlowProvider>
      <DiagramInner {...props} />
    </ReactFlowProvider>
  );
}

function DiagramInner(props: TreeDiagramProps) {
  const { t } = useTreeT();
  const text = useText();
  const theme = useTheme();
  const flow = useReactFlow();
  const { model, expanded, direction, onReady } = props;
  const wrapRef = useRef<HTMLDivElement>(null);
  const probeRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState<{ width: number; height: number; gapX: number; gapY: number }>({
    ...FALLBACK_CARD,
    gapX: FALLBACK_GAP.x,
    gapY: FALLBACK_GAP.y,
  });
  const pendingFocus = useRef<string | null>(null);
  const rtl = direction === 'rtl';

  // Medida de la tarjeta (la define el CSS del tema) y de los huecos; se repite al cambiar de tema.
  useLayoutEffect(() => {
    const measure = () => {
      const probe = probeRef.current;
      if (!probe) return;
      const rect = probe.getBoundingClientRect();
      const gapX = resolveThemeLength('--th-space-4', wrapRef.current ?? undefined);
      const gapY = resolveThemeLength('--th-space-8', wrapRef.current ?? undefined);
      const next = {
        width: rect.width > 0 ? Math.round(rect.width) : FALLBACK_CARD.width,
        height: rect.height > 0 ? Math.round(rect.height) : FALLBACK_CARD.height,
        gapX: Number.isFinite(gapX) && gapX > 0 ? gapX : FALLBACK_GAP.x,
        gapY: Number.isFinite(gapY) && gapY > 0 ? gapY * 2 : FALLBACK_GAP.y,
      };
      setSize((prev) => (prev.width === next.width && prev.height === next.height && prev.gapX === next.gapX && prev.gapY === next.gapY ? prev : next));
    };
    measure();
    const stop = theme.onChange(measure);
    const observer = typeof ResizeObserver === 'function' && probeRef.current ? new ResizeObserver(measure) : null;
    if (observer && probeRef.current) observer.observe(probeRef.current);
    return () => {
      stop();
      observer?.disconnect();
    };
  }, [theme]);

  const items = useMemo(() => flattenVisible(model, expanded), [model, expanded]);

  const layout = useMemo(() => {
    const nodes = items.map((item) => ({ id: item.id, width: size.width, height: size.height }));
    const edges = items.filter((item) => item.parentId !== null).map((item) => ({ source: item.parentId as string, target: item.id }));
    return { result: layoutTree(nodes, edges, { direction, gapX: size.gapX, gapY: size.gapY }), edges };
  }, [items, size, direction]);

  const { defs, locale } = props;
  // Cada nodo es un grupo con nombre propio (M-13): el lector de pantalla anuncia «nodo del organigrama: <nombre>».
  const nodes = useMemo<CardNodeType[]>(
    () =>
      items.map((item) => {
        const row = model.nodes.get(item.id)?.row;
        return {
          id: item.id,
          type: 'card' as const,
          position: layout.result.positions.get(item.id) ?? { x: 0, y: 0 },
          data: { nodeId: item.id },
          width: size.width,
          height: size.height,
          draggable: false,
          selectable: false,
          connectable: false,
          focusable: false,
          ariaRole: 'group' as const,
          ariaLabel: row ? labelText(row, defs, locale, text) : item.id,
          domAttributes: { 'aria-roledescription': t('diagram.nodeRole') },
        };
      }),
    [items, layout, size, model, defs, locale, text, t],
  );

  // Las aristas sólo dibujan la jerarquía: ésta la cuentan los grupos y el listado, así que no se anuncian.
  const edges = useMemo<Edge[]>(
    () =>
      layout.edges.map((edge) => ({
        id: `${edge.source}->${edge.target}`,
        source: edge.source,
        target: edge.target,
        type: 'smoothstep',
        selectable: false,
        focusable: false,
        ariaRole: 'none' as const,
        // `null` quita la etiqueta en inglés («Edge from … to …») que @xyflow pone por omisión; el tipo no lo admite.
        ariaLabel: null as unknown as undefined,
        domAttributes: { 'aria-hidden': true },
      })),
    [layout],
  );

  /** Medidas del encuadre: el diagrama entero, la franja de las raíces y el lienzo. */
  const fitInput = useCallback((): FitInput => {
    const box = wrapRef.current?.getBoundingClientRect();
    const width = box && box.width > 0 ? box.width : FALLBACK_VIEW.width;
    const height = box && box.height > 0 ? box.height : FALLBACK_VIEW.height;
    let left = Infinity;
    let right = -Infinity;
    for (const id of model.roots) {
      const position = layout.result.positions.get(id);
      if (!position) continue;
      left = Math.min(left, position.x);
      right = Math.max(right, position.x + size.width);
    }
    return {
      diagram: { width: Math.max(1, layout.result.width), height: Math.max(1, layout.result.height) },
      roots: Number.isFinite(left) ? { x: left, width: right - left } : { x: 0, width: Math.max(1, layout.result.width) },
      view: { width, height },
      padding: Math.round(Math.min(width, height) * FIT_PADDING_RATIO),
      rtl,
    };
  }, [layout, model.roots, size.width, rtl]);

  /**
   * Ajusta el diagrama al encuadre. Se calcula con la disposición propia (que ya conoce el tamaño de cada
   * tarjeta) y no con `fitView`, que espera a que @xyflow mida los nodos. `legible` no baja de 0,85 y se
   * ancla en la raíz; `all` enseña todo el diagrama aunque sea pequeño.
   */
  const fit = useCallback(
    (mode: FitMode, duration: number) => flow.setViewport(fitViewport(fitInput(), mode), { duration }),
    [flow, fitInput],
  );

  // «Ajustar» alterna entre el encuadre legible y todo el diagrama; cualquier otro movimiento lo deja en «a mano».
  const fitState = useRef<'legible' | 'all' | 'manual'>('legible');
  const [showingAll, setShowingAll] = useState(false);
  const setFitState = useCallback((next: 'legible' | 'all' | 'manual') => {
    fitState.current = next;
    setShowingAll(next === 'all');
  }, []);
  const onManual = useCallback(() => setFitState('manual'), [setFitState]);
  const onFitClick = () => {
    const next: FitMode = fitState.current === 'legible' && fitModesDiffer(fitInput()) ? 'all' : 'legible';
    setFitState(next);
    void fit(next, cameraDuration(300));
  };

  // Zoom semántico: lejos sólo se ven el título y el contador, y el texto crece para seguir siendo legible.
  const [far, setFar] = useState(false);
  useOnViewportChange({
    onChange: (viewport) => {
      const next = isFarZoom(viewport.zoom);
      setFar((prev) => (prev === next ? prev : next));
      if (next) wrapRef.current?.style.setProperty('--kx-far-font', `${farFontSize(viewport.zoom, size.height / FAR_FONT_CEILING)}px`);
    },
  });

  // Encuadre inicial: cuando hay nodos dispuestos se ajusta una vez y se avisa de que está listo.
  const fitted = useRef(false);
  const mounted = useRef(true);
  const frame = useRef(0);
  const fitNow = useRef(fit);
  useEffect(() => {
    fitNow.current = fit;
  }, [fit]);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      // Con StrictMode los efectos se montan, desmontan y montan otra vez: el encuadre debe poder repetirse.
      fitted.current = false;
      cancelAnimationFrame(frame.current);
      onReady(false);
    };
  }, [onReady]);
  const hasNodes = nodes.length > 0;
  useEffect(() => {
    if (!hasNodes) {
      fitted.current = false;
      onReady(false);
      return;
    }
    if (fitted.current) return;
    fitted.current = true;
    const done = () => {
      if (mounted.current) onReady(true);
    };
    // @xyflow aplica su encuadre por defecto al terminar de montarse: se espera un fotograma para ajustar después
    // (con la disposición más reciente, que puede haber cambiado al medir las tarjetas).
    frame.current = requestAnimationFrame(() => {
      setFitState('legible');
      void Promise.resolve(fitNow.current('legible', 0)).then(done, done);
    });
  }, [hasNodes, onReady, setFitState]);

  /** Centra un nodo si queda fuera del encuadre actual (o a la fuerza, para la búsqueda). */
  const center = useCallback(
    (id: string, force: boolean) => {
      const position = layout.result.positions.get(id);
      const wrap = wrapRef.current;
      if (!position || !wrap) return;
      const { x, y, zoom } = flow.getViewport();
      const box = wrap.getBoundingClientRect();
      const left = position.x * zoom + x;
      const top = position.y * zoom + y;
      const inside =
        left >= 0 && top >= 0 && left + size.width * zoom <= box.width && top + size.height * zoom <= box.height;
      if (inside && !force) return;
      setFitState('manual');
      void flow.setCenter(position.x + size.width / 2, position.y + size.height / 2, {
        zoom: Math.min(MAX_ZOOM, Math.max(zoom, FAR_ZOOM)),
        duration: cameraDuration(300),
      });
    },
    [flow, layout, size, setFitState],
  );

  const reveal = props.reveal;
  const lastReveal = useRef(0);
  useEffect(() => {
    if (!reveal || reveal.seq === lastReveal.current) return;
    lastReveal.current = reveal.seq;
    center(reveal.id, true);
  }, [reveal, center]);

  // Foco del teclado: el botón de la tarjeta recibe el foco real tras pintar.
  useLayoutEffect(() => {
    const id = pendingFocus.current;
    if (id === null) return;
    const element = wrapRef.current?.querySelector<HTMLElement>(`[data-kx-part="main"][data-kx-id="${CSS.escape(id)}"]`);
    if (element) {
      element.focus({ preventScroll: true });
      pendingFocus.current = null;
    }
  });

  const { onCurrent, onToggle, onOpen } = props;
  const onKey = useCallback(
    (event: KeyboardEvent<HTMLElement>, id: string) => {
      const action = diagramKeyAction(model, expanded, id, event, { rtl });
      if (!action) return;
      event.preventDefault();
      switch (action.type) {
        case 'focus':
          pendingFocus.current = action.id;
          onCurrent(action.id);
          center(action.id, false);
          break;
        case 'expand':
          onToggle(action.id, true);
          break;
        case 'collapse':
          onToggle(action.id, false);
          break;
        case 'activate':
          onOpen(action.id);
          break;
        default:
          break;
      }
    },
    [model, expanded, rtl, onCurrent, onToggle, onOpen, center],
  );

  const context = useMemo<DiagramContextValue>(
    () => ({
      config: props.config,
      model,
      expanded,
      defs: props.defs,
      counts: props.counts,
      selectedId: props.selectedId,
      currentId: props.currentId,
      matches: props.matches,
      query: props.query,
      branchLoading: props.branchLoading,
      locale: props.locale,
      firstId: items[0]?.id ?? null,
      direction,
      t,
      text,
      onCurrent,
      onSelect: props.onSelect,
      onToggle,
      onOpen,
      onKey,
    }),
    [props.config, model, expanded, props.defs, props.counts, props.selectedId, props.currentId, props.matches, props.query, props.branchLoading, props.locale, items, direction, t, text, onCurrent, props.onSelect, onToggle, onOpen, onKey],
  );

  const ariaLabelConfig = useMemo(
    () => ({
      'node.a11yDescription.default': t('diagram.nodeHint'),
      'node.a11yDescription.keyboardDisabled': t('diagram.nodeHint'),
      'controls.ariaLabel': t('diagram.controls'),
      'controls.zoomIn.ariaLabel': t('diagram.zoomIn'),
      'controls.zoomOut.ariaLabel': t('diagram.zoomOut'),
      'controls.fitView.ariaLabel': t('diagram.fit'),
      'minimap.ariaLabel': t('diagram.minimap'),
    }),
    [t],
  );

  const controlsAt: PanelPosition = rtl ? 'bottom-right' : 'bottom-left';
  // El minimapa abajo, en el lado contrario a los controles: arriba taparía la raíz, que es lo primero que se mira.
  const minimapAt: PanelPosition = rtl ? 'bottom-left' : 'bottom-right';
  const probeNode = model.nodes.get(items[0]?.id ?? '') ?? undefined;
  const name = text(props.config.namePlural) || text(props.config.name);
  const { onShowList } = props;
  const onListClick = (event: MouseEvent<HTMLAnchorElement>) => {
    // Con teclas modificadoras o botón central el navegador abre el enlace donde corresponda.
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onShowList();
  };

  return (
    <DiagramContext.Provider value={context}>
      <div
        ref={wrapRef}
        data-kx-part="diagram"
        data-kx-zoom={far ? 'far' : 'near'}
        role="region"
        aria-label={t('diagram.label', { name })}
        aria-roledescription={t('diagram.roleDescription')}
      >
        <a
          href={layoutHref('list')}
          data-kx-part="list-link"
          data-kx-help="tree.listlink"
          data-th="action"
          data-th-kind="secondary"
          onClick={onListClick}
        >
          <Icon name="list-bullets" />
          {t('diagram.asList')}
        </a>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={NODE_TYPES}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={false}
          nodesFocusable={false}
          edgesFocusable={false}
          disableKeyboardA11y
          zoomOnDoubleClick={false}
          minZoom={MIN_ZOOM}
          maxZoom={MAX_ZOOM}
          onlyRenderVisibleElements={nodes.length > CULL_THRESHOLD}
          onNodeClick={(_event, node) => onCurrent(node.id)}
          onMoveStart={(event) => {
            // Con evento es el usuario (arrastrar, rueda, gesto); los movimientos de la cámara no lo llevan.
            if (event) onManual();
          }}
          deleteKeyCode={null}
          proOptions={PRO_OPTIONS}
          ariaLabelConfig={ariaLabelConfig}
        >
          <Panel position={controlsAt} data-kx-part="controls" data-kx-help="tree.zoom" role="group" aria-label={t('diagram.controls')}>
            <Action
              kind="secondary"
              icon="zoom-in"
              aria-label={t('diagram.zoomIn')}
              onClick={() => {
                onManual();
                void flow.zoomIn({ duration: cameraDuration(150) });
              }}
            />
            <Action
              kind="secondary"
              icon="zoom-out"
              aria-label={t('diagram.zoomOut')}
              onClick={() => {
                onManual();
                void flow.zoomOut({ duration: cameraDuration(150) });
              }}
            />
            <Action
              kind="secondary"
              icon="fit"
              aria-label={t('diagram.fit')}
              aria-pressed={showingAll}
              title={t(showingAll ? 'diagram.fitLegible' : 'diagram.fitAll', { zoom: FIT_MIN_ZOOM })}
              onClick={onFitClick}
            />
          </Panel>
          <MiniMap
            position={minimapAt}
            ariaLabel={t('diagram.minimap')}
            pannable
            zoomable
            nodeClassName={(node) => (node.id === props.selectedId ? 'kx-minimap-selected' : props.matches.has(node.id) ? 'kx-minimap-match' : '')}
          />
        </ReactFlow>
        <div data-kx-part="probe" aria-hidden="true">
          <div ref={probeRef} data-kx-part="card">
            <span data-kx-part="main">
              {probeNode ? <CardBody row={probeNode.row} defs={props.defs} locale={props.locale} query="" text={text} /> : null}
            </span>
          </div>
        </div>
      </div>
    </DiagramContext.Provider>
  );
}
