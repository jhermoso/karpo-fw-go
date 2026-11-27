// Agrupación de etiquetas de claves ajenas por fotograma.
import { describe, expect, it } from 'vitest';
import type { EntityApi, EntityConfig, Row } from '../contracts';
import { normalizeEntityConfig } from '../config';
import { ApiError, parseProblem } from './errors';
import { LABEL_BULK_THRESHOLD, createLabelBatcher } from './hooks';

const config: EntityConfig = normalizeEntityConfig({
  id: 'facility',
  apiEndpoint: '/facilities',
  displayField: 'name',
  grid: { columns: [{ field: 'name' }] },
});

function fakeApi(
  rows: Record<string, Record<string, unknown>>,
  options: { listFails?: boolean; error?: ApiError } = {},
) {
  const log = { gets: [] as string[], lists: 0 };
  const api = {
    async get(_cfg: EntityConfig, id: string): Promise<Row> {
      log.gets.push(id);
      if (options.error) throw options.error;
      const row = rows[id];
      if (!row) throw parseProblem(404, { title: 'Not Found' });
      return { id, ...row } as Row;
    },
    async list() {
      log.lists += 1;
      if (options.listFails) throw new Error('boom');
      const items = Object.entries(rows).map(([id, row]) => ({ id, ...row }) as Row);
      return { items, total: items.length, page: 1, pageSize: 1000 };
    },
  } as unknown as EntityApi;
  return { api, log };
}

function manualFrame() {
  const queue: (() => void)[] = [];
  return { frame: (cb: () => void) => queue.push(cb), flush: () => queue.splice(0).forEach((cb) => cb()), queue };
}

const rows = Object.fromEntries(Array.from({ length: 8 }, (_v, i) => [`f${i}`, { name: `Instalación ${i}` }]));

describe('createLabelBatcher', () => {
  it('agrupa lo del mismo fotograma: un identificador repetido se pide una vez', async () => {
    const { api, log } = fakeApi(rows);
    const { frame, flush, queue } = manualFrame();
    const batcher = createLabelBatcher(api, () => config, frame);
    const a = batcher.enqueue('facility', 'f1');
    const b = batcher.enqueue('facility', 'f1');
    const c = batcher.enqueue('facility', 'f2');
    expect(queue).toHaveLength(1); // un solo fotograma programado
    expect(log.gets).toEqual([]); // nada se pide antes del fotograma
    flush();
    expect(await Promise.all([a, b, c])).toEqual(['Instalación 1', 'Instalación 1', 'Instalación 2']);
    expect(log.gets.sort()).toEqual(['f1', 'f2']);
    expect(log.lists).toBe(0);
    // el siguiente fotograma programa de nuevo
    const d = batcher.enqueue('facility', 'f3');
    expect(queue).toHaveLength(1);
    flush();
    expect(await d).toBe('Instalación 3');
  });

  it(`con ${LABEL_BULK_THRESHOLD} o más de una entidad lee una página y pide uno a uno sólo lo que falte`, async () => {
    const { api, log } = fakeApi(rows);
    const { frame, flush } = manualFrame();
    const batcher = createLabelBatcher(api, () => config, frame);
    const ids = ['f0', 'f1', 'f2', 'f3', 'f4', 'ausente'];
    const labels = ids.map((id) => batcher.enqueue('facility', id));
    flush();
    expect(await Promise.all(labels)).toEqual([
      'Instalación 0',
      'Instalación 1',
      'Instalación 2',
      'Instalación 3',
      'Instalación 4',
      undefined, // 404 -> sin etiqueta, no error
    ]);
    expect(log.lists).toBe(1);
    expect(log.gets).toEqual(['ausente']);
  });

  it('si la página falla, se intenta uno a uno', async () => {
    const { api, log } = fakeApi(rows, { listFails: true });
    const { frame, flush } = manualFrame();
    const batcher = createLabelBatcher(api, () => config, frame);
    const ids = ['f0', 'f1', 'f2', 'f3', 'f4', 'f5', 'f6', 'f7'];
    const labels = ids.map((id) => batcher.enqueue('facility', id));
    flush();
    expect((await Promise.all(labels))[7]).toBe('Instalación 7');
    expect(log.lists).toBe(1);
    expect(log.gets.sort()).toEqual(ids);
  });

  it('entidad desconocida -> sin etiqueta; otros errores rechazan; el campo de visualización cae a `name` y a `id`', async () => {
    const { api } = fakeApi({ a: { other: 1 }, b: { name: 'Be' } });
    const { frame, flush } = manualFrame();
    const batcher = createLabelBatcher(api, (name) => (name === 'facility' ? config : undefined), frame);
    const unknown = batcher.enqueue('persona', 'x');
    const noName = batcher.enqueue('facility', 'a');
    const byName = batcher.enqueue('facility', 'b');
    flush();
    expect(await unknown).toBeUndefined();
    expect(await noName).toBe('a');
    expect(await byName).toBe('Be');

    const failing = fakeApi({}, { error: parseProblem(500, { title: 'Boom' }) });
    const f = manualFrame();
    const failBatcher = createLabelBatcher(failing.api, () => config, f.frame);
    const rejected = failBatcher.enqueue('facility', 'x');
    f.flush();
    await expect(rejected).rejects.toBeInstanceOf(ApiError);
  });

  it('usa requestAnimationFrame o un temporizador cuando no se inyecta el fotograma', async () => {
    const { api } = fakeApi(rows);
    const batcher = createLabelBatcher(api, () => config);
    expect(await batcher.enqueue('facility', 'f1')).toBe('Instalación 1');
    const original = globalThis.requestAnimationFrame;
    globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) => setTimeout(() => cb(0), 0)) as unknown as typeof requestAnimationFrame;
    try {
      const withRaf = createLabelBatcher(api, () => config);
      expect(await withRaf.enqueue('facility', 'f2')).toBe('Instalación 2');
    } finally {
      globalThis.requestAnimationFrame = original;
    }
  });
});
