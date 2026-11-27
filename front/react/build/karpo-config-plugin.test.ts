import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import type { AddressInfo } from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, createServer, type ViteDevServer } from 'vite';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { karpoConfigPlugin, relativeConfigPath } from './karpo-config-plugin.ts';

const here = path.dirname(fileURLToPath(import.meta.url));
const sourceDir = path.resolve(here, '../../angular/config/entities');
const FACILITY = 'facilities/facility.config.json';

/** Forma mínima de lo que devuelve `build()` (el empaquetador es un detalle de Vite). */
interface BuildOutput {
  output: { fileName: string; type: string; source?: string | Uint8Array }[];
}

describe('relativeConfigPath', () => {
  const mount = '/config/go/';

  it('acepta un JSON bajo el prefijo y descarta la consulta', () => {
    expect(relativeConfigPath(`${mount}${FACILITY}?v=1`, mount)).toBe(FACILITY);
  });

  it.each([
    ['/otra/ruta.json'],
    [`${mount}`],
    [`${mount}../package.json`],
    [`${mount}facilities/../../package.json`],
    [`${mount}%2e%2e/package.json`],
    [`${mount}facilities\\facility.config.json`],
    [`${mount}facilities/readme.md`],
    [`${mount}facilities//facility.config.json`],
    [`${mount}%E0%A4%A.json`],
  ])('descarta %s', (url) => {
    expect(relativeConfigPath(url, mount)).toBeNull();
  });
});

describe('karpoConfigPlugin en desarrollo', () => {
  let vite: ViteDevServer;
  let server: http.Server;
  let origin: string;

  const rawStatus = (rawPath: string) =>
    new Promise<number>((resolve, reject) => {
      http
        .get({ host: '127.0.0.1', port: new URL(origin).port, path: rawPath }, (res) => {
          res.resume();
          resolve(res.statusCode ?? 0);
        })
        .on('error', reject);
    });

  beforeAll(async () => {
    vite = await createServer({
      configFile: false,
      root: here,
      logLevel: 'silent',
      appType: 'custom',
      server: { middlewareMode: true },
      plugins: [karpoConfigPlugin({ sourceDir })],
    });
    server = http.createServer((req, res) => {
      vite.middlewares(req, res, () => {
        res.statusCode = 418;
        res.end('delegado');
      });
    });
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  });

  afterAll(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    await vite.close();
  });

  it('sirve /config/go/facilities/facility.config.json idéntico al de front/angular', async () => {
    const res = await fetch(`${origin}/config/go/${FACILITY}`);
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toContain('application/json');
    const served = Buffer.from(await res.arrayBuffer());
    const original = await readFile(path.join(sourceDir, FACILITY));
    expect(served.equals(original)).toBe(true);
    expect(JSON.parse(served.toString('utf8')).id).toBe('facility');
  });

  it('sirve los 8 JSON de Go', async () => {
    const files = [
      'assets/asset',
      'facilities/facility',
      'geography/country',
      'geography/holiday',
      'hr/work-center',
      'products/product',
      'purchases/supplier',
      'shipments/carrier',
    ];
    for (const file of files) {
      const res = await fetch(`${origin}/config/go/${file}.config.json`);
      expect(res.status, file).toBe(200);
    }
  });

  it('responde 404 a un fichero que no existe y a un recorrido de rutas', async () => {
    expect((await fetch(`${origin}/config/go/facilities/nada.config.json`)).status).toBe(404);
    // fetch normaliza `%2e%2e`; se envía la ruta tal cual con http para que llegue al plugin.
    expect(await rawStatus('/config/go/%2e%2e/%2e%2e/package.json')).toBe(404);
    expect(await rawStatus('/config/go/facilities/..%2f..%2fpackage.json')).toBe(404);
    expect((await fetch(`${origin}/config/go/`)).status).toBe(404);
  });

  it('sólo permite leer', async () => {
    const res = await fetch(`${origin}/config/go/${FACILITY}`, { method: 'PUT', body: '{}' });
    expect(res.status).toBe(405);
  });

  it('delega lo que no es suyo', async () => {
    const res = await fetch(`${origin}/api/lo-que-sea`);
    expect(res.status).toBe(418);
  });
});

describe('karpoConfigPlugin al construir', () => {
  let root: string;

  beforeAll(async () => {
    root = await mkdtemp(path.join(os.tmpdir(), 'karpo-plugin-'));
    await writeFile(path.join(root, 'index.html'), '<!doctype html><title>x</title><p>x</p>');
  });

  afterAll(async () => {
    await rm(root, { recursive: true, force: true });
  });

  it('copia la configuración a <outDir>/config/go sin escribir en disco', async () => {
    const result = (await build({
      configFile: false,
      root,
      logLevel: 'silent',
      build: { write: false },
      plugins: [karpoConfigPlugin({ sourceDir })],
    })) as unknown as BuildOutput | BuildOutput[];
    const output = Array.isArray(result) ? result.flatMap((r) => r.output) : result.output;
    const asset = output.find((item) => item.fileName === `config/go/${FACILITY}`);
    expect(asset?.type).toBe('asset');
    const source = asset?.source ?? '';
    const original = await readFile(path.join(sourceDir, FACILITY));
    expect(Buffer.from(source).equals(original)).toBe(true);
    expect(output.filter((item) => item.fileName.startsWith('config/go/'))).toHaveLength(8);
  });

  it('copia también las carpetas extra bajo su propio prefijo', async () => {
    const extra = path.resolve(here, '../demo/config');
    const result = (await build({
      configFile: false,
      root,
      logLevel: 'silent',
      build: { write: false },
      plugins: [karpoConfigPlugin({ sourceDir, extraEmit: [{ sourceDir: extra, mountPath: '/config/' }] })],
    })) as unknown as BuildOutput | BuildOutput[];
    const output = Array.isArray(result) ? result.flatMap((r) => r.output) : result.output;
    expect(output.some((item) => item.fileName === 'config/manifest.json')).toBe(true);
    expect(output.filter((item) => item.fileName.startsWith('config/go/'))).toHaveLength(8);
  });
});
