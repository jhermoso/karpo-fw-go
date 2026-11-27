import type { ComponentType } from 'react';
import type { EditorProps } from './editor-props';
import { getFieldType } from './registry';

/**
 * Editor del tipo de campo de `def.kind`. `onChange` se llama con cada cambio (valor ya analizado,
 * o el texto tal cual si no se entiende: `validate` lo marcará `format`); `onCommit(move)` con
 * Enter/Tab/Shift+Tab; `onCancel` con Esc. `variant="form"` deja el Tab en paz.
 */
export function FieldEditor(props: EditorProps) {
  const Editor = getFieldType(props.def.kind).Editor as ComponentType<EditorProps>;
  return <Editor {...props} />;
}
