// Selección que publica la vista activa y que la paleta usa para las órdenes de la selección.
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Row } from '../contracts';
import { clearSelection, publishSelection, useSelection } from './selection';

const rows = (...ids: string[]): Row[] => ids.map((id) => ({ id }) as Row);

describe('selección publicada por la vista', () => {
  it('es de una entidad; una lista vacía o clearSelection la borran', () => {
    const hook = renderHook(({ entity }: { entity: string }) => useSelection(entity), { initialProps: { entity: 'a' } });
    expect(hook.result.current).toEqual([]);

    act(() => publishSelection('a', rows('1', '2')));
    expect(hook.result.current.map((row) => row.id)).toEqual(['1', '2']);

    hook.rerender({ entity: 'b' });
    expect(hook.result.current).toEqual([]);
    hook.rerender({ entity: 'a' });

    // Una lista vacía de otra entidad no borra la selección de ésta.
    act(() => publishSelection('b', []));
    expect(hook.result.current).toHaveLength(2);

    act(() => publishSelection('a', []));
    expect(hook.result.current).toEqual([]);

    act(() => publishSelection('a', rows('3')));
    act(() => clearSelection('otra'));
    expect(hook.result.current).toHaveLength(1);
    act(() => clearSelection());
    expect(hook.result.current).toEqual([]);
  });

  it('sin entidad no devuelve nada', () => {
    const hook = renderHook(() => useSelection(undefined));
    act(() => publishSelection('a', rows('1')));
    expect(hook.result.current).toEqual([]);
    act(() => clearSelection());
  });
});
