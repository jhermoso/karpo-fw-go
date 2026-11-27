import type { PropsWithChildren } from 'react';
import type { Tone } from '../contracts';
import { Action } from './Action';
import { Badge } from './Badge';
import { useT } from './i18n';

export interface ChipProps {
  tone?: Tone;
  /** Con `onRemove` aparece el botón de quitar (acción `quiet`). */
  onRemove?(): void;
  /** Nombre accesible del botón de quitar; por defecto sale del texto del chip. */
  removeLabel?: string;
}

function plainText(node: unknown): string | undefined {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) {
    const parts = node.map(plainText);
    return parts.every((part) => part !== undefined) ? parts.join('') : undefined;
  }
  return undefined;
}

/** Etiqueta o filtro activo: `badge` con el botón de quitar `quiet` dentro (A3 §6.4). */
export function Chip({ tone, onRemove, removeLabel, children }: PropsWithChildren<ChipProps>) {
  const t = useT('ui');
  const text = plainText(children);
  const removeName =
    removeLabel ?? (text ? t('chip.remove', { label: text }) : t('chip.removeGeneric'));
  return (
    <Badge tone={tone} data-kx-ui="chip">
      <span data-kx-part="label">{children}</span>
      {onRemove ? <Action kind="quiet" icon="x" aria-label={removeName} onClick={() => onRemove()} /> : null}
    </Badge>
  );
}
