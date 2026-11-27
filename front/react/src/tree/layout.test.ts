import { describe, expect, it } from 'vitest';
import { layoutTree } from './layout';

const size = { width: 100, height: 40 };
const nodes = ['r', 'a', 'b', 'c', 'a1'].map((id) => ({ id, ...size }));
const edges = [
  { source: 'r', target: 'a' },
  { source: 'r', target: 'b' },
  { source: 'r', target: 'c' },
  { source: 'a', target: 'a1' },
];
const options = { direction: 'ltr' as const, gapX: 20, gapY: 60 };

describe('disposición del diagrama', () => {
  it('pone cada nivel en su fila, de arriba abajo, con el hueco pedido', () => {
    const { positions } = layoutTree(nodes, edges, options);
    const y = (id: string) => positions.get(id)!.y;
    expect(y('r')).toBe(0);
    expect(y('a')).toBe(size.height + options.gapY);
    expect(y('a')).toBe(y('b'));
    expect(y('a')).toBe(y('c'));
    expect(y('a1')).toBe(2 * (size.height + options.gapY));
  });

  it('mantiene el orden de los hermanos de izquierda a derecha y centra el padre', () => {
    const { positions } = layoutTree(nodes, edges, options);
    const x = (id: string) => positions.get(id)!.x;
    expect(x('a')).toBeLessThan(x('b'));
    expect(x('b')).toBeLessThan(x('c'));
    expect(x('c') - x('b')).toBeGreaterThanOrEqual(size.width + options.gapX);
    expect(x('r') + size.width / 2).toBeCloseTo((x('a') + x('c') + size.width) / 2, 5);
  });

  it('en rtl los hermanos van en espejo y el diagrama ocupa el mismo ancho', () => {
    const ltr = layoutTree(nodes, edges, options);
    const rtl = layoutTree(nodes, edges, { ...options, direction: 'rtl' });
    const x = (id: string) => rtl.positions.get(id)!.x;
    expect(x('a')).toBeGreaterThan(x('b'));
    expect(x('b')).toBeGreaterThan(x('c'));
    expect(rtl.width).toBe(ltr.width);
    expect(rtl.height).toBe(ltr.height);
    for (const [id, p] of ltr.positions) {
      expect(rtl.positions.get(id)!.x).toBeCloseTo(ltr.width - p.x - size.width, 5);
      expect(rtl.positions.get(id)!.y).toBe(p.y);
    }
  });

  it('en un árbol de varios niveles cada nivel sigue el orden de documento y nada se solapa', () => {
    const ids = ['r', 'a', 'b', 'a1', 'a2', 'b1', 'b2', 'b21'];
    const tree = ids.map((id) => ({ id, ...size }));
    const links = [
      ['r', 'a'],
      ['r', 'b'],
      ['a', 'a1'],
      ['a', 'a2'],
      ['b', 'b1'],
      ['b', 'b2'],
      ['b2', 'b21'],
    ].map(([source, target]) => ({ source: source!, target: target! }));
    const { positions } = layoutTree(tree, links, options);
    const order = (level: string[]) => [...level].sort((p, q) => positions.get(p)!.x - positions.get(q)!.x);
    expect(order(['a1', 'a2', 'b1', 'b2'])).toEqual(['a1', 'a2', 'b1', 'b2']);
    const boxes = ids.map((id) => ({ id, ...positions.get(id)! }));
    for (const first of boxes) {
      for (const second of boxes) {
        if (first.id >= second.id || first.y !== second.y) continue;
        expect(Math.abs(first.x - second.x)).toBeGreaterThanOrEqual(size.width + options.gapX);
      }
    }
  });

  it('un árbol vacío no tiene tamaño y uno de un solo nodo queda en el origen', () => {
    expect(layoutTree([], [], options)).toEqual({ positions: new Map(), width: 0, height: 0 });
    const single = layoutTree([{ id: 'x', ...size }], [], options);
    expect(single.positions.get('x')).toEqual({ x: 0, y: 0 });
    expect(single.width).toBe(size.width);
  });
});
