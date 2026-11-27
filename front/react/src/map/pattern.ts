// Trama de «sin dato» del mapa (M-2): el color por sí solo no distingue una delimitación sin dato de una
// con el primer paso de la paleta, así que lo que no tiene dato lleva además unas rayas diagonales.
// MapLibre las pinta con `fill-pattern`, que pide una imagen: se dibuja en un `<canvas>` con los colores
// del tema ya resueltos (`resolveThemeColor`) y se registra con `map.addImage`. Cambian con el tema o el
// modo, así que cada combinación de colores tiene su propio nombre de imagen.

/** Prefijo del nombre de la imagen; el sufijo cambia cuando cambian los colores. */
export const HATCH_IMAGE_PREFIX = 'kx-missing-hatch';

/** Lado de la tesela de la trama en píxeles CSS (R3-4, M-2: más rala que los 8 px de antes). */
export const HATCH_SIZE = 12;

/** Grosor de la raya en píxeles CSS (R3-4, M-2: fina; antes era la quinta parte de la tesela). */
export const HATCH_STROKE = 1;

export interface HatchColors {
  /** Color de fondo (el de «sin dato»). */
  base: string;
  /** Color de las rayas (el de la trama del tema). */
  line: string;
}

/** Lo que `map.addImage` necesita (y el `pixelRatio` con que registrarla). */
export interface HatchImage {
  name: string;
  width: number;
  height: number;
  data: Uint8ClampedArray;
  pixelRatio: number;
}

/** `rgb(r, g, b)` o `rgba(r, g, b, a)` (lo que devuelve la sonda del tema) → `[r, g, b, a]` con `a` de 0 a 255. */
export function parseRgb(color: string): [number, number, number, number] | null {
  const match = /^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)(?:[,\s/]+([\d.]+%?))?\s*\)$/i.exec(color.trim());
  if (!match) return null;
  const [r, g, b] = [match[1], match[2], match[3]].map((part) => Math.min(255, Math.max(0, Math.round(Number(part)))));
  const alphaText = match[4];
  let alpha = 255;
  if (alphaText !== undefined) {
    const raw = alphaText.endsWith('%') ? Number.parseFloat(alphaText) / 100 : Number(alphaText);
    alpha = Math.min(255, Math.max(0, Math.round(raw * 255)));
  }
  return [r!, g!, b!, alpha];
}

/** Rayas a 45° sin lienzo (jsdom): el píxel es raya si su diagonal cae en el primer tramo del periodo. */
function drawByHand(
  colors: { base: [number, number, number, number]; line: [number, number, number, number] },
  size: number,
  stroke: number,
): Uint8ClampedArray {
  const data = new Uint8ClampedArray(size * size * 4);
  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      const paint = (x + y) % size < stroke ? colors.line : colors.base;
      data.set(paint, (y * size + x) * 4);
    }
  }
  return data;
}

function drawOnCanvas(
  colors: HatchColors,
  size: number,
  stroke: number,
  canvasFactory: () => HTMLCanvasElement | null,
): Uint8ClampedArray | null {
  try {
    const canvas = canvasFactory();
    const ctx = canvas?.getContext('2d', { willReadFrequently: true });
    if (!canvas || !ctx) return null;
    canvas.width = size;
    canvas.height = size;
    ctx.fillStyle = colors.base;
    ctx.fillRect(0, 0, size, size);
    ctx.strokeStyle = colors.line;
    ctx.lineWidth = stroke;
    ctx.lineCap = 'butt';
    ctx.beginPath();
    // Tres diagonales (la central y las dos de las esquinas) para que la tesela encaje con sus vecinas.
    for (const offset of [-size, 0, size]) {
      ctx.moveTo(offset, size);
      ctx.lineTo(offset + size, 0);
    }
    ctx.stroke();
    return new Uint8ClampedArray(ctx.getImageData(0, 0, size, size).data);
  } catch {
    return null;
  }
}

function defaultCanvas(): HTMLCanvasElement | null {
  return typeof document === 'undefined' ? null : document.createElement('canvas');
}

/**
 * Dibuja la tesela de la trama con esos colores. `size` y `pixelRatio` dan el tamaño en píxeles de
 * pantalla (la imagen mide `size × pixelRatio` y se registra con ese `pixelRatio`); `stroke` es el grosor de
 * la raya en píxeles CSS. Con lienzo se pinta
 * en él; sin lienzo (jsdom) se rellena a mano si los colores son `rgb()`; si no, devuelve `null`.
 */
export function drawHatch(
  colors: HatchColors,
  version: number,
  options: { size?: number; stroke?: number; pixelRatio?: number; canvas?: () => HTMLCanvasElement | null } = {},
): HatchImage | null {
  const pixelRatio = Math.max(1, Math.round(options.pixelRatio ?? 1));
  const size = Math.max(2, Math.round(options.size ?? HATCH_SIZE)) * pixelRatio;
  // La raya mide lo mismo en píxeles CSS con cualquier densidad de pantalla.
  const stroke = Math.max(1, Math.round(options.stroke ?? HATCH_STROKE)) * pixelRatio;
  const name = `${HATCH_IMAGE_PREFIX}-${version}`;
  const drawn = drawOnCanvas(colors, size, stroke, options.canvas ?? defaultCanvas);
  if (drawn) return { name, width: size, height: size, data: drawn, pixelRatio };
  const base = parseRgb(colors.base);
  const line = parseRgb(colors.line);
  if (!base || !line) return null;
  return { name, width: size, height: size, data: drawByHand({ base, line }, size, stroke), pixelRatio };
}
