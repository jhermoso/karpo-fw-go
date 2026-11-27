// Editores de los tipos de campo con controles nativos (`input`, `textarea`, `select`,
// casilla) y los ganchos de accesibilidad: `aria-invalid`, `aria-describedby`, `aria-required`.
// El borde, el foco y el estado inválido los viste el tema por atributo (A3 §5).
import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type ComponentType,
  type KeyboardEvent,
} from 'react';
import type { FieldKind } from '../contracts';
import { getCodec, toBoolean } from './codecs';
import { handleEditorKey, type EditorProps } from './editor-props';
import { useFieldText } from './hooks';
import { findOption, isEmpty, sameValue } from './values';

const INPUT_MODE: Partial<Record<FieldKind, 'decimal' | 'numeric' | 'email' | 'tel' | 'url' | 'text'>> = {
  number: 'decimal',
  decimal: 'decimal',
  money: 'decimal',
  percentage: 'decimal',
  date: 'numeric',
  datetime: 'numeric',
  email: 'email',
  phone: 'tel',
  url: 'url',
};

const PLACEHOLDER_KEY: Partial<Record<FieldKind, string>> = {
  number: 'placeholder.number',
  decimal: 'placeholder.decimal',
  money: 'placeholder.decimal',
  percentage: 'placeholder.percentage',
  date: 'placeholder.date',
  datetime: 'placeholder.datetime',
  email: 'placeholder.email',
  phone: 'placeholder.phone',
  url: 'placeholder.url',
};

function useAutoFocus<T extends HTMLElement>(autoFocus: boolean | undefined, select = false) {
  const ref = useRef<T>(null);
  useEffect(() => {
    if (!autoFocus || !ref.current) return;
    ref.current.focus();
    if (select && 'select' in ref.current && typeof ref.current.select === 'function') {
      ref.current.select();
    }
  }, [autoFocus, select]);
  return ref;
}

function TextLikeEditor({ kind, ...props }: EditorProps & { kind: FieldKind }) {
  const { value, def, onChange, onCommit, onCancel, autoFocus, invalid, describedBy, id, ariaLabel } = props;
  const variant = props.variant ?? 'cell';
  const { t, locale, resolve } = useFieldText();
  const codec = getCodec(kind);
  const multiline = kind === 'textarea';
  const ref = useAutoFocus<HTMLInputElement & HTMLTextAreaElement>(autoFocus, true);

  // Texto en curso. Se relee del padre sólo si éste cambia el valor por su cuenta (deshacer…).
  const [draft, setDraft] = useState(() => codec.editText(value, def, locale, resolve));
  const [seen, setSeen] = useState(value);
  const parsed = codec.parse(draft, def, locale, resolve);
  const current = parsed.ok ? parsed.value : draft;
  if (seen !== value) {
    setSeen(value);
    if (!sameValue(value, current)) setDraft(codec.editText(value, def, locale, resolve));
  }

  function onInput(event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>): void {
    const text = event.target.value;
    setDraft(text);
    const result = codec.parse(text, def, locale, resolve);
    // Si no se entiende, sale el texto tal cual y `validate` dará `format`.
    const next = result.ok ? result.value : text;
    setSeen(next);
    onChange(next);
  }

  const shared = {
    id,
    ref,
    value: draft,
    onChange: onInput,
    onKeyDown: (event: KeyboardEvent<HTMLElement>) =>
      handleEditorKey(event, { variant, multiline, onCommit, onCancel }),
    readOnly: def.editable === false,
    placeholder: PLACEHOLDER_KEY[kind] ? t(PLACEHOLDER_KEY[kind]) : undefined,
    autoComplete: 'off',
    'data-kx-ui': 'input',
    'data-kx-align': codec.align === 'end' ? 'end' : undefined,
    'aria-invalid': invalid || undefined,
    'aria-describedby': describedBy,
    'aria-required': def.required || undefined,
    'aria-label': id ? undefined : (ariaLabel ?? resolve(def.label)),
  } as const;

  if (multiline) return <textarea {...shared} rows={3} />;
  return (
    <input
      {...shared}
      type="text"
      inputMode={INPUT_MODE[kind] ?? 'text'}
      spellCheck={kind === 'text' ? undefined : false}
      autoCapitalize={kind === 'text' ? undefined : 'off'}
    />
  );
}

function BooleanEditor(props: EditorProps) {
  const { value, def, onChange, onCommit, onCancel, autoFocus, invalid, describedBy, id, ariaLabel } = props;
  const variant = props.variant ?? 'cell';
  const { resolve } = useFieldText();
  const ref = useAutoFocus<HTMLInputElement>(autoFocus);
  return (
    <input
      ref={ref}
      id={id}
      type="checkbox"
      data-kx-ui="input"
      checked={toBoolean(value) === true}
      disabled={def.editable === false}
      onChange={(event) => onChange(event.target.checked)}
      onKeyDown={(event) => handleEditorKey(event, { variant, onCommit, onCancel })}
      aria-invalid={invalid || undefined}
      aria-describedby={describedBy}
      aria-required={def.required || undefined}
      aria-label={id ? undefined : (ariaLabel ?? resolve(def.label))}
    />
  );
}

function SelectEditor(props: EditorProps) {
  const { value, def, onChange, onCommit, onCancel, autoFocus, invalid, describedBy, id, ariaLabel } = props;
  const variant = props.variant ?? 'cell';
  const { t, locale, resolve } = useFieldText();
  const ref = useAutoFocus<HTMLSelectElement>(autoFocus);
  const options = def.options ?? [];
  const known = isEmpty(value) ? undefined : findOption(options, value, locale, resolve);
  const selected = known ? known.value : isEmpty(value) ? '' : String(value);
  return (
    <select
      ref={ref}
      id={id}
      data-kx-ui="input"
      value={selected}
      disabled={def.editable === false}
      onChange={(event) => onChange(event.target.value === '' ? null : event.target.value)}
      onKeyDown={(event) => handleEditorKey(event, { variant, onCommit, onCancel })}
      aria-invalid={invalid || undefined}
      aria-describedby={describedBy}
      aria-required={def.required || undefined}
      aria-label={id ? undefined : (ariaLabel ?? resolve(def.label))}
    >
      <option value="">{t('select.empty')}</option>
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {resolve(option.label)}
        </option>
      ))}
      {!isEmpty(value) && !known ? <option value={String(value)}>{String(value)}</option> : null}
    </select>
  );
}

function textLike(kind: FieldKind): ComponentType<EditorProps> {
  function KindEditor(props: EditorProps) {
    return <TextLikeEditor {...props} kind={kind} />;
  }
  KindEditor.displayName = `${kind}Editor`;
  return KindEditor;
}

export const simpleEditors = {
  text: textLike('text'),
  textarea: textLike('textarea'),
  number: textLike('number'),
  decimal: textLike('decimal'),
  money: textLike('money'),
  percentage: textLike('percentage'),
  date: textLike('date'),
  datetime: textLike('datetime'),
  email: textLike('email'),
  phone: textLike('phone'),
  url: textLike('url'),
  boolean: BooleanEditor,
  select: SelectEditor,
  badge: SelectEditor,
} as const satisfies Partial<Record<FieldKind, ComponentType<EditorProps>>>;
