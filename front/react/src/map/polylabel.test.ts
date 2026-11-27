import { describe, expect, it } from 'vitest';
import { distanceToPolygon, polylabel, type PolygonRing } from './polylabel';

const square: PolygonRing = [
  [0, 0],
  [10, 0],
  [10, 10],
  [0, 10],
  [0, 0],
];

/** «C» ancha: el centroide cae en la bahía vacía, fuera del polígono. */
const cShape: PolygonRing = [
  [0, 0],
  [10, 0],
  [10, 3],
  [3, 3],
  [3, 7],
  [10, 7],
  [10, 10],
  [0, 10],
  [0, 0],
];

describe('distanceToPolygon', () => {
  it('es positiva dentro, negativa fuera y cero en el contorno', () => {
    expect(distanceToPolygon(5, 5, [square])).toBeCloseTo(5);
    expect(distanceToPolygon(2, 5, [square])).toBeCloseTo(2);
    expect(distanceToPolygon(-3, 5, [square])).toBeCloseTo(-3);
    expect(distanceToPolygon(0, 5, [square])).toBe(0);
  });

  it('un hueco cuenta como fuera', () => {
    const hole: PolygonRing = [
      [4, 4],
      [6, 4],
      [6, 6],
      [4, 6],
      [4, 4],
    ];
    expect(distanceToPolygon(5, 5, [square, hole])).toBeLessThan(0);
    expect(distanceToPolygon(2, 5, [square, hole])).toBeCloseTo(2);
  });
});

describe('polylabel', () => {
  it('en un cuadrado es el centro y su distancia al borde', () => {
    const pole = polylabel([square], 0.01);
    expect(pole.x).toBeCloseTo(5, 1);
    expect(pole.y).toBeCloseTo(5, 1);
    expect(pole.distance).toBeCloseTo(5, 1);
  });

  it('en una «C» queda dentro del polígono aunque el centroide caiga fuera', () => {
    // El centroide de la «C» está en la bahía (x ≈ 4,6, y = 5) y la bahía no es parte del polígono.
    const area = (ring: PolygonRing) => {
      let sum = 0;
      for (let i = 0; i < ring.length - 1; i += 1) sum += ring[i]![0] * ring[i + 1]![1] - ring[i + 1]![0] * ring[i]![1];
      return sum / 2;
    };
    expect(area(cShape)).toBeGreaterThan(0);
    expect(distanceToPolygon(4.6, 5, [cShape])).toBeLessThan(0);

    const pole = polylabel([cShape], 0.01);
    expect(distanceToPolygon(pole.x, pole.y, [cShape])).toBeGreaterThan(0);
    // Los brazos miden 3 de ancho (círculo de radio 1,5), pero en la esquina donde el brazo se une al lomo
    // cabe uno mayor: el que toca los dos lados exteriores y la esquina entrante, de radio 3·√2 / (1 + √2).
    expect(pole.distance).toBeCloseTo((3 * Math.SQRT2) / (1 + Math.SQRT2), 2);
    // Y nunca en la bahía (x > 3 con 3 < y < 7).
    expect(pole.x > 3 && pole.y > 3 && pole.y < 7).toBe(false);
  });

  it('evita el hueco: el polo de un cuadrado con un hueco central está en una esquina de la corona', () => {
    const hole: PolygonRing = [
      [3, 3],
      [7, 3],
      [7, 7],
      [3, 7],
      [3, 3],
    ];
    const pole = polylabel([square, hole], 0.01);
    expect(distanceToPolygon(pole.x, pole.y, [square, hole])).toBeCloseTo(pole.distance, 1);
    // Como en la «C»: el mayor círculo está en una esquina de la corona, junto al hueco.
    expect(pole.distance).toBeCloseTo((3 * Math.SQRT2) / (1 + Math.SQRT2), 2);
    expect(distanceToPolygon(pole.x, pole.y, [hole])).toBeLessThan(0);
  });

  it('en un rectángulo alargado, el polo está en la línea media y su distancia es la mitad del lado corto', () => {
    const wide: PolygonRing = [
      [0, 0],
      [50, 0],
      [50, 4],
      [0, 4],
      [0, 0],
    ];
    const pole = polylabel([wide], 0.01);
    expect(pole.y).toBeCloseTo(2, 1);
    expect(pole.distance).toBeCloseTo(2, 1);
  });

  it('es determinista y devuelve el centro de la caja en un polígono degenerado', () => {
    expect(polylabel([cShape], 0.01)).toEqual(polylabel([cShape], 0.01));
    expect(polylabel([])).toEqual({ x: 0, y: 0, distance: 0 });
    const flat: PolygonRing = [
      [0, 2],
      [4, 2],
      [0, 2],
    ];
    expect(polylabel([flat])).toEqual({ x: 2, y: 2, distance: 0 });
  });
});
