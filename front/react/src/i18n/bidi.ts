// Aislamiento bidireccional de las interpolaciones (M-12, D-R27).
//
// Una cadena traducida lleva el sentido de escritura de su idioma; un valor interpolado (un número, un
// nombre, un rango) puede llevar el contrario o ninguno. En un idioma de derecha a izquierda, «cambios 4»
// o «de 1.204» se desordenan sin aislamiento. El estándar (UAX #9) lo resuelve con los aislantes Unicode:
// FSI (U+2068, «first strong isolate») antes del valor y PDI (U+2069) después. No añaden ningún elemento
// ni cambian lo que lee un lector de pantalla, y el texto sigue siendo texto (`textContent`).
//
// Sólo se aplica con dirección RTL: en LTR las cadenas quedan limpias (y las pruebas, legibles).

/** FIRST STRONG ISOLATE: abre un aislante cuyo sentido lo da la primera letra fuerte del valor. */
export const FSI = '\u2068';
/** POP DIRECTIONAL ISOLATE: cierra el aislante abierto por FSI. */
export const PDI = '\u2069';

/** Envuelve un texto en FSI … PDI. Una cadena vacía se queda vacía. */
export function isolate(text: string): string {
  return text === '' ? text : `${FSI}${text}${PDI}`;
}

type Direction = 'ltr' | 'rtl';

/**
 * Dirección forzada por instancia de i18next (`?dir=rtl` con es/en, que no tienen sentido RTL propio). La
 * pone `I18nProvider` desde el mismo valor con el que escribe `<html dir>`; sin ella manda el idioma.
 */
const forced = new WeakMap<object, Direction>();

/** Fija (o, con `null`, quita) la dirección forzada de una instancia. */
export function setIsolateDirection(instance: object, direction: Direction | null): void {
  if (direction === null) forced.delete(instance);
  else forced.set(instance, direction);
}

/** Dirección forzada de una instancia, si la hay. */
export function isolateDirection(instance: object): Direction | undefined {
  return forced.get(instance);
}
