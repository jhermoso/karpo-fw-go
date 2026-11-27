import type * as MapLibre from 'maplibre-gl';
import type { Map as MapLibreMap, Marker } from 'maplibre-gl';
import { useCallback, useEffect, useRef, useState, type RefObject } from 'react';
import { resolveThemeLength, useTheme } from '../theme';
import { cameraDuration, motionAllowed } from '../tree/motion';
import { loadMapEngine } from './engine';
import { balancePadding, boundsPixels, featuresFromRendered, fitPadding, type BBox, type GeoFeature, type PixelBox } from './geo';
import {
  clampToBox,
  commonKind,
  fitLabel,
  pickLabels,
  readMeter,
  textOfKind,
  FALLBACK_METER,
  type LabelCandidate,
  type LabelFit,
  type LabelKind,
} from './labels';
import { drawHatch, HATCH_SIZE, HATCH_STROKE, type HatchImage } from './pattern';
import {
  LAYER_ACTIVE,
  LAYER_FILL,
  LAYER_HOVER,
  SOURCE_ID,
  buildMapStyle,
  codeFilter,
  resolveMapColors,
  vectorFilter,
  type MapDataSource,
} from './style';

export interface MapCanvasProps {
  source: MapDataSource;
  /** Entidades del nivel: rótulos y límites. Con teselas vectoriales las informa el propio lienzo (`onFeatures`). */
  features: readonly GeoFeature[];
  /** Paso de la paleta (1..7) de cada código con dato. */
  steps: ReadonlyMap<string, number>;
  /** Valor de cada código con dato (para el rótulo). */
  values: ReadonlyMap<string, number>;
  formatValue(value: number): string;
  nameOf(feature: GeoFeature): string;
  /** Caja a la que se encuadra al cambiar `fitKey`. */
  bounds: BBox | null;
  fitKey: string;
  /** Lo que se superpone al lienzo (la leyenda, el aviso de la fuente): el encuadre le deja su sitio (M-3). */
  overlays?: readonly RefObject<HTMLElement | null>[];
  /** Delimitación marcada desde fuera (la fila enfocada de la tabla). */
  activeCode: string | null;
  label: string;
  /** Un clic en una delimitación. */
  onActivate(code: string): void;
  onHover(code: string | null): void;
  /** Teselas vectoriales: las entidades que se ven tras cargar las teselas. */
  onFeatures?(features: GeoFeature[]): void;
  /** `true` cuando el mapa está pintado tras el último cambio. */
  onReady(ready: boolean): void;
  /** Falló la carga del motor del mapa. */
  onError?(error: unknown): void;
}

const FIT_PADDING_FALLBACK = 32;
const FIT_MAX_ZOOM = 10;
const FIT_DURATION = 600;
const SETTLE_MS = 400;
const LABEL_CLASS = 'kx-map-label';
/** Con una sola delimitación se deja este margen (fracción del lienzo) para que no llene la pantalla (M-4). */
const SINGLE_CROWD = 0.2;

function createLabel(feature: GeoFeature): HTMLElement {
  const element = document.createElement('div');
  element.className = LABEL_CLASS;
  element.dataset.kxPart = 'label';
  element.setAttribute('aria-hidden', 'true');
  element.dataset.kxCode = feature.code;
  // Los nombres vienen de los datos: su dirección la decide su propio texto (M-12).
  const name = document.createElement('span');
  name.dataset.kxPart = 'name';
  name.dir = 'auto';
  const value = document.createElement('span');
  value.dataset.kxPart = 'value';
  element.append(name, value);
  return element;
}

/** Rectángulo de `element` relativo al origen de `origin`, o `null` si no ocupa sitio (sin estilos, jsdom). */
function relativeBox(element: Element, origin: DOMRect): PixelBox | null {
  const rect = element.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return null;
  return { left: rect.left - origin.left, top: rect.top - origin.top, right: rect.right - origin.left, bottom: rect.bottom - origin.top };
}

export function MapCanvas(props: MapCanvasProps) {
  const theme = useTheme();
  const wrapRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [map, setMap] = useState<MapLibreMap | null>(null);
  const latest = useRef(props);
  const markers = useRef(new Map<string, { marker: Marker; element: HTMLElement }>());
  const maplibreRef = useRef<typeof MapLibre | null>(null);
  /** Entidades ya informadas (`null` = nada todavía: lo primero que se lea se informa aunque esté vacío). */
  const reportedFeatures = useRef<string | null>(null);
  /** Mapa recién creado: su primer estilo ya está aplicado y no hay que reaplicarlo. */
  const fresh = useRef<MapLibreMap | null>(null);
  /** Trama de «sin dato» vigente (cambia con los colores del tema) y las que quedaron atrás. */
  const hatch = useRef<{ key: string; version: number; image: HatchImage | null }>({ key: '', version: 0, image: null });

  const styleBuilder = useRef<() => ReturnType<typeof buildMapStyle>>(() => {
    throw new Error('El estilo del mapa se pide antes de montar el lienzo');
  });
  useEffect(() => {
    latest.current = props;
  });

  /** Estilo con los colores de AHORA (el tema puede haber cambiado de modo o de tema). */
  const currentStyle = useCallback(() => {
    const { source, steps } = latest.current;
    const wrap = wrapRef.current ?? undefined;
    const colors = resolveMapColors((token) => theme.resolveColor(token, wrap));
    // La trama se dibuja con los colores de ahora; sólo se rehace si cambiaron (otro tema u otro modo). Tesela de
    // `--th-space-3` (12 px) y raya de `--th-edge-width` (1 px): rala y fina, para que «sin dato» no domine (M-2).
    const size = resolveThemeLength('--th-space-3', wrap);
    const stroke = resolveThemeLength('--th-edge-width', wrap);
    const pixelRatio = typeof window === 'undefined' ? 1 : window.devicePixelRatio || 1;
    const key = `${colors.missing}|${colors.missingHatch}|${size}|${stroke}|${pixelRatio}`;
    if (hatch.current.key !== key) {
      const version = hatch.current.version + 1;
      const image = drawHatch(
        { base: colors.missing, line: colors.missingHatch },
        version,
        {
          size: Number.isFinite(size) && size > 0 ? size : HATCH_SIZE,
          stroke: Number.isFinite(stroke) && stroke > 0 ? stroke : HATCH_STROKE,
          pixelRatio,
        },
      );
      hatch.current = { key, version, image };
    }
    const hatchImage = hatch.current.image?.name;
    return buildMapStyle({ source, steps, colors, ...(hatchImage ? { hatchImage } : {}) });
  }, [theme]);
  useEffect(() => {
    styleBuilder.current = currentStyle;
  }, [currentStyle]);

  /** Registra la trama vigente en el mapa y retira las de colores anteriores. */
  const registerHatch = useCallback((target: MapLibreMap) => {
    const image = hatch.current.image;
    if (!image) return;
    try {
      if (!target.hasImage(image.name)) {
        target.addImage(image.name, { width: image.width, height: image.height, data: image.data }, { pixelRatio: image.pixelRatio });
      }
      for (let version = 1; version < hatch.current.version; version += 1) {
        const stale = `${image.name.replace(/-\d+$/, '')}-${version}`;
        if (target.hasImage(stale)) target.removeImage(stale);
      }
    } catch {
      // Aún no hay estilo cargado: `styleimagemissing` la pedirá cuando haga falta.
    }
  }, []);

  // Creación y destrucción del mapa.
  useEffect(() => {
    let cancelled = false;
    let created: MapLibreMap | null = null;
    const store = markers.current;
    void loadMapEngine().then(
      ({ maplibre }) => {
        const container = containerRef.current;
        if (cancelled || !container) return;
        maplibreRef.current = maplibre;
        created = new maplibre.Map({
          container,
          style: styleBuilder.current(),
          center: [0, 20],
          zoom: 1,
          attributionControl: false,
          dragRotate: false,
          pitchWithRotate: false,
          maxPitch: 0,
          renderWorldCopies: false,
          fadeDuration: 0,
        });
        created.touchZoomRotate.disableRotation();
        // `idle` (todo pintado) se escucha desde el nacimiento del mapa: puede llegar antes de que React lo vea.
        created.on('idle', () => latest.current.onReady(true));
        // Si el estilo pide la trama antes de registrarla (o `setStyle` la tiró), se da en el momento.
        created.on('styleimagemissing', (event: { id: string }) => {
          const image = hatch.current.image;
          if (image && event.id === image.name && created && !created.hasImage(image.name)) registerHatch(created);
        });
        registerHatch(created);
        fresh.current = created;
        setMap(created);
      },
      (error: unknown) => latest.current.onError?.(error),
    );
    return () => {
      cancelled = true;
      for (const { marker } of store.values()) marker.remove();
      store.clear();
      created?.remove();
      setMap(null);
    };
  }, [registerHatch]);

  // Estilo: cambia con la fuente, los pasos de color o el tema (modo claro/oscuro, otro tema).
  const { source, steps } = props;
  useEffect(() => {
    if (!map) return;
    let settle: ReturnType<typeof setTimeout> | undefined;
    const apply = () => {
      latest.current.onReady(false);
      map.setStyle(currentStyle());
      registerHatch(map);
      // Si el cambio no provoca ningún dibujo nuevo MapLibre no emite `idle`: se da por pintado si ya está cargado.
      clearTimeout(settle);
      settle = setTimeout(() => {
        if (map.loaded()) latest.current.onReady(true);
      }, SETTLE_MS);
    };
    // El estilo con el que nació el mapa ya está aplicado: sólo se reaplica si algo cambió después.
    if (fresh.current === map) fresh.current = null;
    else apply();
    const stop = theme.onChange(apply);
    return () => {
      clearTimeout(settle);
      stop();
    };
  }, [map, source, steps, theme, currentStyle, registerHatch]);

  // El usuario toma la cámara: mientras no lo haga, un cambio de tamaño de la leyenda reencuadra.
  const interacted = useRef(false);

  // Eventos del mapa.
  useEffect(() => {
    if (!map) return;
    let hovered: string | null = null;
    const codeAt = (event: { point: unknown }): string | null => {
      const hit = map.queryRenderedFeatures(event.point as never, { layers: [LAYER_FILL] });
      const code = hit[0]?.properties?.code;
      return typeof code === 'string' ? code : null;
    };
    const onClick = (event: { point: unknown }) => {
      const code = codeAt(event);
      if (code !== null) latest.current.onActivate(code);
    };
    const onMove = (event: { point: unknown }) => {
      const code = codeAt(event);
      if (code === hovered) return;
      hovered = code;
      if (map.getLayer(LAYER_HOVER)) map.setFilter(LAYER_HOVER, codeFilter(code) as never);
      map.getCanvas().style.cursor = code === null ? '' : 'pointer';
      latest.current.onHover(code);
    };
    const onLeave = () => {
      if (hovered === null) return;
      hovered = null;
      if (map.getLayer(LAYER_HOVER)) map.setFilter(LAYER_HOVER, codeFilter(null) as never);
      map.getCanvas().style.cursor = '';
      latest.current.onHover(null);
    };
    // Un movimiento con `originalEvent` es del usuario (arrastrar, rueda, gestos); el de `fitBounds` no.
    const onMoveStart = (event: { originalEvent?: unknown }) => {
      if (event.originalEvent) interacted.current = true;
    };
    map.on('click', onClick as never);
    map.on('mousemove', onMove as never);
    map.on('mouseout', onLeave);
    map.on('movestart', onMoveStart as never);
    return () => {
      map.off('click', onClick as never);
      map.off('mousemove', onMove as never);
      map.off('mouseout', onLeave);
      map.off('movestart', onMoveStart as never);
    };
  }, [map]);

  // Delimitación marcada desde fuera.
  const { activeCode } = props;
  useEffect(() => {
    if (!map) return;
    const apply = () => {
      if (map.getLayer(LAYER_ACTIVE)) map.setFilter(LAYER_ACTIVE, codeFilter(activeCode) as never);
    };
    apply();
    map.on('styledata', apply);
    return () => {
      map.off('styledata', apply);
    };
  }, [map, activeCode, source, steps]);

  // Encuadre al cambiar de nivel (el vuelo sólo con «sin preferencia» de movimiento). El relleno suma el
  // tamaño de la leyenda por el lado en que está (M-3) y, con una sola delimitación, deja aire alrededor (M-4).
  const { bounds, fitKey, features } = props;
  const featureCount = features.length;
  const fitNow = useCallback(
    (animate: boolean) => {
      const wrap = wrapRef.current;
      const target = latest.current.bounds;
      if (!map || !wrap || !target) return;
      const origin = wrap.getBoundingClientRect();
      const canvas = { width: origin.width, height: origin.height };
      const boxes = (latest.current.overlays ?? []).flatMap((ref) => {
        const box = ref.current ? relativeBox(ref.current, origin) : null;
        return box ? [box] : [];
      });
      const base = resolveThemeLength('--th-space-8', wrap);
      let padding = fitPadding(
        canvas,
        boxes,
        Number.isFinite(base) && base > 0 ? base : FIT_PADDING_FALLBACK,
        featureCount === 1 ? SINGLE_CROWD : 0,
      );
      const camera = [
        [target[0], target[1]],
        [target[2], target[3]],
      ] as [[number, number], [number, number]];
      // Si el mapa queda más pequeño que el hueco que deja el relleno, se centra en el lienzo en vez de arrimarse
      // al lado contrario a la leyenda.
      if (boxes.length > 0 && typeof map.cameraForBounds === 'function') {
        const zoom = map.cameraForBounds(camera, { padding, maxZoom: FIT_MAX_ZOOM })?.zoom;
        if (zoom !== undefined && Number.isFinite(zoom)) padding = balancePadding(canvas, padding, boundsPixels(target, zoom));
      }
      const moves = animate && motionAllowed();
      try {
        map.fitBounds(camera, { padding, maxZoom: FIT_MAX_ZOOM, animate: moves, duration: cameraDuration(FIT_DURATION) });
      } catch {
        // El relleno no deja sitio al mapa (lienzo diminuto): se encuadra sin él.
        map.fitBounds(camera, { padding: FIT_PADDING_FALLBACK, maxZoom: FIT_MAX_ZOOM, animate: moves, duration: cameraDuration(FIT_DURATION) });
      }
    },
    [map, featureCount],
  );
  const fitRef = useRef(fitNow);
  useEffect(() => {
    fitRef.current = fitNow;
  }, [fitNow]);

  useEffect(() => {
    if (!map || !bounds) return;
    interacted.current = false;
    fitRef.current(true);
    // Sólo cuando cambia el lugar que se mira (o llegan por primera vez sus límites).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [map, fitKey, bounds === null]);

  // La leyenda (o el aviso) cambia de tamaño (llegan los valores, otro idioma): si nadie ha movido la cámara, se reencuadra.
  const { overlays } = props;
  useEffect(() => {
    const elements = (overlays ?? []).flatMap((ref) => (ref.current ? [ref.current] : []));
    if (!map || elements.length === 0 || typeof ResizeObserver !== 'function') return undefined;
    const sizes = new Map<Element, string>();
    const observer = new ResizeObserver(() => {
      let changed = false;
      for (const element of elements) {
        const rect = element.getBoundingClientRect();
        const next = `${Math.round(rect.width)}x${Math.round(rect.height)}`;
        const previous = sizes.get(element);
        sizes.set(element, next);
        if (previous !== undefined && previous !== next) changed = true;
      }
      if (changed && !interacted.current && latest.current.bounds) fitRef.current(false);
    });
    for (const element of elements) observer.observe(element);
    return () => observer.disconnect();
  }, [map, overlays]);

  // Teselas vectoriales: las entidades visibles se leen del propio mapa cuando termina de pintar.
  useEffect(() => {
    if (!map || source.kind !== 'pmtiles') return;
    const collect = () => {
      if (!map.getSource(SOURCE_ID)) return;
      const items = map.querySourceFeatures(SOURCE_ID, {
        sourceLayer: source.layer,
        filter: vectorFilter(source) as never,
      });
      const found = featuresFromRendered(items as never);
      const key = found.map((f) => f.code).join(',');
      if (key === reportedFeatures.current) return;
      reportedFeatures.current = key;
      latest.current.onFeatures?.(found);
    };
    reportedFeatures.current = null;
    map.on('idle', collect);
    return () => {
      map.off('idle', collect);
    };
  }, [map, source]);

  // Rótulos como marcadores HTML, medidos con el tipo de letra real del tema.
  const { values, formatValue, nameOf } = props;
  const stepsRef = useRef(steps);
  useEffect(() => {
    stepsRef.current = steps;
  }, [steps]);
  useEffect(() => {
    const maplibre = maplibreRef.current;
    if (!map || !maplibre) return;
    const store = markers.current;
    const wanted = new Set(features.map((f) => f.code));
    for (const [code, entry] of store) {
      if (!wanted.has(code)) {
        entry.marker.remove();
        store.delete(code);
      }
    }
    for (const feature of features) {
      let entry = store.get(feature.code);
      if (!entry) {
        const element = createLabel(feature);
        const marker = new maplibre.Marker({ element, anchor: 'center' }).setLngLat(feature.center).addTo(map);
        entry = { marker, element };
        store.set(feature.code, entry);
      } else {
        entry.marker.setLngLat(feature.center);
      }
      // Sobre los pasos oscuros de la paleta el texto cambia al color «inverso» del tema (lo lee el CSS).
      const step = stepsRef.current.get(feature.code);
      if (step === undefined) delete entry.element.dataset.kxStep;
      else entry.element.dataset.kxStep = String(step);
    }

    const place = () => {
      const canvas = map.getCanvas();
      const viewport = { width: canvas.clientWidth || canvas.width, height: canvas.clientHeight || canvas.height };
      const first = store.values().next().value;
      const meter = first ? readMeter(first.element) : FALLBACK_METER;
      type Placed = { fit: LabelFit; name: string; text: string; value: string; dx: number; dy: number };
      // Una pasada de rotulado: qué cabe en cada delimitación (`only` fuerza una variante) y dónde se pondría.
      const pass = (only?: LabelKind) => {
        const candidates: LabelCandidate[] = [];
        const fits = new Map<string, Placed>();
        for (const feature of features) {
          const value = values.get(feature.code);
          const valueText = value !== undefined && value > 0 ? formatValue(value) : '';
          const name = nameOf(feature);
          const texts = { name, shortName: feature.shortName, code: feature.code, value: valueText };
          const sw = map.project([feature.bbox[0], feature.bbox[1]]);
          const ne = map.project([feature.bbox[2], feature.bbox[3]]);
          const box = {
            left: Math.min(sw.x, ne.x),
            right: Math.max(sw.x, ne.x),
            top: Math.min(sw.y, ne.y),
            bottom: Math.max(sw.y, ne.y),
          };
          const fit = fitLabel(texts, meter, { width: box.right - box.left, height: box.bottom - box.top }, only);
          if (!fit) continue;
          // El rótulo va en el polo de inaccesibilidad, sin salirse de la caja de su delimitación.
          const pole = map.project(feature.center);
          const at = clampToBox(pole, fit, box);
          candidates.push({
            code: feature.code,
            x: at.x,
            y: at.y,
            featureWidth: box.right - box.left,
            featureHeight: box.bottom - box.top,
            width: fit.width,
            height: fit.height,
          });
          fits.set(feature.code, { fit, name, text: textOfKind(texts, fit.kind), value: valueText, dx: at.x - pole.x, dy: at.y - pole.y });
        }
        return { fits, visible: new Set(pickLabels(candidates, viewport)) };
      };
      // Un nivel, un criterio (M-6): la primera pasada mira qué variante cabe en cada delimitación; la segunda
      // rotula todas con la más corta de las elegidas («nombre sólo si cabe entero en todas; si no, código en todas»).
      const trial = pass();
      const kind = commonKind([...trial.visible].map((code) => trial.fits.get(code)!.fit));
      const { fits, visible } = pass(kind);
      for (const [code, entry] of store) {
        const placed = fits.get(code);
        if (!placed || !visible.has(code)) {
          entry.element.hidden = true;
          delete entry.element.dataset.kxKind;
          continue;
        }
        const { fit, name, text, value, dx, dy } = placed;
        entry.element.querySelector('[data-kx-part="name"]')!.textContent = fit.lines.join('\n');
        entry.element.querySelector('[data-kx-part="value"]')!.textContent = fit.showValue ? value : '';
        // Lo abreviado lleva el nombre completo en `title` (y la tabla del nivel lo nombra entero).
        if (text === name) entry.element.removeAttribute('title');
        else entry.element.title = name;
        entry.element.dataset.kxKind = fit.kind;
        entry.marker.setOffset([dx, dy]);
        entry.element.hidden = false;
      }
    };
    place();
    map.on('moveend', place);
    map.on('resize', place);
    // Otro tema puede traer otra tipografía: se vuelve a medir cuando ya está aplicada. Y las fuentes web
    // acaban de cargarse después de la primera medida.
    let frame = 0;
    const stopTheme = theme.onChange(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(place);
    });
    const fonts = typeof document !== 'undefined' ? document.fonts : undefined;
    fonts?.addEventListener?.('loadingdone', place);
    return () => {
      cancelAnimationFrame(frame);
      stopTheme();
      fonts?.removeEventListener?.('loadingdone', place);
      map.off('moveend', place);
      map.off('resize', place);
    };
  }, [map, features, values, formatValue, nameOf, theme]);

  return (
    <div ref={wrapRef} data-kx-part="canvas" data-kx-help="map.canvas" role="region" aria-label={props.label}>
      <div ref={containerRef} data-kx-part="map" />
    </div>
  );
}
