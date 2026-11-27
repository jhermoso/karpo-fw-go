// resolveThemeColor y las demás sondas, con getComputedStyle simulado (jsdom no calcula light-dark()).
import { afterEach, describe, expect, it, vi } from 'vitest';
import { measureIconStyle, resolveThemeColor, resolveThemeLength } from './probe';

function mockComputed(values: Partial<CSSStyleDeclaration>) {
  return vi.spyOn(window, 'getComputedStyle').mockReturnValue(values as CSSStyleDeclaration);
}

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = '';
});

describe('resolveThemeColor', () => {
  it('pinta var(token) en una sonda y devuelve el color calculado', () => {
    let painted = '';
    vi.spyOn(window, 'getComputedStyle').mockImplementation((el: Element) => {
      painted = (el as HTMLElement).style.getPropertyValue('color');
      return { color: 'rgb(11, 122, 110)' } as CSSStyleDeclaration;
    });
    expect(resolveThemeColor('--th-semantic-up-strong')).toBe('rgb(11, 122, 110)');
    expect(painted).toBe('var(--th-semantic-up-strong)');
  });

  it('acepta var(--x) y expresiones de color tal cual', () => {
    let painted = '';
    vi.spyOn(window, 'getComputedStyle').mockImplementation((el: Element) => {
      painted = (el as HTMLElement).style.getPropertyValue('color');
      return { color: 'rgb(0, 0, 0)' } as CSSStyleDeclaration;
    });
    resolveThemeColor('var(--th-text-link)');
    expect(painted).toBe('var(--th-text-link)');
    resolveThemeColor('color-mix(in srgb, var(--th-text-link) 50%, white)');
    expect(painted).toContain('color-mix');
  });

  it('mide en el contenedor indicado (el modo de un contenedor cambia el color)', () => {
    const container = document.createElement('div');
    container.setAttribute('data-th-mode', 'dark');
    document.body.appendChild(container);
    let parent: Element | null = null;
    vi.spyOn(window, 'getComputedStyle').mockImplementation((el: Element) => {
      parent = el.parentElement;
      return { color: 'rgb(76, 194, 176)' } as CSSStyleDeclaration;
    });
    expect(resolveThemeColor('--th-semantic-up-strong', container)).toBe('rgb(76, 194, 176)');
    expect(parent).toBe(container);
  });

  it('conserva el alfa del color (el velo del tema trae transparencia)', () => {
    mockComputed({ color: 'rgba(9, 11, 15, 0.4)' });
    expect(resolveThemeColor('--th-surface-scrim')).toBe('rgba(9, 11, 15, 0.4)');
  });

  it('no deja la sonda en el DOM', () => {
    mockComputed({ color: 'rgb(1, 1, 1)' });
    resolveThemeColor('--th-text-primary');
    expect(document.documentElement.querySelectorAll('span[aria-hidden]')).toHaveLength(0);
  });

  it('quita la sonda aunque getComputedStyle falle', () => {
    vi.spyOn(window, 'getComputedStyle').mockImplementation(() => {
      throw new Error('boom');
    });
    expect(() => resolveThemeColor('--th-text-primary')).toThrow('boom');
    expect(document.documentElement.querySelectorAll('span[aria-hidden]')).toHaveLength(0);
  });

  it('devuelve cadena vacía si la expresión no es un color', () => {
    mockComputed({ color: 'rgb(0, 0, 0)' });
    expect(resolveThemeColor('esto no es un color')).toBe('');
  });

  it('devuelve tal cual un color que no es rgb() cuando no hay lienzo (jsdom)', () => {
    mockComputed({ color: 'oklab(0.5 0.1 0.1)' });
    expect(resolveThemeColor('--th-text-link')).toBe('oklab(0.5 0.1 0.1)');
  });

  it('convierte oklab() a rgb() con un lienzo 1x1 cuando existe', () => {
    mockComputed({ color: 'oklab(0.5 0.1 0.1)' });
    const context = {
      clearRect: vi.fn(),
      fillRect: vi.fn(),
      fillStyle: '',
      getImageData: () => ({ data: [120, 60, 40, 255] }),
    };
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(context as unknown as CanvasRenderingContext2D);
    expect(resolveThemeColor('--th-text-link')).toBe('rgb(120, 60, 40)');
    expect(context.fillStyle).toBe('oklab(0.5 0.1 0.1)');
  });

  it('conserva el alfa al convertir con el lienzo', () => {
    mockComputed({ color: 'color(srgb 0.1 0.2 0.3 / 0.5)' });
    const context = {
      clearRect: vi.fn(),
      fillRect: vi.fn(),
      fillStyle: '',
      getImageData: () => ({ data: [26, 51, 77, 128] }),
    };
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(context as unknown as CanvasRenderingContext2D);
    expect(resolveThemeColor('--th-text-link')).toBe('rgba(26, 51, 77, 0.502)');
  });
});

describe('resolveThemeLength', () => {
  it('lee la medida calculada de la variable', () => {
    mockComputed({ paddingInlineStart: '8px' } as CSSStyleDeclaration);
    expect(resolveThemeLength('--th-space-2')).toBe(8);
  });

  it('es NaN si no se puede medir', () => {
    mockComputed({ paddingInlineStart: '' } as CSSStyleDeclaration);
    expect(resolveThemeLength('--th-space-2')).toBeNaN();
  });
});

describe('measureIconStyle', () => {
  it('relleno (glossy-2003) o contorno (modern-flat, classic-1995) según el fill calculado', () => {
    mockComputed({ fill: 'rgb(0, 0, 0)' });
    expect(measureIconStyle()).toBe('filled');
    vi.restoreAllMocks();
    mockComputed({ fill: 'none' });
    expect(measureIconStyle()).toBe('outline');
    vi.restoreAllMocks();
    mockComputed({ fill: '' });
    expect(measureIconStyle()).toBe('outline');
  });
});
