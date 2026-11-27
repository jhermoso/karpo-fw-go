import { useT } from './i18n';

export interface SkeletonProps {
  /** Número de líneas (por defecto 1); la última, si hay varias, es más corta. */
  lines?: number;
  /** Con etiqueta se anuncia como `status`; sin ella es decorativo (`aria-hidden`). */
  label?: string;
  /** Con `true` y sin `label`, anuncia «Cargando contenido». */
  announce?: boolean;
}

/** Esqueleto de carga: `surface.layer.level3` y `motion.duration.long` (PT-6). */
export function Skeleton({ lines = 1, label, announce }: SkeletonProps) {
  const t = useT('ui');
  const name = label ?? (announce ? t('skeleton.loading') : undefined);
  const count = Math.max(1, Math.floor(lines));
  return (
    <span
      data-kx-ui="skeleton"
      role={name ? 'status' : undefined}
      aria-hidden={name ? undefined : true}
      aria-label={name}
    >
      {Array.from({ length: count }, (_, index) => (
        <span key={index} data-kx-part="line" />
      ))}
    </span>
  );
}
