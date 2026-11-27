// Contrato de ganchos de Theros Front (análisis 03 §2.3) y comprobación del DOM.
//
// La lista es CERRADA: un valor fuera de ella no lo viste ningún tema (el front no falla, simplemente
// sale sin estilo), así que se comprueba en las pruebas. `assertTherosHooks(container)` lo usan las
// pruebas de todos los módulos que emiten ganchos (cuadrícula, detalle, primitivas...).

/** Hash del diccionario v3 de Theros Front que este front sabe vestir. */
export const EXPECTED_DICTIONARY_HASH =
  'sha256:2276b82045ae16302c9e07c65a1a4d71dddd3695b4fe490c8f2d4205e65eaeee';

/** Valores cerrados de los atributos `data-th-*` (A3 §2.3). Los de presencia llevan `''`/`'true'`. */
export const THEROS_VALUES = {
  'data-th': [
    'collection',
    'collection-empty',
    'record',
    'sheet',
    'section',
    'field',
    'field-error',
    'reference',
    'badge',
    'action',
    'feedback',
    'nav',
    'nav-item',
    'brand',
    'hint',
    'toolbar',
  ],
  'data-th-kind': ['primary', 'secondary', 'destructive', 'quiet'],
  'data-th-tone': ['error', 'warning', 'success', 'info', 'neutral'],
  'data-th-state': ['inactive', 'new', 'dirty'],
  'data-th-format': ['code', 'amount'],
  'data-th-trend': ['up', 'down', 'flat'],
  'data-th-layer': ['1', '2', '3'],
  'data-th-elevation': ['raised', 'overlay', 'modal'],
  'data-th-required': ['', 'true'],
  'data-th-readonly': ['', 'true'],
  'data-th-drop': ['over'],
  'data-th-mode': ['light', 'dark', 'high-contrast', 'compact', 'spacious'],
} as const;

/** Atributos de texto libre: sólo se exige que no estén vacíos. */
export const THEROS_FREE_TEXT = ['data-th-reason', 'data-th-icon'] as const;

export type TherosAttribute = keyof typeof THEROS_VALUES | (typeof THEROS_FREE_TEXT)[number];

/** Los 17 roles que el tema conoce (A3 §2.3); `toolbar` NO está: es un error (A3 §2.3, Contrato §4). */
export const THEROS_ROLES = [
  'grid',
  'table',
  'row',
  'columnheader',
  'gridcell',
  'cell',
  'status',
  'alert',
  'dialog',
  'tooltip',
  'menu',
  'menuitem',
  'listbox',
  'option',
  'banner',
  'navigation',
  'search',
] as const;

/**
 * Roles ARIA concretos. El tema sólo viste los de `THEROS_ROLES`, pero un componente puede usar otros
 * (tree, tab, switch...): sólo se rechaza lo que no es un rol ARIA (una errata).
 */
export const ARIA_ROLES: readonly string[] = [
  'alert', 'alertdialog', 'application', 'article', 'banner', 'blockquote', 'button', 'caption',
  'cell', 'checkbox', 'code', 'columnheader', 'combobox', 'complementary', 'contentinfo',
  'definition', 'deletion', 'dialog', 'directory', 'document', 'emphasis', 'feed', 'figure', 'form',
  'generic', 'grid', 'gridcell', 'group', 'heading', 'img', 'insertion', 'link', 'list', 'listbox',
  'listitem', 'log', 'main', 'mark', 'marquee', 'math', 'menu', 'menubar', 'menuitem',
  'menuitemcheckbox', 'menuitemradio', 'meter', 'navigation', 'none', 'note', 'option', 'paragraph',
  'presentation', 'progressbar', 'radio', 'radiogroup', 'region', 'row', 'rowgroup', 'rowheader',
  'scrollbar', 'search', 'searchbox', 'separator', 'slider', 'spinbutton', 'status', 'strong',
  'subscript', 'superscript', 'switch', 'tab', 'table', 'tablist', 'tabpanel', 'term', 'textbox',
  'time', 'timer', 'toolbar', 'tooltip', 'tree', 'treegrid', 'treeitem',
];

const ARIA_ATTRIBUTE_NAMES = [
  'activedescendant', 'atomic', 'autocomplete', 'braillelabel', 'brailleroledescription', 'busy',
  'checked', 'colcount', 'colindex', 'colindextext', 'colspan', 'controls', 'current', 'describedby',
  'description', 'details', 'disabled', 'dropeffect', 'errormessage', 'expanded', 'flowto',
  'grabbed', 'haspopup', 'hidden', 'invalid', 'keyshortcuts', 'label', 'labelledby', 'level', 'live',
  'modal', 'multiline', 'multiselectable', 'orientation', 'owns', 'placeholder', 'posinset',
  'pressed', 'readonly', 'relevant', 'required', 'roledescription', 'rowcount', 'rowindex',
  'rowindextext', 'rowspan', 'selected', 'setsize', 'sort', 'valuemax', 'valuemin', 'valuenow',
  'valuetext',
];

/** Atributos ARIA que el tema viste, con los valores que viste (`aria-current`: sólo true y page). */
const ARIA_THEMED_VALUES: Readonly<Record<string, readonly string[]>> = {
  'aria-selected': ['true', 'false'],
  'aria-invalid': ['true', 'false'],
  'aria-disabled': ['true', 'false'],
  'aria-busy': ['true', 'false'],
  'aria-checked': ['true', 'false', 'mixed'],
  'aria-pressed': ['true', 'false', 'mixed'],
  'aria-sort': ['ascending', 'descending', 'none', 'other'],
  'aria-current': ['true', 'false', 'page'],
};

const THEROS_ATTRIBUTE_NAMES: readonly string[] = [...Object.keys(THEROS_VALUES), ...THEROS_FREE_TEXT];

/** Propiedades de disposición que puede llevar el atributo `style` (misma regla que el lint de TSX). */
const LAYOUT_STYLE_PROPERTIES = new Set([
  'transform',
  'translate',
  'inset',
  'top',
  'right',
  'bottom',
  'left',
  'inset-block',
  'inset-block-start',
  'inset-block-end',
  'inset-inline',
  'inset-inline-start',
  'inset-inline-end',
  'width',
  'height',
  'inline-size',
  'block-size',
]);

export interface HookViolation {
  element: Element;
  /** Descripción corta del elemento: `div[data-th="action"]`. */
  where: string;
  attribute: string;
  value: string;
  message: string;
}

export interface HookCheckOptions {
  /** Exige que `role` sea uno de los 17 del tema (por defecto: cualquier rol ARIA concreto). */
  strictRoles?: boolean;
  /** `layout`: el atributo `style` sólo puede llevar variables `--kx-*` y propiedades de disposición. */
  inlineStyle?: 'ignore' | 'layout';
  /** Modos adicionales que define el tema comprobado (valores de `data-th-mode`). */
  extraModes?: readonly string[];
}

function describe(el: Element): string {
  const parts = [el.localName];
  const dataTh = el.getAttribute('data-th');
  if (dataTh !== null) parts.push(`[data-th="${dataTh}"]`);
  const role = el.getAttribute('role');
  if (role !== null) parts.push(`[role="${role}"]`);
  const id = el.getAttribute('id');
  if (id) parts.push(`#${id}`);
  return parts.join('');
}

/** ¿Existe un elemento con ese id en el árbol de `el` (adjunto o no al documento)? */
function idExists(el: Element, id: string): boolean {
  const root = el.getRootNode();
  if ('getElementById' in root) return (root as Document | ShadowRoot).getElementById(id) !== null;
  const top = root as Element;
  return top.id === id || top.querySelector(`[id="${CSS.escape(id)}"]`) !== null;
}

function tokens(value: string): string[] {
  return value.split(/\s+/).filter(Boolean);
}

/** Devuelve TODAS las violaciones del contrato en `container` y sus descendientes. */
export function findTherosHookViolations(
  container: Element,
  options: HookCheckOptions = {},
): HookViolation[] {
  const { strictRoles = false, inlineStyle = 'ignore', extraModes = [] } = options;
  const violations: HookViolation[] = [];
  const elements = [container, ...Array.from(container.querySelectorAll('*'))];

  for (const el of elements) {
    const report = (attribute: string, value: string, message: string) => {
      violations.push({ element: el, where: describe(el), attribute, value, message });
    };

    for (const attr of Array.from(el.attributes)) {
      const name = attr.name;
      const value = attr.value;

      if (name === 'data-th' || name.startsWith('data-th-')) {
        if (!THEROS_ATTRIBUTE_NAMES.includes(name)) {
          report(name, value, `«${name}» no es un gancho del contrato (A3 §2.3)`);
          continue;
        }
        if ((THEROS_FREE_TEXT as readonly string[]).includes(name)) {
          if (value.trim() === '') report(name, value, `«${name}» no puede estar vacío`);
          continue;
        }
        const allowed: readonly string[] = THEROS_VALUES[name as keyof typeof THEROS_VALUES];
        if (name === 'data-th-state' || name === 'data-th-mode') {
          const valid = name === 'data-th-mode' ? [...allowed, ...extraModes] : allowed;
          const list = tokens(value);
          if (name === 'data-th-state' && list.length === 0) {
            report(name, value, 'data-th-state vacío: omite el atributo');
          }
          for (const token of list) {
            if (!valid.includes(token)) {
              report(name, value, `«${token}» no es un valor de ${name} (${valid.join(', ')})`);
            }
          }
          continue;
        }
        if (!allowed.includes(value)) {
          report(name, value, `«${value}» no es un valor de ${name} (${allowed.join(', ')})`);
        }
        continue;
      }

      if (name.startsWith('aria-')) {
        const bare = name.slice(5);
        if (!ARIA_ATTRIBUTE_NAMES.includes(bare)) {
          report(name, value, `«${name}» no es un atributo ARIA`);
          continue;
        }
        const themed = ARIA_THEMED_VALUES[name];
        if (themed && !themed.includes(value)) {
          report(name, value, `«${value}» no es un valor de ${name} que vista el tema (${themed.join(', ')})`);
        }
        continue;
      }

      if (name === 'dir' && !['ltr', 'rtl', 'auto'].includes(value)) {
        report(name, value, '«dir» sólo admite ltr, rtl o auto');
        continue;
      }

      if (name === 'role') {
        for (const role of tokens(value)) {
          if (role === 'toolbar') {
            report(name, value, 'role="toolbar" está prohibido: usa data-th="toolbar" (A3 §2.3)');
          } else if (strictRoles) {
            if (!(THEROS_ROLES as readonly string[]).includes(role)) {
              report(name, value, `«${role}» no es un rol que vista el tema (${THEROS_ROLES.join(', ')})`);
            }
          } else if (!ARIA_ROLES.includes(role)) {
            report(name, value, `«${role}» no es un rol ARIA`);
          }
        }
        continue;
      }

      if (name === 'style' && inlineStyle === 'layout') {
        const style = (el as HTMLElement).style;
        for (let i = 0; i < style.length; i += 1) {
          const property = style.item(i);
          if (!property.startsWith('--kx-') && !LAYOUT_STYLE_PROPERTIES.has(property)) {
            report('style', property, `style="${property}": sólo variables --kx-* y propiedades de disposición`);
          }
        }
      }
    }

    // Reglas de uso (A3 §2.3, Contrato §4 y §7).
    const dataTh = el.getAttribute('data-th');
    const role = el.getAttribute('role');
    if (el.hasAttribute('data-th-kind') && dataTh !== 'action') {
      report('data-th-kind', el.getAttribute('data-th-kind') ?? '', 'data-th-kind sólo va en data-th="action"');
    }
    if (dataTh === 'action' && el.getAttribute('aria-disabled') === 'true') {
      if (el.localName !== 'button' || el.getAttribute('type') !== 'button') {
        report('aria-disabled', 'true', 'una acción no disponible es <button type="button" aria-disabled="true">');
      }
      if (!el.hasAttribute('data-th-reason')) {
        report('data-th-reason', '', 'una acción no disponible lleva data-th-reason (el motivo visible)');
      }
      const describedBy = el.getAttribute('aria-describedby');
      if (!describedBy) {
        report('aria-describedby', '', 'una acción no disponible lleva aria-describedby (el motivo accesible)');
      } else if (!tokens(describedBy).every((id) => idExists(el, id))) {
        report('aria-describedby', describedBy, 'aria-describedby apunta a un elemento que no existe');
      }
    }
    if (dataTh === 'record' && el.localName !== 'tr' && role !== 'row') {
      report('role', role ?? '', 'un data-th="record" que no es <tr> lleva role="row"');
    }
    if (
      dataTh === 'collection' &&
      el.localName !== 'table' &&
      role !== 'grid' &&
      role !== 'table'
    ) {
      report('role', role ?? '', 'un data-th="collection" es <table> o lleva role="grid" o role="table"');
    }
    if (dataTh === 'feedback' && role !== 'status' && role !== 'alert') {
      report('role', role ?? '', 'un data-th="feedback" lleva role="status" o role="alert"');
    }
  }

  return violations;
}

export class TherosHookError extends Error {
  readonly violations: readonly HookViolation[];

  constructor(violations: readonly HookViolation[]) {
    super(
      `El DOM incumple el contrato de ganchos de Theros Front (${violations.length}):\n` +
        violations.map((v) => `  - ${v.where}: ${v.message}`).join('\n'),
    );
    this.name = 'TherosHookError';
    this.violations = violations;
  }
}

/**
 * Comprueba que el DOM sólo usa ganchos del contrato de Theros Front (A3 §2.3). Lanza
 * `TherosHookError` con todas las violaciones. Para pruebas de componentes y de contrato.
 */
export function assertTherosHooks(container: Element, options: HookCheckOptions = {}): void {
  const violations = findTherosHookViolations(container, options);
  if (violations.length > 0) throw new TherosHookError(violations);
}
