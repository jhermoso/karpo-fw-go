// Descubrimiento de idiomas y espacios de nombres (PLAN §4.6): un fichero por idioma y espacio,
// `src/locales/<idioma>/<espacio>.json`, descubiertos con import.meta.glob. Añadir un fichero o un idioma
// no toca código ajeno.
//
// POR DEMANDA (D-R37, M-11): el glob NO es `eager`. Cada fichero es un trozo aparte que se pide con
// `loadBundles` y se guarda en una caché del módulo; al arrancar sólo se pide el idioma activo y los espacios
// del marco (`FRAME_NAMESPACES`), y el resto (`grid`, `detail`, `tree`, `map`, `help`) con su vista
// (`ensureNamespaces`, en `bundles.ts`). Las pruebas lo cargan todo de una vez (`preloadAllLocales`).
import type { Resource } from 'i18next';

/** Idioma de referencia: siempre completo; el resto cae a él si les falta una clave. */
export const REFERENCE_LOCALE = 'es';
/** Idioma de pseudolocalización (sólo en desarrollo): `[Ḥóḷá]` para cazar textos escritos a mano. */
export const PSEUDO_LOCALE = 'xx';
export const FALLBACK_LOCALE = REFERENCE_LOCALE;

/**
 * Reserva por idioma (D-R38): lo que le falte al árabe (sobre todo los textos de la configuración de una
 * entidad aún sin traducir) se completa con inglés, no con castellano. El resto cae a `FALLBACK_LOCALE`.
 */
const RESERVE_LOCALES: Readonly<Record<string, string>> = { ar: 'en' };

/** Idioma de reserva de `lng` (por su idioma base: `ar-EG` → `en`). */
export function reserveFor(lng: string): string {
  return RESERVE_LOCALES[baseLanguage(lng)] ?? FALLBACK_LOCALE;
}

/** `fallbackLng` de i18next: una lista por idioma con reserva propia y la de siempre para el resto. */
export function fallbackLanguages(): Record<string, string[]> {
  const table: Record<string, string[]> = { default: [FALLBACK_LOCALE] };
  for (const [lng, reserve] of Object.entries(RESERVE_LOCALES)) table[lng] = [reserve];
  return table;
}

type LocaleModules = Record<string, unknown>;

/**
 * Espacios del marco: los que necesita la primera pantalla (cabecera, menú, botón de ayuda, acceso, errores)
 * y los que el trozo inicial ya trae empaquetados (`ui` y `fields`, de `ui/i18n.ts`). Se piden al arrancar.
 * Los de las vistas (`grid`, `detail`, `tree`, `map`) llegan con ellas (`ensureNamespaces`).
 */
export const FRAME_NAMESPACES: readonly string[] = ['core', 'ui', 'fields', 'auth', 'errors', 'help'];

/**
 * Idiomas con paridad de claves exigida por la prueba de contrato (`locales.contract.test.ts`): bastan por
 * sí solos. Cualquier otro puede ir incompleto y cae al de referencia, que entonces se carga también.
 */
const COMPLETE_LOCALES: readonly string[] = [REFERENCE_LOCALE, 'en'];

const LOADERS: Record<string, () => Promise<unknown>> = import.meta.glob('../locales/*/*.json', { import: 'default' });

const PATH = /(?:^|\/)locales\/([^/]+)\/([^/]+)\.json$/;

interface LocaleFile {
  lng: string;
  ns: string;
  load: () => Promise<unknown>;
}

const FILES: readonly LocaleFile[] = Object.entries(LOADERS).flatMap(([path, load]) => {
  const match = PATH.exec(path.replaceAll('\\', '/'));
  return match ? [{ lng: match[1] ?? '', ns: match[2] ?? '', load }] : [];
});

/** Agrupa los ficheros `…/locales/<idioma>/<espacio>.json` en recursos de i18next. */
function group(modules: LocaleModules): Resource {
  const resources: Resource = {};
  for (const [path, content] of Object.entries(modules)) {
    const match = PATH.exec(path.replaceAll('\\', '/'));
    if (!match || typeof content !== 'object' || content === null) continue;
    const [, lng = '', ns = ''] = match;
    (resources[lng] ??= {})[ns] = content as Record<string, never>;
  }
  return resources;
}

/** Lo ya cargado: idioma → espacio → contenido (lo llena `loadBundles`). */
const loaded: Resource = {};
const inFlight = new Map<string, Promise<void>>();

/**
 * Recursos de i18next.
 *  - Con `modules` (rutas → contenido): los agrupa, como el glob eager de antes (las pruebas pasan los suyos).
 *  - Sin argumentos: una copia de lo que ya se cargó (`loadBundles`, `preloadAllLocales`); NO descubre nada.
 */
export function collectResources(modules?: LocaleModules): Resource {
  if (modules) return group(modules);
  return Object.fromEntries(Object.entries(loaded).map(([lng, byNs]) => [lng, { ...byNs }]));
}

/** Idiomas con ficheros, el de referencia primero. */
export function listLocales(resources: Resource): string[] {
  return Object.keys(resources)
    .filter((lng) => lng !== PSEUDO_LOCALE)
    .sort((a, b) => (a === REFERENCE_LOCALE ? -1 : b === REFERENCE_LOCALE ? 1 : a.localeCompare(b)));
}

/** Idiomas que tienen ficheros (el de referencia primero, sin `xx`), cargados o no. */
export function availableLocales(): string[] {
  return listLocales(Object.fromEntries(FILES.map((file) => [file.lng, {}])));
}

/** Todos los espacios que existen en algún idioma. */
export function availableNamespaces(): string[] {
  return [...new Set(FILES.map((file) => file.ns))].sort();
}

function hasFile(lng: string, ns: string): boolean {
  if (lng === PSEUDO_LOCALE) return hasFile(REFERENCE_LOCALE, ns);
  return FILES.some((file) => file.lng === lng && file.ns === ns);
}

/** ¿Está ya en memoria el espacio de ese idioma? */
export function isLoaded(lng: string, ns: string): boolean {
  return loaded[lng]?.[ns] !== undefined;
}

/**
 * Pide los ficheros de un idioma y espacios. Los espacios sin fichero en ese idioma se ignoran (un idioma
 * puede ir incompleto). Idempotente y concurrente: cada fichero se descarga una vez. El idioma `xx`
 * (pseudolocalización) se deriva del de referencia.
 */
export async function loadBundles(lng: string, namespaces: readonly string[]): Promise<void> {
  if (lng === PSEUDO_LOCALE) {
    await loadBundles(REFERENCE_LOCALE, namespaces);
    for (const ns of namespaces) {
      const source = loaded[REFERENCE_LOCALE]?.[ns];
      if (source !== undefined && !isLoaded(PSEUDO_LOCALE, ns)) {
        (loaded[PSEUDO_LOCALE] ??= {})[ns] = pseudoTree(source) as Record<string, never>;
      }
    }
    return;
  }
  await Promise.all(
    namespaces.map((ns) => {
      const file = FILES.find((candidate) => candidate.lng === lng && candidate.ns === ns);
      if (!file || isLoaded(lng, ns)) return undefined;
      const key = `${lng}/${ns}`;
      let pending = inFlight.get(key);
      if (!pending) {
        pending = file.load().then((content) => {
          if (typeof content === 'object' && content !== null) (loaded[lng] ??= {})[ns] = content as Record<string, never>;
        });
        inFlight.set(key, pending);
        // Si la descarga falla, se podrá reintentar.
        pending.catch(() => inFlight.delete(key));
      }
      return pending;
    }),
  );
}

/** Sólo para las pruebas: vacía lo cargado para volver a medir el arranque por demanda. */
export function resetLoadedLocales(): void {
  for (const lng of Object.keys(loaded)) delete loaded[lng];
  inFlight.clear();
}

/** Carga todos los idiomas y espacios (pruebas y herramientas; la aplicación no lo usa). */
export async function preloadAllLocales(): Promise<void> {
  await Promise.all(availableLocales().map((lng) => loadBundles(lng, availableNamespaces())));
}

/**
 * Idiomas cuyos ficheros hacen falta para que `lng` no enseñe claves: él mismo y, si no tiene paridad
 * garantizada (`COMPLETE_LOCALES`), su reserva (`ar` → `en`; el resto, el de referencia). `xx` se deriva del
 * de referencia.
 */
export function languagesNeeded(lng: string): string[] {
  if (lng === PSEUDO_LOCALE) return [REFERENCE_LOCALE, PSEUDO_LOCALE];
  return COMPLETE_LOCALES.includes(lng) ? [lng] : [reserveFor(lng), lng];
}

/**
 * ¿Están ya en memoria los espacios del marco del idioma (y los de su reserva)? `xx` no cuenta por sí mismo:
 * `createI18n` lo deriva de `es` al crear la instancia, así que le basta con tener `es`.
 */
export function isFrameLoaded(lng: string, namespaces: readonly string[] = FRAME_NAMESPACES): boolean {
  return languagesNeeded(lng)
    .filter((language) => language !== PSEUDO_LOCALE)
    .every((language) => namespaces.every((ns) => isLoaded(language, ns) || !hasFile(language, ns)));
}

const ACCENTS: Record<string, string> = {
  a: 'á', b: 'ƀ', c: 'ç', d: 'ḓ', e: 'é', f: 'ƒ', g: 'ĝ', h: 'ḩ', i: 'í', j: 'ĵ', k: 'ķ', l: 'ḷ', m: 'ḿ',
  n: 'ñ', o: 'ó', p: 'ṕ', q: 'ɋ', r: 'ŕ', s: 'ś', t: 'ţ', u: 'ú', v: 'ṽ', w: 'ŵ', x: 'ẋ', y: 'ý', z: 'ź',
  A: 'Á', B: 'Ɓ', C: 'Ç', D: 'Ḓ', E: 'É', F: 'Ƒ', G: 'Ĝ', H: 'Ḩ', I: 'Í', J: 'Ĵ', K: 'Ķ', L: 'Ḷ', M: 'Ḿ',
  N: 'Ñ', O: 'Ó', P: 'Ṕ', Q: 'Ɋ', R: 'Ŕ', S: 'Ś', T: 'Ţ', U: 'Ú', V: 'Ṽ', W: 'Ŵ', X: 'Ẋ', Y: 'Ý', Z: 'Ź',
};

/**
 * Pseudolocaliza un texto: acentúa las letras y lo encierra en corchetes, conservando intactos los
 * marcadores `{{x}}` y `$t(…)` de i18next. Un texto que sale sin corchetes en pantalla está escrito a mano.
 */
export function pseudoLocalize(text: string): string {
  let out = '';
  let rest = text;
  const keep = /\{\{[^}]*\}\}|\$t\([^)]*\)/;
  for (;;) {
    const match = keep.exec(rest);
    const chunk = match ? rest.slice(0, match.index) : rest;
    out += Array.from(chunk, (ch) => ACCENTS[ch] ?? ch).join('');
    if (!match) break;
    out += match[0];
    rest = rest.slice(match.index + match[0].length);
  }
  return `[${out}]`;
}

function pseudoTree(node: unknown): unknown {
  if (typeof node === 'string') return pseudoLocalize(node);
  if (Array.isArray(node)) return node.map(pseudoTree);
  if (typeof node === 'object' && node !== null) {
    return Object.fromEntries(Object.entries(node).map(([key, value]) => [key, pseudoTree(value)]));
  }
  return node;
}

/** Recursos del idioma `xx` derivados de los del idioma de referencia. */
export function buildPseudoResources(resources: Resource): Resource[string] {
  return pseudoTree(resources[REFERENCE_LOCALE] ?? {}) as Resource[string];
}

const RTL_LANGUAGES = new Set(['ar', 'he', 'fa', 'ur', 'ps', 'sd', 'ug', 'yi', 'dv']);

/** Idioma base en minúsculas: `ar-EG` → `ar`. */
export function baseLanguage(lng: string): string {
  return lng.split(/[-_]/)[0]?.toLowerCase() ?? '';
}

/** Dirección de escritura de un idioma (D-R16): árabe, hebreo, persa y urdu van de derecha a izquierda. */
export function directionFor(lng: string): 'ltr' | 'rtl' {
  return RTL_LANGUAGES.has(baseLanguage(lng)) ? 'rtl' : 'ltr';
}

/**
 * Elige un idioma admitido a partir de uno pedido (`es-ES` → `es`) o, si no hay, del reserva.
 * Los idiomas admitidos se comparan en minúsculas.
 */
export function pickLocale(requested: string | null | undefined, supported: readonly string[]): string {
  if (requested) {
    const lower = requested.toLowerCase().replace('_', '-');
    const exact = supported.find((s) => s.toLowerCase() === lower);
    if (exact) return exact;
    const base = baseLanguage(lower);
    const byBase = supported.find((s) => s.toLowerCase() === base);
    if (byBase) return byBase;
  }
  return supported.includes(FALLBACK_LOCALE) ? FALLBACK_LOCALE : (supported[0] ?? FALLBACK_LOCALE);
}
