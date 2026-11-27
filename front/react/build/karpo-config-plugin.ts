// Plugin de Vite: publica, en sólo lectura, la configuración por entidad del runtime Angular
// (front/angular/config/entities) bajo /config/go/ en desarrollo y en `preview`, y la copia a
// <outDir>/config/go al construir. No modifica ni copia nada dentro de front/angular.
import { readdir, readFile, realpath, stat } from 'node:fs/promises';
import type { IncomingMessage, ServerResponse } from 'node:http';
import path from 'node:path';
import type { Plugin } from 'vite';

export interface KarpoConfigPluginOptions {
  /** Carpeta con los `*.config.json` (sólo se lee). */
  sourceDir: string;
  /** Prefijo público, con barra inicial y final. */
  mountPath?: string;
  /**
   * Carpetas con más JSON que sólo se copian al construir (en desarrollo, Vite ya las sirve desde su raíz).
   * Cada una se publica bajo su `mountPath` (p. ej. `demo/config` en `/config/`).
   */
  extraEmit?: ReadonlyArray<{ sourceDir: string; mountPath: string }>;
}

type Next = (error?: unknown) => void;

const JSON_TYPE = 'application/json; charset=utf-8';

/** Convierte la URL pedida en una ruta relativa segura dentro de `mount`, o `null` si no aplica. */
export function relativeConfigPath(rawUrl: string | undefined, mount: string): string | null {
  if (!rawUrl) return null;
  const pathname = rawUrl.split('?')[0]?.split('#')[0] ?? '';
  if (!pathname.startsWith(mount)) return null;
  let rel: string;
  try {
    rel = decodeURIComponent(pathname.slice(mount.length));
  } catch {
    return null;
  }
  if (rel === '' || rel.includes('\0') || rel.includes('\\')) return null;
  const parts = rel.split('/');
  if (parts.some((part) => part === '' || part === '.' || part === '..')) return null;
  if (!rel.endsWith('.json')) return null;
  return rel;
}

async function listJsonFiles(root: string, rel = ''): Promise<string[]> {
  const entries = await readdir(path.join(root, rel), { withFileTypes: true });
  const out: string[] = [];
  for (const entry of entries) {
    const child = rel === '' ? entry.name : `${rel}/${entry.name}`;
    if (entry.isDirectory()) out.push(...(await listJsonFiles(root, child)));
    else if (entry.isFile() && entry.name.endsWith('.json')) out.push(child);
  }
  return out.sort();
}

function respond(res: ServerResponse, status: number, body: string | Buffer, type = JSON_TYPE) {
  res.statusCode = status;
  res.setHeader('Content-Type', type);
  res.setHeader('Cache-Control', 'no-cache');
  res.end(body);
}

export function karpoConfigPlugin(options: KarpoConfigPluginOptions): Plugin {
  const mount = options.mountPath ?? '/config/go/';
  const sourceDir = path.resolve(options.sourceDir);

  async function handle(req: IncomingMessage, res: ServerResponse, next: Next) {
    const rel = relativeConfigPath(req.url, mount);
    if (rel === null) {
      // Una ruta bajo /config/go/ que no es un JSON válido no se delega: es un 404 nuestro.
      if (req.url && req.url.split('?')[0]?.startsWith(mount)) {
        respond(res, 404, '{"error":"not found"}');
        return;
      }
      next();
      return;
    }
    if (req.method !== 'GET' && req.method !== 'HEAD') {
      res.setHeader('Allow', 'GET, HEAD');
      respond(res, 405, '{"error":"method not allowed"}');
      return;
    }
    try {
      const file = path.join(sourceDir, ...rel.split('/'));
      // Defensa en profundidad frente a enlaces simbólicos que salgan de la carpeta.
      const real = await realpath(file);
      const base = await realpath(sourceDir);
      if (real !== base && !real.startsWith(base + path.sep)) {
        respond(res, 404, '{"error":"not found"}');
        return;
      }
      if (!(await stat(real)).isFile()) {
        respond(res, 404, '{"error":"not found"}');
        return;
      }
      const body = await readFile(real);
      respond(res, 200, req.method === 'HEAD' ? '' : body);
    } catch {
      respond(res, 404, '{"error":"not found"}');
    }
  }

  const middleware = (req: IncomingMessage, res: ServerResponse, next: Next) => {
    void handle(req, res, next);
  };

  return {
    name: 'karpo-config',
    configureServer(server) {
      server.middlewares.use(middleware);
    },
    configurePreviewServer(server) {
      server.middlewares.use(middleware);
    },
    async generateBundle() {
      const sources = [
        { sourceDir, mountPath: mount },
        ...(options.extraEmit ?? []).map((extra) => ({
          sourceDir: path.resolve(extra.sourceDir),
          mountPath: extra.mountPath,
        })),
      ];
      for (const source of sources) {
        const files = await listJsonFiles(source.sourceDir);
        for (const rel of files) {
          this.emitFile({
            type: 'asset',
            fileName: `${source.mountPath.replace(/^\/+/, '')}${rel}`,
            source: await readFile(path.join(source.sourceDir, ...rel.split('/'))),
          });
        }
      }
    },
  };
}
