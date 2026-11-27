import type { ComponentPropsWithoutRef } from 'react';

export interface ToolbarProps extends Omit<ComponentPropsWithoutRef<'div'>, 'role'> {
  /** Barra de búsqueda: añade `role="search"` (y el nombre accesible `label`). */
  search?: boolean;
  /** Nombre accesible; sólo se emite con `search`, porque un `div` genérico no admite nombre. */
  label?: string;
}

/**
 * Barra de herramientas: `[data-th="toolbar"]`, fila con hueco y separador (el tema lo viste).
 * Nunca lleva `role="toolbar"` (A3 §2.3: rompe el pulso de la acción principal y el rol de grupo).
 */
export function Toolbar({ search, label, children, ...rest }: ToolbarProps) {
  return (
    <div
      {...rest}
      data-th="toolbar"
      role={search ? 'search' : undefined}
      aria-label={search ? label : undefined}
    >
      {children}
    </div>
  );
}
