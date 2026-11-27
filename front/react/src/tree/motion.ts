// Movimiento: las animaciones de cámara (zoom, centrado, vuelo) sólo con «no-preference».

/** `true` si el usuario no ha pedido reducir el movimiento. Sin `matchMedia` (jsdom viejo) no hay animación. */
export function motionAllowed(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
  return window.matchMedia('(prefers-reduced-motion: no-preference)').matches;
}

/** Duración de una animación de cámara: la pedida si se permite movimiento y 0 si no. */
export function cameraDuration(ms: number): number {
  return motionAllowed() ? ms : 0;
}
