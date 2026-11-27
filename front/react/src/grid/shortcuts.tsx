// Atajos de la cuadrícula para el panel `Shift+?` (ámbito `grid`). El teclado real lo maneja el
// `onKeyDown` de la rejilla, que corre antes que el registro (el registro respeta `preventDefault`), de
// modo que `run` sólo se ejecuta cuando el evento llega de un hijo que la rejilla no interceptó.
import { matchesEvent, parseKeys, useShortcut, type Platform } from '../ui';

export interface GridShortcut {
  id: string;
  keys: string;
  /** Clave del espacio `grid` (con prefijo, para que el panel del marco la traduzca). */
  labelKey: string;
}

const define = (id: string, keys: string): GridShortcut => ({ id: `grid.${id}`, keys, labelKey: `grid:shortcuts.${id}` });

export const GRID_SHORTCUTS: readonly GridShortcut[] = [
  define('move', 'ArrowDown'),
  define('next', 'Tab'),
  define('prev', 'Shift+Tab'),
  define('home', 'Home'),
  define('end', 'End'),
  define('first', 'Mod+Home'),
  define('last', 'Mod+End'),
  define('pageDown', 'PageDown'),
  define('pageUp', 'PageUp'),
  define('edit', 'Enter'),
  define('editF2', 'F2'),
  define('cancel', 'Escape'),
  define('clear', 'Delete'),
  define('undo', 'Mod+Z'),
  define('redo', 'Mod+Shift+Z'),
  define('save', 'Mod+S'),
  define('copy', 'Mod+C'),
  define('paste', 'Mod+V'),
  define('fill', 'Mod+D'),
  define('select', 'Space'),
  define('range', 'Shift+ArrowDown'),
  define('open', 'Mod+Enter'),
  define('search', '/'),
];

/**
 * ¿Es una de las teclas de la cuadrícula? El `onKeyDown` de la rejilla las corta antes de que lleguen al
 * registro de atajos: éste llama a `preventDefault` en cuanto casa una tecla, y eso bloquearía, por ejemplo,
 * el Tab que debe sacar el foco de la rejilla (B-4) o el Enter de un botón de la celda.
 */
export function matchesGridShortcut(event: KeyboardEvent, platform: Platform): boolean {
  return GRID_SHORTCUTS.some((shortcut) => matchesEvent(parseKeys(shortcut.keys), event, platform));
}

function Registration({ shortcut, run }: { shortcut: GridShortcut; run(): void }) {
  useShortcut({ id: shortcut.id, scope: 'grid', keys: shortcut.keys, labelKey: shortcut.labelKey, run });
  return null;
}

/** Registra los atajos de la cuadrícula mientras esté montada. `handlers` por identificador corto (`undo`…). */
export function GridShortcuts({ handlers }: { handlers: Partial<Record<string, () => void>> }) {
  return (
    <>
      {GRID_SHORTCUTS.map((shortcut) => (
        <Registration
          key={shortcut.id}
          shortcut={shortcut}
          run={handlers[shortcut.id.slice('grid.'.length)] ?? noop}
        />
      ))}
    </>
  );
}

function noop(): void {
  /* la acción real la hace el onKeyDown de la rejilla o el navegador (copiar y pegar) */
}
