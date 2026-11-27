import { describe, expect, it } from 'vitest';
import type { Row } from '../contracts';
import {
  aggregateRows,
  binLabels,
  binOf,
  boundaryCode,
  boundaryIds,
  chainOf,
  computeBins,
  isLocatable,
  keyOf,
  numeric,
  readPath,
  type ChainLink,
} from './aggregate';

const row = (id: string, extra: Record<string, unknown>): Row => ({ id, ...extra }) as Row;

// Madrid (municipio) ⊂ provincia ES300 ⊂ comunidad ES30 ⊂ agrupación ES3 ⊂ España.
const municipality = { id: 'm-madrid', level: 'local', lauCode: '28079', ineCode: '28079' };
const ancestors = [
  { id: 'p-madrid', level: '3', nutsCode: 'ES300' },
  { id: 'c-madrid', level: '2', nutsCode: 'ES30' },
  { id: 'n1', level: '1', nutsCode: 'ES3' },
  { id: 'spain', level: 'country', geoCode: 'ES' },
];

describe('código de una delimitación', () => {
  it('cada nivel se une a la geometría con su código', () => {
    expect(boundaryCode({ level: 'country', geoCode: 'ES' })).toBe('ES');
    expect(boundaryCode({ level: '2', nutsCode: 'ES30' })).toBe('ES30');
    expect(boundaryCode({ level: '3', nutsCode: 'ES300', ineCode: '28' })).toBe('ES300');
    expect(boundaryCode({ level: 'local', lauCode: '28079', ineCode: '28079' })).toBe('28079');
    expect(boundaryCode({ level: 'local', ineCode: '08019' })).toBe('08019');
    expect(boundaryCode({ level: 1, nutsCode: 'ES3' })).toBe('ES3');
    expect(boundaryCode({ level: 'country' })).toBe('');
  });

  it('sólo se puede colocar lo que tiene nivel y código', () => {
    expect(isLocatable({ level: '2', nutsCode: 'ES30' })).toBe(true);
    expect(isLocatable({ nutsCode: 'ES30' })).toBe(false);
    expect(isLocatable({ level: '2' })).toBe(false);
  });

  it('la cadena es la propia delimitación y sus antecesores, omitiendo lo que no se puede colocar', () => {
    expect(chainOf(municipality, ancestors)).toEqual([
      { level: 'local', code: '28079' },
      { level: '3', code: 'ES300' },
      { level: '2', code: 'ES30' },
      { level: '1', code: 'ES3' },
      { level: 'country', code: 'ES' },
    ]);
    expect(chainOf(municipality, [{ id: 'sin-datos' }, ...ancestors.slice(2)])).toHaveLength(3);
  });
});

describe('agregado por delimitación subiendo por los antecesores', () => {
  const chainMadrid: ChainLink[] = chainOf(municipality, ancestors);
  const chainGetafe: ChainLink[] = chainOf(
    { id: 'm-getafe', level: 'local', lauCode: '28065' },
    ancestors,
  );
  const chainBarcelona: ChainLink[] = chainOf({ id: 'm-bcn', level: 'local', lauCode: '08019' }, [
    { id: 'p-bcn', level: '3', nutsCode: 'ES511' },
    { id: 'c-cat', level: '2', nutsCode: 'ES51' },
    { id: 'spain', level: 'country', geoCode: 'ES' },
  ]);
  const chains = new Map([
    ['m-madrid', chainMadrid],
    ['m-getafe', chainGetafe],
    ['m-bcn', chainBarcelona],
  ]);
  const rows = [
    row('1', { location: { geoBoundary: 'm-madrid' }, area: '100.50' }),
    row('2', { location: { geoBoundary: 'm-madrid' }, area: '200' }),
    row('3', { location: { geoBoundary: 'm-getafe' }, area: 50 }),
    row('4', { location: { geoBoundary: 'm-bcn' }, area: '10.25' }),
    row('5', { location: { geoBoundary: 'desconocida' }, area: 1 }),
    row('6', { location: {}, area: 1 }),
  ];

  it('cuenta filas en cada nivel de la cadena', () => {
    const { totals, unplaced } = aggregateRows(rows, { boundaryField: 'location.geoBoundary', aggregate: 'count' }, chains);
    expect(totals.get(keyOf('local', '28079'))).toBe(2);
    expect(totals.get(keyOf('local', '28065'))).toBe(1);
    expect(totals.get(keyOf('3', 'ES300'))).toBe(3);
    expect(totals.get(keyOf('2', 'ES30'))).toBe(3);
    expect(totals.get(keyOf('2', 'ES51'))).toBe(1);
    expect(totals.get(keyOf('country', 'ES'))).toBe(4);
    expect(totals.get(keyOf('country', 'FR'))).toBeUndefined();
    // Las dos filas sin delimitación conocida no cuentan, y se sabe cuántas son.
    expect(unplaced).toBe(2);
  });

  it('suma un campo (cadenas decimales de Go incluidas)', () => {
    const { totals } = aggregateRows(
      rows,
      { boundaryField: 'location.geoBoundary', aggregate: 'sum', valueField: 'area' },
      chains,
    );
    expect(totals.get(keyOf('local', '28079'))).toBeCloseTo(300.5);
    expect(totals.get(keyOf('2', 'ES30'))).toBeCloseTo(350.5);
    expect(totals.get(keyOf('country', 'ES'))).toBeCloseTo(360.75);
  });

  it('un nivel repetido en la cadena no se cuenta dos veces y un campo ausente suma cero', () => {
    const odd = new Map([['x', [{ level: '2', code: 'ES30' }, { level: '2', code: 'ES30' }]]]);
    const { totals } = aggregateRows([row('1', { b: 'x' })], { boundaryField: 'b', aggregate: 'count' }, odd);
    expect(totals.get(keyOf('2', 'ES30'))).toBe(1);
    const sum = aggregateRows([row('1', { b: 'x' })], { boundaryField: 'b', aggregate: 'sum', valueField: 'nada' }, odd);
    expect(sum.totals.get(keyOf('2', 'ES30'))).toBe(0);
  });

  it('acepta la delimitación como cadena o como objeto con id, y lista las distintas', () => {
    const list = [row('1', { b: 'a' }), row('2', { b: { id: 'b' } }), row('3', { b: 'a' }), row('4', { b: '' }), row('5', {})];
    expect(boundaryIds(list, 'b').sort()).toEqual(['a', 'b']);
    expect(readPath({ a: { b: { c: 1 } } }, 'a.b.c')).toBe(1);
    expect(readPath({ a: 1 }, 'a.b')).toBeUndefined();
    expect(numeric('12.5')).toBe(12.5);
    expect(numeric('abc')).toBe(0);
    expect(numeric(null)).toBe(0);
  });
});

describe('clases de color', () => {
  it('con un solo valor distinto hay una clase, la más oscura', () => {
    const bins = computeBins([30, 30]);
    expect(bins).toHaveLength(1);
    expect(bins[0]).toMatchObject({ step: 7, from: 30, to: 30 });
    expect(binOf(30, bins)).toBe(0);
  });

  it('reparte los pasos de la paleta de claro a oscuro, hasta siete clases', () => {
    const bins = computeBins(Array.from({ length: 20 }, (_unused, i) => i + 1));
    expect(bins).toHaveLength(7);
    expect(bins.map((b) => b.step)).toEqual([1, 2, 3, 4, 5, 6, 7]);
    expect(bins[0]!.from).toBe(1);
    expect(bins[6]!.to).toBe(20);
    expect(binOf(1, bins)).toBe(0);
    expect(binOf(20, bins)).toBe(6);
    expect(binOf(10, bins)).toBe(3);
    // Las clases son contiguas.
    for (let i = 1; i < bins.length; i += 1) expect(bins[i]!.from).toBeCloseTo(bins[i - 1]!.to);
  });

  it('no hay más clases que valores distintos y lo que no tiene dato no tiene clase', () => {
    expect(computeBins([3, 9, 9]).map((b) => b.step)).toEqual([1, 7]);
    expect(computeBins([])).toEqual([]);
    expect(computeBins([0, -2, Number.NaN])).toEqual([]);
    const bins = computeBins([2, 8]);
    expect(binOf(undefined, bins)).toBe(-1);
    expect(binOf(0, bins)).toBe(-1);
    expect(binOf(5, [])).toBe(-1);
  });

  it('los textos de la leyenda no se solapan con enteros', () => {
    const bins = computeBins([12, 18]);
    const labels = binLabels(bins, true, String, (from, to) => `${from}-${to}`, (value) => value);
    expect(labels.map((l) => l.text)).toEqual(['12-14', '15-18']);
    expect(binLabels(computeBins([5]), true, String, (a, b) => `${a}-${b}`, (v) => v).map((l) => l.text)).toEqual(['5']);
  });
});
