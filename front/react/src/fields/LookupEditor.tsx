// Editor de claves ajenas: combobox con búsqueda en el servidor (`EntityApi.list` con el filtro
// `search`), etiqueta resuelta (`displayField`) y valor guardado = `valueField`.
// cmdk pone el teclado de la lista (flechas, Inicio/Fin, Enter) y Radix Popover la posición.
// El `input` es propio (no `Command.Input`, que fija `aria-expanded="true"` y su propio `id`).
import * as Popover from '@radix-ui/react-popover';
import { Command, useCommandState } from 'cmdk';
import {
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
  type Ref,
} from 'react';
import { ApiContext, ManifestContext } from '../contracts';
import { Icon } from '../ui/Icon';
import { handleEditorKey, stopReactBubbling, type EditorProps } from './editor-props';
import { useFieldText, useLookupLabel } from './hooks';
import { searchLookup, type LookupOption } from './lookup-cache';

const SEARCH_DELAY_MS = 180;

interface SearchResult {
  /** Consulta a la que responde. */
  key: string;
  items: LookupOption[];
  error: boolean;
}

interface ComboInputProps {
  open: boolean;
  listId: string | undefined;
  ref?: Ref<HTMLInputElement>;
  value: string;
  placeholder: string;
  ariaLabel: string | undefined;
  id: string | undefined;
  readOnly: boolean;
  invalid: boolean | undefined;
  describedBy: string | undefined;
  required: boolean;
  onInput(event: ChangeEvent<HTMLInputElement>): void;
  onKeyDown(event: KeyboardEvent<HTMLInputElement>): void;
  onFocus(): void;
  onBlur(): void;
}

/** Vive dentro de `Command` para leer la opción activa (`aria-activedescendant`). */
function ComboInput({ ref, ...props }: ComboInputProps) {
  const activeId = useCommandState((state) => state.selectedItemId);
  return (
    <input
      ref={ref}
      id={props.id}
      type="text"
      role="combobox"
      aria-haspopup="listbox"
      aria-autocomplete="list"
      aria-expanded={props.open}
      aria-controls={props.open ? props.listId : undefined}
      aria-activedescendant={props.open ? activeId : undefined}
      aria-invalid={props.invalid || undefined}
      aria-describedby={props.describedBy}
      aria-required={props.required || undefined}
      aria-label={props.ariaLabel}
      data-kx-ui="input"
      autoComplete="off"
      spellCheck={false}
      readOnly={props.readOnly}
      placeholder={props.placeholder}
      value={props.value}
      onChange={props.onInput}
      onKeyDown={props.onKeyDown}
      onFocus={props.onFocus}
      onBlur={props.onBlur}
    />
  );
}

export function LookupEditor(props: EditorProps) {
  const { value, def, onChange, onCommit, onCancel, autoFocus, invalid, describedBy, id, ariaLabel } = props;
  const variant = props.variant ?? 'cell';
  const lookup = def.lookup;
  const api = useContext(ApiContext);
  const manifest = useContext(ManifestContext);
  const { t, resolve } = useFieldText();
  const { label: currentLabel } = useLookupLabel(def, value);

  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [debounced, setDebounced] = useState('');
  const [touched, setTouched] = useState(false);
  const [navigated, setNavigated] = useState(false);
  const [result, setResult] = useState<SearchResult>();
  const [listId, setListId] = useState<string>();
  const inputRef = useRef<HTMLInputElement>(null);
  const anchorRef = useRef<HTMLDivElement>(null);
  // Identificador que cmdk da a la lista, para `aria-controls`.
  const listRef = useCallback((element: HTMLDivElement | null) => {
    if (element?.id) setListId(element.id);
  }, []);

  useEffect(() => {
    if (autoFocus) inputRef.current?.focus();
  }, [autoFocus]);

  // La consulta se envía con una pequeña espera; la primera (lista inicial) sale enseguida.
  useEffect(() => {
    if (debounced === query) return undefined;
    const timer = setTimeout(() => setDebounced(query), SEARCH_DELAY_MS);
    return () => clearTimeout(timer);
  }, [query, debounced]);

  useEffect(() => {
    if (!open || !api || !lookup) return undefined;
    const controller = new AbortController();
    searchLookup(api, manifest, lookup, debounced, controller.signal).then(
      (items) => setResult({ key: debounced, items, error: false }),
      () => {
        if (!controller.signal.aborted) setResult({ key: debounced, items: [], error: true });
      },
    );
    return () => controller.abort();
  }, [open, api, manifest, lookup, debounced]);

  const items = result?.items ?? [];
  const loading = open && Boolean(api && lookup) && (result?.key !== debounced || debounced !== query);
  const failed = open && !loading && Boolean(result?.error);
  const empty = open && !loading && !failed && items.length === 0;
  const fieldLabel = resolve(def.label);

  function closeList(): void {
    setOpen(false);
    setTouched(false);
    setNavigated(false);
    setQuery('');
    setDebounced('');
  }

  function choose(option: LookupOption): void {
    onChange(option.value);
    closeList();
  }

  function onInput(event: ChangeEvent<HTMLInputElement>): void {
    const text = event.target.value;
    setTouched(true);
    setNavigated(false);
    setQuery(text);
    setOpen(true);
    // Borrar todo el texto quita la selección.
    if (text.trim() === '') onChange(null);
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>): void {
    if (event.nativeEvent.isComposing) return;
    switch (event.key) {
      case 'ArrowDown':
      case 'ArrowUp':
        setNavigated(true);
        if (!open) {
          event.preventDefault();
          setOpen(true);
        }
        return; // cmdk mueve la opción activa
      case 'Enter':
        // Con la lista abierta y una búsqueda o un recorrido hechos, Enter elige (lo hace cmdk);
        // si no, confirma el valor actual como cualquier otro editor.
        if (open && items.length > 0 && (touched || navigated)) return;
        handleEditorKey(event, { variant, onCommit, onCancel });
        return;
      case 'Escape':
        if (open) {
          event.preventDefault();
          event.stopPropagation();
          closeList();
          return;
        }
        handleEditorKey(event, { variant, onCommit, onCancel });
        return;
      default:
        handleEditorKey(event, { variant, onCommit, onCancel });
    }
  }

  const shownText = touched ? query : (currentLabel ?? (value === null || value === undefined ? '' : String(value)));

  return (
    <Command shouldFilter={false} loop label={t('lookup.searchLabel', { label: fieldLabel })}>
      <Popover.Root open={open} onOpenChange={(next) => (next ? setOpen(true) : closeList())}>
        <Popover.Anchor asChild>
          <div ref={anchorRef} data-kx-ui="lookup">
            <ComboInput
              ref={inputRef}
              open={open}
              listId={listId}
              id={id}
              ariaLabel={id ? undefined : (ariaLabel ?? fieldLabel)}
              value={shownText}
              placeholder={t('lookup.placeholder')}
              readOnly={def.editable === false}
              invalid={invalid}
              describedBy={describedBy}
              required={def.required}
              onInput={onInput}
              onKeyDown={onKeyDown}
              onFocus={() => setOpen(true)}
              onBlur={closeList}
            />
            <Icon name="chevron-down" />
          </div>
        </Popover.Anchor>
        <Popover.Portal>
          <Popover.Content
            role="presentation"
            data-kx-ui="lookup-popup"
            side="bottom"
            align="start"
            sideOffset={0}
            onOpenAutoFocus={(event) => event.preventDefault()}
            onCloseAutoFocus={(event) => event.preventDefault()}
            onEscapeKeyDown={(event) => event.preventDefault()}
            onFocusOutside={(event) => event.preventDefault()}
            onInteractOutside={(event) => {
              if (anchorRef.current?.contains(event.target as Node)) event.preventDefault();
            }}
            // Mantener el foco en el campo al pulsar una opción.
            onMouseDown={(event) => event.preventDefault()}
            {...stopReactBubbling}
          >
            <Command.List
              ref={listRef}
              data-th-elevation="overlay"
              label={t('lookup.searchLabel', { label: fieldLabel })}
            >
              {items.map((option, index) => (
                <Command.Item
                  key={option.value}
                  value={String(index)}
                  data-kx-chosen={String(value ?? '') === option.value ? 'true' : undefined}
                  onSelect={() => choose(option)}
                >
                  <span data-kx-part="option-label">{option.label}</span>
                  {String(value ?? '') === option.value ? <Icon name="check" /> : null}
                </Command.Item>
              ))}
            </Command.List>
            {loading ? (
              <p data-kx-part="status" role="status">
                {t('lookup.loading')}
              </p>
            ) : null}
            {empty ? (
              <p data-kx-part="status" role="status">
                {t('lookup.empty')}
              </p>
            ) : null}
            {failed ? (
              <p data-kx-part="status" role="alert">
                {t('lookup.error')}
              </p>
            ) : null}
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </Command>
  );
}
