// Textos de la configuración: literal, clave de i18n (`*Key`) y objeto por idioma.
// Resolución (PLAN §4.3): clave traducida en el idioma activo -> diccionario de la configuración
// (`manifest.i18n[lng]`) -> objeto por idioma -> literal.
import i18next from 'i18next';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import type { Text } from '../contracts';
import { FALLBACK_LOCALE, reserveFor } from '../i18n/locales';

/** Función que traduce una clave con el sistema de i18n; `undefined` si la clave no existe. */
export type ConfigTranslate = (key: string, lng: string) => string | undefined;

type Dictionary = Record<string, string>;

const dictionaries = new Map<string, Dictionary>();

function flatten(value: unknown, prefix: string, out: Dictionary): void {
  if (typeof value === 'string') {
    if (prefix) out[prefix] = value;
    return;
  }
  if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
    for (const [key, child] of Object.entries(value)) {
      flatten(child, prefix ? `${prefix}.${key}` : key, out);
    }
  }
}

/** Registra el diccionario de la configuración de un idioma (plano o anidado). */
export function registerConfigDictionary(lng: string, dictionary: unknown): void {
  const flat: Dictionary = {};
  flatten(dictionary, '', flat);
  dictionaries.set(lng, { ...(dictionaries.get(lng) ?? {}), ...flat });
}

export function clearConfigDictionaries(): void {
  dictionaries.clear();
}

export function getConfigDictionary(lng: string): Readonly<Dictionary> {
  return dictionaries.get(lng) ?? {};
}

function lookupDictionary(key: string, lng: string): string | undefined {
  const base = lng.split('-')[0] ?? lng;
  const own = dictionaries.get(lng)?.[key] ?? (base !== lng ? dictionaries.get(base)?.[key] : undefined);
  if (own !== undefined) return own;
  // Reserva del idioma (D-R38): al árabe le faltan los textos de las entidades sin traducir y cae al inglés,
  // no al literal castellano de la configuración. Los demás idiomas no tienen reserva propia.
  const reserve = reserveFor(base);
  return reserve !== FALLBACK_LOCALE && reserve !== base ? dictionaries.get(reserve)?.[key] : undefined;
}

function pickByLang(byLang: Record<string, string>, lng: string): string | undefined {
  const base = lng.split('-')[0] ?? lng;
  return byLang[lng] ?? byLang[base];
}

interface I18nLike {
  exists?: (key: string, options?: Record<string, unknown>) => boolean;
  t?: (key: string, options?: Record<string, unknown>) => unknown;
}

/** Busca la clave tal cual y en el espacio `config` (donde se cargan los diccionarios). */
export function translateWith(source: unknown, key: string, lng: string): string | undefined {
  const i18n = source as I18nLike | undefined;
  if (!i18n || typeof i18n.exists !== 'function' || typeof i18n.t !== 'function') return undefined;
  for (const candidate of key.includes(':') ? [key] : [key, `config:${key}`]) {
    if (i18n.exists(candidate, { lng })) {
      const value = i18n.t(candidate, { lng });
      if (typeof value === 'string') return value;
    }
  }
  return undefined;
}

/** Traductor por defecto: la instancia global de i18next si está inicializada. */
function globalTranslate(key: string, lng: string): string | undefined {
  if (!i18next.isInitialized) return undefined;
  return translateWith(i18next, key, lng);
}

/** Resuelve un `Text`: clave en el idioma activo, diccionario de la configuración, idioma, literal. */
export function resolveText(text: Text | undefined, lng: string, translate?: ConfigTranslate): string {
  if (!text) return '';
  if (text.key) {
    const translated = (translate ?? globalTranslate)(text.key, lng);
    if (translated !== undefined) return translated;
    const fromDictionary = lookupDictionary(text.key, lng);
    if (fromDictionary !== undefined) return fromDictionary;
  }
  if (text.byLang) {
    const byLang = pickByLang(text.byLang, lng);
    if (byLang !== undefined) return byLang;
  }
  return text.literal || text.key || '';
}

/** Gancho que devuelve el resolvedor de textos del idioma activo (se actualiza al cambiar de idioma). */
export function useText(): (text: Text | undefined) => string {
  const { i18n } = useTranslation();
  const lng = i18n?.resolvedLanguage ?? i18n?.language ?? 'es';
  return useCallback(
    (text: Text | undefined) =>
      resolveText(text, lng, (key, language) => translateWith(i18n, key, language)),
    [i18n, lng],
  );
}

const LANG_KEY = /^[a-z]{2,3}(?:[-_][A-Za-z0-9]+)*$/;

/** Construye un `Text` desde un valor de la configuración (cadena u objeto por idioma) y su clave. */
export function toText(value: unknown, key?: unknown): Text | undefined {
  const k = typeof key === 'string' && key.trim() !== '' ? key.trim() : undefined;
  if (typeof value === 'string') return k ? { literal: value, key: k } : { literal: value };
  if (typeof value === 'number' || typeof value === 'boolean') {
    return { literal: String(value), ...(k ? { key: k } : {}) };
  }
  if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
    const entries = Object.entries(value as Record<string, unknown>).filter(
      ([lang, v]) => typeof v === 'string' && LANG_KEY.test(lang),
    ) as [string, string][];
    if (entries.length > 0) {
      const byLang = Object.fromEntries(entries);
      const literal = byLang.es ?? byLang.en ?? entries[0]![1];
      return { literal, byLang, ...(k ? { key: k } : {}) };
    }
  }
  if (k) return { literal: '', key: k };
  return undefined;
}

/** `camelCase` -> «Camel case», para etiquetas que faltan en la configuración. */
export function humanize(field: string): string {
  const spaced = field
    .replace(/[_-]+/g, ' ')
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .trim()
    .toLowerCase();
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}
