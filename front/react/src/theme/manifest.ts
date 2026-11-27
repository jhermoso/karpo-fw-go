// Lectura de `manifest.json` de un tema de Theros Front (análisis 03 §3 y §7.2).
//
// Es la fuente fiable de «qué soporta este tema»: modos (sólo `modern-flat` y derivados tienen oscuro),
// cultura (dirección, idioma, tendencia), densidad, estilo de iconos y el hash del diccionario.
// El manifiesto real pesa unos 170 KB por el informe de contraste (PT-14 pide uno ligero): aquí sólo
// se conserva lo que el front usa.
import type { ThemeInfo, ThemeMode } from '../contracts';

export interface ThemeManifestInfo extends Omit<ThemeInfo, 'iconStyle'> {
  /** `undefined` si el manifiesto no lo declara: el proveedor lo mide con una sonda. */
  iconStyle?: ThemeInfo['iconStyle'];
  density?: string;
  locale?: string;
  /** `green-up` (por defecto) o `red-up` (la bolsa china). */
  trend?: string;
  /** Hash de lo resuelto: cambia si cambia cualquier valor del tema. */
  resolvedHash?: string;
}

type Json = Record<string, unknown>;

function isRecord(value: unknown): value is Json {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function valueOf(entry: unknown): string | undefined {
  if (isRecord(entry) && typeof entry.value === 'string') return entry.value;
  return typeof entry === 'string' ? entry : undefined;
}

/** Modos del tema según `design.mode` y, si falta, según los modos que el manifiesto declara. */
export function modesFromManifest(design: unknown, declaredModes: unknown): ThemeMode[] {
  const base = isRecord(design) ? valueOf(design['design.mode']) : undefined;
  if (base === 'system') return ['light', 'dark', 'system'];
  if (base === 'dark') return ['dark'];
  if (base === 'light') return ['light'];
  return isRecord(declaredModes) && 'dark' in declaredModes ? ['light', 'dark', 'system'] : ['light'];
}

/** Interpreta el JSON de `manifest.json`; tolera campos ausentes (un tema mínimo sigue valiendo). */
export function parseThemeManifest(raw: unknown, fallbackName: string): ThemeManifestInfo {
  const m: Json = isRecord(raw) ? raw : {};
  const dictionary = isRecord(m.dictionary) ? m.dictionary : {};
  const culture = isRecord(m.culture) ? m.culture : {};
  const design = isRecord(m.design) ? m.design : {};
  const look = isRecord(m.look) ? m.look : {};
  const declaredModes = isRecord(m.modes) ? m.modes : {};

  const iconStyle = valueOf(look['look.control.icon.style']);
  const direction = culture.direction === 'rtl' || culture.direction === 'ltr' ? culture.direction : undefined;

  return {
    name: typeof m.theme === 'string' && m.theme !== '' ? m.theme : fallbackName,
    modes: modesFromManifest(design, declaredModes),
    highContrast: 'high-contrast' in declaredModes,
    iconStyle: iconStyle === 'outline' || iconStyle === 'filled' ? iconStyle : undefined,
    direction,
    dictionaryHash: typeof dictionary.hash === 'string' ? dictionary.hash : '',
    density: valueOf(design['design.density']),
    locale: typeof culture.locale === 'string' ? culture.locale : undefined,
    trend: typeof culture.trend === 'string' ? culture.trend : undefined,
    resolvedHash: typeof m.resolved === 'string' ? m.resolved : undefined,
  };
}

/** Descarga y interpreta el manifiesto. Falla si la respuesta no es 2xx o no es JSON. */
export async function loadThemeManifest(
  url: string,
  fallbackName: string,
  options: { signal?: AbortSignal; fetcher?: typeof fetch } = {},
): Promise<ThemeManifestInfo> {
  const fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
  const response = await fetcher(url, { signal: options.signal, headers: { Accept: 'application/json' } });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  return parseThemeManifest(await response.json(), fallbackName);
}
