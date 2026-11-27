// Contexto compartido entre la cabecera, el cuerpo y los campos de una ficha.
import { createContext, useContext } from 'react';
import type { UseFormReturn } from 'react-hook-form';
import type { EntityConfig, FieldDef, Row, SectionDef } from '../contracts';
import type { FormMode } from './form-model';

export interface SheetContextValue {
  config: EntityConfig;
  /** Modo efectivo (`edit` baja a `read` si no hay permiso o la entidad no se edita). */
  mode: FormMode;
  row: Row | undefined;
  /** Prefijo único de los ids de los controles de esta ficha. */
  uid: string;
  form: UseFormReturn<Record<string, unknown>>;
  /** Valores con los que se abrió la ficha (por clave de formulario). */
  baseline: Record<string, unknown>;
  /** Mensaje traducido del error del campo, o `undefined` si no hay. */
  errorText(key: string, def: FieldDef): string | undefined;
  isFieldVisible(def: FieldDef): boolean;
  isSectionVisible(section: SectionDef): boolean;
  /** Clave del campo que recibe el foco al abrirse. */
  autoFocusKey: string | undefined;
  requestCancel(): void;
  fieldId(key: string): string;
  clearServerError(key: string): void;
}

export const SheetContext = createContext<SheetContextValue | null>(null);

export function useSheet(): SheetContextValue {
  const value = useContext(SheetContext);
  if (!value) throw new Error('Falta el contexto de la ficha (<Sheet>).');
  return value;
}
