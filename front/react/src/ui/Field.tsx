import { cloneElement, isValidElement, useId, type ReactElement, type ReactNode } from 'react';

/** Propiedades que `Field` entrega al control para enlazarlo con su etiqueta, ayuda y error. */
export interface FieldControlProps {
  id: string;
  'aria-describedby'?: string;
  'aria-invalid'?: true;
  'aria-required'?: true;
}

export interface FieldProps {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  required?: boolean;
  readOnly?: boolean;
  /** Un elemento (se le añaden `id` y `aria-*`) o una función que recibe esas propiedades. */
  children?: ReactNode | ((control: FieldControlProps) => ReactNode);
}

/**
 * Campo de ficha: `[data-th="field"]` con `label`, `[data-th="hint"]` y `[data-th="field-error"]`;
 * el control queda enlazado con `aria-describedby` y `aria-invalid` (el tema pone el asterisco de
 * obligatorio, el estado inválido y el de sólo lectura).
 */
export function Field({ id, label, hint, error, required, readOnly, children }: FieldProps) {
  const reactId = useId();
  const hintId = hint ? `${id}-hint-${reactId}` : undefined;
  const errorId = error ? `${id}-error-${reactId}` : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(' ') || undefined;
  const control: FieldControlProps = {
    id,
    'aria-describedby': describedBy,
    'aria-invalid': error ? true : undefined,
    'aria-required': required ? true : undefined,
  };

  let body: ReactNode;
  if (typeof children === 'function') {
    body = children(control);
  } else if (isValidElement(children)) {
    const element = children as ReactElement<Record<string, unknown>>;
    const existing = element.props['aria-describedby'];
    body = cloneElement(element, {
      ...control,
      'aria-describedby': [existing, describedBy].filter(Boolean).join(' ') || undefined,
    });
  } else {
    body = children;
  }

  return (
    <div
      data-th="field"
      data-th-required={required ? '' : undefined}
      data-th-readonly={readOnly ? '' : undefined}
    >
      <label htmlFor={id}>{label}</label>
      {body}
      {hint ? (
        <p data-th="hint" id={hintId}>
          {hint}
        </p>
      ) : null}
      {error ? (
        <p data-th="field-error" data-th-tone="error" id={errorId}>
          {error}
        </p>
      ) : null}
    </div>
  );
}
