// Modelo del organigrama (PLAN §4.11): un árbol de filas construido desde una lista plana (`flat`:
// padre por `parentField`) o desde el endpoint de organigrama de Go (`chart`: árbol anidado con
// profundidad limitada y carga perezosa). Todo aquí es puro: sin React, sin red.
import type { Row, ViewDef } from '../contracts';

export type TreeViewDef = Extract<ViewDef, { kind: 'tree' }>;
export type ChartSourceDef = Extract<TreeViewDef['source'], { type: 'chart' }>;

export interface TreeNode {
  id: string;
  row: Row;
  parentId: string | null;
  /** Identificadores de los hijos, en el orden en que llegaron. */
  children: string[];
  /**
   * Fuente `chart`: los hijos de este nodo no se han pedido todavía (está en el último nivel de lo
   * cargado). Al desplegarlo se pide más profundidad. Una rama `flat` nunca es frontera.
   */
  frontier: boolean;
}

export interface TreeModel {
  nodes: Map<string, TreeNode>;
  /** Raíces en orden. */
  roots: string[];
}

export function emptyModel(): TreeModel {
  return { nodes: new Map(), roots: [] };
}

function text(value: unknown): string {
  if (value === undefined || value === null) return '';
  return String(value).trim();
}

/** El valor de un campo que puede ser anidado (`location.geoBoundary`). */
export function readPath(row: unknown, path: string): unknown {
  let value: unknown = row;
  for (const part of path.split('.')) {
    if (typeof value !== 'object' || value === null) return undefined;
    value = (value as Record<string, unknown>)[part];
  }
  return value;
}

/** Padre de una fila: la cadena del campo, o `null` si está vacío (raíz). */
export function parentOf(row: Row, parentField: string): string | null {
  const value = readPath(row, parentField);
  // `parentField` puede ser una clave ajena objeto ({id,...}) en algunas configuraciones.
  if (typeof value === 'object' && value !== null) {
    const id = text((value as Record<string, unknown>).id);
    return id === '' ? null : id;
  }
  const id = text(value);
  return id === '' ? null : id;
}

/**
 * Árbol desde una lista plana. Raíces: padre vacío o ausente. Una fila cuyo padre no está en la
 * lista (huérfana) o que forma parte de un ciclo se trata también como raíz, para que ninguna fila
 * desaparezca ni el recorrido se cuelgue.
 */
export function buildFromFlat(rows: readonly Row[], parentField: string): TreeModel {
  const nodes = new Map<string, TreeNode>();
  for (const row of rows) {
    if (!row.id || nodes.has(row.id)) continue;
    nodes.set(row.id, { id: row.id, row, parentId: null, children: [], frontier: false });
  }
  const roots: string[] = [];
  for (const node of nodes.values()) {
    const parent = parentOf(node.row, parentField);
    if (parent !== null && parent !== node.id && nodes.has(parent)) {
      node.parentId = parent;
      nodes.get(parent)!.children.push(node.id);
    } else {
      roots.push(node.id);
    }
  }
  // Ciclos: lo que no se alcanza desde las raíces cuelga de su primer miembro en el orden de la lista.
  const reached = new Set<string>();
  const visit = (start: string) => {
    const stack = [start];
    while (stack.length > 0) {
      const id = stack.pop()!;
      if (reached.has(id)) continue;
      reached.add(id);
      for (const child of nodes.get(id)!.children) stack.push(child);
    }
  };
  roots.forEach(visit);
  for (const node of nodes.values()) {
    if (reached.has(node.id)) continue;
    const parent = node.parentId !== null ? nodes.get(node.parentId) : undefined;
    if (parent) parent.children = parent.children.filter((c) => c !== node.id);
    node.parentId = null;
    roots.push(node.id);
    visit(node.id);
  }
  return { nodes, roots };
}

type Obj = Record<string, unknown>;

function isObj(value: unknown): value is Obj {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** Un `GET …/chart?depth=` ya normalizado a camelCase: `{ position, reports: [...] }`. */
export interface ChartTree {
  row: Row;
  reports: ChartTree[];
}

/** Lee un nodo del organigrama con los nombres que declara la configuración (`nodeField`, `childrenField`). */
export function parseChart(data: unknown, source: Pick<ChartSourceDef, 'nodeField' | 'childrenField'>): ChartTree | null {
  if (!isObj(data)) return null;
  const rowValue = data[source.nodeField];
  if (!isObj(rowValue)) return null;
  const id = rowValue.id ?? rowValue.ID ?? rowValue.Id;
  const row = { ...rowValue, id: id === undefined || id === null ? '' : String(id) } as Row;
  if (!row.id) return null;
  const children = Array.isArray(data[source.childrenField]) ? (data[source.childrenField] as unknown[]) : [];
  const reports: ChartTree[] = [];
  for (const child of children) {
    const parsed = parseChart(child, source);
    if (parsed) reports.push(parsed);
  }
  return { row, reports };
}

/**
 * Árbol desde las raíces de la lista y los organigramas pedidos (`chart`). `loaded` dice hasta qué
 * profundidad se pidió cada raíz: los nodos de ese último nivel quedan como frontera (puede haber
 * más debajo). Raíz sin organigrama aún: nodo frontera sin hijos.
 */
export function buildFromChart(
  roots: readonly Row[],
  charts: ReadonlyMap<string, ChartTree>,
  depth: number,
): TreeModel {
  const model = emptyModel();
  for (const row of roots) {
    if (!row.id || model.nodes.has(row.id)) continue;
    const chart = charts.get(row.id);
    model.roots.push(row.id);
    if (chart) attachChart(model, chart, null, 1, depth);
    else model.nodes.set(row.id, { id: row.id, row, parentId: null, children: [], frontier: true });
  }
  return model;
}

function attachChart(model: TreeModel, chart: ChartTree, parentId: string | null, level: number, depth: number): void {
  const node: TreeNode = {
    id: chart.row.id,
    row: chart.row,
    parentId,
    children: [],
    frontier: level >= depth && chart.reports.length === 0,
  };
  // El último nivel pedido puede tener hijos sin cargar aunque `reports` venga vacío.
  model.nodes.set(node.id, node);
  for (const report of chart.reports) {
    if (model.nodes.has(report.row.id)) continue;
    node.children.push(report.row.id);
    attachChart(model, report, node.id, level + 1, depth);
  }
}

/**
 * Sustituye la rama de `rootId` por un organigrama más profundo (carga perezosa al desplegar un
 * nodo frontera). Devuelve un modelo nuevo; el original no se toca.
 */
export function mergeChart(model: TreeModel, chart: ChartTree, depth: number): TreeModel {
  const base = model.nodes.get(chart.row.id);
  if (!base) return model;
  const nodes = new Map(model.nodes);
  // Se quita la rama antigua y se vuelve a montar con la nueva.
  const drop = [...base.children];
  while (drop.length > 0) {
    const id = drop.pop()!;
    const node = nodes.get(id);
    if (!node) continue;
    drop.push(...node.children);
    nodes.delete(id);
  }
  const next: TreeModel = { nodes, roots: model.roots };
  attachChart(next, chart, base.parentId, 1, depth);
  return next;
}

// ---------------------------------------------------------------------------------------------
// Recorridos
// ---------------------------------------------------------------------------------------------

export interface Descendants {
  /** Descendientes conocidos (sin contar el propio nodo). */
  count: number;
  /** Hay ramas sin cargar por debajo: el número real puede ser mayor. */
  partial: boolean;
}

/** Recuento de descendientes de todos los nodos, en una sola pasada. */
export function countDescendants(model: TreeModel): Map<string, Descendants> {
  const out = new Map<string, Descendants>();
  const order: string[] = [];
  const stack = [...model.roots];
  while (stack.length > 0) {
    const id = stack.pop()!;
    order.push(id);
    for (const child of model.nodes.get(id)?.children ?? []) stack.push(child);
  }
  for (let i = order.length - 1; i >= 0; i -= 1) {
    const id = order[i]!;
    const node = model.nodes.get(id)!;
    let count = 0;
    let partial = node.frontier;
    for (const child of node.children) {
      const d = out.get(child)!;
      count += 1 + d.count;
      partial ||= d.partial;
    }
    out.set(id, { count, partial });
  }
  return out;
}

export function hasChildren(node: TreeNode): boolean {
  return node.children.length > 0 || node.frontier;
}

export function ancestorsOf(model: TreeModel, id: string): string[] {
  const out: string[] = [];
  let cursor = model.nodes.get(id)?.parentId ?? null;
  for (let guard = 0; cursor !== null && guard < 10_000; guard += 1) {
    out.push(cursor);
    cursor = model.nodes.get(cursor)?.parentId ?? null;
  }
  return out;
}

export interface VisibleItem {
  id: string;
  /** Nivel (1 = raíz): `aria-level`. */
  level: number;
  /** Hermanos del mismo padre: `aria-setsize`. */
  setSize: number;
  /** Posición entre hermanos, desde 1: `aria-posinset`. */
  posInSet: number;
  parentId: string | null;
  expandable: boolean;
  expanded: boolean;
}

/** Nodos visibles en orden de documento, según los desplegados. */
export function flattenVisible(model: TreeModel, expanded: ReadonlySet<string>): VisibleItem[] {
  const out: VisibleItem[] = [];
  const walk = (ids: readonly string[], level: number, parentId: string | null) => {
    ids.forEach((id, index) => {
      const node = model.nodes.get(id);
      if (!node) return;
      const expandable = hasChildren(node);
      const isOpen = expandable && expanded.has(id);
      out.push({ id, level, setSize: ids.length, posInSet: index + 1, parentId, expandable, expanded: isOpen });
      if (isOpen) walk(node.children, level + 1, id);
    });
  };
  walk(model.roots, 1, null);
  return out;
}

/** Hermanos de un nodo (incluido él), en orden. */
export function siblingsOf(model: TreeModel, id: string): string[] {
  const node = model.nodes.get(id);
  if (!node) return [];
  return node.parentId === null ? model.roots : (model.nodes.get(node.parentId)?.children ?? []);
}

/** Quita acentos y mayúsculas para comparar. */
export function fold(value: string): string {
  // `\p{M}` (marcas de combinación) y no `\p{Diacritic}`: éste incluye signos sueltos como «·» o «^».
  return value.normalize('NFD').replace(/\p{M}/gu, '').toLocaleLowerCase();
}

/**
 * Identificadores cuyo texto contiene la consulta, en orden de documento (se recorre el árbol
 * entero, también lo plegado).
 */
export function findMatches(model: TreeModel, query: string, textOf: (node: TreeNode) => string): string[] {
  const needle = fold(query.trim());
  if (needle === '') return [];
  const out: string[] = [];
  const walk = (ids: readonly string[]) => {
    for (const id of ids) {
      const node = model.nodes.get(id);
      if (!node) continue;
      if (fold(textOf(node)).includes(needle)) out.push(id);
      walk(node.children);
    }
  };
  walk(model.roots);
  return out;
}

/** Conjunto desplegado más los antecesores de las coincidencias (la búsqueda despliega). */
export function expandForMatches(model: TreeModel, expanded: ReadonlySet<string>, matches: readonly string[]): Set<string> {
  const next = new Set(expanded);
  for (const id of matches) for (const ancestor of ancestorsOf(model, id)) next.add(ancestor);
  return next;
}

/** Todos los nodos que tienen hijos (para «desplegar todo»). */
export function expandableIds(model: TreeModel): string[] {
  const out: string[] = [];
  for (const node of model.nodes.values()) if (node.children.length > 0) out.push(node.id);
  return out;
}

/** Texto con la parte coincidente señalada: `[antes, coincidencia, después]` o `null` si no hay. */
export function splitMatch(value: string, query: string): [string, string, string] | null {
  const needle = fold(query.trim());
  if (needle === '') return null;
  // `fold` conserva la longitud del texto compuesto (é -> e); en texto descompuesto no hay marca.
  const index = fold(value).indexOf(needle);
  if (index < 0 || fold(value).length !== value.length) return null;
  return [value.slice(0, index), value.slice(index, index + needle.length), value.slice(index + needle.length)];
}
