import { describe, expect, it } from 'vitest';
import type { Row } from '../contracts';
import {
  ancestorsOf,
  buildFromChart,
  buildFromFlat,
  countDescendants,
  expandForMatches,
  findMatches,
  flattenVisible,
  mergeChart,
  parentOf,
  parseChart,
  siblingsOf,
  splitMatch,
  type ChartTree,
} from './model';

const row = (id: string, extra: Record<string, unknown> = {}): Row => ({ id, ...extra }) as Row;

describe('árbol desde una lista plana', () => {
  const rows = [
    row('a', { name: 'Dirección', partOf: '' }),
    row('b', { name: 'Ventas', partOf: 'a' }),
    row('c', { name: 'Compras', partOf: 'a' }),
    row('d', { name: 'Zona norte', partOf: 'b' }),
    row('e', { name: 'Otra raíz' }),
  ];

  it('las raíces son las filas con padre vacío o ausente y los hijos conservan el orden de la lista', () => {
    const model = buildFromFlat(rows, 'partOf');
    expect(model.roots).toEqual(['a', 'e']);
    expect(model.nodes.get('a')?.children).toEqual(['b', 'c']);
    expect(model.nodes.get('b')?.children).toEqual(['d']);
    expect(model.nodes.get('d')?.parentId).toBe('b');
    expect([...model.nodes.values()].every((node) => !node.frontier)).toBe(true);
  });

  it('una fila huérfana, su propio padre o un ciclo no se pierden: pasan a ser raíces', () => {
    const model = buildFromFlat(
      [row('x', { p: 'no-existe' }), row('y', { p: 'y' }), row('m', { p: 'n' }), row('n', { p: 'm' })],
      'p',
    );
    expect(model.roots).toEqual(['x', 'y', 'm']);
    expect(model.nodes.get('m')?.children).toEqual(['n']);
    expect(model.nodes.get('n')?.parentId).toBe('m');
    expect(model.nodes.size).toBe(4);
  });

  it('acepta un padre en forma de objeto con id y campos anidados', () => {
    expect(parentOf(row('a', { partOf: { id: 'p' } }), 'partOf')).toBe('p');
    expect(parentOf(row('a', { rel: { parent: 'q' } }), 'rel.parent')).toBe('q');
    expect(parentOf(row('a', { partOf: null }), 'partOf')).toBeNull();
  });

  it('ignora identificadores repetidos y filas sin id', () => {
    const model = buildFromFlat([row('a'), row('a'), { id: '' } as Row], 'p');
    expect(model.nodes.size).toBe(1);
  });
});

describe('árbol desde el organigrama (chart)', () => {
  const source = { nodeField: 'position', childrenField: 'reports' };
  const raw = {
    position: { id: 'r', title: 'Raíz' },
    reports: [
      { position: { id: 'r1', title: 'Uno' }, reports: [{ position: { id: 'r11' }, reports: [] }] },
      { position: { id: 'r2' }, reports: [] },
    ],
  };

  it('parseChart respeta los nombres de la configuración y descarta nodos sin id', () => {
    const tree = parseChart(raw, source)!;
    expect(tree.row.id).toBe('r');
    expect(tree.reports.map((n) => n.row.id)).toEqual(['r1', 'r2']);
    expect(parseChart({ position: {} }, source)).toBeNull();
    expect(parseChart(null, source)).toBeNull();
    expect(parseChart({ nodo: { id: 'z' } }, { nodeField: 'nodo', childrenField: 'hijos' })?.row.id).toBe('z');
  });

  it('construye el mismo árbol que la fuente flat y marca como frontera el último nivel pedido', () => {
    const chart = parseChart(raw, source)!;
    const fromChart = buildFromChart([row('r')], new Map([['r', chart]]), 3);
    const fromFlat = buildFromFlat(
      [row('r'), row('r1', { up: 'r' }), row('r2', { up: 'r' }), row('r11', { up: 'r1' })],
      'up',
    );
    expect(fromChart.roots).toEqual(fromFlat.roots);
    for (const [id, node] of fromFlat.nodes) {
      expect(fromChart.nodes.get(id)?.children).toEqual(node.children);
      expect(fromChart.nodes.get(id)?.parentId).toBe(node.parentId);
    }
    // Profundidad 3: r11 está en el nivel 3 y no tiene hijos conocidos, así que puede tener más.
    expect(fromChart.nodes.get('r11')?.frontier).toBe(true);
    expect(fromChart.nodes.get('r2')?.frontier).toBe(false);
    // Una raíz sin organigrama aún es frontera sin hijos.
    const pending = buildFromChart([row('r')], new Map(), 3);
    expect(pending.nodes.get('r')).toMatchObject({ frontier: true, children: [] });
  });

  it('mergeChart sustituye la rama de un nodo frontera por una más profunda sin tocar el original', () => {
    const base = buildFromChart([row('r')], new Map([['r', parseChart(raw, source)!]]), 3);
    const deeper: ChartTree = {
      row: row('r11'),
      reports: [{ row: row('r111'), reports: [{ row: row('r1111'), reports: [] }] }],
    };
    const merged = mergeChart(base, deeper, 3);
    expect(merged.nodes.get('r11')?.children).toEqual(['r111']);
    expect(merged.nodes.get('r111')?.parentId).toBe('r11');
    expect(merged.nodes.get('r1111')?.frontier).toBe(true);
    expect(merged.nodes.get('r11')?.frontier).toBe(false);
    expect(base.nodes.has('r111')).toBe(false);
    expect(merged.nodes.get('r1')?.children).toEqual(['r11']);
    expect(mergeChart(base, { row: row('nada'), reports: [] }, 3)).toBe(base);
  });
});

describe('recorridos', () => {
  const model = buildFromFlat(
    [
      row('a', { n: 'Dirección' }),
      row('b', { n: 'Ventas', p: 'a' }),
      row('c', { n: 'Compras', p: 'a' }),
      row('d', { n: 'Zona norte', p: 'b' }),
      row('e', { n: 'Zona sur', p: 'b' }),
    ],
    'p',
  );

  it('cuenta descendientes de todos los nodos', () => {
    const counts = countDescendants(model);
    expect(counts.get('a')).toEqual({ count: 4, partial: false });
    expect(counts.get('b')).toEqual({ count: 2, partial: false });
    expect(counts.get('d')).toEqual({ count: 0, partial: false });
  });

  it('el recuento avisa de ramas sin cargar', () => {
    const chart = buildFromChart(
      [row('r')],
      new Map([['r', { row: row('r'), reports: [{ row: row('s'), reports: [] }] }]]),
      2,
    );
    expect(countDescendants(chart).get('r')).toEqual({ count: 1, partial: true });
  });

  it('aplana en orden de documento con nivel, tamaño del grupo y posición', () => {
    const visible = flattenVisible(model, new Set(['a']));
    expect(visible.map((v) => v.id)).toEqual(['a', 'b', 'c']);
    expect(visible[1]).toMatchObject({ level: 2, setSize: 2, posInSet: 1, parentId: 'a', expandable: true, expanded: false });
    expect(visible[2]).toMatchObject({ level: 2, setSize: 2, posInSet: 2, expandable: false });
    const all = flattenVisible(model, new Set(['a', 'b']));
    expect(all.map((v) => v.id)).toEqual(['a', 'b', 'd', 'e', 'c']);
    expect(all[2]).toMatchObject({ level: 3, setSize: 2, posInSet: 1 });
    expect(flattenVisible(model, new Set()).map((v) => v.id)).toEqual(['a']);
  });

  it('busca sin distinguir acentos ni mayúsculas, en todo el árbol, y despliega los antecesores', () => {
    const textOf = (node: { row: Row }) => String(node.row.n);
    expect(findMatches(model, 'ZONA', textOf)).toEqual(['d', 'e']);
    expect(findMatches(model, 'direccion', textOf)).toEqual(['a']);
    expect(findMatches(model, '   ', textOf)).toEqual([]);
    expect(ancestorsOf(model, 'e')).toEqual(['b', 'a']);
    expect([...expandForMatches(model, new Set(), ['e'])].sort()).toEqual(['a', 'b']);
    expect(siblingsOf(model, 'c')).toEqual(['b', 'c']);
    expect(siblingsOf(model, 'a')).toEqual(['a']);
  });

  it('parte un texto en antes, coincidencia y después', () => {
    expect(splitMatch('Dirección general', 'ecci')).toEqual(['Dir', 'ecci', 'ón general']);
    expect(splitMatch('Dirección', 'DIRECCION')).toEqual(['', 'Dirección', '']);
    expect(splitMatch('Dirección', 'xyz')).toBeNull();
    expect(splitMatch('Dirección', '')).toBeNull();
  });
});
