// Teclado del listado `role=tree` (WAI-ARIA Authoring Practices: Tree View). Puro: decide qué
// hacer ante una tecla y deja a la vista aplicarlo.
import { fold, hasChildren, siblingsOf, type TreeModel, type VisibleItem } from './model';

export type TreeAction =
  | { type: 'focus'; id: string }
  | { type: 'expand'; id: string }
  | { type: 'collapse'; id: string }
  | { type: 'expand-siblings'; id: string }
  | { type: 'select'; id: string }
  | { type: 'activate'; id: string };

export interface TreeKeyEvent {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
}

/**
 * Acción de una tecla sobre el elemento `currentId`. En `rtl` las flechas laterales se invierten
 * (la derecha pliega y la izquierda despliega). Devuelve `null` si la tecla no es de navegación.
 */
export function treeKeyAction(
  items: readonly VisibleItem[],
  currentId: string | null,
  event: TreeKeyEvent,
  options: { rtl: boolean },
): TreeAction | null {
  if (items.length === 0) return null;
  const index = currentId === null ? -1 : items.findIndex((item) => item.id === currentId);
  const current = index >= 0 ? items[index]! : undefined;
  if (event.ctrlKey || event.metaKey || event.altKey) return null;

  const expandKey = options.rtl ? 'ArrowLeft' : 'ArrowRight';
  const collapseKey = options.rtl ? 'ArrowRight' : 'ArrowLeft';

  switch (event.key) {
    case 'ArrowDown':
      return { type: 'focus', id: items[Math.min(items.length - 1, index + 1)]!.id };
    case 'ArrowUp':
      return { type: 'focus', id: items[Math.max(0, index < 0 ? 0 : index - 1)]!.id };
    case 'Home':
      return { type: 'focus', id: items[0]!.id };
    case 'End':
      return { type: 'focus', id: items[items.length - 1]!.id };
    case expandKey: {
      if (!current || !current.expandable) return null;
      if (!current.expanded) return { type: 'expand', id: current.id };
      const child = items[index + 1];
      return child && child.parentId === current.id ? { type: 'focus', id: child.id } : null;
    }
    case collapseKey: {
      if (!current) return null;
      if (current.expandable && current.expanded) return { type: 'collapse', id: current.id };
      return current.parentId !== null ? { type: 'focus', id: current.parentId } : null;
    }
    case '*':
      return current ? { type: 'expand-siblings', id: current.id } : null;
    case 'Enter':
      return current ? { type: 'activate', id: current.id } : null;
    case ' ':
      return current ? { type: 'select', id: current.id } : null;
    default:
      return null;
  }
}

/** Una tecla imprimible (una sola letra o cifra, sin modificadores), candidata a búsqueda por letra. */
export function isTypeAheadKey(event: TreeKeyEvent): boolean {
  return event.key.length === 1 && event.key !== ' ' && event.key !== '*' && !event.ctrlKey && !event.metaKey && !event.altKey;
}

/**
 * Búsqueda por letra: con una sola letra repetida se avanza entre los que empiezan por ella; con
 * varias, el primero cuyo texto empieza por lo escrito. Empieza después del actual y da la vuelta.
 */
export function typeAheadTarget(
  items: readonly VisibleItem[],
  currentId: string | null,
  buffer: string,
  labelOf: (id: string) => string,
): string | null {
  if (items.length === 0 || buffer === '') return null;
  const needle = fold(buffer);
  const repeated = needle.length > 1 && [...needle].every((ch) => ch === needle[0]);
  const search = repeated ? needle[0]! : needle;
  const start = currentId === null ? 0 : items.findIndex((item) => item.id === currentId);
  // Con varias letras se parte del propio actual (puede ya coincidir); con una repetida, del siguiente.
  const offset = search.length === 1 || repeated ? 1 : 0;
  for (let step = 0; step < items.length; step += 1) {
    const item = items[(Math.max(0, start) + offset + step) % items.length]!;
    if (fold(labelOf(item.id)).startsWith(search)) return item.id;
  }
  return null;
}

/**
 * Teclado del diagrama: padre (↑), primer hijo (↓; en una rama plegada, la despliega), hermanos
 * (← y →; en `rtl` el orden visual de los hermanos va en espejo), Inicio/Fin entre hermanos,
 * `+` y `-` despliegan y pliegan, Intro abre en edición y Espacio selecciona.
 */
export function diagramKeyAction(
  model: TreeModel,
  expanded: ReadonlySet<string>,
  id: string,
  event: TreeKeyEvent,
  options: { rtl: boolean },
): TreeAction | null {
  const node = model.nodes.get(id);
  if (!node || event.ctrlKey || event.metaKey || event.altKey) return null;
  const siblings = siblingsOf(model, id);
  const index = siblings.indexOf(id);
  const open = hasChildren(node) && expanded.has(id);
  const next = options.rtl ? 'ArrowLeft' : 'ArrowRight';
  const previous = options.rtl ? 'ArrowRight' : 'ArrowLeft';
  switch (event.key) {
    case 'ArrowUp':
      return node.parentId !== null ? { type: 'focus', id: node.parentId } : null;
    case 'ArrowDown': {
      if (!hasChildren(node)) return null;
      if (!open) return { type: 'expand', id };
      const first = node.children[0];
      return first ? { type: 'focus', id: first } : null;
    }
    case next: {
      const sibling = siblings[index + 1];
      return sibling ? { type: 'focus', id: sibling } : null;
    }
    case previous: {
      const sibling = siblings[index - 1];
      return sibling ? { type: 'focus', id: sibling } : null;
    }
    case 'Home':
      return siblings[0] && siblings[0] !== id ? { type: 'focus', id: siblings[0] } : null;
    case 'End': {
      const last = siblings[siblings.length - 1];
      return last && last !== id ? { type: 'focus', id: last } : null;
    }
    case '+':
    case '=':
      return hasChildren(node) && !open ? { type: 'expand', id } : null;
    case '-':
    case '_':
      return open ? { type: 'collapse', id } : null;
    case 'Enter':
      return { type: 'activate', id };
    default:
      return null;
  }
}
