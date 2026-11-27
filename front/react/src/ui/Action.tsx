import { useId, type ComponentPropsWithRef, type MouseEvent } from 'react';
import { Icon } from './Icon';
import { Spinner } from './Spinner';

export type ActionKind = 'primary' | 'secondary' | 'ghost' | 'danger' | 'quiet' | 'destructive';
export type ThemeActionKind = 'primary' | 'secondary' | 'destructive' | 'quiet';

export interface ActionProps extends Omit<ComponentPropsWithRef<'button'>, 'onClick' | 'type'> {
  /** `ghost`≡`quiet` y `danger`≡`destructive` (nombres del contrato de Theros: A3 §2.3). */
  kind?: ActionKind;
  /** Motivo por el que no está disponible: `aria-disabled` + `data-th-reason` + `aria-describedby`. */
  disabledReason?: string;
  /** Acción en curso: se atenúa (`aria-busy`) y no admite otro pulso. */
  busy?: boolean;
  /** Nombre de icono (ver `Icon`). Con `children` vacíos pon también `aria-label`. */
  icon?: string;
  /** Por defecto `button`: la acción principal de un formulario es la única `submit`. */
  type?: 'button' | 'submit' | 'reset';
  onClick?(event: MouseEvent<HTMLButtonElement>): void;
}

const KIND_MAP: Record<ActionKind, ThemeActionKind> = {
  primary: 'primary',
  secondary: 'secondary',
  ghost: 'quiet',
  quiet: 'quiet',
  danger: 'destructive',
  destructive: 'destructive',
};

/**
 * Botón con los ganchos de Theros: `data-th="action"` + `data-th-kind`. Una acción no disponible
 * es `button type="button" aria-disabled="true"` (nunca `submit`) con el motivo en
 * `data-th-reason` (lo pinta el tema) y, accesible, en `aria-describedby` (A3 §2.3).
 */
export function Action({
  kind = 'secondary',
  disabledReason,
  busy,
  icon,
  type = 'button',
  onClick,
  children,
  className,
  'aria-describedby': describedBy,
  ...rest
}: ActionProps) {
  const reasonId = useId();
  const unavailable = Boolean(disabledReason);
  const blocked = unavailable || Boolean(busy);
  const describedIds = [describedBy, unavailable ? reasonId : undefined].filter(Boolean).join(' ');

  return (
    <>
      <button
        {...rest}
        className={className}
        type={unavailable ? 'button' : type}
        data-th="action"
        data-th-kind={KIND_MAP[kind]}
        aria-disabled={unavailable || undefined}
        aria-busy={busy || undefined}
        data-th-reason={disabledReason}
        aria-describedby={describedIds || undefined}
        onClick={(event) => {
          if (blocked) {
            event.preventDefault();
            return;
          }
          onClick?.(event);
        }}
      >
        {busy ? <Spinner decorative /> : icon ? <Icon name={icon} /> : null}
        {children}
      </button>
      {unavailable ? (
        <span id={reasonId} hidden>
          {disabledReason}
        </span>
      ) : null}
    </>
  );
}
