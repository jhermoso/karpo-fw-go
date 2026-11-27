// Para los procesos que dejó `npm run karpo:local` (karpo y vite), registrados en
// .karpo-local/pids.json. Antes de matar un pid comprueba que sigue siendo el mismo programa: un
// pid reutilizado por otro proceso (tras un reinicio, por ejemplo) no se toca.
import { spawnSync } from 'node:child_process';
import { existsSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { killTree, readPids } from './lib/procesos.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const PID_FILE = path.resolve(here, '..', '.karpo-local', 'pids.json');

/** Nombre del programa de un pid, o `null` si el proceso ya no existe. */
export function imageName(pid, platform = process.platform) {
  if (platform === 'win32') {
    const result = spawnSync('tasklist', ['/FI', `PID eq ${pid}`, '/FO', 'CSV', '/NH'], { encoding: 'utf8', windowsHide: true });
    const first = (result.stdout ?? '').trim().split(/\r?\n/)[0] ?? '';
    const match = /^"([^"]+)"/.exec(first);
    return match ? match[1] : null;
  }
  const result = spawnSync('ps', ['-p', String(pid), '-o', 'comm='], { encoding: 'utf8' });
  const name = (result.stdout ?? '').trim();
  return name === '' ? null : path.basename(name);
}

/** ¿Es el proceso registrado? karpo es el binario compilado; vite corre dentro de node. */
export function matchesEntry(entry, image) {
  if (!image) return false;
  const lower = image.toLowerCase();
  if (entry.name === 'karpo') return lower.startsWith('karpo');
  if (entry.name === 'vite') return lower.startsWith('node');
  return false;
}

export function stopRecorded({ file = PID_FILE, nameOf = imageName, kill = killTree, log = console.log } = {}) {
  const entries = readPids(file);
  let stopped = 0;
  for (const entry of entries) {
    const image = nameOf(entry.pid);
    if (image === null) {
      log(`pid ${entry.pid} (${entry.name}): ya no existe`);
    } else if (!matchesEntry(entry, image)) {
      log(`pid ${entry.pid} (${entry.name}): ahora es «${image}», no se toca`);
    } else if (kill(entry.pid)) {
      stopped += 1;
      log(`pid ${entry.pid} (${entry.name}): parado`);
    } else {
      log(`pid ${entry.pid} (${entry.name}): no se pudo parar`);
    }
  }
  if (existsSync(file)) writeFileSync(file, '[]\n');
  if (entries.length === 0) log('No hay procesos registrados.');
  return stopped;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  stopRecorded();
}
