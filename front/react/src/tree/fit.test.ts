import { describe, expect, it } from 'vitest';
import {
  FAR_ZOOM,
  FIT_MAX_ZOOM,
  FIT_MIN_ZOOM,
  MIN_ZOOM,
  farFontSize,
  fitModesDiffer,
  fitViewport,
  isFarZoom,
  zoomToFitAll,
  type FitInput,
} from './fit';

const view = { width: 1200, height: 700 };
const padding = 56;

/** Un organigrama de `width × height` con las raíces en el centro de su primera fila. */
const input = (width: number, height: number, roots = { x: width / 2 - 120, width: 240 }): FitInput => ({
  diagram: { width, height },
  roots,
  view,
  padding,
});

describe('encuadre del diagrama', () => {
  it('un diagrama que cabe se ajusta entero, centrado, sin agrandarse más de la escala natural', () => {
    const small = input(600, 300);
    expect(zoomToFitAll(small)).toBe(FIT_MAX_ZOOM);
    expect(fitModesDiffer(small)).toBe(false);
    const viewport = fitViewport(small, 'legible');
    expect(viewport.zoom).toBe(1);
    expect(viewport.x).toBe((1200 - 600) / 2);
    expect(viewport.y).toBe((700 - 300) / 2);
    // Si cabe, «todo» y «legible» son lo mismo.
    expect(fitViewport(small, 'all')).toEqual(viewport);
  });

  it('un diagrama grande no baja del zoom mínimo legible: se centra en la raíz y el resto se desplaza', () => {
    const big = input(6000, 2400);
    expect(zoomToFitAll(big)).toBeLessThan(FIT_MIN_ZOOM);
    expect(fitModesDiffer(big)).toBe(true);
    const legible = fitViewport(big, 'legible');
    expect(legible.zoom).toBe(FIT_MIN_ZOOM);
    // La raíz (centro en x = 3000) queda en el centro de la ventana y arriba, con su aire.
    expect(legible.x + 3000 * legible.zoom).toBeCloseTo(view.width / 2, 5);
    expect(legible.y).toBe(padding);
  });

  it('con varias raíces que no caben juntas se empieza por el principio del diagrama: a la izquierda, o a la derecha en RTL', () => {
    // Un bosque de seis raíces (1 500 de ancho, a 0,85 son 1 275 y la ventana deja 1 088): no caben juntas.
    const forest = input(6000, 2400, { x: 300, width: 1500 });
    const ltr = fitViewport(forest, 'legible');
    expect(ltr.zoom).toBe(FIT_MIN_ZOOM);
    // El borde izquierdo del diagrama (donde pueden estar los hijos de la primera raíz) queda a la vista.
    expect(ltr.x).toBe(padding);
    // En RTL el diagrama está en espejo: su principio es el borde derecho.
    const rtl = fitViewport({ ...forest, rtl: true }, 'legible');
    expect(rtl.x + 6000 * rtl.zoom).toBeCloseTo(view.width - padding, 5);
    // Si caben juntas, se centran.
    const few = fitViewport(input(6000, 2400, { x: 2800, width: 400 }), 'legible');
    expect(few.x + (2800 + 200) * few.zoom).toBeCloseTo(view.width / 2, 5);
  });

  it('«todo» enseña el diagrama entero aunque quede pequeño, hasta el zoom mínimo de la cámara', () => {
    const big = input(6000, 2400);
    const all = fitViewport(big, 'all');
    expect(all.zoom).toBeCloseTo(zoomToFitAll(big), 8);
    expect(all.zoom).toBeLessThan(FIT_MIN_ZOOM);
    expect(all.x).toBeCloseTo((view.width - 6000 * all.zoom) / 2, 5);
    // Un diagrama descomunal no baja del mínimo de la cámara.
    expect(fitViewport(input(1_000_000, 1_000_000), 'all').zoom).toBe(MIN_ZOOM);
  });

  it('el zoom legible multiplicado por el tamaño de letra del tema da, como poco, 11 píxeles', () => {
    // El tema de la demo escribe el texto de las tarjetas a 13 px o más: a 0,85 son 11 px.
    const THEME_FONT = 13;
    expect(FIT_MIN_ZOOM * THEME_FONT).toBeGreaterThanOrEqual(11);
  });

  it('lejos es por debajo de 0,6 y entonces el texto crece para verse a 11 píxeles, con tope', () => {
    expect(isFarZoom(FAR_ZOOM - 0.01)).toBe(true);
    expect(isFarZoom(FAR_ZOOM)).toBe(false);
    expect(isFarZoom(1)).toBe(false);
    // A 0,4 de zoom, 11 píxeles de pantalla son 27,5 del diagrama.
    expect(farFontSize(0.4, 100) * 0.4).toBeCloseTo(11, 8);
    // Con la tarjeta pequeña no crece más que el tope, aunque entonces se vea más pequeño.
    expect(farFontSize(0.1, 30)).toBe(30);
    expect(farFontSize(0.5, 30) * 0.5).toBeGreaterThanOrEqual(11);
  });
});
