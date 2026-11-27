// La configuración de la demo (demo/config) leída del disco, para pruebas: `go/…` sale de las 8
// configuraciones de Go (front/angular/config/entities, sólo lectura) y el resto de demo/config.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { LoadReport } from '../manifest';
import { loadManifestWithReport } from '../manifest';

export const DEMO_CONFIG_DIR = resolve(import.meta.dirname, '../../../demo/config');
export const GO_CONFIG_DIR = resolve(import.meta.dirname, '../../../../angular/config/entities');
export const DEMO_CONFIG_BASE = 'http://localhost/config/';

/** Ruta de un `…/config/<ruta>` en disco. */
export function diskPathOf(relative: string): string {
  return relative.startsWith('go/') ? resolve(GO_CONFIG_DIR, relative.slice(3)) : resolve(DEMO_CONFIG_DIR, relative);
}

/** `fetch` que sirve `http://localhost/config/…` desde el disco. */
export const demoDiskFetch = (async (input: RequestInfo | URL) => {
  const url = String(input);
  if (!url.startsWith(DEMO_CONFIG_BASE)) return new Response('no encontrado', { status: 404 });
  try {
    return new Response(readFileSync(diskPathOf(url.slice(DEMO_CONFIG_BASE.length)), 'utf8'), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  } catch {
    return new Response('no encontrado', { status: 404 });
  }
}) as typeof fetch;

let report: Promise<LoadReport> | undefined;

/** Manifiesto de la demo normalizado, con su informe de avisos (una sola lectura por proceso). */
export function loadDemoReport(): Promise<LoadReport> {
  report ??= loadManifestWithReport(`${DEMO_CONFIG_BASE}manifest.json`, { fetch: demoDiskFetch, logWarnings: false });
  return report;
}

/** Los `i18n/<idioma>.json` del manifiesto de la demo, tal cual están en el disco. */
export function readDemoJson<T = Record<string, unknown>>(relative: string): T {
  return JSON.parse(readFileSync(diskPathOf(relative), 'utf8')) as T;
}
