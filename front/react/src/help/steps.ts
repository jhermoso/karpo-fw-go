// Construcción de los pasos del recorrido (PLAN §4.8, D-R36): los de la configuración (HelpStepDef con Text),
// en su orden, seguidos de las anclas `[data-kx-help]` visibles que ningún paso cubre, en el orden del DOM
// (`mergeSteps`). Un ancla oculta con `data-kx-help-fallback` se explica sobre el elemento de reserva con el
// texto «idle». Un paso sin elemento visible se omite; un paso sin texto traducido también (no se enseña
// una clave a un usuario).
import { resolveText } from '../config/text';
import type { HelpStepDef, Text } from '../contracts';

/** Lo que el recorrido necesita del traductor (i18next lo cumple). */
export interface HelpTranslator {
  language: string;
  exists(key: string, options: { ns: string }): boolean;
  t(key: string, options: { ns: string }): string;
}

export interface ResolvedStep {
  /** Elemento destacado; `undefined` = popover centrado, sin elemento. */
  element?: Element;
  title: string;
  body: string;
  /** Ancla o selector del que salió (para depurar y para las pruebas). */
  source: string;
  /** Paso de un ancla oculta explicado sobre su elemento de reserva (texto `idle`). */
  fallback?: boolean;
}

export const ANCHOR_ATTRIBUTE = 'data-kx-help';
/** En un ancla oculta: ancla (o selector) del elemento visible sobre el que se explica mientras está oculta. */
export const FALLBACK_ATTRIBUTE = 'data-kx-help-fallback';

const ANCHOR_NAME = /^([^.]+)\.(.+)$/;

/** `grid.toolbar` → espacio `grid`, nombre `toolbar`. Un ancla sin punto no tiene textos. */
export function parseAnchor(anchor: string): { ns: string; name: string } | null {
  const match = ANCHOR_NAME.exec(anchor);
  return match ? { ns: match[1] ?? '', name: match[2] ?? '' } : null;
}

/** ¿Se ve el elemento? `checkVisibility` donde existe; si no, display/visibility por la cascada. */
export function isVisible(el: Element): boolean {
  if (!el.isConnected) return false;
  const withCheck = el as Element & { checkVisibility?: (o?: { visibilityProperty?: boolean }) => boolean };
  if (typeof withCheck.checkVisibility === 'function') return withCheck.checkVisibility({ visibilityProperty: true });
  const view = el.ownerDocument.defaultView;
  if (!view) return true;
  for (let node: Element | null = el; node; node = node.parentElement) {
    const style = view.getComputedStyle(node);
    if (style.display === 'none') return false;
    if (node === el && style.visibility === 'hidden') return false;
  }
  return true;
}

function cssString(value: string): string {
  return `"${value.replaceAll('\\', '\\\\').replaceAll('"', '\\"')}"`;
}

function firstVisible(root: ParentNode, selector: string): Element | undefined {
  try {
    return Array.from(root.querySelectorAll(selector)).find(isVisible);
  } catch {
    return undefined; // selector inválido en la configuración: se omite el paso
  }
}

/** Busca el elemento de un `target`: primero como ancla `data-kx-help`, luego como selector CSS. */
export function resolveTarget(target: string, root: ParentNode = document): Element | undefined {
  return (
    firstVisible(root, `[${ANCHOR_ATTRIBUTE}=${cssString(target)}]`) ?? firstVisible(root, target)
  );
}

/** Escapa HTML: driver.js escribe título y texto con innerHTML. Los saltos de línea pasan a `<br>`. */
export function escapeHtml(text: string): string {
  return text
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
    .replaceAll('\n', '<br>');
}

/**
 * Resuelve un `Text` de la configuración con la misma cadena que el resto del front (`resolveText`): clave
 * en el idioma activo (`ns:clave`, espacio `core` por omisión), diccionario de la configuración, traducción
 * por idioma y, por último, el literal.
 */
export function resolveHelpText(text: Text | undefined, translator: HelpTranslator): string {
  if (!text) return '';
  const resolved = resolveText(text, translator.language, (key) => {
    const sep = key.indexOf(':');
    const ns = sep > 0 ? key.slice(0, sep) : 'core';
    const bare = sep > 0 ? key.slice(sep + 1) : key;
    return translator.exists(bare, { ns }) ? translator.t(bare, { ns }) : undefined;
  });
  // Una clave sin traducir ni literal no se enseña a un usuario: el paso queda sin texto y se omite.
  return resolved === text.key ? '' : resolved;
}

/** Pasos que salen de `HelpStepDef[]`; omite los que no tienen elemento visible o texto. */
export function stepsFromConfig(
  defs: readonly HelpStepDef[],
  translator: HelpTranslator,
  root: ParentNode = document,
): ResolvedStep[] {
  const steps: ResolvedStep[] = [];
  for (const def of defs) {
    const element = resolveTarget(def.target, root);
    if (!element) continue;
    const title = resolveHelpText(def.title, translator);
    const body = resolveHelpText(def.body, translator);
    if (title === '' && body === '') continue;
    steps.push({ element, title, body, source: def.target });
  }
  return steps;
}

function anchorText(
  translator: HelpTranslator,
  ns: string,
  prefix: string,
): { title: string; body: string } {
  const titleKey = `${prefix}.title`;
  const bodyKey = `${prefix}.body`;
  return {
    title: translator.exists(titleKey, { ns }) ? translator.t(titleKey, { ns }) : '',
    body: translator.exists(bodyKey, { ns }) ? translator.t(bodyKey, { ns }) : '',
  };
}

/**
 * Pasos que salen de las anclas, en el orden del DOM, con `t('<ns>:help.<nombre>.title|body')`.
 *  - Un ancla visible con texto da un paso sobre su elemento.
 *  - Un ancla oculta con `data-kx-help-fallback` (elemento efímero: barra de lote, de selección...) da un paso
 *    sobre el elemento de reserva con `help.<nombre>.idle.title|body`; sin esos textos, o sin elemento de
 *    reserva visible, se omite. Si otro elemento con el mismo ancla está visible, manda el visible.
 */
export function stepsFromAnchors(translator: HelpTranslator, root: ParentNode = document): ResolvedStep[] {
  const elements = Array.from(root.querySelectorAll(`[${ANCHOR_ATTRIBUTE}]`));
  const shown = new Set<string>();
  for (const element of elements) {
    const anchor = element.getAttribute(ANCHOR_ATTRIBUTE) ?? '';
    const parsed = parseAnchor(anchor);
    if (!parsed || !isVisible(element)) continue;
    const { title, body } = anchorText(translator, parsed.ns, `help.${parsed.name}`);
    if (title !== '' || body !== '') shown.add(anchor);
  }

  const steps: ResolvedStep[] = [];
  const done = new Set<string>();
  for (const element of elements) {
    const anchor = element.getAttribute(ANCHOR_ATTRIBUTE) ?? '';
    const parsed = parseAnchor(anchor);
    if (!parsed || done.has(anchor)) continue;
    if (isVisible(element)) {
      if (!shown.has(anchor)) continue;
      const { title, body } = anchorText(translator, parsed.ns, `help.${parsed.name}`);
      done.add(anchor);
      steps.push({ element, title, body, source: anchor });
      continue;
    }
    const reserve = element.getAttribute(FALLBACK_ATTRIBUTE);
    if (!reserve || shown.has(anchor)) continue;
    const target = resolveTarget(reserve, root);
    if (!target) continue;
    const { title, body } = anchorText(translator, parsed.ns, `help.${parsed.name}.idle`);
    if (title === '' && body === '') continue;
    done.add(anchor);
    steps.push({ element: target, title, body, source: anchor, fallback: true });
  }
  return steps;
}

/**
 * Recorrido completo (D-R36): los pasos de la configuración en su orden y, detrás, las anclas con texto que
 * ningún paso de la configuración cubre, en el orden del documento. Un ancla queda cubierta si algún paso
 * de la configuración resalta su mismo elemento o cita su nombre como `target`. Un paso de reserva (ancla
 * oculta) se compara sólo por nombre: su elemento es el de otra ancla y no lo cubre ni lo duplica.
 */
export function mergeSteps(configSteps: readonly ResolvedStep[], anchorSteps: readonly ResolvedStep[]): ResolvedStep[] {
  const elements = new Set<Element>();
  const names = new Set<string>();
  for (const step of configSteps) {
    if (step.element) elements.add(step.element);
    names.add(step.source);
  }
  const merged = [...configSteps];
  for (const step of anchorSteps) {
    if (names.has(step.source)) continue;
    if (!step.fallback && step.element && elements.has(step.element)) continue;
    names.add(step.source);
    if (!step.fallback && step.element) elements.add(step.element);
    merged.push(step);
  }
  return merged;
}
