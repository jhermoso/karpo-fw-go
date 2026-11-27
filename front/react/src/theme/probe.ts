// Sondas de estilo calculado (análisis 03 §7.1 y §6.5).
//
// Las variables `--th-*` de color son `light-dark(claro, oscuro)` SIN resolver: leerlas con
// getPropertyValue devuelve el texto, no un color. Para MapLibre, canvas y driver.js hay que pintar
// la variable en un elemento y leer el color calculado.

/** `token` puede ser `--th-x`, `var(--th-x)` o cualquier expresión de color CSS (color-mix...). */
function toCssValue(token: string): string {
  const trimmed = token.trim();
  return trimmed.startsWith('--') ? `var(${trimmed})` : trimmed;
}

const RGB_FUNCTION = /^rgba?\(/i;

/**
 * Convierte un color calculado que no es `rgb()` (Chromium devuelve `oklab()` o `color(srgb ...)` para
 * los `color-mix()` no sRGB) en `rgb()`/`rgba()` con un lienzo 1x1; MapLibre y driver.js no entienden
 * `oklab()`. Si no hay lienzo (jsdom), devuelve el texto calculado tal cual.
 */
function toRgbString(computed: string, doc: Document): string {
  if (RGB_FUNCTION.test(computed) || computed === '') return computed;
  try {
    const canvas = doc.createElement('canvas');
    canvas.width = 1;
    canvas.height = 1;
    const ctx = canvas.getContext('2d', { willReadFrequently: true });
    if (!ctx) return computed;
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = computed;
    ctx.fillRect(0, 0, 1, 1);
    const [r = 0, g = 0, b = 0, a = 255] = ctx.getImageData(0, 0, 1, 1).data;
    return a === 255 ? `rgb(${r}, ${g}, ${b})` : `rgba(${r}, ${g}, ${b}, ${Math.round((a / 255) * 1000) / 1000})`;
  } catch {
    return computed;
  }
}

/**
 * Resuelve un token (o expresión de color) al `rgb()` que ve el usuario en `el` (por defecto, la raíz):
 * respeta el modo claro/oscuro, el contraste alto y los colores forzados del contenedor.
 * Devuelve `''` si la expresión no es un color válido.
 */
export function resolveThemeColor(token: string, el?: Element): string {
  const doc = el?.ownerDocument ?? document;
  const host: Element = el ?? doc.documentElement;
  const probe = doc.createElement('span');
  probe.setAttribute('aria-hidden', 'true');
  probe.style.setProperty('position', 'absolute');
  probe.style.setProperty('visibility', 'hidden');
  probe.style.setProperty('pointer-events', 'none');
  probe.style.setProperty('inline-size', '0');
  probe.style.setProperty('block-size', '0');
  probe.style.setProperty('color', toCssValue(token));
  // Una expresión inválida deja `color` vacío: el calculado sería el heredado, no el pedido.
  if (probe.style.getPropertyValue('color') === '') return '';
  host.appendChild(probe);
  try {
    const view = doc.defaultView ?? window;
    const computed = view.getComputedStyle(probe).color.trim();
    return toRgbString(computed, doc);
  } finally {
    probe.remove();
  }
}

/**
 * Mide el estilo de icono del tema: `glossy-2003` pinta los `svg[data-th-icon]` rellenos y los demás
 * de contorno (A3 §6.5). Sólo se usa cuando `manifest.json` no lo declara.
 */
export function measureIconStyle(root?: Element): 'outline' | 'filled' {
  const doc = root?.ownerDocument ?? document;
  const host: Element = root ?? doc.documentElement;
  const ns = 'http://www.w3.org/2000/svg';
  const svg = doc.createElementNS(ns, 'svg');
  svg.setAttribute('data-th-icon', 'probe');
  svg.setAttribute('aria-hidden', 'true');
  svg.style.setProperty('position', 'absolute');
  svg.style.setProperty('visibility', 'hidden');
  svg.style.setProperty('pointer-events', 'none');
  host.appendChild(svg);
  try {
    const fill = (doc.defaultView ?? window).getComputedStyle(svg).fill.trim();
    return fill === 'none' || fill === '' ? 'outline' : 'filled';
  } finally {
    svg.remove();
  }
}

/**
 * Resuelve una variable de medida del tema (`--th-space-2`, `--th-edge-rounding-medium`) a píxeles en
 * `el`. Para librerías que piden números (driver.js: `stagePadding`, `stageRadius`). `NaN` si no se
 * puede medir (sin tema cargado, jsdom).
 */
export function resolveThemeLength(token: string, el?: Element): number {
  const doc = el?.ownerDocument ?? document;
  const host: Element = el ?? doc.documentElement;
  const probe = doc.createElement('span');
  probe.setAttribute('aria-hidden', 'true');
  probe.style.setProperty('position', 'absolute');
  probe.style.setProperty('visibility', 'hidden');
  probe.style.setProperty('pointer-events', 'none');
  probe.style.setProperty('padding-inline-start', toCssValue(token));
  host.appendChild(probe);
  try {
    return Number.parseFloat((doc.defaultView ?? window).getComputedStyle(probe).paddingInlineStart);
  } finally {
    probe.remove();
  }
}
