// @vitest-environment jsdom
// Piezas de la ayuda: teclas del recorrido, anclas, textos y fusión de pasos (con jsdom para las anclas).
import { afterEach, describe, expect, it } from 'vitest';
import {
  escapeHtml,
  mergeSteps,
  parseAnchor,
  resolveHelpText,
  stepsFromAnchors,
  stepsFromConfig,
  type HelpTranslator,
  type ResolvedStep,
} from './steps';
import { tourKeyAction } from './tour';

const inPage = { insidePopover: false, inWidget: false };

describe('tourKeyAction', () => {
  it('LTR: derecha avanza, izquierda retrocede, Esc cierra', () => {
    expect(tourKeyAction('ArrowRight', 'ltr', inPage)).toBe('next');
    expect(tourKeyAction('ArrowLeft', 'ltr', inPage)).toBe('prev');
    expect(tourKeyAction('Escape', 'ltr', inPage)).toBe('close');
  });

  it('RTL: las flechas se invierten, Esc no', () => {
    expect(tourKeyAction('ArrowLeft', 'rtl', inPage)).toBe('next');
    expect(tourKeyAction('ArrowRight', 'rtl', inPage)).toBe('prev');
    expect(tourKeyAction('Escape', 'rtl', inPage)).toBe('close');
  });

  it('dentro de un widget las flechas son suyas, salvo si el foco está en el popover', () => {
    expect(tourKeyAction('ArrowRight', 'ltr', { insidePopover: false, inWidget: true })).toBeNull();
    expect(tourKeyAction('ArrowRight', 'ltr', { insidePopover: true, inWidget: true })).toBe('next');
    expect(tourKeyAction('Escape', 'ltr', { insidePopover: false, inWidget: true })).toBe('close');
  });

  it('otras teclas no hacen nada', () => {
    for (const key of ['Enter', 'ArrowUp', 'ArrowDown', 'Tab', 'a', ' ']) {
      expect(tourKeyAction(key, 'ltr', inPage), key).toBeNull();
    }
  });
});

describe('parseAnchor', () => {
  it('separa espacio de nombres y nombre en el primer punto', () => {
    expect(parseAnchor('grid.toolbar')).toEqual({ ns: 'grid', name: 'toolbar' });
    expect(parseAnchor('grid.toolbar.filters')).toEqual({ ns: 'grid', name: 'toolbar.filters' });
    expect(parseAnchor('help.button')).toEqual({ ns: 'help', name: 'button' });
  });

  it('un ancla sin punto no tiene textos', () => {
    expect(parseAnchor('suelto')).toBeNull();
    expect(parseAnchor('.raro')).toBeNull();
  });
});

describe('escapeHtml', () => {
  it('neutraliza etiquetas y comillas y conserva los saltos de línea', () => {
    expect(escapeHtml('<b>"a" & \'b\'</b>')).toBe('&lt;b&gt;&quot;a&quot; &amp; &#39;b&#39;&lt;/b&gt;');
    expect(escapeHtml('uno\ndos')).toBe('uno<br>dos');
  });
});

describe('resolveHelpText', () => {
  const translator = (language: string, known: Record<string, string> = {}): HelpTranslator => ({
    language,
    exists: (key, { ns }) => `${ns}:${key}` in known,
    t: (key, { ns }) => known[`${ns}:${key}`] ?? key,
  });

  it('prefiere la clave, luego el idioma (completo o base) y por último el literal', () => {
    const t = translator('en-GB', { 'core:a.b': 'por clave' });
    expect(resolveHelpText({ literal: 'L', key: 'a.b' }, t)).toBe('por clave');
    expect(resolveHelpText({ literal: 'L', key: 'help:nada', byLang: { en: 'inglés' } }, t)).toBe('inglés');
    expect(resolveHelpText({ literal: 'L', byLang: { 'en-GB': 'británico', en: 'inglés' } }, t)).toBe('británico');
    expect(resolveHelpText({ literal: 'L', byLang: { fr: 'fr' } }, t)).toBe('L');
    expect(resolveHelpText(undefined, t)).toBe('');
  });

  it('una clave con prefijo ns: busca en ese espacio', () => {
    const t = translator('es', { 'grid:help.x': 'ayuda x' });
    expect(resolveHelpText({ literal: 'L', key: 'grid:help.x' }, t)).toBe('ayuda x');
  });
});

describe('mergeSteps y anclas de reserva (D-R36)', () => {
  const texts: Record<string, string> = {};
  const translator: HelpTranslator = {
    language: 'es',
    exists: (key, { ns }) => `${ns}:${key}` in texts,
    t: (key, { ns }) => texts[`${ns}:${key}`] ?? key,
  };
  const withText = (ns: string, name: string, title = name, body = `cuerpo ${name}`, idle = false) => {
    const prefix = idle ? `help.${name}.idle` : `help.${name}`;
    texts[`${ns}:${prefix}.title`] = title;
    texts[`${ns}:${prefix}.body`] = body;
  };
  const html = (markup: string) => {
    document.body.innerHTML = markup;
    return document.body;
  };
  const sources = (steps: ResolvedStep[]) => steps.map((step) => step.source);

  afterEach(() => {
    document.body.innerHTML = '';
    for (const key of Object.keys(texts)) delete texts[key];
  });

  it('conserva el orden de la configuración y añade las anclas visibles no cubiertas en orden de documento', () => {
    for (const name of ['a', 'b', 'c', 'd']) withText('x', name);
    const root = html(`
      <div data-kx-help="x.a">a</div><div id="m">m</div><div data-kx-help="x.b">b</div>
      <div data-kx-help="x.c">c</div><div data-kx-help="x.d" hidden>d</div>`);
    const config = stepsFromConfig(
      [
        { target: '#m', title: { literal: 'M' }, body: { literal: 'm' } },
        { target: 'x.c', title: { literal: 'C de la configuración' }, body: { literal: 'c' } },
      ],
      translator,
      root,
    );
    const merged = mergeSteps(config, stepsFromAnchors(translator, root));
    expect(sources(merged)).toEqual(['#m', 'x.c', 'x.a', 'x.b']);
    expect(merged[1]?.title).toBe('C de la configuración');
  });

  it('no duplica un ancla citada por un paso de la configuración, ni por nombre ni por elemento', () => {
    for (const name of ['a', 'b']) withText('x', name);
    const root = html('<div data-kx-help="x.a" id="a">a</div><div data-kx-help="x.b">b</div>');
    const config = stepsFromConfig(
      [
        { target: 'x.b', title: { literal: 'B' }, body: { literal: 'b' } },
        { target: '#a', title: { literal: 'A' }, body: { literal: 'a' } },
      ],
      translator,
      root,
    );
    const merged = mergeSteps(config, stepsFromAnchors(translator, root));
    expect(sources(merged)).toEqual(['x.b', '#a']);
  });

  it('sin configuración el resultado son las anclas; sin anclas, la configuración', () => {
    withText('x', 'a');
    const root = html('<div data-kx-help="x.a">a</div>');
    const anchors = stepsFromAnchors(translator, root);
    expect(mergeSteps([], anchors)).toEqual(anchors);
    const config: ResolvedStep[] = [{ title: 't', body: 'b', source: '#z' }];
    expect(mergeSteps(config, [])).toEqual(config);
  });

  it('un ancla oculta con reserva da un paso sobre el elemento de reserva con el texto idle', () => {
    withText('grid', 'toolbar');
    withText('grid', 'batch-bar', 'Barra de lote', 'normal');
    withText('grid', 'batch-bar', 'Aparece al editar', 'Guarda de golpe', true);
    const root = html(`
      <div data-kx-help="grid.toolbar" id="tb">herramientas</div>
      <div data-kx-help="grid.batch-bar" data-kx-help-fallback="grid.toolbar" hidden>lote</div>`);
    const steps = stepsFromAnchors(translator, root);
    expect(steps.map((s) => s.title)).toEqual(['toolbar', 'Aparece al editar']);
    expect(steps[1]?.element).toBe(root.querySelector('#tb'));
    expect(steps[1]?.body).toBe('Guarda de golpe');
    expect(steps[1]?.source).toBe('grid.batch-bar');
    expect(steps[1]?.fallback).toBe(true);
  });

  it('el paso de reserva no lo cubre ni lo descarta un paso de la configuración sobre el mismo elemento', () => {
    withText('grid', 'batch-bar', 'Barra de lote', 'normal');
    withText('grid', 'batch-bar', 'Aparece al editar', 'Guarda de golpe', true);
    const root = html(`
      <div data-kx-help="grid.toolbar" id="tb">herramientas</div>
      <div data-kx-help="grid.batch-bar" data-kx-help-fallback="grid.toolbar" hidden>lote</div>`);
    const config = stepsFromConfig([{ target: '#tb', title: { literal: 'T' }, body: { literal: 't' } }], translator, root);
    const merged = mergeSteps(config, stepsFromAnchors(translator, root));
    expect(merged.map((s) => s.title)).toEqual(['T', 'Aparece al editar']);
  });

  it('sin texto idle un ancla oculta no produce paso (como antes)', () => {
    withText('grid', 'toolbar');
    withText('grid', 'batch-bar', 'Barra de lote', 'normal');
    const root = html(`
      <div data-kx-help="grid.toolbar">herramientas</div>
      <div data-kx-help="grid.batch-bar" data-kx-help-fallback="grid.toolbar" hidden>lote</div>`);
    expect(sources(stepsFromAnchors(translator, root))).toEqual(['grid.toolbar']);
  });

  it('con el elemento de reserva oculto o inexistente no hay paso', () => {
    withText('grid', 'toolbar');
    withText('grid', 'batch-bar', 'Barra de lote', 'normal');
    withText('grid', 'batch-bar', 'Aparece al editar', 'Guarda de golpe', true);
    withText('grid', 'otra', 'otra', 'otra', true);
    const root = html(`
      <div data-kx-help="grid.toolbar" hidden>herramientas</div>
      <div data-kx-help="grid.batch-bar" data-kx-help-fallback="grid.toolbar" hidden>lote</div>
      <div data-kx-help="grid.otra" data-kx-help-fallback="grid.nada" hidden>otra</div>`);
    expect(stepsFromAnchors(translator, root)).toEqual([]);
  });

  it('visible, el ancla usa su texto normal y no el idle', () => {
    withText('grid', 'toolbar');
    withText('grid', 'batch-bar', 'Barra de lote', 'normal');
    withText('grid', 'batch-bar', 'Aparece al editar', 'Guarda de golpe', true);
    const root = html(`
      <div data-kx-help="grid.toolbar" id="tb">herramientas</div>
      <div data-kx-help="grid.batch-bar" data-kx-help-fallback="grid.toolbar" id="bb">lote</div>`);
    const steps = stepsFromAnchors(translator, root);
    expect(steps.map((s) => s.title)).toEqual(['toolbar', 'Barra de lote']);
    expect(steps[1]?.element).toBe(root.querySelector('#bb'));
    expect(steps[1]?.fallback).toBeUndefined();
  });
});
