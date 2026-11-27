import { useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { useInRouterContext, useLocation, useNavigate } from 'react-router';
import type { EntityRoute, EntityViewProps } from '../contracts';
import { useText } from '../config';
import { useHelp } from '../help';
import { buildEntityTarget, targetToString } from '../shell/route';
import { fieldDefFor } from '../tree/fields';
import { Action, Feedback, Kbd, Skeleton, Toolbar } from '../ui';
import { binLabels, keyOf, type Bin } from './aggregate';
import { boundsOfFeatures, type GeoFeature } from './geo';
import { numberRangeFormatter, useMapT } from './i18n';
import { MapBreadcrumbs } from './MapBreadcrumbs';
import { MapCanvas } from './MapCanvas';
import { MapLegend } from './MapLegend';
import { MapTable } from './MapTable';
import { displayName } from './names';
import {
  childLevel,
  mapLayoutOf,
  navParams,
  parseNav,
  redirectTarget,
  skippableFeature,
  skipsSingleLevels,
  upTarget,
  type Crumb,
  type MapLayout,
  type MapNav,
  type MapViewDef,
} from './nav';
import { isSchematicSource } from './sources';
import { levelValues, useCrumbs, useEntryResolver, useMapLevel, useMapValues } from './use-map-level';

/** Vista de mapa de una entidad: coropletas por nivel (mundo, país, región, ciudad) y su tabla. */
export function MapView(props: EntityViewProps) {
  const view = props.config.views.find((v): v is MapViewDef => v.kind === 'map');
  if (!view) return <MapMissing />;
  return <MapViewInner {...props} view={view} />;
}

function MapMissing() {
  const { t } = useMapT();
  return <Feedback tone="warning">{t('missing')}</Feedback>;
}

const emptyFeatures: GeoFeature[] = [];

/**
 * Cambia la URL por otra SIN añadir una entrada al historial (R3-4, M-5): «Atrás» no rebota a un nivel que no se ve.
 * `EntityRoute` no sabe de `replace` (es el contrato de las vistas), así que se pide al enrutador. Se monta sólo
 * cuando hay una redirección y navega una vez por destino.
 */
function MapRedirect({ to, defaultView }: { to: EntityRoute; defaultView: EntityViewProps['config']['defaultView'] }) {
  const location = useLocation();
  const go = useNavigate();
  const url = targetToString(
    buildEntityTarget(to, { currentEntity: to.entity, currentSearch: new URLSearchParams(location.search), defaultView }),
  );
  const last = useRef('');
  useEffect(() => {
    if (last.current === url) return;
    last.current = url;
    void go(url, { replace: true });
  }, [go, url]);
  return null;
}

function MapViewInner({ config, route, navigate, onStatus, view }: EntityViewProps & { view: MapViewDef }) {
  const { t, locale } = useMapT();
  const text = useText();
  const statusId = useId();
  // Identidad estable por valor: de ella cuelgan la fuente de datos y el estilo del mapa.
  const parsed = parseNav(view, route.params);
  const nav = useMemo<MapNav>(() => ({ index: parsed.index, code: parsed.code }), [parsed.index, parsed.code]);
  // La disposición (mapa o tabla) vive en la URL: la cambia el botón de esta vista o el control de la cabecera.
  const layout = mapLayoutOf(route.params);
  const level = view.levels[nav.index]!;
  const levelName = text(level.label);
  const rootName = text(view.levels[0]!.label);
  const name = text(config.namePlural) || text(config.name);

  useHelp(`${config.id}:map`, config.help.length > 0 ? config.help : undefined);

  const state = useMapLevel(view, nav);
  const valuesState = useMapValues(config, view);
  const shown = state.shown;
  const shownLevel = shown ? view.levels[shown.nav.index]! : level;
  // Un nivel con una sola delimitación con detalle no se enseña (Q-10): se entra a él por la URL, se redirige al
  // siguiente (M-5) y mientras tanto se sigue «cargando», sin pintar el cuadrado de un solo elemento.
  const holding = skippableFeature(view, shown) !== null;
  const features = shown && !holding ? shown.features : emptyFeatures;
  const { values, steps, bins } = useMemo(
    () => levelValues(features, valuesState.totals, shownLevel.boundaryLevel),
    [features, valuesState.totals, shownLevel.boundaryLevel],
  );

  const number = useMemo(() => new Intl.NumberFormat(locale, { maximumFractionDigits: 2, numberingSystem: 'latn' }), [locale]);
  const range = useMemo(() => numberRangeFormatter(locale, { maximumFractionDigits: 2, numberingSystem: 'latn' }), [locale]);
  const formatValue = useCallback((value: number) => number.format(value), [number]);
  const nameOf = useCallback((feature: GeoFeature) => displayName(feature, locale), [locale]);

  // Migas: las que se conocen por haber hecho clic, o las que se deducen de la URL.
  const [known, setKnown] = useState<readonly Crumb[] | null>(null);
  const crumbs = useCrumbs(view, nav, known, rootName);
  const crumbName = useCallback(
    (crumb: Crumb) =>
      crumb.index === 0 || crumb.code === null
        ? crumb.name
        : displayName({ code: crumb.code, name: crumb.name, level: view.levels[crumb.index - 1]!.boundaryLevel }, locale),
    [view.levels, locale],
  );

  const navKey = `${nav.index}:${nav.code ?? ''}`;
  const [notice, setNotice] = useState<{ key: string; text: string } | null>(null);
  const [hover, setHover] = useState<string | null>(null);
  const [activeCode, setActiveCode] = useState<string | null>(null);
  const [canvasReady, setCanvasReady] = useState(false);
  const [engineError, setEngineError] = useState<unknown>(null);
  const sectionRef = useRef<HTMLElement>(null);
  const currentCrumb = useRef<HTMLSpanElement>(null);
  const legendRef = useRef<HTMLElement>(null);
  const noticeRef = useRef<HTMLParagraphElement>(null);
  const overlays = useMemo(() => [legendRef, noticeRef], []);
  /** Tras navegar, si el foco se perdió (la miga que lo tenía ya no es un botón), pasa al lugar actual. */
  const restoreFocus = useRef(false);
  /** Lugar al que se acaba de entrar con un clic: si al cargar sólo tiene un elemento, se sigue bajando (M-4). */
  const arrived = useRef<string | null>(null);
  const entering = useRef(false);
  const resolveEntry = useEntryResolver(view);

  const go = useCallback(
    (to: MapNav, trail: readonly Crumb[] | null) => {
      restoreFocus.current = true;
      setKnown(trail);
      setHover(null);
      setActiveCode(null);
      navigate({ ...route, view: 'map', params: navParams(view, to, route.params) });
    },
    [navigate, route, view],
  );

  useEffect(() => {
    if (!restoreFocus.current) return;
    restoreFocus.current = false;
    const active = document.activeElement;
    if (!active || active === document.body || !sectionRef.current?.contains(active)) currentCrumb.current?.focus({ preventScroll: true });
  }, [navKey]);

  const goCrumb = (position: number) => {
    const crumb = crumbs[position];
    if (!crumb || crumb.skipped || position === crumbs.length - 1) return;
    go({ index: crumb.index, code: crumb.code }, crumbs.slice(0, position + 1));
  };

  const up = upTarget(crumbs);
  const goUp = () => {
    if (up >= 0) goCrumb(up);
  };

  const enter = (code: string) => {
    if (!shown || state.loading || entering.current) return;
    const feature = shown.features.find((f) => f.code === code);
    if (!feature) return;
    const featureName = nameOf(feature);
    const child = childLevel(view, shown.nav.index);
    if (!feature.hasChildren || !child) {
      setNotice({ key: navKey, text: t('status.noDetail', { name: featureName }) });
      return;
    }
    const entered: Crumb = { index: shown.nav.index + 1, code, name: feature.name, bbox: feature.bbox };
    const base = crumbs;
    // Un nivel con una sola delimitación no se enseña: se baja hasta el primero con varias (M-4).
    entering.current = true;
    resolveEntry(entered).then(
      (result) => {
        entering.current = false;
        arrived.current = `${result.nav.index}:${result.nav.code ?? ''}`;
        go(result.nav, [...base, ...result.crumbs]);
      },
      () => {
        entering.current = false;
        arrived.current = `${entered.index}:${code}`;
        go({ index: entered.index, code }, [...base, entered]);
      },
    );
  };
  const enterRef = useRef(enter);
  useEffect(() => {
    enterRef.current = enter;
  });

  // Con teselas vectoriales no se puede mirar un nivel sin abrirlo: si al llegar a un lugar sólo tiene una
  // delimitación con detalle se baja a ella (sólo al llegar por un clic: volver atrás o abrir una URL lo conservan).
  const single = shown && shown.nav.index === nav.index && shown.nav.code === nav.code && !state.loading ? shown.features : null;
  useEffect(() => {
    if (arrived.current !== navKey || single === null) return;
    arrived.current = null;
    const only = single.length === 1 ? single[0]! : undefined;
    if (only?.hasChildren && skipsSingleLevels(view) && childLevel(view, nav.index)) enterRef.current(only.code);
  }, [navKey, single, view, nav.index]);

  // Entrar por la URL (el favorito, un enlace, «recargar») en un nivel saltable: se sigue bajando con `replace`. Sólo con
  // fuentes GeoJSON, que se pueden mirar sin abrir; con teselas vectoriales la vista ya baja al llegar por un clic.
  const redirect = redirectTarget(view, shown, nav);
  const redirectRoute: EntityRoute | null = redirect ? { ...route, view: 'map', params: navParams(view, redirect, route.params) } : null;
  const inRouter = useInRouterContext();
  const outside = useRef({ navigate, route: redirectRoute });
  useEffect(() => {
    outside.current = { navigate, route: redirectRoute };
  });
  const redirectKey = redirect ? `${redirect.index}:${redirect.code}` : '';
  useEffect(() => {
    // Sin enrutador (la vista incrustada en otro anfitrión) se navega con lo que da el anfitrión.
    if (inRouter || redirectKey === '' || !outside.current.route) return;
    setHover(null);
    setActiveCode(null);
    outside.current.navigate(outside.current.route);
  }, [inRouter, redirectKey]);

  const setLayout = (next: MapLayout) => {
    navigate({ ...route, view: 'map', params: { ...route.params, layout: next } });
  };

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== 'Escape' || event.defaultPrevented || nav.index === 0) return;
    const target = event.target;
    if (target instanceof HTMLElement && target.closest('input, textarea, select, [role="dialog"], [role="menu"], [role="listbox"]')) return;
    event.preventDefault();
    goUp();
  };

  const integerValues = useMemo(() => [...values.values()].every((v) => Number.isInteger(v)), [values]);
  const legendItems = useMemo(
    () =>
      binLabels(
        bins as Bin[],
        integerValues,
        (value) => number.format(value),
        (from, to, fromValue, toValue) => (range ? range(fromValue, toValue) : t('legend.range', { from, to })),
        (value) => t('legend.single', { value }),
      ),
    [bins, integerValues, number, range, t],
  );
  const sumField = view.valueField ? text(fieldDefFor(config, view.valueField).label) || view.valueField : '';
  const legendTitle = view.aggregate === 'sum' && view.valueField ? t('legend.sum', { field: sumField }) : t('legend.count', { name });

  const schematic = isSchematicSource(state.sourceId, state.sourceDef);
  const noticeElement = schematic ? (
    <p ref={noticeRef} data-kx-part="notice" role="note">
      {t('schematic.notice')}
      <span> · {t('schematic.source', { source: state.sourceId })}</span>
    </p>
  ) : null;
  const hoverFeature = hover ? features.find((f) => f.code === hover) : undefined;
  const child = childLevel(view, shown?.nav.index ?? nav.index);
  const loading = state.loading || valuesState.loading || holding;
  const failed = state.error !== null && state.error !== undefined ? state.error : engineError;
  const ready = !failed && !loading && (layout === 'list' || state.missing || canvasReady);
  const currentNotice = notice && notice.key === navKey ? notice.text : null;
  const status = failed
    ? ''
    : state.missing
      ? t('status.noGeometry', { name: crumbName(crumbs[crumbs.length - 1]!) })
      : currentNotice ??
        (state.loading || holding
          ? t('status.loading', { level: levelName })
          : t('status.level', { level: levelName, count: features.length }));
  // A dónde se encuadra, una sola vez por lugar: a lo que mide lo cargado. Con teselas vectoriales las entidades se
  // leen de las teselas que hay cargadas con la cámara donde está (con el mundo a la vista están todas); si ahí no
  // aparece nada (un tileset sin detalle a zoom bajo) se usa la caja de la delimitación que se abre, que contiene a
  // todo lo de debajo. No se vuelve a encuadrar cuando llegan más teselas: lo leído tras acercar sería parcial.
  const crumbBox = crumbs[crumbs.length - 1]?.bbox;
  const loadedBounds = useMemo(() => (state.loading ? null : boundsOfFeatures(features)), [state.loading, features]);
  const bounds = loadedBounds ?? (state.source?.kind === 'pmtiles' && !state.loading ? (crumbBox ?? null) : null);
  const canvasLabel = t('canvas.label', { name, level: levelName });
  const caption = t('table.caption', { level: `${levelName}: ${crumbName(crumbs[crumbs.length - 1]!)}` });

  // La cabecera de la entidad cuenta cuántos hay (M-7): todos los de la entidad y los que caen en el lugar que se mira.
  const statusOut = useRef(onStatus);
  useEffect(() => {
    statusOut.current = onStatus;
  });
  const reported = useRef('');
  const total = valuesState.rows;
  const here =
    nav.index === 0
      ? total - valuesState.unplaced
      : (valuesState.totals.get(keyOf(view.levels[nav.index - 1]!.boundaryLevel, nav.code ?? '')) ?? 0);
  useEffect(() => {
    const matched = Math.max(0, here);
    const key = `${total}|${matched}|${valuesState.loading}`;
    if (key === reported.current) return;
    reported.current = key;
    statusOut.current?.({ total, matched, loading: valuesState.loading });
  }, [total, here, valuesState.loading]);

  const asTable = layout === 'map';

  return (
    <section
      ref={sectionRef}
      data-kx-view="map"
      data-kx-layout={layout}
      data-kx-level={level.id}
      data-kx-ready={ready ? 'true' : undefined}
      aria-label={t('view.label', { name })}
      aria-busy={loading || undefined}
      onKeyDown={onKeyDown}
    >
      <Toolbar>
        <MapBreadcrumbs crumbs={crumbs} nameOf={crumbName} onGo={goCrumb} currentRef={currentCrumb} />
        <div data-kx-part="actions">
          <Action
            kind="secondary"
            data-kx-part="layout"
            data-kx-help="map.layout"
            icon={asTable ? 'table' : 'map'}
            onClick={() => setLayout(asTable ? 'list' : 'map')}
          >
            {t(asTable ? 'layout.toTable' : 'layout.toMap')}
          </Action>
          <Action
            kind="quiet"
            data-kx-part="up"
            icon="arrow-up"
            disabledReason={nav.index === 0 ? t('up.none') : undefined}
            onClick={goUp}
          >
            {t('up.label')} <Kbd>Esc</Kbd>
          </Action>
        </div>
      </Toolbar>

      <p id={statusId} role="status" data-kx-part="status">
        {status}
      </p>

      <div data-kx-part="body">
        {failed ? (
          <Feedback tone="error">{engineError ? t('error.engine') : state.sourceDef ? t('error.load') : t('error.source', { id: state.sourceId })}</Feedback>
        ) : layout === 'map' ? (
          <div data-kx-part="stage" aria-busy={loading || undefined}>
            <div data-kx-part="viewport">
              {state.source && !holding ? (
                <MapCanvas
                  source={state.source}
                  features={features}
                  steps={steps}
                  values={values}
                  formatValue={formatValue}
                  nameOf={nameOf}
                  bounds={bounds}
                  fitKey={navKey}
                  overlays={overlays}
                  activeCode={activeCode}
                  label={canvasLabel}
                  onActivate={enter}
                  onHover={setHover}
                  onFeatures={state.reportFeatures}
                  onReady={setCanvasReady}
                  onError={setEngineError}
                />
              ) : state.missing ? null : (
                <Skeleton lines={8} label={t('loading')} />
              )}
              <div data-kx-part="overlay">
                {hoverFeature ? (
                  <p data-kx-part="hover" aria-hidden="true">
                    <strong dir="auto">{nameOf(hoverFeature)}</strong>
                    {values.get(hoverFeature.code) ? <span>{formatValue(values.get(hoverFeature.code)!)}</span> : null}
                    {child && !hoverFeature.hasChildren ? <em>{t('canvas.noDetail')}</em> : null}
                  </p>
                ) : null}
                <MapLegend title={legendTitle} items={legendItems} figureRef={legendRef} />
              </div>
              {noticeElement}
            </div>
            <p data-kx-part="hint">{t('canvas.hint')}</p>
          </div>
        ) : holding ? (
          <Skeleton lines={8} label={t('loading')} />
        ) : (
          <>
            <MapTable
              features={features}
              values={values}
              nameOf={nameOf}
              formatValue={formatValue}
              caption={caption}
              locale={locale}
              canEnter={child !== undefined}
              onEnter={enter}
              onActive={setActiveCode}
              activeCode={activeCode}
            />
            <MapLegend title={legendTitle} items={legendItems} />
            {noticeElement}
          </>
        )}
        {valuesState.unplaced > 0 ? (
          <p data-kx-part="unplaced">{t('unplaced', { count: valuesState.unplaced })}</p>
        ) : null}
      </div>
      {redirectRoute && inRouter ? <MapRedirect to={redirectRoute} defaultView={config.defaultView} /> : null}
    </section>
  );
}
