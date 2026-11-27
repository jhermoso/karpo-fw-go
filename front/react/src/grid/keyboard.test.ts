// Teclado de la cuadrícula: movimiento de la celda activa, rangos y tras confirmar una edición.
import { describe, expect, it } from 'vitest';
import {
  HEADER_ROW,
  afterCommit,
  inRange,
  isMultiCell,
  isNavigationKey,
  isPrintable,
  nextPosition,
  rangeBetween,
  stepCell,
  type Dimensions,
} from './keyboard';

const dims: Dimensions = { rows: 10, cols: 5, page: 4 };
const at = (row: number, col: number) => ({ row, col });

describe('nextPosition', () => {
  it('las flechas se detienen en los bordes; arriba llega a la cabecera', () => {
    expect(nextPosition(at(3, 2), { key: 'ArrowDown' }, dims)).toEqual(at(4, 2));
    expect(nextPosition(at(9, 2), { key: 'ArrowDown' }, dims)).toEqual(at(9, 2));
    expect(nextPosition(at(0, 2), { key: 'ArrowUp' }, dims)).toEqual(at(HEADER_ROW, 2));
    expect(nextPosition(at(HEADER_ROW, 2), { key: 'ArrowUp' }, dims)).toEqual(at(HEADER_ROW, 2));
    expect(nextPosition(at(3, 0), { key: 'ArrowLeft' }, dims)).toEqual(at(3, 0));
    expect(nextPosition(at(3, 4), { key: 'ArrowRight' }, dims)).toEqual(at(3, 4));
    expect(nextPosition(at(3, 1), { key: 'ArrowRight' }, dims)).toEqual(at(3, 2));
  });

  it('en RTL las flechas horizontales se invierten', () => {
    const rtl = { ...dims, rtl: true };
    expect(nextPosition(at(3, 2), { key: 'ArrowRight' }, rtl)).toEqual(at(3, 1));
    expect(nextPosition(at(3, 2), { key: 'ArrowLeft' }, rtl)).toEqual(at(3, 3));
  });

  it('Inicio y Fin van a los extremos de la fila; con Mod, a los de la cuadrícula', () => {
    expect(nextPosition(at(3, 2), { key: 'Home' }, dims)).toEqual(at(3, 0));
    expect(nextPosition(at(3, 2), { key: 'End' }, dims)).toEqual(at(3, 4));
    expect(nextPosition(at(3, 2), { key: 'Home', ctrlKey: true }, dims)).toEqual(at(0, 0));
    expect(nextPosition(at(3, 2), { key: 'End', metaKey: true }, dims)).toEqual(at(9, 4));
    expect(nextPosition(at(HEADER_ROW, 2), { key: 'Home', ctrlKey: true }, { ...dims, rows: 0 })).toEqual(at(HEADER_ROW, 0));
    expect(nextPosition(at(HEADER_ROW, 2), { key: 'End', ctrlKey: true }, { ...dims, rows: 0 })).toEqual(at(HEADER_ROW, 4));
  });

  it('RePág y AvPág saltan una ventana', () => {
    expect(nextPosition(at(2, 1), { key: 'PageDown' }, dims)).toEqual(at(6, 1));
    expect(nextPosition(at(8, 1), { key: 'PageDown' }, dims)).toEqual(at(9, 1));
    expect(nextPosition(at(6, 1), { key: 'PageUp' }, dims)).toEqual(at(2, 1));
    expect(nextPosition(at(1, 1), { key: 'PageUp' }, dims)).toEqual(at(0, 1));
    expect(nextPosition(at(HEADER_ROW, 1), { key: 'PageUp' }, dims)).toEqual(at(HEADER_ROW, 1));
    expect(nextPosition(at(0, 1), { key: 'PageDown' }, { ...dims, page: 0 })).toEqual(at(1, 1));
  });

  it('Tab y Mayús+Tab no son teclas de navegación: el navegador saca el foco de la rejilla (B-4)', () => {
    for (const shiftKey of [false, true]) {
      for (const pos of [at(1, 1), at(1, 4), at(0, 0), at(9, 4), at(HEADER_ROW, 0), at(HEADER_ROW, 3)]) {
        expect(nextPosition(pos, { key: 'Tab', shiftKey }, dims)).toBeNull();
      }
    }
    expect(isNavigationKey('Tab')).toBe(false);
  });

  it('ignora lo que no es navegación y las cuadrículas sin columnas', () => {
    expect(nextPosition(at(1, 1), { key: 'a' }, dims)).toBeNull();
    expect(nextPosition(at(1, 1), { key: 'ArrowDown' }, { ...dims, cols: 0 })).toBeNull();
    expect(isNavigationKey('PageDown')).toBe(true);
    expect(isNavigationKey('Enter')).toBe(false);
  });
});

describe('teclas imprimibles', () => {
  it('sólo un carácter sin modificadores abre el editor', () => {
    expect(isPrintable({ key: 'a' })).toBe(true);
    expect(isPrintable({ key: '5' })).toBe(true);
    expect(isPrintable({ key: ' ' })).toBe(false);
    expect(isPrintable({ key: 'a', ctrlKey: true })).toBe(false);
    expect(isPrintable({ key: 'a', metaKey: true })).toBe(false);
    expect(isPrintable({ key: 'Enter' })).toBe(false);
  });
});

describe('rangos', () => {
  it('normaliza el rectángulo entre dos celdas, en cualquier sentido', () => {
    const range = rangeBetween(at(5, 3), at(2, 1));
    expect(range).toEqual({ top: 2, bottom: 5, start: 1, end: 3 });
    expect(inRange(range, 3, 2)).toBe(true);
    expect(inRange(range, 6, 2)).toBe(false);
    expect(inRange(null, 0, 0)).toBe(false);
    expect(isMultiCell(range)).toBe(true);
    expect(isMultiCell(rangeBetween(at(2, 2), at(2, 2)))).toBe(false);
    expect(isMultiCell(null)).toBe(false);
    expect(rangeBetween(at(HEADER_ROW, 0), at(2, 1)).top).toBe(0);
  });
});

describe('tras confirmar una edición', () => {
  it('Enter baja, Shift+Enter sube, Tab avanza y no se sale de la cuadrícula', () => {
    expect(afterCommit(at(2, 1), 'down', dims)).toEqual(at(3, 1));
    expect(afterCommit(at(9, 1), 'down', dims)).toEqual(at(9, 1));
    expect(afterCommit(at(0, 1), 'up', dims)).toEqual(at(0, 1));
    expect(afterCommit(at(2, 1), 'up', dims)).toEqual(at(1, 1));
    expect(afterCommit(at(2, 1), 'next', dims)).toEqual(at(2, 2));
    expect(afterCommit(at(2, 4), 'next', dims)).toEqual(at(3, 0));
    expect(afterCommit(at(9, 4), 'next', dims)).toEqual(at(9, 4));
    expect(afterCommit(at(2, 1), 'prev', dims)).toEqual(at(2, 0));
    expect(afterCommit(at(2, 0), 'prev', dims)).toEqual(at(1, 4));
    expect(afterCommit(at(0, 0), 'prev', dims)).toEqual(at(0, 0));
    expect(afterCommit(at(2, 1), undefined, dims)).toEqual(at(2, 1));
  });

  it('stepCell recorre las celdas de la fila y, al llegar al extremo, pasa a la fila contigua', () => {
    expect(stepCell(at(1, 1), 'next', dims)).toEqual(at(1, 2));
    expect(stepCell(at(1, 4), 'next', dims)).toEqual(at(2, 0));
    expect(stepCell(at(1, 1), 'prev', dims)).toEqual(at(1, 0));
    expect(stepCell(at(2, 0), 'prev', dims)).toEqual(at(1, 4));
  });

  it('stepCell devuelve null donde no hay celda a la que ir (la última y la primera)', () => {
    expect(stepCell(at(9, 4), 'next', dims)).toBeNull();
    expect(stepCell(at(0, 0), 'prev', dims)).toBeNull();
    expect(stepCell(at(0, 0), 'next', { ...dims, rows: 0 })).toBeNull();
    expect(stepCell(at(0, 0), 'next', { ...dims, cols: 0 })).toBeNull();
  });
});
