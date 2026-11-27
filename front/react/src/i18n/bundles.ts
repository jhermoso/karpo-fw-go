// Carga por demanda de idiomas y espacios de nombres (D-R37, M-11).
//
// i18next nace con los espacios del marco del idioma activo (`createI18n`); el resto llega con su vista:
// los cargadores diferidos de `shell/lazy.ts` y `shell/views.tsx` llaman a `ensureNamespaces([...])` antes
// de montarla, de modo que ningún componente pinta una clave cruda. Y al cambiar de idioma
// (`loadLanguage`) se piden, para el idioma nuevo, los mismos espacios que ya tenía la instancia.
//
// Sólo se gestionan las instancias que `createI18n` creó con los recursos de `src/locales` (las que reciben
// recursos propios, en pruebas o integraciones, no se tocan). Se guardan con `WeakRef`: una instancia que ya
// nadie usa no se retiene.
import type { i18n as I18nInstance } from 'i18next';
import { FRAME_NAMESPACES, collectResources, languagesNeeded, loadBundles } from './locales';

const managed = new Set<WeakRef<I18nInstance>>();
const managedOnce = new WeakSet<I18nInstance>();
/** Espacios que alguna vista ya pidió en esta página: una instancia nueva (otra montura) arranca con ellos. */
const requested = new Set<string>();

/** Sólo para las pruebas: olvida los espacios que ya pidieron las vistas. */
export function forgetRequestedNamespaces(): void {
  requested.clear();
}

/** Espacios con los que debe nacer una instancia: los del marco y los que ya pidió alguna vista. */
export function bootNamespaces(): string[] {
  return [...new Set([...FRAME_NAMESPACES, ...requested])];
}

/** Anota una instancia creada con los recursos de `src/locales` para que reciba los espacios que se pidan. */
export function manageInstance(instance: I18nInstance): void {
  if (managedOnce.has(instance)) return;
  managedOnce.add(instance);
  managed.add(new WeakRef(instance));
}

/** ¿La instancia la creó `createI18n` con los recursos de `src/locales`? */
export function isManaged(instance: I18nInstance): boolean {
  return managedOnce.has(instance);
}

function liveInstances(): I18nInstance[] {
  const live: I18nInstance[] = [];
  for (const ref of [...managed]) {
    const instance = ref.deref();
    if (instance) live.push(instance);
    else managed.delete(ref);
  }
  return live;
}

/** Copia a la instancia lo cargado en la caché que a ella le falta (sin pisar lo que ya tenga). */
function fill(instance: I18nInstance, languages: readonly string[], namespaces: readonly string[]): void {
  const resources = collectResources();
  for (const lng of languages) {
    for (const ns of namespaces) {
      const content = resources[lng]?.[ns];
      if (content !== undefined && !instance.hasResourceBundle(lng, ns)) {
        instance.addResourceBundle(lng, ns, structuredClone(content), true, false);
      }
    }
  }
}

/** Espacios que la instancia ya conoce (los de su configuración). */
function knownNamespaces(instance: I18nInstance): string[] {
  const ns = instance.options.ns;
  return typeof ns === 'string' ? [ns] : [...(ns ?? [])];
}

/**
 * Asegura que los espacios están cargados y en las instancias gestionadas, para su idioma activo (y la
 * reserva que éste necesite). Se llama desde los cargadores diferidos ANTES de montar una vista. Nunca
 * rechaza: si un fichero no se descarga, la vista usa los textos por omisión de cada componente.
 */
export async function ensureNamespaces(namespaces: readonly string[]): Promise<void> {
  for (const ns of namespaces) requested.add(ns);
  const instances = liveInstances();
  const languages = new Set(instances.flatMap((instance) => languagesNeeded(instance.language)));
  try {
    await Promise.all([...languages].map((lng) => loadBundles(lng, namespaces)));
  } catch (cause) {
    console.warn('[karpo] no se pudieron cargar los textos de la vista', cause);
  }
  for (const instance of instances) fill(instance, languagesNeeded(instance.language), namespaces);
}

/**
 * Prepara un idioma para una instancia gestionada ANTES de cambiarla a él: pide los espacios que la instancia
 * ya conoce (los del marco y los de las vistas ya visitadas) en el idioma nuevo.
 */
export async function loadLanguage(instance: I18nInstance, lng: string): Promise<void> {
  if (!isManaged(instance)) return;
  const namespaces = [...new Set([...bootNamespaces(), ...knownNamespaces(instance)])];
  const languages = languagesNeeded(lng);
  try {
    await Promise.all(languages.map((language) => loadBundles(language, namespaces)));
  } catch (cause) {
    console.warn('[karpo] no se pudieron cargar los textos del idioma', cause);
  }
  fill(instance, languages, namespaces);
}

/**
 * Carga, antes de crear la instancia, el idioma inicial con los espacios del marco (y la reserva que
 * necesite). `I18nProvider` lo espera para no pintar nunca una clave cruda.
 */
export async function prepareLocale(lng: string, namespaces: readonly string[] = bootNamespaces()): Promise<void> {
  await Promise.all(languagesNeeded(lng).map((language) => loadBundles(language, namespaces)));
}
