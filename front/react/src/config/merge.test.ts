import { describe, expect, it } from 'vitest';
import { mergeOverlay } from './merge';

describe('mergeOverlay', () => {
  it('funde objetos en profundidad sin tocar el original', () => {
    const base = { a: 1, b: { c: 2, d: { e: 3 } }, list: [1, 2] };
    const overlay = { b: { d: { f: 4 } }, g: 5 };
    const merged = mergeOverlay(base, overlay) as Record<string, unknown>;
    expect(merged).toEqual({ a: 1, b: { c: 2, d: { e: 3, f: 4 } }, list: [1, 2], g: 5 });
    expect(base).toEqual({ a: 1, b: { c: 2, d: { e: 3 } }, list: [1, 2] });
    expect(overlay).toEqual({ b: { d: { f: 4 } }, g: 5 });
  });

  it('`null` borra la clave', () => {
    expect(mergeOverlay({ a: 1, b: 2 }, { a: null })).toEqual({ b: 2 });
  });

  it('los arrays con identidad (`id`, `field`, `action`) se funden por identidad y añaden lo nuevo', () => {
    const base = {
      sections: [
        { id: 'main', title: 'Uno', fields: [{ field: 'a', type: 'text' }, { field: 'b', type: 'text' }] },
        { id: 'other', title: 'Dos' },
      ],
      actions: [{ action: 'view', label: 'Ver' }],
    };
    const merged = mergeOverlay(base, {
      sections: [
        { id: 'main', fields: [{ field: 'b', lookupEntity: 'x' }, { field: 'c', type: 'date' }] },
        { id: 'extra', title: 'Tres' },
      ],
      actions: [{ action: 'view', label: 'Mirar' }, { action: 'edit' }],
    }) as typeof base;
    expect(merged.sections.map((s) => s.id)).toEqual(['main', 'other', 'extra']);
    expect(merged.sections[0]).toMatchObject({ title: 'Uno' });
    expect(merged.sections[0]!.fields).toEqual([
      { field: 'a', type: 'text' },
      { field: 'b', type: 'text', lookupEntity: 'x' },
      { field: 'c', type: 'date' },
    ]);
    expect(merged.actions).toEqual([{ action: 'view', label: 'Mirar' }, { action: 'edit' }]);
  });

  it('las opciones se funden por `value`: [{value, labelKey}] pone la clave sin sustituir el array (B-2 c)', () => {
    const base = {
      enumValues: [
        { value: 'land', label: 'Terrenos', color: 'blue' },
        { value: 'machinery', label: 'Maquinaria' },
        { value: 'other', label: 'Otro' },
      ],
      options: [
        { value: true, label: 'Sí' },
        { value: false, label: 'No' },
      ],
    };
    const merged = mergeOverlay(base, {
      enumValues: [
        { value: 'machinery', labelKey: 'entities.asset.options.class.machinery' },
        { value: 'land', labelKey: 'entities.asset.options.class.land' },
      ],
      options: [{ value: 'true', labelKey: 'yes' }],
    }) as typeof base;
    expect(merged.enumValues).toEqual([
      { value: 'land', label: 'Terrenos', color: 'blue', labelKey: 'entities.asset.options.class.land' },
      { value: 'machinery', label: 'Maquinaria', labelKey: 'entities.asset.options.class.machinery' },
      { value: 'other', label: 'Otro' },
    ]);
    // El valor booleano y su cadena son la misma identidad (manda el escalar de la superposición).
    expect(merged.options).toEqual([
      { value: 'true', label: 'Sí', labelKey: 'yes' },
      { value: false, label: 'No' },
    ]);
    expect((mergeOverlay(base, { enumValues: [{ value: 'new', labelKey: 'k' }] }) as typeof base).enumValues).toHaveLength(4);
    // Sin identidad completa en la superposición, el array se sustituye como siempre.
    expect(mergeOverlay(base, { enumValues: [{ value: 'a' }, { label: 'sin valor' }] })).toMatchObject({
      enumValues: [{ value: 'a' }, { label: 'sin valor' }],
    });
  });

  it('los arrays sin identidad se sustituyen y lo escalar manda la superposición', () => {
    expect(mergeOverlay({ tags: ['a', 'b'] }, { tags: ['z'] })).toEqual({ tags: ['z'] });
    expect(mergeOverlay({ list: [{ x: 1 }] }, { list: [{ x: 2 }, 3] })).toEqual({ list: [{ x: 2 }, 3] });
    expect(mergeOverlay({ a: 1 }, { a: 'dos' })).toEqual({ a: 'dos' });
    expect(mergeOverlay({ a: { b: 1 } }, { a: 'plano' })).toEqual({ a: 'plano' });
    expect(mergeOverlay('x', undefined)).toBe('x');
    expect(mergeOverlay(undefined, undefined)).toBeUndefined();
    expect(mergeOverlay({ a: 1 }, 5)).toBe(5);
  });
});
