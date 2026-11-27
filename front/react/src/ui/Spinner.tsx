import { useT } from './i18n';

export interface SpinnerProps {
  /** Texto para lectores de pantalla; por defecto «Cargando». */
  label?: string;
  /** Dentro de un control que ya tiene nombre: sin rol ni texto. */
  decorative?: boolean;
}

/**
 * Indicador de carga: `semantic.info.strong` del tema (PT-6; `--kx-spinner-color` lo sustituye
 * cuando `karpo.ext` lo defina). Sólo gira con `prefers-reduced-motion: no-preference`.
 */
export function Spinner({ label, decorative }: SpinnerProps) {
  const t = useT('ui');
  return (
    <span
      data-kx-ui="spinner"
      role={decorative ? undefined : 'status'}
      aria-hidden={decorative ? true : undefined}
    >
      <svg viewBox="0 0 24 24" focusable="false" aria-hidden="true">
        <circle data-kx-part="track" cx="12" cy="12" r="9" fill="none" strokeWidth="3" />
        <path data-kx-part="arc" d="M12 3a9 9 0 0 1 9 9" fill="none" strokeWidth="3" strokeLinecap="round" />
      </svg>
      {decorative ? null : <span data-kx-part="sr-only">{label ?? t('spinner.loading')}</span>}
    </span>
  );
}
