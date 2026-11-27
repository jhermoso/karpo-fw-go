import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { matchesEntry, stopRecorded } from './karpo-stop.mjs';

const dirs = [];
function pidFile(entries) {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'karpo-stop-test-'));
  dirs.push(dir);
  const file = path.join(dir, 'pids.json');
  writeFileSync(file, JSON.stringify(entries));
  return file;
}
afterEach(() => {
  while (dirs.length) rmSync(dirs.pop(), { recursive: true, force: true });
});

describe('karpo:stop', () => {
  it('reconoce el programa de cada proceso registrado', () => {
    expect(matchesEntry({ name: 'karpo' }, 'karpo.exe')).toBe(true);
    expect(matchesEntry({ name: 'vite' }, 'node.exe')).toBe(true);
    expect(matchesEntry({ name: 'karpo' }, 'chrome.exe')).toBe(false);
    expect(matchesEntry({ name: 'vite' }, null)).toBe(false);
    expect(matchesEntry({ name: 'otro' }, 'node.exe')).toBe(false);
  });

  it('sólo mata los pids que siguen siendo el mismo programa y vacía el registro', () => {
    const file = pidFile([
      { pid: 101, name: 'karpo' },
      { pid: 102, name: 'vite' },
      { pid: 103, name: 'karpo' },
    ]);
    const killed = [];
    const names = new Map([[101, 'karpo.exe'], [102, 'explorer.exe'], [103, null]]);
    const stopped = stopRecorded({ file, nameOf: (pid) => names.get(pid), kill: (pid) => (killed.push(pid), true), log: () => {} });
    expect(stopped).toBe(1);
    expect(killed).toEqual([101]);
    expect(JSON.parse(readFileSync(file, 'utf8'))).toEqual([]);
  });

  it('sin registro no hace nada', () => {
    const lines = [];
    expect(stopRecorded({ file: path.join(os.tmpdir(), 'no-existe-karpo.json'), log: (l) => lines.push(l) })).toBe(0);
    expect(lines).toEqual(['No hay procesos registrados.']);
  });
});
