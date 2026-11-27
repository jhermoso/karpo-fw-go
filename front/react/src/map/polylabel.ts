// Polo de inaccesibilidad (Q-9): el punto de un polígono más alejado de su contorno, que es donde cabe
// el rótulo más grande. Cálculo propio (sin dependencia): rejilla adaptativa de celdas ordenadas por
// la mejor distancia que aún podrían alcanzar, que se subdividen mientras puedan mejorar el mejor
// punto hallado en más que la precisión pedida. Puro.

/** Un punto `[x, y]` de un anillo. */
export type Point = readonly [number, number];
/** Un anillo (lista de puntos; cerrado o no) del polígono: el primero es el exterior y el resto, huecos. */
export type PolygonRing = readonly Point[];

export interface PolePoint {
  x: number;
  y: number;
  /** Distancia del punto al contorno más cercano (en las unidades de las coordenadas). */
  distance: number;
}

interface Cell {
  x: number;
  y: number;
  /** Semilado de la celda. */
  h: number;
  /** Distancia con signo del centro al polígono (positiva dentro). */
  d: number;
  /** Mejor distancia que podría alcanzar un punto de la celda. */
  max: number;
}

/** Cola con prioridad por `max` (la mayor primero): montículo binario. */
class CellQueue {
  private readonly items: Cell[] = [];

  get length(): number {
    return this.items.length;
  }

  push(cell: Cell): void {
    const items = this.items;
    items.push(cell);
    let index = items.length - 1;
    while (index > 0) {
      const parent = (index - 1) >> 1;
      if (items[parent]!.max >= items[index]!.max) break;
      [items[parent], items[index]] = [items[index]!, items[parent]!];
      index = parent;
    }
  }

  pop(): Cell | undefined {
    const items = this.items;
    const top = items[0];
    const last = items.pop();
    if (items.length > 0 && last) {
      items[0] = last;
      let index = 0;
      for (;;) {
        const left = index * 2 + 1;
        const right = left + 1;
        let largest = index;
        if (left < items.length && items[left]!.max > items[largest]!.max) largest = left;
        if (right < items.length && items[right]!.max > items[largest]!.max) largest = right;
        if (largest === index) break;
        [items[largest], items[index]] = [items[index]!, items[largest]!];
        index = largest;
      }
    }
    return top;
  }
}

/** Distancia al cuadrado de un punto a un segmento. */
function segmentDistanceSq(px: number, py: number, a: Point, b: Point): number {
  let x = a[0];
  let y = a[1];
  let dx = b[0] - x;
  let dy = b[1] - y;
  if (dx !== 0 || dy !== 0) {
    const t = ((px - x) * dx + (py - y) * dy) / (dx * dx + dy * dy);
    if (t > 1) {
      x = b[0];
      y = b[1];
    } else if (t > 0) {
      x += dx * t;
      y += dy * t;
    }
  }
  dx = px - x;
  dy = py - y;
  return dx * dx + dy * dy;
}

/** Distancia con signo de un punto al polígono: positiva dentro, negativa fuera. */
export function distanceToPolygon(x: number, y: number, rings: readonly PolygonRing[]): number {
  let inside = false;
  let minSq = Infinity;
  for (const ring of rings) {
    for (let i = 0, j = ring.length - 1; i < ring.length; j = i, i += 1) {
      const a = ring[i]!;
      const b = ring[j]!;
      if (a[1] > y !== b[1] > y && x < ((b[0] - a[0]) * (y - a[1])) / (b[1] - a[1]) + a[0]) inside = !inside;
      minSq = Math.min(minSq, segmentDistanceSq(x, y, a, b));
    }
  }
  return minSq === 0 ? 0 : (inside ? 1 : -1) * Math.sqrt(minSq);
}

function makeCell(x: number, y: number, h: number, rings: readonly PolygonRing[]): Cell {
  const d = distanceToPolygon(x, y, rings);
  return { x, y, h, d, max: d + h * Math.SQRT2 };
}

/** Celda en el centroide del anillo exterior (un buen primer candidato en las formas convexas). */
function centroidCell(rings: readonly PolygonRing[]): Cell {
  const ring = rings[0]!;
  let area = 0;
  let x = 0;
  let y = 0;
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i, i += 1) {
    const a = ring[i]!;
    const b = ring[j]!;
    const cross = a[0] * b[1] - b[0] * a[1];
    x += (a[0] + b[0]) * cross;
    y += (a[1] + b[1]) * cross;
    area += cross * 3;
  }
  if (area === 0) return makeCell(ring[0]![0], ring[0]![1], 0, rings);
  return makeCell(x / area, y / area, 0, rings);
}

/**
 * Polo de inaccesibilidad de un polígono con huecos. `precision` es el error admitido en las
 * unidades de las coordenadas (por omisión, una milésima de la menor dimensión del polígono).
 * Un polígono sin anillo exterior o degenerado devuelve el centro de su caja con distancia 0.
 */
export function polylabel(rings: readonly PolygonRing[], precision?: number): PolePoint {
  const outer = rings[0];
  if (!outer || outer.length === 0) return { x: 0, y: 0, distance: 0 };

  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const [px, py] of outer) {
    minX = Math.min(minX, px);
    minY = Math.min(minY, py);
    maxX = Math.max(maxX, px);
    maxY = Math.max(maxY, py);
  }
  const width = maxX - minX;
  const height = maxY - minY;
  const cellSize = Math.min(width, height);
  if (cellSize === 0) return { x: minX + width / 2, y: minY + height / 2, distance: 0 };
  const epsilon = precision && precision > 0 ? precision : cellSize / 1000;

  const queue = new CellQueue();
  let h = cellSize / 2;
  for (let x = minX; x < maxX; x += cellSize) {
    for (let y = minY; y < maxY; y += cellSize) queue.push(makeCell(x + h, y + h, h, rings));
  }

  let best = centroidCell(rings);
  const boxCell = makeCell(minX + width / 2, minY + height / 2, 0, rings);
  if (boxCell.d > best.d) best = boxCell;

  for (let cell = queue.pop(); cell; cell = queue.pop()) {
    if (cell.d > best.d) best = cell;
    // Esta celda ya no puede mejorar el mejor punto en más que la precisión: no se subdivide.
    if (cell.max - best.d <= epsilon) continue;
    h = cell.h / 2;
    queue.push(makeCell(cell.x - h, cell.y - h, h, rings));
    queue.push(makeCell(cell.x + h, cell.y - h, h, rings));
    queue.push(makeCell(cell.x - h, cell.y + h, h, rings));
    queue.push(makeCell(cell.x + h, cell.y + h, h, rings));
  }
  return { x: best.x, y: best.y, distance: best.d };
}
