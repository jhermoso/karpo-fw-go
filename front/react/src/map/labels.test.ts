import { describe, expect, it, vi } from 'vitest';
import {
  canvasMeasure,
  clampToBox,
  clearMeasureCache,
  commonKind,
  fitLabel,
  fontOf,
  labelVariants,
  pickLabels,
  textOfKind,
  wrapLines,
  type LabelCandidate,
  type LabelMeter,
  type Measure,
} from './labels';

/** Cada carácter mide 10 píxeles: las cuentas de las pruebas se hacen de cabeza. */
const ten: Measure = (text) => text.length * 10;
const meter: LabelMeter = { name: ten, value: ten, lineHeight: 16, paddingX: 4, paddingY: 2, maxWidth: 170 };

describe('wrapLines', () => {
  it('parte por palabras con la medida inyectada', () => {
    expect(wrapLines('Comunidad Foral de Navarra', 150, ten)).toEqual(['Comunidad Foral', 'de Navarra']);
    expect(wrapLines('Castilla y León', 150, ten)).toEqual(['Castilla y León']);
    expect(wrapLines('', 100, ten)).toEqual([]);
  });

  it('también parte tras un guion y conserva los espacios', () => {
    expect(wrapLines('Castilla-La Mancha', 110, ten)).toEqual(['Castilla-La', 'Mancha']);
    expect(wrapLines('Castilla-La Mancha', 90, ten)).toEqual(['Castilla-', 'La Mancha']);
  });

  it('no parte palabras: una más ancha que el máximo, o demasiadas líneas, devuelve null', () => {
    expect(wrapLines('Extraordinariamente', 100, ten)).toBeNull();
    expect(wrapLines('uno dos tres cuatro cinco seis', 30, ten, 3)).toBeNull();
    expect(wrapLines('aa bb cc', 20, ten, 3)).toEqual(['aa', 'bb', 'cc']);
    expect(wrapLines('aa bb cc dd', 20, ten, 3)).toBeNull();
  });
});

describe('fitLabel', () => {
  const texts = { name: 'Comunidad Foral de Navarra', shortName: 'Navarra', code: 'ES22', value: '12' };

  it('usa el nombre completo, partido, si cabe', () => {
    const fit = fitLabel(texts, meter, { width: 200, height: 80 })!;
    expect(fit.kind).toBe('full');
    expect(fit.lines).toEqual(['Comunidad Foral', 'de Navarra']);
    expect(fit.showValue).toBe(true);
    // Ancho = línea más larga (15 caracteres) + relleno; alto = 3 líneas (2 del nombre y el valor) + relleno.
    expect(fit).toMatchObject({ width: 150 + 8, height: 3 * 16 + 4 });
  });

  it('cae al nombre corto y luego al código cuando el largo no cabe', () => {
    const narrow = fitLabel(texts, meter, { width: 90, height: 80 })!;
    expect(narrow).toMatchObject({ kind: 'short', lines: ['Navarra'] });
    const tiny = fitLabel(texts, meter, { width: 52, height: 80 })!;
    expect(tiny).toMatchObject({ kind: 'code', lines: ['ES22'] });
    expect(fitLabel(texts, meter, { width: 30, height: 80 })).toBeNull();
  });

  it('sin altura para el valor lo omite antes de renunciar al nombre', () => {
    // 90 de ancho: el nombre largo no cabe; el corto sí, pero con el valor son 36 de alto y sólo hay 20.
    expect(fitLabel(texts, meter, { width: 90, height: 20 })).toMatchObject({ kind: 'short', showValue: false, lines: ['Navarra'] });
    expect(fitLabel(texts, meter, { width: 90, height: 36 })).toMatchObject({ kind: 'short', showValue: true });
    expect(fitLabel(texts, meter, { width: 90, height: 10 })).toBeNull();
  });

  it('respeta el ancho máximo del rótulo y el de la caja, nunca desborda', () => {
    for (const width of [60, 80, 100, 140, 400]) {
      const fit = fitLabel(texts, meter, { width, height: 200 });
      if (!fit) continue;
      expect(fit.width).toBeLessThanOrEqual(Math.min(width, meter.maxWidth));
      for (const line of fit.lines) expect(ten(line) + 2 * meter.paddingX).toBeLessThanOrEqual(fit.width);
    }
  });

  it('sin variantes repetidas: si el corto es igual al largo, no se prueba dos veces', () => {
    expect(labelVariants({ name: 'Madrid', shortName: 'Madrid', code: 'ES30' }).map((v) => v.kind)).toEqual(['full', 'code']);
    expect(labelVariants({ name: 'Madrid', shortName: '', code: '' }).map((v) => v.kind)).toEqual(['full']);
  });
});

describe('criterio único por nivel (R3-4, M-6)', () => {
  const big = { name: 'Comunidad Foral de Navarra', shortName: 'Navarra', code: 'ES22', value: '' };
  const small = { name: 'Comunidad Valenciana', shortName: 'Valencia', code: 'ES52', value: '' };
  const tiny = { name: 'La Rioja', shortName: '', code: 'ES23', value: '' };

  it('una mezcla de tamaños produce un único kind: el más corto de los elegidos', () => {
    const fits = [
      fitLabel(big, meter, { width: 200, height: 80 })!, // full
      fitLabel(small, meter, { width: 90, height: 80 })!, // short
      fitLabel(tiny, meter, { width: 52, height: 80 })!, // code
    ];
    expect(fits.map((fit) => fit.kind)).toEqual(['full', 'short', 'code']);
    expect(commonKind(fits)).toBe('code');
    expect(commonKind(fits.slice(0, 2))).toBe('short');
  });

  it('si todo cabe entero, la variante común es la completa; sin rótulos no hay nada que unificar', () => {
    const fits = [fitLabel(big, meter, { width: 200, height: 80 })!, fitLabel(small, meter, { width: 200, height: 80 })!];
    expect(commonKind(fits)).toBe('full');
    expect(commonKind([])).toBe('full');
  });

  it('con `only` sólo se prueba esa variante: el código sigue valiendo aunque el nombre completo cupiera', () => {
    const wide = { width: 200, height: 80 };
    expect(fitLabel(big, meter, wide)!.kind).toBe('full');
    expect(fitLabel(big, meter, wide, 'code')).toMatchObject({ kind: 'code', lines: ['ES22'] });
    expect(fitLabel(big, meter, wide, 'short')).toMatchObject({ kind: 'short', lines: ['Navarra'] });
    // Si no cabe esa variante no se cae a otra: sin rótulo.
    expect(fitLabel(big, meter, { width: 52, height: 80 }, 'full')).toBeNull();
  });

  it('sin nombre corto la variante «corta» es el nombre: todas las delimitaciones admiten la misma variante', () => {
    expect(textOfKind(tiny, 'short')).toBe('La Rioja');
    expect(textOfKind(tiny, 'full')).toBe('La Rioja');
    expect(textOfKind(tiny, 'code')).toBe('ES23');
    expect(fitLabel(tiny, meter, { width: 200, height: 80 }, 'short')).toMatchObject({ kind: 'short', lines: ['La Rioja'] });
  });
});

describe('pickLabels', () => {
  const label = (code: string, x: number, y: number, featureWidth: number, featureHeight: number, width = 60, height = 30): LabelCandidate => ({
    code,
    x,
    y,
    featureWidth,
    featureHeight,
    width,
    height,
  });
  const viewport = { width: 1000, height: 600 };

  it('pinta los de mayor delimitación y descarta los que se pisan', () => {
    const picked = pickLabels(
      [label('chico', 105, 100, 80, 80), label('grande', 100, 100, 300, 300), label('lejos', 500, 100, 90, 90)],
      viewport,
    );
    expect(picked).toEqual(['grande', 'lejos']);
  });

  it('descarta los que no caben en su delimitación o quedan fuera del lienzo', () => {
    expect(pickLabels([label('minimo', 100, 100, 10, 10)], viewport)).toEqual([]);
    // Un rótulo nunca es mayor que su delimitación: ni siquiera cabe «casi».
    expect(pickLabels([label('casi', 100, 100, 55, 30)], viewport)).toEqual([]);
    expect(pickLabels([label('justo', 100, 100, 60, 30)], viewport)).toEqual(['justo']);
    expect(pickLabels([label('fuera', -500, 100, 300, 300)], viewport)).toEqual([]);
    expect(pickLabels([label('fuera', 100, 5000, 300, 300)], viewport)).toEqual([]);
    expect(pickLabels([label('borde', 5, 5, 300, 300)], viewport)).toEqual(['borde']);
  });

  it('el orden es determinista: a igual tamaño, por código', () => {
    const items = [label('b', 100, 100, 200, 200), label('a', 110, 100, 200, 200)];
    expect(pickLabels(items, viewport)).toEqual(['a']);
    expect(pickLabels([...items].reverse(), viewport)).toEqual(['a']);
  });
});

describe('clampToBox', () => {
  it('mantiene el rótulo dentro de la caja de su delimitación', () => {
    const box = { left: 0, top: 0, right: 100, bottom: 50 };
    const size = { width: 40, height: 20 };
    expect(clampToBox({ x: 50, y: 25 }, size, box)).toEqual({ x: 50, y: 25 });
    expect(clampToBox({ x: 5, y: 2 }, size, box)).toEqual({ x: 20, y: 10 });
    expect(clampToBox({ x: 99, y: 49 }, size, box)).toEqual({ x: 80, y: 40 });
    // Si no cabe, se centra.
    expect(clampToBox({ x: 0, y: 0 }, { width: 120, height: 20 }, box)).toEqual({ x: 50, y: 10 });
  });
});

describe('canvasMeasure', () => {
  it('mide con el lienzo y cachea por (texto, fuente)', () => {
    clearMeasureCache();
    const measureText = vi.fn((text: string) => ({ width: text.length * 5 }));
    const ctx = { font: '', measureText } as unknown as CanvasRenderingContext2D;
    const a = canvasMeasure('bold 13px X', () => ctx);
    const b = canvasMeasure('13px X', () => ctx);
    expect(a('España')).toBe(30);
    expect(a('España')).toBe(30);
    expect(measureText).toHaveBeenCalledTimes(1);
    expect(b('España')).toBe(30);
    expect(measureText).toHaveBeenCalledTimes(2);
    expect(ctx.font).toBe('13px X');
  });

  it('sin lienzo (jsdom) estima por caracteres y no falla', () => {
    clearMeasureCache();
    expect(canvasMeasure('13px X', () => null)('abcd')).toBe(28);
  });

  it('compone la fuente del lienzo desde las propiedades calculadas', () => {
    expect(fontOf({ fontStyle: 'normal', fontWeight: '700', fontSize: '13px', fontFamily: 'Inter, sans-serif' })).toBe('700 13px Inter, sans-serif');
  });
});
