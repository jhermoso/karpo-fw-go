// Disposición del diagrama (PLAN §4.11): dagre de arriba abajo. En `rtl` los hermanos van en
// espejo (el primero a la derecha). Puro: recibe tamaños y aristas, devuelve posiciones.
import dagre from '@dagrejs/dagre';

export interface LayoutNode {
  id: string;
  width: number;
  height: number;
}

export interface LayoutEdge {
  source: string;
  target: string;
}

export interface LayoutOptions {
  direction: 'ltr' | 'rtl';
  /** Hueco entre hermanos (horizontal). */
  gapX: number;
  /** Hueco entre niveles (vertical). */
  gapY: number;
}

export interface LayoutResult {
  /** Esquina superior izquierda de cada nodo. */
  positions: Map<string, { x: number; y: number }>;
  width: number;
  height: number;
}

/**
 * Posiciones de un árbol. Los nodos llegan en orden de documento y las aristas en el orden de los
 * hijos; ese orden entre hermanos se conserva (restricciones de dagre).
 */
export function layoutTree(nodes: readonly LayoutNode[], edges: readonly LayoutEdge[], options: LayoutOptions): LayoutResult {
  const positions = new Map<string, { x: number; y: number }>();
  if (nodes.length === 0) return { positions, width: 0, height: 0 };

  const graph = new dagre.graphlib.Graph();
  graph.setGraph({ rankdir: 'TB', nodesep: options.gapX, ranksep: options.gapY, marginx: 0, marginy: 0, ranker: 'tight-tree' });
  graph.setDefaultEdgeLabel(() => ({}));
  for (const node of nodes) graph.setNode(node.id, { width: node.width, height: node.height });
  for (const edge of edges) graph.setEdge(edge.source, edge.target);
  // dagre minimiza cruces y puede reordenar hermanos; en un árbol no hay cruces que minimizar, así que
  // se fija el orden de los hermanos con restricciones (el de las aristas, que es el de la lista).
  const constraints: { left: string; right: string }[] = [];
  const lastChild = new Map<string, string>();
  for (const edge of edges) {
    const previous = lastChild.get(edge.source);
    if (previous !== undefined) constraints.push({ left: previous, right: edge.target });
    lastChild.set(edge.source, edge.target);
  }
  dagre.layout(graph, { constraints, disableOptimalOrderHeuristic: true });

  let maxRight = 0;
  let maxBottom = 0;
  const raw = new Map<string, { x: number; y: number; w: number }>();
  for (const node of nodes) {
    const placed = graph.node(node.id);
    const x = placed.x - node.width / 2;
    const y = placed.y - node.height / 2;
    raw.set(node.id, { x, y, w: node.width });
    maxRight = Math.max(maxRight, x + node.width);
    maxBottom = Math.max(maxBottom, y + node.height);
  }
  for (const [id, { x, y, w }] of raw) {
    positions.set(id, { x: options.direction === 'rtl' ? maxRight - (x + w) : x, y });
  }
  return { positions, width: maxRight, height: maxBottom };
}
