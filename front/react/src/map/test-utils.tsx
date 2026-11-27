// Arnés de pruebas del mapa: MapLibre simulado (jsdom no tiene WebGL), tema simulado con
// `resolveColor` determinista y `onChange` disparable, y los GeoJSON de la demo servidos por MSW
// desde el disco. Se monta contra el backend falso de `demo/mocks/node.ts`. Sólo para pruebas.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { http, HttpResponse } from 'msw';
import type { ReactElement } from 'react';
import { beforeEach, vi } from 'vitest';
import { ThemeContext, type EntityConfig, type ThemeApi, type ViewStatus } from '../contracts';
import { server } from '../../demo/mocks/node';
import { renderEntityView, type RenderedView } from '../tree/test-utils';
import { resetMapEngine } from './engine';
import { clearMeasureCache } from './labels';
import { MapView } from './MapView';

export { installDemoBackend } from '../tree/test-utils';

const GEO_DIR = resolve(import.meta.dirname, '../../demo/public/geo');

/** Sirve `/geo/*.geojson` de la demo desde el disco (404 si el fichero no existe). */
export function installGeoFiles(): void {
  beforeEach(() => {
    server.use(
      http.get('*/geo/:file', ({ params }) => {
        const file = String(params.file);
        if (!/^[\w-]+\.geojson$/.test(file)) return new HttpResponse(null, { status: 404 });
        try {
          return new HttpResponse(readFileSync(resolve(GEO_DIR, file), 'utf8'), {
            status: 200,
            headers: { 'content-type': 'application/geo+json' },
          });
        } catch {
          return new HttpResponse(null, { status: 404 });
        }
      }),
    );
  });
}

// ---------------------------------------------------------------------------------------------
// MapLibre simulado
// ---------------------------------------------------------------------------------------------

type Listener = (event?: unknown) => void;

interface FakeLayer {
  id: string;
  type: string;
  filter?: unknown;
  paint?: Record<string, unknown>;
  'source-layer'?: string;
}

export interface FakeStyle {
  version: number;
  sources: Record<string, { type: string; url?: string; data?: unknown }>;
  layers: FakeLayer[];
}

/** El `Map` de MapLibre, con lo que usa la vista y ganchos para que la prueba lo maneje. */
export class FakeMap {
  static instances: FakeMap[] = [];
  /** Píxeles por grado de la proyección simulada. */
  static scale = 100;
  /** Entidades que «contienen las teselas» de los mapas que se creen; `querySourceFeatures` las filtra. */
  static defaultSourceFeatures: { properties: Record<string, string>; geometry: unknown }[] = [];

  options: Record<string, unknown>;
  style: FakeStyle;
  container: HTMLElement;
  removed = false;
  readonly setStyleCalls: FakeStyle[] = [];
  readonly fitBoundsCalls: { bounds: [[number, number], [number, number]]; options: Record<string, unknown> }[] = [];
  readonly setFilterCalls: { layer: string; filter: unknown }[] = [];
  /** Código que devuelve `queryRenderedFeatures` (lo que hay bajo el ratón). */
  hit: string | null = null;
  /** Entidades de las teselas (se pueden cambiar durante la prueba). */
  sourceFeatures = FakeMap.defaultSourceFeatures;
  touchZoomRotate = { disableRotation: () => undefined };
  /** Imágenes registradas con `addImage` (la trama de «sin dato»). */
  readonly images = new Map<string, { width: number; height: number; data: Uint8ClampedArray; pixelRatio: number | undefined }>();
  private listeners = new Map<string, Set<Listener>>();
  private canvas = document.createElement('canvas');

  constructor(options: Record<string, unknown>) {
    this.options = options;
    this.style = options.style as FakeStyle;
    this.container = options.container as HTMLElement;
    this.container.dataset.fakeMap = 'true';
    this.canvas.width = 1200;
    this.canvas.height = 800;
    FakeMap.instances.push(this);
    setTimeout(() => this.emit('idle'), 0);
  }

  static get last(): FakeMap {
    const map = FakeMap.instances[FakeMap.instances.length - 1];
    if (!map) throw new Error('No se ha creado ningún mapa');
    return map;
  }

  on(type: string, listener: Listener): this {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type)!.add(listener);
    return this;
  }

  off(type: string, listener: Listener): this {
    this.listeners.get(type)?.delete(listener);
    return this;
  }

  once(type: string, listener: Listener): this {
    const wrapper: Listener = (event) => {
      this.off(type, wrapper);
      listener(event);
    };
    return this.on(type, wrapper);
  }

  emit(type: string, event?: unknown): void {
    for (const listener of [...(this.listeners.get(type) ?? [])]) listener(event);
  }

  listenerCount(type: string): number {
    return this.listeners.get(type)?.size ?? 0;
  }

  setStyle(style: FakeStyle): this {
    this.style = style;
    this.setStyleCalls.push(style);
    setTimeout(() => {
      this.emit('styledata');
      this.emit('idle');
    }, 0);
    return this;
  }

  loaded(): boolean {
    return true;
  }

  hasImage(id: string): boolean {
    return this.images.has(id);
  }

  addImage(id: string, image: { width: number; height: number; data: Uint8ClampedArray }, options?: { pixelRatio?: number }): this {
    this.images.set(id, { width: image.width, height: image.height, data: image.data, pixelRatio: options?.pixelRatio });
    return this;
  }

  removeImage(id: string): this {
    this.images.delete(id);
    return this;
  }

  getLayer(id: string): FakeLayer | undefined {
    return this.style.layers.find((layer) => layer.id === id);
  }

  getSource(id: string): unknown {
    return this.style.sources[id];
  }

  setFilter(layer: string, filter: unknown): this {
    this.setFilterCalls.push({ layer, filter });
    const found = this.getLayer(layer);
    if (found) found.filter = filter;
    return this;
  }

  /** Centro de la vista simulada: lo último a lo que se encuadró. */
  private center: [number, number] = [0, 20];

  fitBounds(bounds: [[number, number], [number, number]], options: Record<string, unknown>): this {
    this.fitBoundsCalls.push({ bounds, options });
    this.center = [(bounds[0][0] + bounds[1][0]) / 2, (bounds[0][1] + bounds[1][1]) / 2];
    return this;
  }

  /** Mueve la cámara simulada (la prueba lo hace antes de disparar `moveend`). */
  setCenter(lngLat: [number, number]): this {
    this.center = lngLat;
    return this;
  }

  /** Proyección lineal con la vista centrada en el último encuadre (1200 × 800 píxeles). */
  project([lng, lat]: [number, number]): { x: number; y: number } {
    return {
      x: 600 + (lng - this.center[0]) * FakeMap.scale,
      y: 400 - (lat - this.center[1]) * FakeMap.scale,
    };
  }

  getCanvas(): HTMLCanvasElement {
    return this.canvas;
  }

  queryRenderedFeatures(): unknown[] {
    return this.hit === null ? [] : [{ properties: { code: this.hit } }];
  }

  /** Como MapLibre: las entidades de las teselas que cumplen el filtro que se pide. */
  querySourceFeatures(_source: string, params?: { filter?: unknown }): unknown[] {
    return this.sourceFeatures.filter((item) => matchesFilter(params?.filter, item.properties));
  }

  remove(): void {
    this.removed = true;
  }

  /** Simula un clic sobre la delimitación `code` (o sobre el vacío con `null`). */
  click(code: string | null): void {
    this.hit = code;
    this.emit('click', { point: { x: 1, y: 1 } });
  }

  hover(code: string | null): void {
    this.hit = code;
    this.emit('mousemove', { point: { x: 1, y: 1 } });
  }

  /** Textos de los rótulos HTML que hay ahora mismo en el mapa y no están ocultos. */
  visibleLabels(): { code: string; name: string; value: string }[] {
    return Array.from(this.container.querySelectorAll<HTMLElement>('.kx-map-label'))
      .filter((element) => !element.hidden)
      .map((element) => ({
        code: element.dataset.kxCode ?? '',
        name: element.querySelector('[data-kx-part="name"]')?.textContent ?? '',
        value: element.querySelector('[data-kx-part="value"]')?.textContent ?? '',
      }));
  }
}

/** Evalúa los filtros que usa la vista: `['==', ['get', clave], valor]` y `['all', …]`. */
export function matchesFilter(filter: unknown, properties: Record<string, string>): boolean {
  if (!Array.isArray(filter)) return true;
  if (filter[0] === 'all') return filter.slice(1).every((part) => matchesFilter(part, properties));
  if (filter[0] === '==' && Array.isArray(filter[1]) && filter[1][0] === 'get') return properties[String(filter[1][1])] === filter[2];
  throw new Error(`Filtro no soportado por el simulador: ${JSON.stringify(filter)}`);
}

export class FakeMarker {
  element: HTMLElement;
  lngLat: [number, number] = [0, 0];
  offset: [number, number] = [0, 0];
  constructor(options: { element: HTMLElement }) {
    this.element = options.element;
  }

  setLngLat(lngLat: [number, number]): this {
    this.lngLat = lngLat;
    return this;
  }

  setOffset(offset: [number, number]): this {
    this.offset = offset;
    return this;
  }

  addTo(map: FakeMap): this {
    map.container.appendChild(this.element);
    return this;
  }

  remove(): this {
    this.element.remove();
    return this;
  }
}

export const addProtocol = vi.fn();

/** Forma del módulo `maplibre-gl` que usa la vista. Se instala con `vi.mock('maplibre-gl', …)`. */
export const setWorkerUrl = vi.fn();
export const fakeMaplibre = { Map: FakeMap, Marker: FakeMarker, addProtocol, removeProtocol: vi.fn(), setWorkerUrl };

/**
 * Reinicia el motor y los mapas creados entre pruebas. También deja a jsdom sin lienzo 2D (no lo tiene): los
 * rótulos se miden con la estimación por caracteres y la trama de «sin dato» se rellena a mano.
 */
export function resetFakeMap(): void {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null as never);
  clearMeasureCache();
  FakeMap.instances.length = 0;
  FakeMap.scale = 100;
  FakeMap.defaultSourceFeatures = [];
  addProtocol.mockClear();
  setWorkerUrl.mockClear();
  resetMapEngine();
}

// ---------------------------------------------------------------------------------------------
// Tema simulado
// ---------------------------------------------------------------------------------------------

export interface FakeTheme extends ThemeApi {
  /** Cambia la paleta y avisa a los suscriptores (como un cambio de tema o de modo). */
  fire(): void;
  /** Cuántos suscriptores hay ahora. */
  subscribers(): number;
  /** Tokens pedidos a `resolveColor`, con el elemento en el que se pidió. */
  requested: { token: string; el: Element | undefined }[];
  palette: number;
}

/** `rgb(...)` derivado del token y de la paleta: distinto en cada token y en cada cambio de paleta. */
export function colorOf(token: string, palette: number): string {
  let hash = palette * 97;
  for (const ch of token) hash = (hash * 31 + ch.charCodeAt(0)) % 16_581_375;
  return `rgb(${hash % 256}, ${Math.floor(hash / 256) % 256}, ${Math.floor(hash / 65_536) % 256})`;
}

export function createFakeTheme(): FakeTheme {
  const subscribers = new Set<() => void>();
  const theme: FakeTheme = {
    info: { name: 'fake', modes: ['light', 'dark'], highContrast: false, iconStyle: 'outline', dictionaryHash: 'sha256:fake' },
    mode: 'light',
    setMode: () => undefined,
    setTheme: () => Promise.resolve(),
    available: ['fake'],
    palette: 1,
    requested: [],
    resolveColor(token, el) {
      theme.requested.push({ token, el });
      return colorOf(token, theme.palette);
    },
    onChange(callback) {
      subscribers.add(callback);
      return () => subscribers.delete(callback);
    },
    fire() {
      theme.palette += 1;
      for (const callback of [...subscribers]) callback();
    },
    subscribers: () => subscribers.size,
  };
  return theme;
}

// ---------------------------------------------------------------------------------------------
// Montaje
// ---------------------------------------------------------------------------------------------

export interface RenderMapOptions {
  entity?: string;
  search?: string;
  locale?: 'es' | 'en';
  dir?: 'ltr' | 'rtl';
  theme?: FakeTheme;
  configure?: (config: EntityConfig) => EntityConfig;
  onStatus?: (status: ViewStatus) => void;
}

export interface RenderedMap extends RenderedView {
  theme: FakeTheme;
  /** El mapa de MapLibre simulado (espera a que la vista lo cree). */
  map(): Promise<FakeMap>;
}

export async function renderMapView(options: RenderMapOptions = {}): Promise<RenderedMap> {
  const theme = options.theme ?? createFakeTheme();
  const view = await renderEntityView(MapView, {
    entity: options.entity ?? 'facility',
    search: options.search ?? '?view=map',
    locale: options.locale,
    dir: options.dir,
    configure: options.configure,
    ...(options.onStatus ? { onStatus: options.onStatus } : {}),
    wrap: (children: ReactElement) => <ThemeContext.Provider value={theme}>{children}</ThemeContext.Provider>,
  });
  return {
    ...view,
    theme,
    map: async () => {
      const { waitFor } = await import('@testing-library/react');
      // El motor se carga con `import()` (MapLibre, PMTiles y su hoja): con la máquina cargada tarda más de un segundo.
      await waitFor(
        () => {
          if (FakeMap.instances.length === 0) throw new Error('El mapa aún no se ha creado');
        },
        { timeout: 5000 },
      );
      return FakeMap.last;
    },
  };
}
