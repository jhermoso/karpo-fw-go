import type { ComponentProps } from 'react';
import type { FieldTypeDef } from '../contracts';
import { getFieldType } from './registry';

/** Visor del tipo de campo de `def.kind` (valor con formato del idioma y ganchos de Theros). */
export function FieldView(props: ComponentProps<FieldTypeDef['Viewer']>) {
  const { Viewer } = getFieldType(props.def.kind);
  return <Viewer {...props} />;
}
