// Editor de selección múltiple: un botón que muestra lo elegido y abre un panel de casillas
// (controles nativos: teclado y lectores de pantalla gratis). Valor: lista de cadenas.
import * as Popover from '@radix-ui/react-popover';
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Badge } from '../ui/Badge';
import { handleEditorKey, stopReactBubbling, type EditorProps } from './editor-props';
import { useFieldText } from './hooks';
import { toStringArray } from './values';

export function MultiSelectEditor(props: EditorProps) {
  const { value, def, onChange, onCommit, onCancel, autoFocus, invalid, describedBy, id } = props;
  const variant = props.variant ?? 'cell';
  const { t, resolve } = useFieldText();
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const selected = toStringArray(value);
  const options = def.options ?? [];
  const fieldLabel = resolve(def.label);

  useEffect(() => {
    if (autoFocus) trigger.current?.focus();
  }, [autoFocus]);

  function toggle(optionValue: string, checked: boolean): void {
    const next = checked
      ? [...selected.filter((item) => item !== optionValue), optionValue]
      : selected.filter((item) => item !== optionValue);
    // Se conserva el orden de las opciones, no el de los pulsos.
    const order = options.map((option) => option.value);
    next.sort((a, b) => order.indexOf(a) - order.indexOf(b));
    onChange(next);
  }

  function onTriggerKeyDown(event: KeyboardEvent<HTMLButtonElement>): void {
    if (event.nativeEvent.isComposing) return;
    if (event.key === 'ArrowDown' || event.key === 'F4') {
      event.preventDefault();
      setOpen(true);
      return;
    }
    // Enter confirma (no abre): el botón lo abre con Espacio o con la flecha.
    handleEditorKey(event, { variant, onCommit, onCancel });
  }

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          ref={trigger}
          id={id}
          type="button"
          data-kx-ui="multiselect"
          aria-haspopup="dialog"
          aria-expanded={open}
          aria-invalid={invalid || undefined}
          aria-describedby={describedBy}
          aria-required={def.required || undefined}
          onKeyDown={onTriggerKeyDown}
        >
          {selected.length === 0 ? (
            <span data-kx-part="placeholder">{t('multiselect.placeholder')}</span>
          ) : (
            <span data-kx-part="chips">
              {selected.map((item) => {
                const option = options.find((candidate) => candidate.value === item);
                return (
                  <Badge key={item} tone={option?.tone}>
                    {option ? resolve(option.label) : item}
                  </Badge>
                );
              })}
            </span>
          )}
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          data-kx-ui="multiselect-popup"
          data-th-elevation="overlay"
          aria-label={t('multiselect.listLabel', { label: fieldLabel })}
          side="bottom"
          align="start"
          sideOffset={0}
          {...stopReactBubbling}
          onKeyDown={(event) => {
            event.stopPropagation();
            if (event.key === 'Enter') {
              event.preventDefault();
              setOpen(false);
            } else if (event.key === 'Tab') {
              event.preventDefault();
              setOpen(false);
              onCommit(event.shiftKey ? 'prev' : 'next');
            }
          }}
        >
          <fieldset data-kx-part="options">
            <legend data-kx-part="sr-only">{fieldLabel}</legend>
            {options.map((option) => (
              <label key={option.value} data-kx-part="option">
                <input
                  type="checkbox"
                  checked={selected.includes(option.value)}
                  onChange={(event) => toggle(option.value, event.target.checked)}
                />
                <span>{resolve(option.label)}</span>
              </label>
            ))}
          </fieldset>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
