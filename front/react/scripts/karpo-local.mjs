// Modo `local` de la demostración (npm run karpo:local): Karpo Go de verdad, sin datos falsos.
//
//   1. compila cmd/karpo (raíz del repositorio, sólo lectura) a una carpeta temporal
//   2. crea un SQLite temporal, un secreto JWT y una contraseña de arranque aleatorios
//   3. busca puertos libres (scripts/lib/procesos.mjs; nunca el 55432 ni una base viva)
//   4. `karpo migrate`, `karpo serve` y espera a /readyz
//   5. siembra los datos del dataset por HTTP (scripts/seed-karpo.mjs)
//   6. guarda las credenciales en .karpo-local/credenciales.json y los pids en .karpo-local/pids.json
//   7. arranca Vite en modo `local` (proxy de /api, /healthz y /readyz a Go: Go no tiene CORS)
//   8. Ctrl+C lo para todo y borra la carpeta temporal; `npm run karpo:stop` limpia huérfanos
//
// Opciones: --smoke    comprueba login y GET /api/v1/facilities (directo y por el proxy de Vite),
//                      para todo y sale (código 0 si va bien)
//           --keep     no borra la carpeta temporal al terminar
//           --no-vite  no arranca Vite (sólo Go sembrado)
import { spawnSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { closeSync, existsSync, mkdirSync, mkdtempSync, openSync, rmSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { freePort, installCleanup, killAllTracked, spawnTracked, waitForUrl } from './lib/procesos.mjs';
import { randomPassword, seed, summarize } from './seed-karpo.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
export const FRONT_DIR = path.resolve(here, '..');
export const REPO_ROOT = path.resolve(FRONT_DIR, '../..');
export const LOCAL_DIR = path.join(FRONT_DIR, '.karpo-local');
export const PID_FILE = path.join(LOCAL_DIR, 'pids.json');
export const CREDENTIALS_FILE = path.join(LOCAL_DIR, 'credenciales.json');
export const SEED_REPORT_FILE = path.join(LOCAL_DIR, 'siembra.json');
export const VITE_CONFIG_FILE = path.join(LOCAL_DIR, 'vite.config.mjs');

/**
 * Configuración de Vite para el modo local. Vite no admite `--mode local` (choca con los ficheros
 * .env.local), así que se arranca con otro nombre de modo y esta configuración, que parte de la
 * del proyecto y le añade el proxy de /api, /healthz y /readyz a KARPO_URL (Go no tiene CORS).
 */
export const VITE_CONFIG_SOURCE = `import baseConfig from '../vite.config.ts';

const PROXIED = ['/api', '/healthz', '/readyz'];

export default async (env) => {
  const base = typeof baseConfig === 'function' ? await baseConfig(env) : await baseConfig;
  const target = process.env.KARPO_URL;
  if (!target) throw new Error('Falta KARPO_URL (usa npm run karpo:local).');
  const proxy = Object.fromEntries(PROXIED.map((prefix) => [prefix, { target, changeOrigin: true }]));
  return { ...base, server: { ...base.server, proxy }, preview: { ...base.preview, proxy } };
};
`;

const BOOTSTRAP_USER = 'admin';

export const randomSecret = (bytes = 36) => randomBytes(bytes).toString('base64url');

function log(line) {
  console.log(`[karpo:local] ${line}`);
}

/** Compila cmd/karpo a `output`; lanza si falla. */
function build(output) {
  log('compilando cmd/karpo (la primera vez tarda unos 20 s)…');
  const result = spawnSync('go', ['build', '-o', output, './cmd/karpo'], {
    cwd: REPO_ROOT,
    // Sin red: si el caché de módulos de Go no tiene algo, falla en vez de descargarlo.
    env: { ...process.env, GOPROXY: 'off' },
    stdio: 'inherit',
    windowsHide: true,
  });
  if (result.error) throw new Error(`no se pudo ejecutar go: ${result.error.message}`);
  if (result.status !== 0) throw new Error(`go build terminó con código ${result.status}`);
}

async function main() {
  const args = new Set(process.argv.slice(2));
  const smoke = args.has('--smoke');
  const keep = args.has('--keep');
  const withVite = !args.has('--no-vite');

  installCleanup();
  const tmp = mkdtempSync(path.join(os.tmpdir(), 'karpo-local-'));
  const cleanTmp = () => {
    if (keep) return;
    try {
      rmSync(tmp, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
    } catch {
      // Un fichero aún abierto en Windows: se borrará al reiniciar la limpieza del sistema.
    }
  };
  process.on('exit', cleanTmp);
  mkdirSync(LOCAL_DIR, { recursive: true });
  // Un registro de procesos de una ejecución anterior que ya no vive no debe sobrevivir a ésta.
  writeFileSync(PID_FILE, '[]\n');

  const binary = path.join(tmp, process.platform === 'win32' ? 'karpo.exe' : 'karpo');
  build(binary);

  const goPort = await freePort({ preferred: 18741, attempts: 300 });
  const goUrl = `http://127.0.0.1:${goPort}`;
  const bootstrapPassword = randomPassword();
  const env = {
    KARPO_SQLITE: path.join(tmp, 'karpo.db').replace(/\\/g, '/'),
    KARPO_JWT_SECRET: randomSecret(),
    KARPO_ADDR: `127.0.0.1:${goPort}`,
    KARPO_BOOTSTRAP_ADMIN_USER: BOOTSTRAP_USER,
    KARPO_BOOTSTRAP_ADMIN_PASSWORD: bootstrapPassword,
    KARPO_EXPORTS_DIR: path.join(tmp, 'exports'),
  };

  log('aplicando el esquema (karpo migrate)…');
  const migrated = spawnSync(binary, ['migrate'], { cwd: tmp, env: { ...process.env, ...env }, stdio: ['ignore', 'ignore', 'inherit'], windowsHide: true });
  if (migrated.status !== 0) throw new Error(`karpo migrate terminó con código ${migrated.status}`);

  const logFile = path.join(LOCAL_DIR, 'karpo.log');
  const out = openSync(logFile, 'w');
  const go = spawnTracked(binary, ['serve'], { name: 'karpo', cwd: tmp, env, stdio: ['ignore', out, out], pidFile: PID_FILE });
  go.exited.then((code) => {
    closeSync(out);
    if (!shuttingDown) {
      console.error(`[karpo:local] karpo se detuvo (código ${code}); mira ${logFile}`);
      shutdown(1);
    }
  });
  let shuttingDown = false;
  function shutdown(code) {
    shuttingDown = true;
    killAllTracked();
    writeFileSync(PID_FILE, '[]\n');
    cleanTmp();
    process.exit(code);
  }
  for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => shutdown(0));

  await waitForUrl(`${goUrl}/readyz`, { timeoutMs: 60_000 });
  log(`karpo escucha en ${goUrl} (SQLite temporal en ${tmp})`);

  const result = await seed({ url: goUrl, user: BOOTSTRAP_USER, password: bootstrapPassword, log: (line) => log(line) });
  console.log(summarize(result));
  writeFileSync(CREDENTIALS_FILE, `${JSON.stringify({ ...result.credentials, note: 'Valores de prueba de una ejecución local; la base es temporal.' }, null, 2)}\n`);
  writeFileSync(SEED_REPORT_FILE, `${JSON.stringify({ steps: result.steps }, null, 2)}\n`);
  log(`credenciales en ${path.relative(process.cwd(), CREDENTIALS_FILE)} (usuario ${result.credentials.username})`);

  let viteUrl = '';
  if (withVite) {
    const vitePort = await freePort({ preferred: 5173, avoid: [goPort], attempts: 300 });
    viteUrl = `http://127.0.0.1:${vitePort}`;
    writeFileSync(VITE_CONFIG_FILE, VITE_CONFIG_SOURCE);
    const viteArgs = [path.join(FRONT_DIR, 'node_modules/vite/bin/vite.js'), '--config', VITE_CONFIG_FILE, '--mode', 'go', '--host', '127.0.0.1', '--port', String(vitePort), '--strictPort'];
    const vite = spawnTracked(process.execPath, viteArgs, {
      name: 'vite',
      cwd: FRONT_DIR,
      env: { KARPO_URL: goUrl },
      stdio: 'inherit',
      pidFile: PID_FILE,
    });
    vite.exited.then((code) => {
      if (!shuttingDown) {
        console.error(`[karpo:local] vite se detuvo (código ${code})`);
        shutdown(1);
      }
    });
    await waitForUrl(`${viteUrl}/config/manifest.json`, { timeoutMs: 60_000 });
    log(`front en ${viteUrl}  ·  API de Go en ${goUrl}`);
  }

  if (smoke) {
    const checks = await runSmoke({ goUrl, viteUrl, credentials: result.credentials });
    console.log(checks.lines.join('\n'));
    shutdown(checks.ok ? 0 : 1);
    return;
  }
  log('Ctrl+C para parar todo.');
  await new Promise(() => {});
}

/** Login y GET /api/v1/facilities contra Go y, si hay Vite, también por su proxy. */
export async function runSmoke({ goUrl, viteUrl, credentials }) {
  const lines = [];
  let ok = true;
  const check = (label, pass, detail = '') => {
    lines.push(`  ${pass ? 'OK ' : 'MAL'} ${label}${detail ? ` (${detail})` : ''}`);
    if (!pass) ok = false;
  };
  const login = await fetch(`${goUrl}/api/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: credentials.username, password: credentials.password }),
  });
  const session = login.status === 200 ? await login.json() : null;
  check('POST /api/auth/login', login.status === 200 && session && !session.mustChangePassword, `HTTP ${login.status}`);
  if (!session) return { ok: false, lines: ['Comprobación:', ...lines] };
  for (const [name, base] of [['Go', goUrl], ['Vite (proxy)', viteUrl]]) {
    if (!base) continue;
    const res = await fetch(`${base}/api/v1/facilities?pageSize=100`, { headers: { Authorization: `Bearer ${session.accessToken}` } });
    const body = res.status === 200 ? await res.json() : null;
    check(`GET /api/v1/facilities por ${name}`, res.status === 200 && body.total > 0, `HTTP ${res.status}${body ? `, ${body.total} instalaciones` : ''}`);
  }
  if (viteUrl) {
    const config = await fetch(`${viteUrl}/config/go/facilities/facility.config.json`);
    check('GET /config/go/facilities/facility.config.json por Vite', config.status === 200, `HTTP ${config.status}`);
    const manifest = await fetch(`${viteUrl}/config/manifest.json`);
    check('GET /config/manifest.json por Vite', manifest.status === 200, `HTTP ${manifest.status}`);
  }
  return { ok, lines: ['Comprobación:', ...lines] };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(`[karpo:local] ${error instanceof Error ? error.message : String(error)}`);
    killAllTracked();
    if (existsSync(PID_FILE)) writeFileSync(PID_FILE, '[]\n');
    process.exit(1);
  });
}
