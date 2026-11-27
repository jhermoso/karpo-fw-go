import type { ComponentPropsWithoutRef } from 'react';
import type { Tone } from '../contracts';

export type ThemeTone = 'error' | 'warning' | 'success' | 'info' | 'neutral';

/** `accent` no existe en el contrato del tema (PT-4): se pinta como `info` y `karpo.ext` lo sobrescribe. */
export function themeToneOf(tone: Tone | undefined): ThemeTone | undefined {
  if (!tone) return undefined;
  return tone === 'accent' ? 'info' : tone;
}

export interface BadgeProps extends ComponentPropsWithoutRef<'span'> {
  tone?: Tone;
}

/** Cápsula tonal: `[data-th="badge"][data-th-tone]` (el tema pone fondo, texto y borde). */
export function Badge({ tone, children, ...rest }: BadgeProps) {
  return (
    <span
      {...rest}
      data-th="badge"
      data-th-tone={themeToneOf(tone) ?? 'neutral'}
      data-kx-tone={tone === 'accent' ? 'accent' : undefined}
    >
      {children}
    </span>
  );
}
