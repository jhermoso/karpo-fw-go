// M-15.2: driver.js se carga con import() al lanzar el recorrido, no al arrancar la aplicación. Se prueba que
// ningún fichero de ejecución lo importa de forma estática, que el recorrido se puede cerrar mientras carga,
// que un fallo de carga termina el recorrido (el foco vuelve a quien lo lanzó) y que preloadTour no abre nada.
import { waitFor } from '@testing-library/react';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { TourEnd, TourOptions } from './tour';

const SRC = path.resolve(__dirname, '..');

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) return sourceFiles(full);
    return /\.(ts|tsx)$/.test(entry) && !/\.test\.(ts|tsx)$/.test(entry) ? [full] : [];
  });
}

function options(onEnd: (end: TourEnd) => void): TourOptions {
  return {
    steps: [
      { title: 'Título', body: 'Texto', source: 'test' },
      { title: 'Segundo', body: 'Otro texto', source: 'test2' },
    ],
    labels: { next: 'Siguiente', prev: 'Anterior', done: 'Terminar', close: 'Cerrar', progress: '{{current}} de {{total}}' },
    direction: 'ltr',
    scrim: () => 'rgba(0, 0, 0, 0.4)',
    motion: false,
    onEnd,
  };
}

const popover = () => document.querySelector<HTMLElement>('.driver-popover');

afterEach(() => {
  document.querySelectorAll('.driver-popover, .driver-overlay, .driver-live-region').forEach((el) => el.remove());
  vi.resetModules();
  vi.doUnmock('driver.js');
});

describe('driver.js diferido', () => {
  it('ningún fichero de ejecución de src lo importa de forma estática (sólo import() y tipos)', () => {
    const offenders = sourceFiles(SRC).filter((file) => {
      const source = readFileSync(file, 'utf8');
      // `import { driver } from 'driver.js'` o `import 'driver.js'`, pero no `import type`.
      return /^import\s+(?!type\b)[^;]*from\s+['"]driver\.js['"]/m.test(source) || /^import\s+['"]driver\.js/m.test(source);
    });
    expect(offenders.map((file) => path.relative(SRC, file))).toEqual([]);
  });

  it('tour.ts lo pide con import() dentro de la función de carga', () => {
    const source = readFileSync(path.join(__dirname, 'tour.ts'), 'utf8');
    expect(source).toMatch(/import\('driver\.js'\)/);
    expect(source).not.toMatch(/^import\s+\{[^}]*\}\s+from\s+'driver\.js'/m);
  });

  it('el recorrido empieza en cuanto driver.js carga: activo al instante y con popover un momento después', async () => {
    const { startTour } = await import('./tour');
    const onEnd = vi.fn();
    const tour = startTour(options(onEnd));
    expect(tour.isActive()).toBe(true);
    await waitFor(() => expect(popover()).not.toBeNull());
    expect(document.querySelector('.driver-popover-title')?.textContent).toBe('Título');
    tour.destroy();
    expect(popover()).toBeNull();
    expect(onEnd).toHaveBeenCalledTimes(1);
    expect(onEnd).toHaveBeenCalledWith({ reason: 'api', completed: false });
    expect(tour.isActive()).toBe(false);
  });

  it('cerrar el recorrido mientras driver.js carga lo cancela: no se abre nada y onEnd llega una vez', async () => {
    const { startTour } = await import('./tour');
    const onEnd = vi.fn();
    const tour = startTour(options(onEnd));
    tour.destroy(); // antes de que se resuelva el import()
    expect(onEnd).toHaveBeenCalledTimes(1);
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(popover()).toBeNull();
    expect(onEnd).toHaveBeenCalledTimes(1);
  });

  it('si driver.js no se puede cargar, el recorrido termina con reason error (y se puede reintentar)', async () => {
    vi.resetModules();
    vi.doMock('driver.js', () => {
      throw new Error('sin red');
    });
    const { startTour } = await import('./tour');
    const onEnd = vi.fn();
    const tour = startTour(options(onEnd));
    await waitFor(() => expect(onEnd).toHaveBeenCalledTimes(1));
    expect(onEnd).toHaveBeenCalledWith({ reason: 'error', completed: false });
    expect(tour.isActive()).toBe(false);
    expect(popover()).toBeNull();
  });

  it('preloadTour sólo precarga: no abre ningún recorrido', async () => {
    const { preloadTour } = await import('./tour');
    preloadTour();
    preloadTour();
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(popover()).toBeNull();
  });
});
