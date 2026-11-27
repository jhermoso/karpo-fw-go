// Utilidades de valores de campo, sin React.
import type { OptionDef, Text, TextResolver } from '../contracts';

/** Vacío para la validación y la edición: nulo, indefinido, cadena en blanco o lista vacía. */
export function isEmpty(value: unknown): boolean {
  if (value === null || value === undefined) return true;
  if (typeof value === 'string') return value.trim() === '';
  if (Array.isArray(value)) return value.length === 0;
  return false;
}

/** Marca única de «sin valor» (D-R35): una raya larga, igual en la rejilla, en la ficha y en el mapa. */
export const EMPTY_MARK = '—';

/** Cadenas hechas sólo de guiones o rayas (`-`, `–`, `—`, `‒`, `−`...) que la configuración usa como «vacío». */
const DASHES_ONLY = /^[\s\-\u2010-\u2015\u2212]+$/u;

/**
 * Texto que se pinta para un valor vacío (D-R35). Sin `emptyValue`, o con uno hecho sólo de guiones, la marca
 * única `EMPTY_MARK`; cualquier otro texto de la configuración («sin fecha», «·») se respeta tal cual.
 */
export function emptyText(emptyValue: string | undefined): string {
  if (emptyValue === undefined || emptyValue.trim() === '' || DASHES_ONLY.test(emptyValue)) return EMPTY_MARK;
  return emptyValue;
}

/** Igualdad tolerante para decidir si el editor debe releer el valor del padre. */
export function sameValue(a: unknown, b: unknown): boolean {
  if (isEmpty(a) && isEmpty(b)) return true;
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((item, index) => String(item) === String(b[index]));
  }
  return a === b || (a !== null && b !== null && typeof a !== 'object' && String(a) === String(b));
}

/** Lista de cadenas de un valor multiselección (admite una cadena separada por comas). */
export function toStringArray(value: unknown): string[] {
  if (Array.isArray(value)) return value.map((item) => String(item));
  if (typeof value === 'string' && value.trim() !== '') {
    return value
      .split(/[,;|]/)
      .map((part) => part.trim())
      .filter(Boolean);
  }
  return [];
}

/** Texto en el idioma (`byLang`) o su literal; la clave de i18n la resuelven los componentes. */
export function textFor(text: Text | undefined, locale: string): string {
  if (!text) return '';
  const base = locale.split('-')[0] ?? locale;
  return text.byLang?.[locale] ?? text.byLang?.[base] ?? text.literal;
}

/**
 * Texto con el resolvedor de la configuración si lo hay (clave traducida, diccionario, idioma, literal);
 * sin él, el objeto por idioma o el literal (`textFor`).
 */
export function resolveWith(text: Text | undefined, locale: string, resolve?: TextResolver): string {
  if (!text) return '';
  return resolve ? resolve(text) : textFor(text, locale);
}

export function optionLabel(option: OptionDef, locale: string, resolve?: TextResolver): string {
  return resolveWith(option.label, locale, resolve);
}

/** Normaliza para comparar etiquetas: sin acentos, sin mayúsculas ni espacios sobrantes. */
export function foldText(text: string): string {
  return text
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')
    .trim()
    .toLowerCase();
}

/**
 * Opción por valor exacto, o por valor/etiqueta sin distinguir mayúsculas ni acentos. La etiqueta es la
 * traducida (con `resolve`, la del idioma activo: se puede pegar «Filled» en inglés), la del objeto por
 * idioma o el literal de la configuración.
 */
export function findOption(
  options: readonly OptionDef[] | undefined,
  needle: unknown,
  locale: string,
  resolve?: TextResolver,
): OptionDef | undefined {
  if (!options || needle === null || needle === undefined) return undefined;
  const raw = String(needle);
  const exact = options.find((option) => option.value === raw);
  if (exact) return exact;
  const folded = foldText(raw);
  if (folded === '') return undefined;
  return (
    options.find((option) => foldText(option.value) === folded) ??
    options.find((option) => foldText(optionLabel(option, locale, resolve)) === folded) ??
    options.find((option) => foldText(optionLabel(option, locale)) === folded) ??
    options.find((option) => foldText(option.label.literal) === folded)
  );
}
