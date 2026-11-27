// Ajuste de columnas al ancho disponible (D-R34, M-3): la rejilla nunca pinta una columna a medias.
// Función pura, sin React ni DOM: primero encoge las columnas cuyo ancho no fijó el usuario hasta su suelo
// (`SHRINK_RATIO` de su ancho, con el mínimo por tipo) y, si aun así no caben, oculta las menos importantes.

export interface FitColumn {
  id: string;
  /** Ancho actual en píxeles (configuración o fijado por el usuario). */
  width: number;
  /** Ancho mínimo por contenido según el tipo de campo. */
  minWidth: number;
  /** El usuario fijó este ancho: no se encoge (sí se puede ocultar). */
  sized?: boolean;
  /** Nunca se oculta: la primera columna y las fijadas. */
  locked?: boolean;
  /** Importancia: mayor = más importante. Sin ella cuenta como 0 y, a igualdad, se oculta antes la de más a la derecha. */
  priority?: number;
}

export interface FitOptions {
  /** Fracción del ancho hasta la que puede encogerse una columna (por omisión 0,7). */
  shrinkRatio?: number;
}

export interface FitResult {
  /** Ancho de cada columna que se pinta (las ocultas no aparecen). */
  widths: Record<string, number>;
  /** Columnas ocultas por falta de espacio, en el orden de pantalla. */
  hidden: string[];
}

export const SHRINK_RATIO = 0.7;

/** Hasta dónde puede encogerse una columna: su ancho si lo fijó el usuario; si no, el mayor de su mínimo y 0,7 del ancho. */
export function floorWidth(column: FitColumn, ratio: number = SHRINK_RATIO): number {
  if (column.sized) return column.width;
  return Math.min(column.width, Math.max(column.minWidth, Math.ceil(column.width * ratio)));
}

function sum(values: readonly number[]): number {
  return values.reduce((total, value) => total + value, 0);
}

/**
 * Reparte `excess` píxeles de reducción entre las columnas en proporción a lo que cada una puede encoger
 * (`capacity`), con enteros: el resto se asigna a las de mayor parte decimal.
 */
function distribute(capacity: readonly number[], excess: number): number[] {
  const total = sum(capacity);
  if (excess <= 0 || total <= 0) return capacity.map(() => 0);
  if (excess >= total) return [...capacity];
  const exact = capacity.map((room) => (excess * room) / total);
  const cuts = exact.map((value) => Math.floor(value));
  let rest = excess - sum(cuts);
  const byFraction = exact
    .map((value, index) => ({ index, fraction: value - Math.floor(value) }))
    .sort((a, b) => b.fraction - a.fraction || b.index - a.index);
  for (const { index } of byFraction) {
    if (rest <= 0) break;
    if ((cuts[index] ?? 0) < (capacity[index] ?? 0)) {
      cuts[index] = (cuts[index] ?? 0) + 1;
      rest -= 1;
    }
  }
  return cuts;
}

/**
 * Ajusta `columns` (en orden de pantalla) a `available` píxeles. Con ancho suficiente (o sin medida, `available`
 * no positivo) no cambia nada. Primero encoge; después oculta por prioridad ascendente (sin prioridad, de derecha
 * a izquierda), sin tocar nunca las `locked`. Si ni las bloqueadas caben, quedan en su suelo y la lista se desplaza.
 */
export function fitColumns(columns: readonly FitColumn[], available: number, options: FitOptions = {}): FitResult {
  const ratio = options.shrinkRatio ?? SHRINK_RATIO;
  const natural = Object.fromEntries(columns.map((column) => [column.id, column.width]));
  if (!Number.isFinite(available) || available <= 0) return { widths: natural, hidden: [] };

  const shown = [...columns];
  const hidden = new Set<string>();
  while (sum(shown.map((column) => floorWidth(column, ratio))) > available) {
    let victim = -1;
    for (let index = shown.length - 1; index >= 0; index -= 1) {
      const column = shown[index];
      if (!column || column.locked) continue;
      const weaker = victim < 0 || (column.priority ?? 0) < (shown[victim]?.priority ?? 0);
      if (weaker) victim = index;
    }
    if (victim < 0) break;
    const [removed] = shown.splice(victim, 1);
    if (removed) hidden.add(removed.id);
  }

  const total = sum(shown.map((column) => column.width));
  const cuts = distribute(
    shown.map((column) => column.width - floorWidth(column, ratio)),
    total - available,
  );
  const widths: Record<string, number> = {};
  shown.forEach((column, index) => {
    widths[column.id] = column.width - (cuts[index] ?? 0);
  });
  return { widths, hidden: columns.filter((column) => hidden.has(column.id)).map((column) => column.id) };
}
