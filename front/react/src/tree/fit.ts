// Encuadre del diagrama del organigrama (M-1). Puro: de las medidas del diagrama y de la ventana sale
// dónde se pone la cámara.
//
// Un organigrama grande no cabe en la ventana a un tamaño legible, y encuadrarlo entero lo deja a escala
// 0,3 con el texto ilegible. Por eso el encuadre inicial tiene un zoom MÍNIMO (`FIT_MIN_ZOOM`): si el
// diagrama entero necesita menos, se queda en ese mínimo, centrado en la raíz, y el resto se desplaza. El
// botón «Ajustar» alterna entre ese encuadre legible y «todo» (hasta `MIN_ZOOM`).

/** Zoom más bajo al que se acerca la cámara (la rueda y «todo»). */
export const MIN_ZOOM = 0.1;
export const MAX_ZOOM = 2;
/** Zoom más bajo del encuadre inicial: a 0,85 el texto del tema sigue siendo legible (≥ 11 px). */
export const FIT_MIN_ZOOM = 0.85;
/** El encuadre no agranda más allá de la escala natural. */
export const FIT_MAX_ZOOM = 1;
/** Por debajo de este zoom el diagrama pasa a «lejos»: sólo el título y el contador. */
export const FAR_ZOOM = 0.6;
/** Tamaño mínimo, en píxeles de pantalla, del texto en el zoom lejano. */
export const MIN_EFFECTIVE_FONT = 11;

export type FitMode = 'legible' | 'all';

export interface Viewport {
  x: number;
  y: number;
  zoom: number;
}

export interface FitInput {
  /** Tamaño de todo el diagrama en su propio espacio. */
  diagram: { width: number; height: number };
  /** Franja que ocupan las raíces (arriba): el encuadre legible se ancla en ella. */
  roots: { x: number; width: number };
  /** Escritura de derecha a izquierda: el diagrama está en espejo y la primera raíz queda a la derecha. */
  rtl?: boolean;
  /** Tamaño del lienzo en píxeles. */
  view: { width: number; height: number };
  /** Aire alrededor, en píxeles. */
  padding: number;
}

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

/** Zoom con el que cabe todo el diagrama (entre `MIN_ZOOM` y `FIT_MAX_ZOOM`). */
export function zoomToFitAll({ diagram, view, padding }: FitInput): number {
  const width = Math.max(1, diagram.width);
  const height = Math.max(1, diagram.height);
  const fit = Math.min((view.width - 2 * padding) / width, (view.height - 2 * padding) / height);
  return clamp(Number.isFinite(fit) ? fit : FIT_MAX_ZOOM, MIN_ZOOM, FIT_MAX_ZOOM);
}

/** ¿Difieren «legible» y «todo»? Si todo cabe a un tamaño legible, el botón sólo reencuadra. */
export function fitModesDiffer(input: FitInput): boolean {
  return zoomToFitAll(input) < FIT_MIN_ZOOM;
}

/**
 * Cámara del encuadre. `all`: todo el diagrama, centrado. `legible`: lo mismo si cabe a 0,85 o más; si no,
 * a 0,85 con la raíz arriba y centrada (el resto se alcanza desplazando). Si hay varias raíces y no caben
 * juntas a ese zoom, se empieza por el principio del diagrama (a la izquierda, o a la derecha en RTL) y no
 * por el medio de todas, que dejaría las primeras fuera de la ventana.
 */
export function fitViewport(input: FitInput, mode: FitMode): Viewport {
  const { diagram, roots, view, padding, rtl = false } = input;
  const all = zoomToFitAll(input);
  const zoom = mode === 'all' ? all : Math.max(all, FIT_MIN_ZOOM);
  if (zoom <= all || mode === 'all') {
    return {
      zoom: all,
      x: (view.width - diagram.width * all) / 2,
      y: (view.height - diagram.height * all) / 2,
    };
  }
  // Una raíz sola (o varias que caben juntas) se centra; un bosque que no cabe empieza por su borde de inicio,
  // el del diagrama entero (sus hijos pueden quedar más allá de la primera raíz).
  const fitsAcross = roots.width * zoom <= view.width - 2 * padding;
  const x = fitsAcross
    ? view.width / 2 - (roots.x + roots.width / 2) * zoom
    : rtl
      ? view.width - padding - diagram.width * zoom
      : padding;
  return { zoom, x, y: padding };
}

/** ¿El zoom es «lejano»? (por debajo de `FAR_ZOOM`) */
export function isFarZoom(zoom: number): boolean {
  return zoom < FAR_ZOOM;
}

/**
 * Tamaño de letra (píxeles del diagrama) que se ve a `MIN_EFFECTIVE_FONT` píxeles de pantalla con ese zoom,
 * sin pasar de `ceiling` (con la tarjeta más pequeña que dos líneas de ese texto no tiene sentido crecer).
 */
export function farFontSize(zoom: number, ceiling: number, theme = MIN_EFFECTIVE_FONT): number {
  return Math.min(Math.max(MIN_EFFECTIVE_FONT, theme) / Math.max(zoom, MIN_ZOOM), ceiling);
}
