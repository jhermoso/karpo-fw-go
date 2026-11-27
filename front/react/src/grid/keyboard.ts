// Teclado de la cuadrícula (PLAN §4.9): cálculo puro de la celda a la que lleva cada tecla de
// navegación y ayudas de rangos. Sin React ni DOM: se prueba aparte.

/** Fila de la cabecera (las de datos son 0..n-1). */
export const HEADER_ROW = -1;

export interface Position {
  row: number;
  col: number;
}

export interface Dimensions {
  /** Filas de datos. */
  rows: number;
  /** Columnas navegables (selección, datos y acciones). */
  cols: number;
  /** Filas que caben en la ventana (RePág/AvPág). */
  page: number;
  /** En RTL las flechas horizontales se invierten. */
  rtl?: boolean;
}

export interface NavKey {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  shiftKey?: boolean;
}

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

/**
 * Celda a la que mueve la tecla, `null` si no es de navegación. `Tab` y `Shift+Tab` NO son teclas de
 * navegación: la cuadrícula es una sola parada de tabulación (patrón APG; WCAG 2.1.1 y 2.1.2, D-R21), así
 * que el navegador saca el foco de la rejilla y `Shift+Tab` vuelve a la celda activa. Sólo al editar,
 * `Tab` confirma y avanza (`stepCell`, desde `afterCommit`).
 */
export function nextPosition(pos: Position, event: NavKey, dims: Dimensions): Position | null {
  const { rows, cols, page, rtl = false } = dims;
  if (cols <= 0) return null;
  const last = rows - 1;
  const lastCol = cols - 1;
  const mod = Boolean(event.ctrlKey || event.metaKey);
  const step = rtl ? -1 : 1;
  switch (event.key) {
    case 'ArrowUp':
      return { row: Math.max(HEADER_ROW, pos.row - 1), col: pos.col };
    case 'ArrowDown':
      return { row: Math.min(Math.max(last, HEADER_ROW), pos.row + 1), col: pos.col };
    case 'ArrowLeft':
      return { row: pos.row, col: clamp(pos.col - step, 0, lastCol) };
    case 'ArrowRight':
      return { row: pos.row, col: clamp(pos.col + step, 0, lastCol) };
    case 'Home':
      return mod ? { row: rows > 0 ? 0 : HEADER_ROW, col: 0 } : { row: pos.row, col: 0 };
    case 'End':
      return mod ? { row: Math.max(last, HEADER_ROW), col: lastCol } : { row: pos.row, col: lastCol };
    case 'PageDown':
      return { row: clamp(pos.row + Math.max(1, page), HEADER_ROW, Math.max(last, HEADER_ROW)), col: pos.col };
    case 'PageUp':
      return { row: Math.max(pos.row <= HEADER_ROW ? HEADER_ROW : 0, pos.row - Math.max(1, page)), col: pos.col };
    default:
      return null;
  }
}

/** Teclas de navegación que la cuadrícula consume (el resto sigue su camino). */
export function isNavigationKey(key: string): boolean {
  return (
    key === 'ArrowUp' ||
    key === 'ArrowDown' ||
    key === 'ArrowLeft' ||
    key === 'ArrowRight' ||
    key === 'Home' ||
    key === 'End' ||
    key === 'PageUp' ||
    key === 'PageDown'
  );
}

/** ¿Es un carácter imprimible que debe abrir el editor sobrescribiendo? */
export function isPrintable(event: { key: string; ctrlKey?: boolean; metaKey?: boolean; altKey?: boolean }): boolean {
  return event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey && event.key !== ' ';
}

export interface Range {
  top: number;
  bottom: number;
  start: number;
  end: number;
}

/** Rango rectangular entre dos celdas (las filas de datos; la cabecera cuenta como 0). */
export function rangeBetween(anchor: Position, focus: Position): Range {
  const a = Math.max(0, anchor.row);
  const b = Math.max(0, focus.row);
  return {
    top: Math.min(a, b),
    bottom: Math.max(a, b),
    start: Math.min(anchor.col, focus.col),
    end: Math.max(anchor.col, focus.col),
  };
}

export function inRange(range: Range | null, row: number, col: number): boolean {
  return range !== null && row >= range.top && row <= range.bottom && col >= range.start && col <= range.end;
}

/** Un rango de una sola celda no es una selección de rango. */
export function isMultiCell(range: Range | null): boolean {
  return range !== null && (range.top !== range.bottom || range.start !== range.end);
}

/**
 * La celda de al lado al confirmar una edición con Tab (`next`) o Mayús+Tab (`prev`): sigue por la fila y,
 * al llegar al extremo, pasa a la primera (o última) celda de la fila contigua. En la última celda de la
 * última fila (o la primera de la primera) devuelve `null`: no hay celda a la que ir. Sólo la usa
 * `afterCommit`; fuera de la edición Tab no mueve la celda activa.
 */
export function stepCell(pos: Position, dir: 'next' | 'prev', dims: Dimensions): Position | null {
  const { rows, cols } = dims;
  if (cols <= 0 || rows <= 0) return null;
  const lastCol = cols - 1;
  if (dir === 'prev') {
    if (pos.col > 0) return { row: pos.row, col: pos.col - 1 };
    if (pos.row <= 0) return null;
    return { row: pos.row - 1, col: lastCol };
  }
  if (pos.col < lastCol) return { row: pos.row, col: pos.col + 1 };
  if (pos.row >= rows - 1) return null;
  return { row: pos.row + 1, col: 0 };
}

/**
 * Tras confirmar una edición con `move`, la celda siguiente. En Enter baja (o sube con Shift), en Tab va
 * a la celda de al lado y en la última sigue en la primera de la fila siguiente. En el último extremo se
 * queda en la misma celda: el foco vuelve a ella y no se pierde.
 */
export function afterCommit(pos: Position, move: 'next' | 'prev' | 'down' | 'up' | undefined, dims: Dimensions): Position {
  if (move === 'down') return { row: Math.min(dims.rows - 1, pos.row + 1), col: pos.col };
  if (move === 'up') return { row: Math.max(0, pos.row - 1), col: pos.col };
  if (move === 'next' || move === 'prev') return stepCell(pos, move, dims) ?? pos;
  return pos;
}
