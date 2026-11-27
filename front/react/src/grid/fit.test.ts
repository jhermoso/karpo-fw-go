// Ajuste de columnas al ancho (D-R34): encoge antes de ocultar, respeta prioridades y no toca la primera ni las fijadas.
import { describe, expect, it } from 'vitest';
import { fitColumns, floorWidth, SHRINK_RATIO, type FitColumn } from './fit';

function col(id: string, extra: Partial<FitColumn> = {}): FitColumn {
  return { id, width: 160, minWidth: 64, ...extra };
}

const sumOf = (widths: Record<string, number>) => Object.values(widths).reduce((total, width) => total + width, 0);

describe('floorWidth', () => {
  it('es el mayor entre el mínimo por tipo y 0,7 del ancho; un ancho fijado por el usuario no encoge', () => {
    expect(SHRINK_RATIO).toBe(0.7);
    expect(floorWidth(col('a'))).toBe(112);
    expect(floorWidth(col('a', { minWidth: 130 }))).toBe(130);
    expect(floorWidth(col('a', { sized: true }))).toBe(160);
    // Nunca por encima de su propio ancho.
    expect(floorWidth(col('a', { width: 80, minWidth: 96 }))).toBe(80);
  });
});

describe('fitColumns', () => {
  const four = [col('a'), col('b'), col('c'), col('d')];

  it('con ancho suficiente no oculta ni encoge nada', () => {
    const result = fitColumns(four, 640);
    expect(result.hidden).toEqual([]);
    expect(result.widths).toEqual({ a: 160, b: 160, c: 160, d: 160 });
  });

  it('sin medida (ancho no positivo o no finito) no cambia nada', () => {
    for (const available of [0, -10, Number.NaN, Number.POSITIVE_INFINITY]) {
      const result = fitColumns(four, available);
      expect(result.hidden).toEqual([]);
      expect(result.widths.a).toBe(160);
    }
  });

  it('encoge antes de ocultar: cabe repartiendo la reducción y no hay ninguna oculta', () => {
    const result = fitColumns(four, 560);
    expect(result.hidden).toEqual([]);
    expect(sumOf(result.widths)).toBe(560);
    for (const width of Object.values(result.widths)) {
      expect(width).toBeGreaterThanOrEqual(112);
      expect(width).toBeLessThanOrEqual(160);
    }
  });

  it('el reparto de la reducción es exacto con enteros aunque no sea divisible', () => {
    const result = fitColumns([col('a'), col('b'), col('c')], 479);
    expect(result.hidden).toEqual([]);
    expect(sumOf(result.widths)).toBe(479);
    expect(Object.values(result.widths).every(Number.isInteger)).toBe(true);
  });

  it('no encoge las columnas con ancho fijado por el usuario: lo absorben las demás', () => {
    const columns = [col('a', { sized: true, width: 200 }), col('b'), col('c')];
    const result = fitColumns(columns, 440);
    expect(result.hidden).toEqual([]);
    expect(result.widths.a).toBe(200);
    expect(result.widths.b! + result.widths.c!).toBe(240);
  });

  it('sin prioridades oculta de derecha a izquierda cuando encoger no basta', () => {
    // Suelos: 4 x 112 = 448 > 400; con tres, 336 <= 400.
    const result = fitColumns(four, 400);
    expect(result.hidden).toEqual(['d']);
    expect(Object.keys(result.widths)).toEqual(['a', 'b', 'c']);
    expect(sumOf(result.widths)).toBeLessThanOrEqual(400);
    const narrower = fitColumns(four, 230);
    expect(narrower.hidden).toEqual(['c', 'd']);
  });

  it('oculta por prioridad ascendente: la de menor prioridad primero, aunque no sea la última', () => {
    const columns = [col('a', { priority: 1 }), col('b', { priority: 9 }), col('c', { priority: 5 }), col('d', { priority: 7 })];
    expect(fitColumns(columns, 400).hidden).toEqual(['a']);
    expect(fitColumns(columns, 230).hidden).toEqual(['a', 'c']);
  });

  it('con prioridad igual desempata de derecha a izquierda; una columna sin prioridad cuenta como 0', () => {
    const columns = [col('a', { priority: 3 }), col('b'), col('c'), col('d', { priority: 3 })];
    // b y c valen 0: se oculta antes la de más a la derecha (c), después b.
    expect(fitColumns(columns, 400).hidden).toEqual(['c']);
    expect(fitColumns(columns, 230).hidden).toEqual(['b', 'c']);
  });

  it('nunca oculta la primera columna ni las fijadas, aunque sean las de menor prioridad', () => {
    const columns = [
      col('first', { locked: true, priority: 0 }),
      col('b', { priority: 5 }),
      col('c', { priority: 6 }),
      col('pinned', { locked: true, priority: -4 }),
    ];
    const result = fitColumns(columns, 300);
    expect(result.hidden).toEqual(['b', 'c']);
    expect(Object.keys(result.widths)).toEqual(['first', 'pinned']);
  });

  it('si ni las bloqueadas caben, las deja en su suelo (la lista se desplaza) y oculta el resto', () => {
    const columns = [col('a', { locked: true }), col('b', { locked: true }), col('c')];
    const result = fitColumns(columns, 100);
    expect(result.hidden).toEqual(['c']);
    expect(result.widths).toEqual({ a: 112, b: 112 });
  });

  it('tras ocultar, el resto vuelve a su ancho natural si ya caben', () => {
    const columns = [col('a'), col('b'), col('c', { priority: -1 })];
    // Los suelos suman 336 > 330: se oculta c y a y b caben al natural (320).
    const result = fitColumns(columns, 330);
    expect(result.hidden).toEqual(['c']);
    expect(result.widths).toEqual({ a: 160, b: 160 });
  });

  it('el mínimo por tipo manda sobre el 0,7: una insignia no baja de su ancho mínimo', () => {
    const columns = [col('state', { width: 120, minWidth: 96 }), col('name', { width: 200 })];
    const result = fitColumns(columns, 250);
    expect(result.hidden).toEqual([]);
    expect(result.widths.state).toBeGreaterThanOrEqual(96);
    expect(result.widths.name).toBeGreaterThanOrEqual(140);
    expect(sumOf(result.widths)).toBe(250);
  });

  it('lista vacía', () => {
    expect(fitColumns([], 300)).toEqual({ widths: {}, hidden: [] });
  });
});
