// Nombres de las delimitaciones en el idioma activo: los países salen de `Intl.DisplayNames` (el
// GeoJSON de la demostración los trae en inglés); el resto, de la propia geometría.
import type { GeoFeature } from './geo';

const displayNames = new Map<string, Intl.DisplayNames | null>();

function regionNames(locale: string): Intl.DisplayNames | null {
  if (!displayNames.has(locale)) {
    try {
      displayNames.set(locale, new Intl.DisplayNames([locale], { type: 'region' }));
    } catch {
      displayNames.set(locale, null);
    }
  }
  return displayNames.get(locale) ?? null;
}

export function displayName(feature: Pick<GeoFeature, 'code' | 'name' | 'level'>, locale: string): string {
  if (feature.level === 'country' && /^[A-Z]{2}$/.test(feature.code)) {
    try {
      const name = regionNames(locale)?.of(feature.code);
      if (name && name !== feature.code) return name;
    } catch {
      // código que Intl no conoce: se queda el de la geometría
    }
  }
  return feature.name;
}
