import * as RadixDialog from '@radix-ui/react-dialog';
import { useId, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { Action } from './Action';
import { Field } from './Field';
import { useT } from './i18n';

export interface DialogProps {
  open: boolean;
  title: string;
  onOpenChange(open: boolean): void;
  /** Texto breve bajo el título; es también la descripción accesible del diálogo. */
  description?: string;
  /** Pie con las acciones (la principal, la primera `submit` si hay formulario). */
  footer?: ReactNode;
  size?: 'small' | 'medium' | 'large';
  children?: ReactNode;
  /** Antes de enfocar el primer control; `preventDefault()` deja el foco donde lo pongas tú. */
  onOpenAutoFocus?(event: Event): void;
}

/**
 * Diálogo modal sobre Radix: `role="dialog"`, foco atrapado y devuelto, Esc, `aria-modal`.
 * El tema viste `[role="dialog"][data-th-elevation]` (fondo, sombra, radio) y el velo.
 */
export function Dialog({
  open,
  title,
  onOpenChange,
  description,
  footer,
  size = 'medium',
  children,
  onOpenAutoFocus,
}: DialogProps) {
  const t = useT('ui');
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay data-kx-ui="dialog-overlay" />
        <RadixDialog.Content
          data-kx-ui="dialog"
          data-kx-size={size}
          data-th-elevation="modal"
          {...(description ? {} : { 'aria-describedby': undefined })}
          onOpenAutoFocus={onOpenAutoFocus}
        >
          <header data-kx-part="header">
            <RadixDialog.Title>{title}</RadixDialog.Title>
            <RadixDialog.Close asChild>
              <Action kind="quiet" icon="x" aria-label={t('dialog.close')} />
            </RadixDialog.Close>
          </header>
          {description ? (
            <RadixDialog.Description data-kx-part="description">{description}</RadixDialog.Description>
          ) : null}
          <div data-kx-part="body">{children}</div>
          {footer ? <footer data-kx-part="footer">{footer}</footer> : null}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}

export interface ConfirmDialogProps extends Omit<DialogProps, 'footer' | 'children' | 'description'> {
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Acción destructiva: botón `destructive` y foco inicial en Cancelar. */
  destructive?: boolean;
  onConfirm(): void;
}

/** Confirmación: la acción principal es el único `submit`; Enter confirma y Esc cancela. */
export function ConfirmDialog({
  message,
  confirmLabel,
  cancelLabel,
  destructive,
  onConfirm,
  onOpenChange,
  ...dialog
}: ConfirmDialogProps) {
  const t = useT('ui');
  function submit(event: FormEvent): void {
    event.preventDefault();
    onConfirm();
    onOpenChange(false);
  }
  return (
    <Dialog {...dialog} onOpenChange={onOpenChange} description={message} size="small">
      <form data-kx-part="form" onSubmit={submit}>
        <div data-kx-part="footer">
          <RadixDialog.Close asChild>
            <Action kind="secondary" autoFocus={destructive}>
              {cancelLabel ?? t('confirm.cancel')}
            </Action>
          </RadixDialog.Close>
          <Action type="submit" kind={destructive ? 'destructive' : 'primary'} autoFocus={!destructive}>
            {confirmLabel ?? t('confirm.confirm')}
          </Action>
        </div>
      </form>
    </Dialog>
  );
}

export interface PromptDialogProps extends Omit<DialogProps, 'footer' | 'children'> {
  /** Etiqueta del campo de motivo. */
  label: string;
  required?: boolean;
  maxLength?: number;
  initialValue?: string;
  submitLabel?: string;
  cancelLabel?: string;
  onSubmit(value: string): void;
}

/** Pide un motivo (por ejemplo, el de un bloqueo): obligatorio o no, con límite de longitud. */
export function PromptDialog({
  label,
  required,
  maxLength,
  initialValue,
  submitLabel,
  cancelLabel,
  onSubmit,
  onOpenChange,
  ...dialog
}: PromptDialogProps) {
  return (
    <Dialog {...dialog} onOpenChange={onOpenChange} size="small">
      <PromptForm
        label={label}
        required={required}
        maxLength={maxLength}
        initialValue={initialValue}
        submitLabel={submitLabel}
        cancelLabel={cancelLabel}
        onSubmit={(value) => {
          onSubmit(value);
          onOpenChange(false);
        }}
      />
    </Dialog>
  );
}

function PromptForm({
  label,
  required,
  maxLength,
  initialValue = '',
  submitLabel,
  cancelLabel,
  onSubmit,
}: Pick<
  PromptDialogProps,
  'label' | 'required' | 'maxLength' | 'initialValue' | 'submitLabel' | 'cancelLabel'
> & { onSubmit(value: string): void }) {
  const t = useT('ui');
  const id = useId();
  const [value, setValue] = useState(initialValue);
  const [attempted, setAttempted] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  const invalid = Boolean(required) && value.trim() === '';

  function submit(event: FormEvent): void {
    event.preventDefault();
    if (invalid) {
      setAttempted(true);
      input.current?.focus();
      return;
    }
    onSubmit(value.trim());
  }

  return (
    <form data-kx-part="form" onSubmit={submit} noValidate>
      <Field
        id={id}
        label={label}
        required={required}
        error={attempted && invalid ? t('prompt.required') : undefined}
        hint={maxLength ? t('prompt.counter', { n: value.length, max: maxLength }) : undefined}
      >
        {(control) => (
          <textarea
            {...control}
            ref={input}
            value={value}
            rows={3}
            maxLength={maxLength}
            autoFocus
            onChange={(event) => setValue(event.target.value)}
          />
        )}
      </Field>
      <div data-kx-part="footer">
        <RadixDialog.Close asChild>
          <Action kind="secondary">{cancelLabel ?? t('prompt.cancel')}</Action>
        </RadixDialog.Close>
        <Action type="submit" kind="primary">
          {submitLabel ?? t('prompt.submit')}
        </Action>
      </div>
    </form>
  );
}
