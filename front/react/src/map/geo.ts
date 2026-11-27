// Geometría de las delimitaciones (PLAN §4.12): lo mínimo de GeoJSON que necesita el mapa, sin
// librerías. Cajas, punto de rótulo (polo de inaccesibilidad, Q-9) y lectura de las propiedades que
// escribe `scripts/gen-geo.mjs`.
import { polylabel, type PolygonRing } from './polylabel';

/** [oeste, sur, este, norte] */
export type BBox = [number, number, number, number];
export type LngLat = [number, number];

export interface GeoFeature {
  /** Identidad de la delimitación: ISO alfa-2 (países), `nutsCode`, `lauCode`/`ineCode`. */
  code: string;
  name: string;
  /** Nombre corto para el rótulo cuando el largo no cabe (M-3); vacío si la geometría no lo trae. */
  shortName: string;
  /** Nivel de Go de lo que se pinta: `country`, `1`, `2`, `3` o `local`. */
  level: string;
  /** Código de la delimitación que la contiene (vacío en el mundo). */
  parentCode: string;
  /** Tiene un nivel inferior con detalle (si no, no se abre). */
  hasChildren: boolean;
  bbox: BBox;
  /** Dónde va el rótulo: el polo de inaccesibilidad del polígono mayor. */
  center: LngLat;
  /** Radio del mayor círculo inscrito en torno a `center`, en grados de Mercator (0 si no se pudo calcular). */
  poleRadius: number;
  props: Record<string, unknown>;
}

export interface GeoGeometry {
  type: string;
  coordinates?: unknown;
}

export interface GeoFeatureLike {
  type?: string;
  properties?: Record<string, unknown> | null;
  geometry?: GeoGeometry | null;
}

export interface GeoCollection {
  type: 'FeatureCollection';
  features: GeoFeatureLike[];
}

type Ring = [number, number][];

/** Polígonos de una geometría: lista de polígonos, cada uno una lista de anillos. */
export function polygonsOf(geometry: GeoGeometry | null | undefined): Ring[][] {
  if (!geometry) return [];
  if (geometry.type === 'Polygon') return [geometry.coordinates as Ring[]];
  if (geometry.type === 'MultiPolygon') return geometry.coordinates as Ring[][];
  return [];
}

export function bboxOfPolygons(polygons: readonly Ring[][]): BBox | null {
  let west = Infinity;
  let south = Infinity;
  let east = -Infinity;
  let north = -Infinity;
  for (const polygon of polygons) {
    for (const ring of polygon) {
      for (const [lng, lat] of ring) {
        west = Math.min(west, lng);
        east = Math.max(east, lng);
        south = Math.min(south, lat);
        north = Math.max(north, lat);
      }
    }
  }
  return Number.isFinite(west) ? [west, south, east, north] : null;
}

export function unionBBox(a: BBox | null, b: BBox | null): BBox | null {
  if (!a) return b;
  if (!b) return a;
  return [Math.min(a[0], b[0]), Math.min(a[1], b[1]), Math.max(a[2], b[2]), Math.max(a[3], b[3])];
}

function ringArea(ring: Ring): number {
  let sum = 0;
  for (let i = 0; i < ring.length; i += 1) {
    const a = ring[i]!;
    const b = ring[(i + 1) % ring.length]!;
    sum += a[0] * b[1] - b[0] * a[1];
  }
  return sum / 2;
}

const RAD = Math.PI / 180;
const MAX_MERCATOR_LAT = 85.0511287798066;

/** Latitud → ordenada de Mercator, en grados (el plano en el que MapLibre dibuja y en el que se coloca el rótulo). */
function mercatorY(lat: number): number {
  const clamped = Math.max(-MAX_MERCATOR_LAT, Math.min(MAX_MERCATOR_LAT, lat));
  return Math.log(Math.tan(Math.PI / 4 + (clamped * RAD) / 2)) / RAD;
}

function latitudeOf(y: number): number {
  return (2 * Math.atan(Math.exp(y * RAD)) - Math.PI / 2) / RAD;
}

/** Punto de rótulo y radio inscrito de un polígono, calculados en el plano de Mercator. */
export function labelPole(polygons: readonly Ring[][], bbox: BBox): { point: LngLat; radius: number } {
  let best: Ring[] | null = null;
  let bestArea = 0;
  for (const polygon of polygons) {
    const exterior = polygon[0];
    if (!exterior) continue;
    const area = Math.abs(ringArea(exterior));
    if (area > bestArea) {
      best = polygon;
      bestArea = area;
    }
  }
  if (best && bestArea > 0) {
    const rings: PolygonRing[] = best.map((ring) => ring.map(([lng, lat]) => [lng, mercatorY(lat)] as const));
    let minX = Infinity;
    let minY = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    for (const [x, y] of rings[0]!) {
      minX = Math.min(minX, x);
      maxX = Math.max(maxX, x);
      minY = Math.min(minY, y);
      maxY = Math.max(maxY, y);
    }
    const pole = polylabel(rings, Math.min(maxX - minX, maxY - minY) / 400);
    if (pole.distance > 0) return { point: [pole.x, latitudeOf(pole.y)], radius: pole.distance };
  }
  return { point: [(bbox[0] + bbox[2]) / 2, (bbox[1] + bbox[3]) / 2], radius: 0 };
}

/** Punto de rótulo del polígono mayor: su polo de inaccesibilidad (el centro de la caja si es degenerado). */
export function labelPoint(polygons: readonly Ring[][], bbox: BBox): LngLat {
  return labelPole(polygons, bbox).point;
}

function text(value: unknown): string {
  return value === undefined || value === null ? '' : String(value);
}

function truthy(value: unknown): boolean {
  return value === true || value === 'true' || value === '1' || value === 1;
}

/** Entidades de una colección GeoJSON con las propiedades de `gen-geo.mjs`; las que no tienen código o geometría se omiten. */
export function featuresFromCollection(collection: GeoCollection | null | undefined): GeoFeature[] {
  const out: GeoFeature[] = [];
  for (const feature of collection?.features ?? []) {
    const props = feature.properties ?? {};
    const code = text(props.code);
    const polygons = polygonsOf(feature.geometry);
    const bbox = bboxOfPolygons(polygons);
    if (code === '' || !bbox) continue;
    const pole = labelPole(polygons, bbox);
    out.push({
      code,
      name: text(props.name) || code,
      shortName: text(props.shortName),
      level: text(props.level),
      parentCode: text(props.parentCode),
      hasChildren: truthy(props.hasChildren),
      bbox,
      center: pole.point,
      poleRadius: pole.radius,
      props,
    });
  }
  return out;
}

/** Caja que abarca varias entidades, o `null` si no hay ninguna. */
export function boundsOfFeatures(features: readonly GeoFeature[]): BBox | null {
  let bounds: BBox | null = null;
  for (const feature of features) bounds = unionBBox(bounds, feature.bbox);
  return bounds;
}

/**
 * Entidades de una colección renderizada por MapLibre (teselas vectoriales): una misma entidad llega
 * recortada en varias teselas, así que se agrupan por código y se unen las cajas.
 */
export function featuresFromRendered(items: readonly GeoFeatureLike[]): GeoFeature[] {
  const byCode = new Map<string, GeoFeature>();
  for (const item of items) {
    const props = item.properties ?? {};
    const code = text(props.code);
    const polygons = polygonsOf(item.geometry);
    const bbox = bboxOfPolygons(polygons);
    if (code === '' || !bbox) continue;
    const pole = labelPole(polygons, bbox);
    const previous = byCode.get(code);
    if (previous) {
      // La entidad llega recortada en varias teselas: la caja es la unión y el rótulo va en la pieza donde más cabe.
      const union = unionBBox(previous.bbox, bbox)!;
      const better = pole.radius > previous.poleRadius;
      byCode.set(code, {
        ...previous,
        bbox: union,
        center: better ? pole.point : previous.center,
        poleRadius: better ? pole.radius : previous.poleRadius,
      });
    } else {
      byCode.set(code, {
        code,
        name: text(props.name) || code,
        shortName: text(props.shortName),
        level: text(props.level),
        parentCode: text(props.parentCode),
        hasChildren: truthy(props.hasChildren),
        bbox,
        center: pole.point,
        poleRadius: pole.radius,
        props: { ...props },
      });
    }
  }
  return [...byCode.values()].sort((a, b) => a.name.localeCompare(b.name));
}

// ---------------------------------------------------------------------------------------------
// Relleno del encuadre
// ---------------------------------------------------------------------------------------------

/** Rectángulo en píxeles del lienzo (origen arriba a la izquierda). */
export interface PixelBox {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** Relleno del encuadre en píxeles, por lados físicos (lo que pide `fitBounds`). */
export interface FitPadding {
  top: number;
  bottom: number;
  left: number;
  right: number;
}

/**
 * Relleno del encuadre: `base` por todos lados y, para lo que se superpone al lienzo (la leyenda, el aviso de la
 * fuente), el sitio que ocupa sumado al lado de su esquina (M-3). Cada estorbo se mide donde está, así que el lado
 * sale solo: con la escritura de derecha a izquierda la leyenda está a la derecha y el relleno también. Por cada
 * uno se reserva la franja, horizontal o vertical, que menos lienzo desperdicia; `crowd` (de 0 a 0,5) añade un
 * relleno proporcional al lienzo por todos lados, para que una delimitación única no ocupe la pantalla entera.
 */
export function fitPadding(
  canvas: { width: number; height: number },
  obstacles: PixelBox | readonly PixelBox[] | null,
  base: number,
  crowd = 0,
): FitPadding {
  const around = (size: number) => Math.max(base, Math.min(0.5, Math.max(0, crowd)) * size);
  const padding: FitPadding = {
    top: around(canvas.height),
    bottom: around(canvas.height),
    left: around(canvas.width),
    right: around(canvas.width),
  };
  const list: readonly PixelBox[] = obstacles === null ? [] : Array.isArray(obstacles) ? obstacles : [obstacles as PixelBox];
  if (canvas.width <= 0 || canvas.height <= 0) return padding;
  const gap = base / 2;
  for (const obstacle of list) {
    const onLeft = (obstacle.left + obstacle.right) / 2 < canvas.width / 2;
    const onTop = (obstacle.top + obstacle.bottom) / 2 < canvas.height / 2;
    const reservedWidth = (onLeft ? obstacle.right : canvas.width - obstacle.left) + gap;
    const reservedHeight = (onTop ? obstacle.bottom : canvas.height - obstacle.top) + gap;
    // Lo que se pierde: una franja de todo el alto con el ancho del estorbo, o de todo el ancho con su alto.
    if (reservedWidth * canvas.height <= reservedHeight * canvas.width) {
      const side = onLeft ? 'left' : 'right';
      padding[side] = Math.max(padding[side], reservedWidth);
    } else {
      const side = onTop ? 'top' : 'bottom';
      padding[side] = Math.max(padding[side], reservedHeight);
    }
  }
  return limited(padding, canvas);
}

/** Lo que mide en píxeles un tramo de longitudes y latitudes a un zoom de MapLibre (Mercator, tesela de 512 px). */
export function boundsPixels(bounds: BBox, zoom: number): { width: number; height: number } {
  const world = 512 * 2 ** zoom;
  return {
    width: ((bounds[2] - bounds[0]) / 360) * world,
    height: ((mercatorY(bounds[3]) - mercatorY(bounds[1])) / 360) * world,
  };
}

/**
 * Equilibra el relleno: si el mapa es más pequeño que el hueco que deja (con el relleno del lado de la
 * leyenda), el lado opuesto recibe lo mismo y el mapa se queda centrado en el lienzo, no pegado al lado
 * contrario a la leyenda. Sólo mientras el mapa siga cabiendo: el zoom no cambia.
 */
export function balancePadding(
  canvas: { width: number; height: number },
  padding: FitPadding,
  map: { width: number; height: number },
): FitPadding {
  const horizontal = Math.max(padding.left, padding.right);
  const vertical = Math.max(padding.top, padding.bottom);
  const fitsAcross = canvas.width - 2 * horizontal >= map.width;
  const fitsDown = canvas.height - 2 * vertical >= map.height;
  return {
    top: fitsDown ? vertical : padding.top,
    bottom: fitsDown ? vertical : padding.bottom,
    left: fitsAcross ? horizontal : padding.left,
    right: fitsAcross ? horizontal : padding.right,
  };
}

/** Nunca más del 40 % del lienzo por lado: MapLibre no encuadra en un lienzo que el relleno deja sin sitio. */
function limited(padding: FitPadding, canvas: { width: number; height: number }): FitPadding {
  const across = (value: number, size: number) => Math.min(value, size * 0.4);
  return {
    top: across(padding.top, canvas.height),
    bottom: across(padding.bottom, canvas.height),
    left: across(padding.left, canvas.width),
    right: across(padding.right, canvas.width),
  };
}
