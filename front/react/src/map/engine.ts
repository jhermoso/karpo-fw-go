// Carga diferida de MapLibre, de su hoja de estilos y del protocolo `pmtiles://` (PLAN §4.12): el motor
// del mapa pesa mucho y sólo se descarga al abrir una vista de mapa. El protocolo lee del origen
// configurado. La hoja (`maplibre.css`, de R0) no va con el resto de estilos del paquete (M-16): en la
// demo se descarga aquí, junto a `maplibre-gl`; quien consume el paquete la importa como
// `@karpo/react/map.css`.
import type * as MapLibre from 'maplibre-gl';

export interface MapEngine {
  maplibre: typeof MapLibre;
}

let loading: Promise<MapEngine> | undefined;
let workerUrlOverride: string | undefined;

/**
 * Fija la dirección del worker de MapLibre (`maplibre-gl-worker.mjs`, servido por el propio despliegue).
 * Sólo hace falta con un empaquetador que no sea Vite; hay que llamarla antes de abrir el primer mapa.
 */
export function setMapWorkerUrl(url: string | undefined): void {
  workerUrlOverride = url;
}

/**
 * Dirección del worker de MapLibre. Por defecto MapLibre lo busca junto a su propio fichero
 * (`import.meta.url`), y eso falla en cuanto un empaquetador lo mueve (Vite en desarrollo y al
 * construir). Con `?url` el empaquetador entrega la del propio despliegue; con otro empaquetador
 * (o sin él) la importación falla y se deja el comportamiento por defecto.
 */
async function bundledWorkerUrl(): Promise<string | undefined> {
  if (workerUrlOverride) return workerUrlOverride;
  try {
    const module = await import('maplibre-gl/dist/maplibre-gl-worker.mjs?url');
    return typeof module.default === 'string' && module.default !== '' ? module.default : undefined;
  } catch {
    return undefined;
  }
}

/** Carga MapLibre una sola vez, con su hoja de estilos, le da su worker propio y registra `pmtiles://` en él. */
export function loadMapEngine(): Promise<MapEngine> {
  loading ??= (async () => {
    const [maplibre, pmtiles, , workerUrl] = await Promise.all([
      import('maplibre-gl'),
      import('pmtiles'),
      // La biblioteca sustituye este import por un módulo vacío (vite.lib.config.ts): su hoja es
      // `@karpo/react/map.css` y no debe colarse en `dist/karpo.css` (M-16).
      import('./maplibre.css'),
      bundledWorkerUrl(),
    ]);
    if (workerUrl && typeof maplibre.setWorkerUrl === 'function') maplibre.setWorkerUrl(workerUrl);
    const protocol = new pmtiles.Protocol();
    try {
      maplibre.addProtocol('pmtiles', protocol.tile);
    } catch {
      // ya registrado (recarga en caliente): se reutiliza el anterior
    }
    return { maplibre };
  })().catch((error: unknown) => {
    loading = undefined;
    throw error;
  });
  return loading;
}

/** Olvida el motor cargado (pruebas). */
export function resetMapEngine(): void {
  loading = undefined;
}
