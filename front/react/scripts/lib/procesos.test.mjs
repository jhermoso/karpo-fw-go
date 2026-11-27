import { spawn } from 'node:child_process';
import net from 'node:net';
import { describe, expect, it } from 'vitest';
import { canListen, freePort, killTree, FORBIDDEN_PORTS, waitForUrl } from './procesos.mjs';

function hold(port) {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once('error', reject);
    server.listen(port, '127.0.0.1', () => resolve(server));
  });
}

describe('freePort', () => {
  it('devuelve un puerto en el que se puede escuchar y que no es 55432', async () => {
    const port = await freePort();
    expect(port).toBeGreaterThan(0);
    expect(port).not.toBe(55432);
    expect(await canListen(port)).toBe(true);
  });

  it('descarta un puerto ocupado y toma el siguiente', async () => {
    const busy = await hold(0);
    const busyPort = busy.address().port;
    try {
      const port = await freePort({ preferred: busyPort });
      expect(port).not.toBe(busyPort);
      expect(port).toBeGreaterThan(busyPort);
    } finally {
      await new Promise((resolve) => busy.close(resolve));
    }
  });

  it('descarta los puertos cuyo listen falla (rangos reservados de Windows)', async () => {
    const rejected = [];
    const probe = async (port) => {
      if (port < 40_003) {
        rejected.push(port);
        return false;
      }
      return true;
    };
    const port = await freePort({ preferred: 40_000, probe });
    expect(port).toBe(40_003);
    expect(rejected).toEqual([40_000, 40_001, 40_002]);
  });

  it('nunca entrega 55432, aunque esté libre y se pida como preferido', async () => {
    expect(FORBIDDEN_PORTS.has(55432)).toBe(true);
    const probed = [];
    const probe = async (port) => {
      probed.push(port);
      return true;
    };
    const port = await freePort({ preferred: 55_432, probe });
    expect(port).toBe(55_433);
    expect(probed).not.toContain(55_432);
  });

  it('respeta la lista `avoid`', async () => {
    const port = await freePort({ preferred: 41_000, avoid: [41_000, 41_001], probe: async () => true });
    expect(port).toBe(41_002);
  });

  it('falla con un mensaje claro si no encuentra ninguno', async () => {
    await expect(freePort({ preferred: 42_000, attempts: 3, probe: async () => false })).rejects.toThrow(
      /sin puerto libre/,
    );
  });
});

describe('killTree', () => {
  it('en Windows usa taskkill /T /F sobre el pid', () => {
    const calls = [];
    const ok = killTree(1234, {
      platform: 'win32',
      run: (cmd, args) => {
        calls.push([cmd, args]);
        return { status: 0 };
      },
    });
    expect(ok).toBe(true);
    expect(calls).toEqual([['taskkill', ['/pid', '1234', '/T', '/F']]]);
  });

  it('en Windows devuelve false si taskkill falla', () => {
    expect(killTree(1234, { platform: 'win32', run: () => ({ status: 128 }) })).toBe(false);
  });

  it('fuera de Windows mata el grupo de procesos y, si no puede, el proceso', () => {
    const signals = [];
    const ok = killTree(77, {
      platform: 'linux',
      signal: (target, sig) => {
        signals.push([target, sig]);
        if (target < 0) throw new Error('ESRCH');
      },
    });
    expect(ok).toBe(true);
    expect(signals).toEqual([
      [-77, 'SIGKILL'],
      [77, 'SIGKILL'],
    ]);
  });

  it('rechaza pids que no son válidos', () => {
    expect(killTree(0)).toBe(false);
    expect(killTree(-5)).toBe(false);
    expect(killTree(Number.NaN)).toBe(false);
  });

  it('mata de verdad un proceso hijo y su árbol', async () => {
    const parent = spawn(
      process.execPath,
      [
        '-e',
        "const { spawn } = require('node:child_process');" +
          "spawn(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], { stdio: 'ignore' });" +
          'setInterval(() => {}, 1000);',
      ],
      { stdio: 'ignore', windowsHide: true, detached: process.platform !== 'win32' },
    );
    const exited = new Promise((resolve) => parent.once('exit', resolve));
    await new Promise((resolve) => setTimeout(resolve, 400));
    expect(killTree(parent.pid)).toBe(true);
    await Promise.race([
      exited,
      new Promise((_, reject) => setTimeout(() => reject(new Error('el proceso no murió')), 10_000)),
    ]);
  });
});

describe('waitForUrl', () => {
  it('espera hasta que la URL responde', async () => {
    let calls = 0;
    const fetchImpl = async () => {
      calls += 1;
      if (calls < 3) throw new Error('ECONNREFUSED');
      return { ok: true, status: 200 };
    };
    const res = await waitForUrl('http://127.0.0.1:1/readyz', { fetchImpl, intervalMs: 1, timeoutMs: 5_000 });
    expect(res.status).toBe(200);
    expect(calls).toBe(3);
  });

  it('falla con el último error al agotar el tiempo', async () => {
    const fetchImpl = async () => ({ ok: false, status: 503 });
    await expect(
      waitForUrl('http://127.0.0.1:1/readyz', { fetchImpl, intervalMs: 1, timeoutMs: 30 }),
    ).rejects.toThrow(/HTTP 503/);
  });
});
