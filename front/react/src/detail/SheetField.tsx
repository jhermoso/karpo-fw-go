// Un campo de la ficha: visor en lectura y editor en edición, con etiqueta, ayuda (`hint`) y error.
import { Controller } from 'react-hook-form';
import { humanize, useText } from '../config';
import type { FieldDef } from '../contracts';
import { EmptyValue, FieldEditor, FieldView } from '../fields';
import { isEmpty } from '../fields/values';
import { Field } from '../ui';
import { fieldKey, fieldSpan, getIn, isWritable } from './form-model';
import { useSheet } from './sheet-context';

/** La anchura en columnas (de 12) llega como variable de disposición por CSSOM (D-R12). */
function useSpan(span: number) {
  return (element: HTMLElement | null) => {
    element?.style.setProperty('--kx-span', String(span));
    // En una ficha de ancho medio (28em a 44em) cabe como mucho el reparto en 2 columnas.
    element?.style.setProperty('--kx-span-mid', String(Math.max(span, MID_SPAN)));
  };
}

/** Columnas (de 12) mínimas de un campo cuando la ficha sólo da para 2 columnas. */
const MID_SPAN = 6;

export function SheetField({ def, columns }: { def: FieldDef; columns: number }) {
  const sheet = useSheet();
  const resolve = useText();
  const key = fieldKey(def.field);
  const id = sheet.fieldId(key);
  const label = resolve(def.label) || humanize(def.field);
  const hint = def.hint ? resolve(def.hint) : undefined;
  const writable = isWritable(def, sheet.mode);
  const setSpan = useSpan(fieldSpan(def, columns));

  if (!writable) {
    // Sólo lectura: lo es la ficha (modo lectura), el campo (`editable: false`) o por ser `immutable`.
    // En el alta no se enseñan los campos que no se pueden escribir: no hay nada que leer.
    if (sheet.mode === 'create') return null;
    const value = sheet.row ? getIn(sheet.row, def.field) : undefined;
    const labelId = `${id}-label`;
    const hintId = hint ? `${id}-hint` : undefined;
    return (
      <div data-kx-part="cell" ref={setSpan}>
        <div
          data-th="field"
          role="group"
          aria-labelledby={labelId}
          aria-describedby={hintId}
          data-th-readonly={sheet.mode === 'edit' ? '' : undefined}
          data-kx-field={def.field}
        >
          <label id={labelId}>{label}</label>
          <div data-kx-part="value">
            {isEmpty(value) ? <EmptyValue def={def} /> : <FieldView value={value} def={def} row={sheet.row} />}
          </div>
          {hint ? (
            <p data-th="hint" id={hintId}>
              {hint}
            </p>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <div data-kx-part="cell" ref={setSpan}>
      <Controller
        name={key}
        control={sheet.form.control}
        render={({ field }) => {
          const error = sheet.errorText(key, def);
          return (
            <div onBlur={field.onBlur}>
              <Field id={id} label={label} hint={hint} error={error} required={def.required}>
                {(control) => (
                  <FieldEditor
                    variant="form"
                    def={def}
                    id={control.id}
                    value={field.value}
                    onChange={(value) => {
                      sheet.clearServerError(key);
                      field.onChange(value);
                    }}
                    onCommit={() => undefined}
                    onCancel={sheet.requestCancel}
                    autoFocus={sheet.autoFocusKey === key}
                    invalid={error !== undefined}
                    describedBy={control['aria-describedby']}
                  />
                )}
              </Field>
            </div>
          );
        }}
      />
    </div>
  );
}
