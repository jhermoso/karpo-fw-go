// Campo de las páginas de entrada: ganchos de Theros (`data-th=field`, `label`, `data-th=field-error`)
// y `aria-describedby` hacia el error (o la ayuda). Interno del módulo de sesión.
import type { InputHTMLAttributes, Ref } from 'react';
import './auth.css';

export interface AuthFieldProps
  extends Omit<InputHTMLAttributes<HTMLInputElement>, 'id' | 'name' | 'onChange' | 'value'> {
  id: string;
  name: string;
  label: string;
  value: string;
  onChange(value: string): void;
  error?: string;
  hint?: string;
  inputRef?: Ref<HTMLInputElement>;
}

export function AuthField({ id, name, label, value, onChange, error, hint, required, inputRef, ...input }: AuthFieldProps) {
  const errorId = `${id}-error`;
  const hintId = `${id}-hint`;
  const describedBy = [error ? errorId : undefined, hint ? hintId : undefined].filter(Boolean).join(' ') || undefined;
  return (
    <div data-th="field" data-th-required={required ? '' : undefined}>
      <label htmlFor={id}>{label}</label>
      <input
        {...input}
        ref={inputRef}
        id={id}
        name={name}
        value={value}
        required={required}
        aria-required={required ? 'true' : undefined}
        aria-invalid={error ? 'true' : undefined}
        aria-describedby={describedBy}
        onChange={(event) => onChange(event.target.value)}
      />
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
