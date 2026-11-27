import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  balancePadding,
  bboxOfPolygons,
  boundsPixels,
  boundsOfFeatures,
  featuresFromCollection,
  featuresFromRendered,
  fitPadding,
  labelPoint,
  labelPole,
  polygonsOf,
  unionBBox,
  type GeoCollection,
} from './geo';
import { displayName } from './names';

const geo = (file: string): GeoCollection =>
  JSON.parse(readFileSync(resolve(import.meta.dirname, '../../demo/public/geo', file), 'utf8')) as GeoCollection;

const square = (w: number, s: number, e: number, n: number) => ({
  type: 'Polygon',
  coordinates: [[[w, s], [e, s], [e, n], [w, n], [w, s]]],
});

describe('geometría de las delimitaciones', () => {
  it('lee las entidades de los GeoJSON de la demo con sus propiedades, cajas y punto de rótulo', () => {
    const world = featuresFromCollection(geo('world.geojson'));
    expect(world).toHaveLength(248);
    const spain = world.find((f) => f.code === 'ES')!;
    expect(spain).toMatchObject({ level: 'country', hasChildren: true });
    expect(spain.center[0]).toBeGreaterThan(spain.bbox[0]);
    expect(spain.center[0]).toBeLessThan(spain.bbox[2]);
    expect(world.find((f) => f.code === 'FR')!.hasChildren).toBe(false);
    const communities = featuresFromCollection(geo('country-ES.geojson'));
    expect(communities).toHaveLength(19);
    expect(communities.every((f) => f.parentCode === 'ES' && f.level === '2')).toBe(true);
    expect(boundsOfFeatures(communities)).toEqual([-9.75, 35.25, 3.65, 43.95]);
    expect(boundsOfFeatures([])).toBeNull();
  });

  it('omite lo que no tiene código o geometría', () => {
    const items = featuresFromCollection({
      type: 'FeatureCollection',
      features: [
        { properties: { code: 'A', name: 'Uno' }, geometry: square(0, 0, 2, 2) },
        { properties: { name: 'Sin código' }, geometry: square(0, 0, 1, 1) },
        { properties: { code: 'C' }, geometry: null },
        { properties: { code: 'D' }, geometry: { type: 'Point', coordinates: [0, 0] } },
      ],
    });
    expect(items.map((f) => f.code)).toEqual(['A']);
    expect(featuresFromCollection(null)).toEqual([]);
    expect(polygonsOf({ type: 'MultiPolygon', coordinates: [square(0, 0, 1, 1).coordinates] } as never)).toHaveLength(1);
  });

  it('el punto de rótulo es el polo de inaccesibilidad del polígono mayor (el centro de la caja si es degenerado)', () => {
    const big = square(0, 0, 10, 10).coordinates as [number, number][][];
    const small = square(20, 20, 21, 21).coordinates as [number, number][][];
    const bbox = bboxOfPolygons([big, small])!;
    const [lng, lat] = labelPoint([big, small], bbox);
    expect(lng).toBeCloseTo(5, 1);
    expect(lat).toBeCloseTo(5, 0);
    const line = [[[0, 0], [4, 0], [8, 0], [0, 0]]] as [number, number][][];
    expect(labelPoint([line], [0, 0, 8, 2])).toEqual([4, 1]);
    expect(unionBBox([0, 0, 1, 1], [-1, 0, 3, 2])).toEqual([-1, 0, 3, 2]);
    expect(unionBBox(null, null)).toBeNull();
  });

  it('en una «C» el rótulo cae dentro del polígono, no en el centroide que queda en la bahía', () => {
    // «C» ancha: brazos de 3 de alto y un lomo de 3 de ancho; el centroide (≈ 4,6; 5) está en la bahía vacía.
    const c = [
      [
        [0, 0],
        [10, 0],
        [10, 3],
        [3, 3],
        [3, 7],
        [10, 7],
        [10, 10],
        [0, 10],
        [0, 0],
      ],
    ] as [number, number][][];
    const { point, radius } = labelPole([c], [0, 0, 10, 10]);
    const [lng, lat] = point;
    const inBay = lng > 3 && lat > 3 && lat < 7;
    expect(inBay).toBe(false);
    expect(radius).toBeGreaterThan(1);
  });

  it('lee el nombre corto de la geometría (vacío si no lo trae)', () => {
    const spain = featuresFromCollection(geo('country-ES.geojson'));
    expect(spain.find((f) => f.code === 'ES22')).toMatchObject({ name: 'Comunidad Foral de Navarra', shortName: 'Navarra' });
    expect(spain.find((f) => f.code === 'ES61')!.shortName).toBe('');
    const world = featuresFromCollection(geo('world.geojson'));
    expect(world.every((f) => f.shortName === '')).toBe(true);
    // También llega por las teselas vectoriales.
    const [found] = featuresFromRendered([
      { properties: { code: 'ES22', name: 'Comunidad Foral de Navarra', shortName: 'Navarra', level: '2' }, geometry: square(-2, 42, -1, 43) },
    ]);
    expect(found!.shortName).toBe('Navarra');
  });

  it('las teselas vectoriales traen la misma entidad recortada varias veces: se agrupa y se une la caja', () => {
    const found = featuresFromRendered([
      { properties: { code: 'ES', name: 'Spain', level: 'country', hasChildren: 'true' }, geometry: square(-9, 36, -2, 44) },
      { properties: { code: 'ES', name: 'Spain', level: 'country', hasChildren: 'true' }, geometry: square(-2, 36, 3, 44) },
      { properties: { code: 'FR', name: 'France', level: 'country', hasChildren: 'false' }, geometry: square(0, 44, 5, 50) },
      { properties: { name: 'sin código' }, geometry: square(0, 0, 1, 1) },
    ]);
    expect(found.map((f) => f.code)).toEqual(['FR', 'ES']);
    const spain = found.find((f) => f.code === 'ES')!;
    expect(spain.bbox).toEqual([-9, 36, 3, 44]);
    expect(spain.hasChildren).toBe(true);
    expect(found.find((f) => f.code === 'FR')!.hasChildren).toBe(false);
  });

  it('los países se nombran en el idioma activo y el resto con el nombre de la geometría', () => {
    expect(displayName({ code: 'ES', name: 'Spain', level: 'country' }, 'es')).toBe('España');
    expect(displayName({ code: 'ES', name: 'Spain', level: 'country' }, 'en')).toBe('Spain');
    expect(displayName({ code: 'DE', name: 'Germany', level: 'country' }, 'es')).toBe('Alemania');
    expect(displayName({ code: 'ES30', name: 'Comunidad de Madrid', level: '2' }, 'en')).toBe('Comunidad de Madrid');
    expect(displayName({ code: 'XX', name: 'Inventado', level: 'country' }, 'es')).toBeTruthy();
  });
});

describe('relleno del encuadre', () => {
  const canvas = { width: 1000, height: 600 };
  // La leyenda: 200 × 150 en la esquina inferior del lado de inicio.
  const legendLeft = { left: 12, top: 438, right: 212, bottom: 588 };
  const legendRight = { left: 788, top: 438, right: 988, bottom: 588 };

  it('sin leyenda es el relleno base por todos lados', () => {
    expect(fitPadding(canvas, null, 32)).toEqual({ top: 32, bottom: 32, left: 32, right: 32 });
  });

  it('suma la leyenda por el lado en el que está: izquierda con la escritura de izquierda a derecha, derecha en RTL', () => {
    // Reservar 200 de ancho cuesta 200 × 600; reservar 150 de alto, 150 × 1000: sale la franja vertical.
    const ltr = fitPadding(canvas, legendLeft, 32);
    expect(ltr).toEqual({ top: 32, bottom: 32, left: 212 + 16, right: 32 });
    const rtl = fitPadding(canvas, legendRight, 32);
    expect(rtl).toEqual({ top: 32, bottom: 32, left: 32, right: 212 + 16 });
  });

  it('reserva la franja horizontal si es la que menos lienzo gasta (leyenda ancha y baja)', () => {
    const wide = { left: 12, top: 548, right: 612, bottom: 588 };
    expect(fitPadding(canvas, wide, 32)).toEqual({ top: 32, bottom: 52 + 16, left: 32, right: 32 });
    // Y si la leyenda va arriba, el relleno va arriba.
    const top = { left: 12, top: 12, right: 612, bottom: 52 };
    expect(fitPadding(canvas, top, 32).top).toBe(52 + 16);
  });

  it('con una sola delimitación deja aire proporcional al lienzo y nunca más del 40 % por lado', () => {
    const single = fitPadding(canvas, null, 32, 0.2);
    expect(single).toEqual({ top: 120, bottom: 120, left: 200, right: 200 });
    const huge = fitPadding({ width: 300, height: 200 }, { left: 0, top: 0, right: 290, bottom: 190 }, 32);
    expect(huge.left).toBeLessThanOrEqual(120);
    expect(huge.top).toBeLessThanOrEqual(80);
  });

  it('suma también el aviso de la fuente (varios estorbos), cada uno por su lado', () => {
    const notice = { left: 700, top: 560, right: 990, bottom: 592 };
    const both = fitPadding(canvas, [legendLeft, notice], 32);
    // La leyenda, a la izquierda; el aviso, fino y ancho, abajo: la franja de abajo mide su alto más el hueco.
    expect(both).toEqual({ top: 32, bottom: 600 - 560 + 16, left: 212 + 16, right: 32 });
    expect(fitPadding(canvas, [], 32)).toEqual(fitPadding(canvas, null, 32));
  });
});

describe('equilibrio del relleno', () => {
  const canvas = { width: 1000, height: 600 };

  it('mide un tramo de mapa en píxeles a un zoom de MapLibre (Mercator, tesela de 512)', () => {
    // El mundo entero a zoom 0 son 512 píxeles de ancho; 90° de longitud, 128.
    expect(boundsPixels([-180, -10, 180, 10], 0).width).toBeCloseTo(512, 8);
    expect(boundsPixels([0, 0, 90, 10], 0).width).toBeCloseTo(128, 8);
    // Cada zoom duplica; cerca del ecuador la altura en grados de Mercator es casi la de latitud.
    expect(boundsPixels([0, 0, 90, 10], 2).width).toBeCloseTo(512, 8);
    expect(boundsPixels([0, -10, 10, 10], 0).height).toBeCloseTo((20.1 / 360) * 512, 0);
    // Y crece con la latitud: 10° al norte de los 60° miden más que 10° en el ecuador.
    expect(boundsPixels([0, 60, 10, 70], 0).height).toBeGreaterThan(boundsPixels([0, 0, 10, 10], 0).height);
  });

  it('si el mapa cabe en el hueco, el lado opuesto recibe el mismo relleno y queda centrado', () => {
    const padding = { top: 32, bottom: 32, left: 228, right: 32 };
    // Un mapa de 700 de ancho cabe entre 228 y 228 (1000 − 456 = 544 < 700: no cabe) ... y uno de 500 sí.
    expect(balancePadding(canvas, padding, { width: 500, height: 400 })).toEqual({ top: 32, bottom: 32, left: 228, right: 228 });
    expect(balancePadding(canvas, padding, { width: 700, height: 400 })).toEqual(padding);
  });

  it('también en vertical (el aviso de abajo) y sin cambiar nada si ya está equilibrado', () => {
    const padding = { top: 32, bottom: 56, left: 32, right: 32 };
    expect(balancePadding(canvas, padding, { width: 500, height: 400 })).toEqual({ top: 56, bottom: 56, left: 32, right: 32 });
    expect(balancePadding(canvas, padding, { width: 500, height: 520 })).toEqual(padding);
    const even = { top: 40, bottom: 40, left: 40, right: 40 };
    expect(balancePadding(canvas, even, { width: 100, height: 100 })).toEqual(even);
  });
});
