// Los 8 JSON reales de Go (front/angular/config/entities), leídos del disco. Sólo para pruebas.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const ROOT = `${resolve(import.meta.dirname, '../../../../angular/config/entities')}/`;

export const REAL_PATHS = [
  'assets/asset.config.json',
  'facilities/facility.config.json',
  'geography/country.config.json',
  'geography/holiday.config.json',
  'hr/work-center.config.json',
  'products/product.config.json',
  'purchases/supplier.config.json',
  'shipments/carrier.config.json',
] as const;

export function readReal(path: (typeof REAL_PATHS)[number]): Record<string, unknown> {
  return JSON.parse(readFileSync(`${ROOT}${path}`, 'utf8')) as Record<string, unknown>;
}

export function readRealById(id: string): Record<string, unknown> {
  const path = REAL_PATHS.find((p) => p.endsWith(`/${id}.config.json`));
  if (!path) throw new Error(`No hay configuración real para ${id}`);
  return readReal(path);
}

export function allReal(): { path: string; json: Record<string, unknown> }[] {
  return REAL_PATHS.map((path) => ({ path, json: readReal(path) }));
}

/** `fetch` en memoria: sirve objetos JSON por URL exacta y 404 para el resto. Sólo para pruebas. */
export function memoryFetch(files: Record<string, unknown>, calls: string[] = []): typeof fetch {
  return (async (input: RequestInfo | URL) => {
    const url = String(input);
    calls.push(url);
    if (url in files) {
      return new Response(JSON.stringify(files[url]), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    }
    return new Response('no encontrado', { status: 404 });
  }) as typeof fetch;
}

/** `fetch` que sirve los 8 JSON reales bajo `baseUrl` (con barra final). */
export function realFetch(baseUrl: string): typeof fetch {
  const files: Record<string, unknown> = {};
  for (const path of REAL_PATHS) files[`${baseUrl}${path}`] = readReal(path);
  return memoryFetch(files);
}
