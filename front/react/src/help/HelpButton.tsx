// HelpButton: el botón de ayuda de la barra (PLAN §4.8). Sin HelpProvider no se pinta nada.
import { useContext, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { HelpContext } from '../contracts';
import { Icon } from '../ui/Icon';
import { HelpRuntimeContext } from './runtime';
import { preloadTour } from './tour';

export interface HelpButtonProps {
  /** Pantalla cuyo recorrido se lanza; por defecto, la activa. */
  screen?: string;
}

export function HelpButton({ screen }: HelpButtonProps) {
  const { t } = useTranslation('help');
  const help = useContext(HelpContext);
  const runtime = useContext(HelpRuntimeContext);
  const ref = useRef<HTMLButtonElement>(null);
  if (!help) return null;

  return (
    <button
      ref={ref}
      type="button"
      data-th="action"
      data-th-kind="quiet"
      data-kx-help="help.button"
      data-kx-help-unseen={runtime?.unseen ? '' : undefined}
      aria-label={t('button.label')}
      aria-haspopup="dialog"
      title={t('button.tooltip')}
      onPointerEnter={preloadTour}
      onFocus={preloadTour}
      onClick={() => (runtime ? runtime.start(screen, ref.current) : help.start(screen))}
    >
      <Icon name="help" />
    </button>
  );
}
