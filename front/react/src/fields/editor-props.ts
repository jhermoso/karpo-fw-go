import type { ComponentProps, KeyboardEvent } from 'react';
import type { FieldTypeDef } from '../contracts';

export type CommitMove = 'next' | 'prev' | 'down' | 'up';

/** Propiedades del contrato (`FieldTypeDef.Editor`) más las opcionales de este módulo. */
export type EditorProps = ComponentProps<FieldTypeDef['Editor']> & {
  /**
   * `cell` (por defecto): Enter y Tab confirman y mueven (`onCommit`), y se interceptan.
   * `form`: Tab no se intercepta (el foco sigue el orden natural) y Enter no se bloquea.
   */
  variant?: 'cell' | 'form';
  /** `id` del control cuando una etiqueta `label for` lo nombra. */
  id?: string;
  /** Nombre accesible si no hay `id`; por defecto, la etiqueta del campo. */
  ariaLabel?: string;
};

interface KeyOptions {
  variant: 'cell' | 'form';
  multiline?: boolean;
  onCommit(move?: CommitMove): void;
  onCancel(): void;
}

/**
 * Teclas de edición: Enter → `down` (Shift+Enter → `up`), Tab → `next` (Shift+Tab → `prev`),
 * Esc → cancelar. En un área de texto, Enter escribe un salto y confirma con Ctrl/Cmd+Enter.
 */
export function handleEditorKey(event: KeyboardEvent<HTMLElement>, options: KeyOptions): void {
  if (event.nativeEvent.isComposing) return;
  const { variant, multiline, onCommit, onCancel } = options;
  switch (event.key) {
    case 'Enter': {
      const modified = event.ctrlKey || event.metaKey;
      if (multiline && !modified) return;
      if (variant === 'cell') {
        event.preventDefault();
        event.stopPropagation();
      }
      onCommit(event.shiftKey && !multiline ? 'up' : 'down');
      return;
    }
    case 'Tab':
      if (variant === 'form') return;
      event.preventDefault();
      event.stopPropagation();
      onCommit(event.shiftKey ? 'prev' : 'next');
      return;
    case 'Escape':
      event.preventDefault();
      event.stopPropagation();
      onCancel();
      return;
    default:
  }
}

/** Los eventos de un flotante en portal suben por el árbol de React hasta la cuadrícula: se cortan aquí. */
export const stopReactBubbling = {
  onKeyDown: (event: { stopPropagation(): void }) => event.stopPropagation(),
  onPointerDown: (event: { stopPropagation(): void }) => event.stopPropagation(),
  onClick: (event: { stopPropagation(): void }) => event.stopPropagation(),
  onDoubleClick: (event: { stopPropagation(): void }) => event.stopPropagation(),
};
