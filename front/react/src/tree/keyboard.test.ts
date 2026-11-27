import { describe, expect, it } from 'vitest';
import type { Row } from '../contracts';
import { diagramKeyAction, isTypeAheadKey, treeKeyAction, typeAheadTarget } from './keyboard';
import { buildFromFlat, flattenVisible } from './model';

const row = (id: string, parent?: string): Row => ({ id, parent: parent ?? '' }) as Row;
const model = buildFromFlat([row('a'), row('b', 'a'), row('c', 'a'), row('d', 'b'), row('e')], 'parent');
const labels: Record<string, string> = { a: 'Alfa', b: 'Beta', c: 'Cobre', d: 'Delta', e: 'Eco' };
const labelOf = (id: string) => labels[id] ?? '';
const ltr = { rtl: false };

describe('teclado del listado', () => {
  const items = flattenVisible(model, new Set(['a']));

  it('flechas, inicio y fin', () => {
    expect(treeKeyAction(items, 'a', { key: 'ArrowDown' }, ltr)).toEqual({ type: 'focus', id: 'b' });
    expect(treeKeyAction(items, 'e', { key: 'ArrowDown' }, ltr)).toEqual({ type: 'focus', id: 'e' });
    expect(treeKeyAction(items, 'a', { key: 'ArrowUp' }, ltr)).toEqual({ type: 'focus', id: 'a' });
    expect(treeKeyAction(items, 'c', { key: 'ArrowUp' }, ltr)).toEqual({ type: 'focus', id: 'b' });
    expect(treeKeyAction(items, 'c', { key: 'Home' }, ltr)).toEqual({ type: 'focus', id: 'a' });
    expect(treeKeyAction(items, 'a', { key: 'End' }, ltr)).toEqual({ type: 'focus', id: 'e' });
  });

  it('derecha despliega, y con la rama abierta baja al primer hijo; izquierda pliega o sube', () => {
    expect(treeKeyAction(items, 'b', { key: 'ArrowRight' }, ltr)).toEqual({ type: 'expand', id: 'b' });
    expect(treeKeyAction(items, 'a', { key: 'ArrowRight' }, ltr)).toEqual({ type: 'focus', id: 'b' });
    expect(treeKeyAction(items, 'c', { key: 'ArrowRight' }, ltr)).toBeNull();
    expect(treeKeyAction(items, 'a', { key: 'ArrowLeft' }, ltr)).toEqual({ type: 'collapse', id: 'a' });
    expect(treeKeyAction(items, 'b', { key: 'ArrowLeft' }, ltr)).toEqual({ type: 'focus', id: 'a' });
    expect(treeKeyAction(items, 'e', { key: 'ArrowLeft' }, ltr)).toBeNull();
  });

  it('en rtl se invierten las flechas laterales', () => {
    const rtl = { rtl: true };
    expect(treeKeyAction(items, 'b', { key: 'ArrowLeft' }, rtl)).toEqual({ type: 'expand', id: 'b' });
    expect(treeKeyAction(items, 'a', { key: 'ArrowRight' }, rtl)).toEqual({ type: 'collapse', id: 'a' });
  });

  it('asterisco, Intro y Espacio; con modificadores no hace nada', () => {
    expect(treeKeyAction(items, 'b', { key: '*' }, ltr)).toEqual({ type: 'expand-siblings', id: 'b' });
    expect(treeKeyAction(items, 'b', { key: 'Enter' }, ltr)).toEqual({ type: 'activate', id: 'b' });
    expect(treeKeyAction(items, 'b', { key: ' ' }, ltr)).toEqual({ type: 'select', id: 'b' });
    expect(treeKeyAction(items, 'b', { key: 'ArrowDown', ctrlKey: true }, ltr)).toBeNull();
    expect(treeKeyAction([], null, { key: 'ArrowDown' }, ltr)).toBeNull();
  });

  it('búsqueda por letra: una letra, varias y repetida', () => {
    const all = flattenVisible(model, new Set(['a', 'b']));
    expect(isTypeAheadKey({ key: 'c' })).toBe(true);
    expect(isTypeAheadKey({ key: 'c', ctrlKey: true })).toBe(false);
    expect(isTypeAheadKey({ key: ' ' })).toBe(false);
    expect(isTypeAheadKey({ key: 'ArrowDown' })).toBe(false);
    expect(typeAheadTarget(all, 'a', 'c', labelOf)).toBe('c');
    expect(typeAheadTarget(all, 'a', 'D', labelOf)).toBe('d');
    // Da la vuelta al final de la lista.
    expect(typeAheadTarget(all, 'e', 'a', labelOf)).toBe('a');
    // Varias letras afinan desde el propio elemento.
    expect(typeAheadTarget(all, 'c', 'co', labelOf)).toBe('c');
    expect(typeAheadTarget(all, 'c', 'zz', labelOf)).toBeNull();
    // Una letra repetida avanza entre los que empiezan por ella.
    const twin = flattenVisible(
      buildFromFlat([row('x'), row('y'), row('z')], 'parent'),
      new Set(),
    );
    const names: Record<string, string> = { x: 'Mesa 1', y: 'Mesa 2', z: 'Sofá' };
    expect(typeAheadTarget(twin, 'x', 'mm', (id) => names[id] ?? '')).toBe('y');
    expect(typeAheadTarget(twin, null, '', labelOf)).toBeNull();
  });
});

describe('teclado del diagrama', () => {
  const open = new Set(['a', 'b']);

  it('arriba va al padre, abajo al primer hijo (y despliega si está plegado)', () => {
    expect(diagramKeyAction(model, open, 'b', { key: 'ArrowUp' }, ltr)).toEqual({ type: 'focus', id: 'a' });
    expect(diagramKeyAction(model, open, 'a', { key: 'ArrowUp' }, ltr)).toBeNull();
    expect(diagramKeyAction(model, open, 'a', { key: 'ArrowDown' }, ltr)).toEqual({ type: 'focus', id: 'b' });
    expect(diagramKeyAction(model, new Set(), 'a', { key: 'ArrowDown' }, ltr)).toEqual({ type: 'expand', id: 'a' });
    expect(diagramKeyAction(model, open, 'c', { key: 'ArrowDown' }, ltr)).toBeNull();
  });

  it('izquierda y derecha van entre hermanos, en espejo en rtl; inicio y fin a los extremos', () => {
    expect(diagramKeyAction(model, open, 'b', { key: 'ArrowRight' }, ltr)).toEqual({ type: 'focus', id: 'c' });
    expect(diagramKeyAction(model, open, 'c', { key: 'ArrowLeft' }, ltr)).toEqual({ type: 'focus', id: 'b' });
    expect(diagramKeyAction(model, open, 'b', { key: 'ArrowLeft' }, ltr)).toBeNull();
    const rtl = { rtl: true };
    expect(diagramKeyAction(model, open, 'b', { key: 'ArrowLeft' }, rtl)).toEqual({ type: 'focus', id: 'c' });
    expect(diagramKeyAction(model, open, 'c', { key: 'ArrowRight' }, rtl)).toEqual({ type: 'focus', id: 'b' });
    expect(diagramKeyAction(model, open, 'b', { key: 'End' }, ltr)).toEqual({ type: 'focus', id: 'c' });
    expect(diagramKeyAction(model, open, 'c', { key: 'Home' }, ltr)).toEqual({ type: 'focus', id: 'b' });
    expect(diagramKeyAction(model, open, 'b', { key: 'Home' }, ltr)).toBeNull();
  });

  it('más y menos despliegan y pliegan; Intro abre', () => {
    expect(diagramKeyAction(model, new Set(), 'a', { key: '+' }, ltr)).toEqual({ type: 'expand', id: 'a' });
    expect(diagramKeyAction(model, open, 'a', { key: '+' }, ltr)).toBeNull();
    expect(diagramKeyAction(model, open, 'a', { key: '-' }, ltr)).toEqual({ type: 'collapse', id: 'a' });
    expect(diagramKeyAction(model, new Set(), 'a', { key: '-' }, ltr)).toBeNull();
    expect(diagramKeyAction(model, open, 'a', { key: 'Enter' }, ltr)).toEqual({ type: 'activate', id: 'a' });
    expect(diagramKeyAction(model, open, 'zzz', { key: 'Enter' }, ltr)).toBeNull();
  });
});
