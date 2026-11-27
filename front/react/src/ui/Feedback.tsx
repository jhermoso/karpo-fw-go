import type { ComponentPropsWithoutRef } from 'react';
import type { Tone } from '../contracts';
import { themeToneOf } from './Badge';

export interface FeedbackProps extends Omit<ComponentPropsWithoutRef<'div'>, 'role'> {
  tone?: Tone;
  /** Por defecto `alert` para error y `status` para el resto. */
  role?: 'status' | 'alert';
  /** `overlay` para lo que flota (avisos emergentes). */
  elevation?: 'raised' | 'overlay' | 'modal';
}

/** Aviso en línea: `[data-th="feedback"][data-th-tone]` con `role=status|alert` (A3 §2.3). */
export function Feedback({ tone = 'info', role, elevation, children, ...rest }: FeedbackProps) {
  const resolvedRole = role ?? (tone === 'error' ? 'alert' : 'status');
  return (
    <div
      {...rest}
      data-th="feedback"
      data-th-tone={themeToneOf(tone)}
      data-kx-tone={tone === 'accent' ? 'accent' : undefined}
      data-th-elevation={elevation}
      role={resolvedRole}
    >
      {children}
    </div>
  );
}
