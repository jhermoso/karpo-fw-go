// Sheet: el formulario de la ficha (react-hook-form + esquema generado de los campos), su cabecera,
// los atajos del ámbito `detail` y las salidas de un guardado (éxito, 400/422 por campo y 409).
import { zodResolver } from '@hookform/resolvers/zod';
import { useCallback, useEffect, useId, useMemo, useRef, useState, type FocusEvent, type KeyboardEvent } from 'react';
import { useForm, useWatch, type FieldValues, type Resolver } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { describeApiError, isApiError, splitFieldErrors, useEntityApi, type ApiError } from '../api';
import { useSession } from '../auth';
import { asExt, evaluateRule, useText } from '../config';
import type { CommandDef, FieldDef, Row, SectionDef } from '../contracts';
import { useFieldErrorMessage } from '../fields/hooks';
import { sameValue } from '../fields/values';
import { ConfirmDialog, Feedback, ShortcutScope, useShortcut, useToast } from '../ui';
import { canWriteEntity } from '../shell/menu';
import { useCommandRunner } from './commands';
import { ConflictDialog, conflictRows, type ConflictRow } from './ConflictDialog';
import {
  buildCreateBody,
  buildFormSchema,
  buildPatch,
  changedFields,
  fieldKey,
  getIn,
  initialValues,
  isWritable,
  setIn,
  uniqueFields,
  type FormMode,
} from './form-model';
import { requestSheetFocus } from './focus-memory';
import { useNeighbors } from './neighbors';
import { SheetBody, computePanels, sectionPanelId } from './SheetBody';
import { SheetContext, type SheetContextValue } from './sheet-context';
import { SheetFooter } from './SheetFooter';
import { SheetHeader } from './SheetHeader';
import { SheetMeta } from './SheetMeta';
import { SheetRelations } from './SheetRelations';
import { UnsavedGuard } from './UnsavedGuard';
import type { DetailViewProps } from './DetailView';

export interface SheetProps extends DetailViewProps {
  /** `edit` baja a `read` si no hay permiso o la entidad no se edita. */
  mode: FormMode;
  sections: SectionDef[];
  row: Row | undefined;
  /** Valores (por clave de formulario) que se vuelven a aplicar sobre la base tras un conflicto. */
  reapply?: Record<string, unknown>;
  tab: string | undefined;
  onTabChange(tab: string): void;
  onSaved(row: Row): void;
  onReload(server: Row): void;
  onReapply(server: Row, mine: Record<string, unknown>): void;
}

interface FeedbackLine {
  id: string;
  text: string;
}

/** Valores del formulario con los campos como objeto anidado (lo que esperan las reglas de visibilidad). */
function nestedValues(defs: readonly FieldDef[], values: Record<string, unknown>): Record<string, unknown> {
  let nested: Record<string, unknown> = {};
  for (const def of defs) nested = setIn(nested, def.field, values[fieldKey(def.field)]);
  return nested;
}

/** Campos visibles y escribibles de las secciones visibles, según los valores dados. */
function activeFieldsFor(
  sections: readonly SectionDef[],
  defs: readonly FieldDef[],
  values: Record<string, unknown>,
  mode: FormMode,
  session: ReturnType<typeof useSession>,
  row: Row | undefined,
): FieldDef[] {
  const ctx = { session, row, form: nestedValues(defs, values) };
  return sections.flatMap((section) => {
    if (section.visibleWhen && !evaluateRule(section.visibleWhen, ctx)) return [];
    return section.fields.filter(
      (def) => isWritable(def, mode) && (!def.visibleWhen || evaluateRule(def.visibleWhen, ctx)),
    );
  });
}

export function Sheet(props: SheetProps) {
  const { config, route, navigate, row, sections, reapply, presentation = 'panel' } = props;
  const { t } = useTranslation(['detail', 'errors']);
  const resolve = useText();
  const api = useEntityApi();
  const session = useSession();
  const toast = useToast();
  const fieldMessage = useFieldErrorMessage();
  const uid = useId();
  const titleId = `${uid}-title`;

  const defs = useMemo(() => uniqueFields(sections.flatMap((section) => section.fields)), [sections]);
  const canWrite = canWriteEntity(config, session);
  const anyWritable = defs.some((def) => isWritable(def, 'edit'));
  const editHidden = props.mode === 'create' ? false : config.editable === false || config.form?.edit === false || !anyWritable;
  const editBlocked = editHidden ? undefined : canWrite ? undefined : t('detail:actions.noPermission');
  const mode: FormMode = props.mode === 'edit' && (editHidden || editBlocked !== undefined) ? 'read' : props.mode;

  // --- formulario ---------------------------------------------------------------------------
  const baseline = useMemo(() => initialValues(defs, row), [defs, row]);
  const defaultValues = useMemo(() => ({ ...baseline, ...reapply }), [baseline, reapply]);

  const modeRef = useRef(mode);
  const rowRef = useRef(row);
  const sessionRef = useRef(session);
  useEffect(() => {
    modeRef.current = mode;
    rowRef.current = row;
    sessionRef.current = session;
  });

  /** Campos que se validan: los visibles y escribibles según lo escrito en este momento (se llama fuera del pintado). */
  const activeFields = useCallback(
    (current: Record<string, unknown>): FieldDef[] =>
      activeFieldsFor(sections, defs, current, modeRef.current, sessionRef.current, rowRef.current),
    [defs, sections],
  );

  const resolver = useCallback<Resolver<FieldValues>>(
    (values, context, options) => {
      const schema = buildFormSchema(activeFields(values as Record<string, unknown>));
      return zodResolver(schema)(values, context, options) as ReturnType<Resolver<FieldValues>>;
    },
    [activeFields],
  );

  const form = useForm<Record<string, unknown>>({
    defaultValues,
    resolver: resolver as Resolver<Record<string, unknown>>,
    mode: 'onTouched',
    shouldFocusError: false,
  });
  const values = useWatch({ control: form.control }) as Record<string, unknown>;
  const writable = useMemo(() => defs.filter((def) => isWritable(def, mode)), [defs, mode]);
  const changes = useMemo(
    () =>
      mode === 'read'
        ? []
        : changedFields(activeFieldsFor(sections, defs, values, mode, session, row), baseline, values),
    [baseline, defs, mode, row, sections, session, values],
  );
  const dirty = changes.length > 0;
  const dirtyRef = useRef(false);
  useEffect(() => {
    dirtyRef.current = dirty;
  });
  const isDirty = useCallback(() => dirtyRef.current, []);

  // Errores del servidor: el texto del servidor sólo es reserva de la clave traducida.
  const serverMessages = useRef<Record<string, string>>({});
  const errors = form.formState.errors;
  const errorText = useCallback(
    (key: string, def: FieldDef): string | undefined => {
      const error = errors[key];
      if (!error) return undefined;
      const code = typeof error.message === 'string' && error.message !== '' ? error.message : 'invalid';
      if (error.type === 'server') {
        return t(`errors:${code}`, { defaultValue: serverMessages.current[key] ?? code });
      }
      return fieldMessage(code, def);
    },
    [errors, fieldMessage, t],
  );
  const clearServerError = useCallback(
    (key: string) => {
      if (form.getFieldState(key).error?.type === 'server') form.clearErrors(key);
    },
    [form],
  );

  // --- visibilidad ---------------------------------------------------------------------------
  const nested = useMemo(() => (mode === 'read' ? undefined : nestedValues(defs, values)), [defs, mode, values]);
  const ruleContext = useMemo(() => ({ session, row, form: nested }), [session, row, nested]);
  const isFieldVisible = useCallback(
    (def: FieldDef) => !def.visibleWhen || evaluateRule(def.visibleWhen, ruleContext),
    [ruleContext],
  );
  const isSectionVisible = useCallback(
    (section: SectionDef) => !section.visibleWhen || evaluateRule(section.visibleWhen, ruleContext),
    [ruleContext],
  );

  // --- pestañas y foco inicial ---------------------------------------------------------------
  const [feedback, setFeedback] = useState<FeedbackLine[]>([]);
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState<{ server: Row; rows: ConflictRow[]; mine: Record<string, unknown> } | null>(null);
  const [resolvingConflict, setResolvingConflict] = useState(false);
  const [discardOpen, setDiscardOpen] = useState(false);

  // Radix no devuelve el foco si el diálogo no tiene disparador (confirmaciones, conflicto): se recuerda
  // el último elemento enfocado dentro de la ficha y se devuelve al cerrar el diálogo sin salir de ella.
  const sectionRef = useRef<HTMLElement>(null);
  const lastFocus = useRef<HTMLElement | null>(null);
  const rememberFocus = useCallback((event: FocusEvent<HTMLElement>) => {
    if (event.target instanceof HTMLElement && sectionRef.current?.contains(event.target)) {
      lastFocus.current = event.target;
    }
  }, []);
  const returnFocus = useCallback(() => {
    const target = lastFocus.current;
    if (target?.isConnected) setTimeout(() => target.focus(), 0);
  }, []);
  const [localTab, setLocalTab] = useState(props.tab);
  const activeTab = localTab;

  const fieldId = useCallback((key: string) => `${uid}-f-${key}`, [uid]);

  // --- cancelar --------------------------------------------------------------------------------
  const exitEdit = useCallback(() => {
    if (mode === 'create') {
      navigate({ entity: config.id });
      return;
    }
    requestSheetFocus();
    if (row) navigate({ entity: config.id, id: row.id, mode: 'read' });
  }, [config.id, mode, navigate, row]);

  const discardAndExit = useCallback(() => {
    dirtyRef.current = false;
    form.reset(defaultValues);
    exitEdit();
  }, [defaultValues, exitEdit, form]);

  const requestCancel = useCallback(() => {
    if (mode === 'read') return;
    if (dirtyRef.current) setDiscardOpen(true);
    else exitEdit();
  }, [exitEdit, mode]);

  // --- guardar ---------------------------------------------------------------------------------
  const focusField = useCallback(
    (keys: readonly string[]) => {
      const first = defs.find((def) => keys.includes(fieldKey(def.field)));
      if (!first) return;
      const section = sections.find((candidate) => candidate.fields.some((field) => field.field === first.field));
      if (section) {
        setLocalTab(sectionPanelId(section));
        props.onTabChange(sectionPanelId(section));
      }
      // La pestaña se muestra en el siguiente fotograma.
      setTimeout(() => document.getElementById(fieldId(fieldKey(first.field)))?.focus(), 0);
    },
    [defs, fieldId, props, sections],
  );

  const applyApiError = useCallback(
    (error: ApiError) => {
      const { matched, unmatched } = splitFieldErrors(
        error,
        defs.map((def) => def.field),
      );
      const lines: FeedbackLine[] = [];
      const keys: string[] = [];
      for (const [field, list] of Object.entries(matched)) {
        const first = list[0];
        if (!first) continue;
        const key = fieldKey(field);
        keys.push(key);
        serverMessages.current[key] = first.message;
        form.setError(key, { type: 'server', message: first.code });
      }
      for (const item of unmatched) {
        const text = t(`errors:${item.code}`, { defaultValue: item.message || item.code });
        lines.push({ id: `${item.field}:${item.code}`, text: item.field === '_' ? text : `${item.field}: ${text}` });
      }
      if (keys.length === 0 && lines.length === 0) {
        const text = describeApiError(error);
        lines.push({ id: 'general', text: t(text.key, { defaultValue: text.fallback }) });
      }
      if (keys.length > 0) {
        lines.unshift({ id: 'fields', text: t('detail:save.fieldErrors', { n: keys.length }) });
      }
      setFeedback(lines);
      if (keys.length > 0) focusField(keys);
    },
    [defs, focusField, form, t],
  );

  const save = useCallback(async () => {
    if (saving || mode === 'read') return;
    setFeedback([]);
    const active = activeFields(form.getValues());
    const valid = await form.trigger(active.map((def) => fieldKey(def.field)));
    if (!valid) {
      const invalid = Object.keys(form.formState.errors);
      setFeedback([{ id: 'fields', text: t('detail:save.fieldErrors', { n: invalid.length }) }]);
      focusField(invalid);
      return;
    }
    const current = form.getValues();
    setSaving(true);
    try {
      if (mode === 'create') {
        const created = await api.create(config, buildCreateBody(active, current));
        toast.show({ tone: 'success', message: t('detail:save.created', { name: resolve(config.name) }) });
        dirtyRef.current = false;
        props.onSaved(created);
        return;
      }
      if (!row) return;
      const patch = buildPatch(changedFields(active, baseline, current), row);
      if (Object.keys(patch).length === 0) {
        toast.show({ tone: 'info', message: t('detail:save.noChanges') });
        exitEdit();
        return;
      }
      const updated = await api.update(config, row.id, patch, row.version as number);
      toast.show({ tone: 'success', message: t('detail:save.saved') });
      dirtyRef.current = false;
      props.onSaved(updated);
    } catch (error) {
      if (!isApiError(error)) {
        setFeedback([{ id: 'general', text: t('errors:server') }]);
      } else if (error.kind === 'conflict' && row) {
        try {
          const server = await api.get(config, row.id);
          const writableActive = active.filter((def) => isWritable(def, 'edit'));
          setConflict({
            server,
            mine: current,
            rows: conflictRows(writableActive, (def) => fieldKey(def.field), baseline, current, server, sameValue),
          });
        } catch (reload) {
          const text = describeApiError(isApiError(reload) ? reload : error);
          setFeedback([{ id: 'conflict', text: t(text.key, { defaultValue: text.fallback }) }]);
        }
      } else {
        applyApiError(error);
      }
    } finally {
      setSaving(false);
    }
  }, [activeFields, api, applyApiError, baseline, config, exitEdit, focusField, form, mode, props, resolve, row, saving, t, toast]);

  const reloadFromServer = useCallback(() => {
    if (!conflict) return;
    setResolvingConflict(true);
    dirtyRef.current = false;
    toast.show({ tone: 'info', message: t('detail:conflict.reloaded') });
    props.onReload(conflict.server);
  }, [conflict, props, t, toast]);

  const reapplyMine = useCallback(() => {
    if (!conflict) return;
    setResolvingConflict(true);
    const mine: Record<string, unknown> = {};
    for (const change of changedFields(writable, baseline, conflict.mine)) mine[change.key] = change.value;
    toast.show({ tone: 'info', message: t('detail:conflict.reapplied') });
    props.onReapply(conflict.server, mine);
  }, [baseline, conflict, props, t, toast, writable]);

  // --- órdenes (cabecera) ---------------------------------------------------------------------
  const runner = useCommandRunner(config, {
    navigate,
    onDialogClosed: returnFocus,
    onDone: (cmd, _row, outcome) => {
      if (cmd.kind === 'delete' && outcome.status === 'done') navigate({ entity: config.id });
    },
  });
  const headerCommands = useMemo<CommandDef[]>(() => {
    if (!row || mode !== 'read') return [];
    return config.commands.filter((cmd) => {
      if (!cmd.surfaces.includes('detail-header') || cmd.kind === 'view' || cmd.kind === 'edit') return false;
      if (!canWrite) return false;
      return !cmd.visibleWhen || evaluateRule(cmd.visibleWhen, { session, row });
    });
  }, [canWrite, config.commands, mode, row, session]);

  // --- anterior / siguiente ---------------------------------------------------------------------
  const neighbors = useNeighbors(config.id, row?.id);
  const goTo = useCallback(
    (target: Row | undefined) => {
      if (!target) return;
      if (mode === 'read') requestSheetFocus();
      navigate({ entity: config.id, id: target.id, mode: route.mode === 'edit' ? 'edit' : 'read' });
    },
    [config.id, mode, navigate, route.mode],
  );

  // --- atajos ------------------------------------------------------------------------------------
  const startEdit = useCallback(() => {
    if (row && !editHidden && editBlocked === undefined) navigate({ entity: config.id, id: row.id, mode: 'edit' });
  }, [config.id, editBlocked, editHidden, navigate, row]);
  const close = props.onClose ?? (() => navigate({ entity: config.id }));

  useShortcut({
    id: 'detail.save',
    scope: 'detail',
    keys: 'Mod+Enter',
    labelKey: 'detail:shortcuts.save',
    run: () => {
      if (mode !== 'read') void save();
    },
  });
  useShortcut({
    id: 'detail.cancel',
    scope: 'detail',
    keys: 'Escape',
    labelKey: 'detail:shortcuts.cancel',
    run: () => (mode === 'read' ? close() : requestCancel()),
  });
  useShortcut({
    id: 'detail.edit',
    scope: 'detail',
    keys: 'E',
    labelKey: 'detail:shortcuts.edit',
    run: () => {
      if (mode === 'read') startEdit();
    },
  });
  useShortcut({ id: 'detail.prev', scope: 'detail', keys: 'Alt+ArrowUp', labelKey: 'detail:shortcuts.prev', run: () => goTo(neighbors.prev) });
  useShortcut({ id: 'detail.next', scope: 'detail', keys: 'Alt+ArrowDown', labelKey: 'detail:shortcuts.next', run: () => goTo(neighbors.next) });

  // Dentro de un campo el registro de atajos sólo deja pasar las combinaciones con Mod: Alt+↑/↓ se atiende aquí.
  function onKeyDown(event: KeyboardEvent<HTMLElement>): void {
    if (!event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    if (event.key === 'ArrowUp' && neighbors.prev) {
      event.preventDefault();
      goTo(neighbors.prev);
    } else if (event.key === 'ArrowDown' && neighbors.next) {
      event.preventDefault();
      goTo(neighbors.next);
    }
  }

  // --- contexto, título y foco inicial ---------------------------------------------------------------
  const panels = useMemo(
    () => computePanels({ mode, row }, sections, config.detail.childGrids, { isFieldVisible, isSectionVisible }),
    [config.detail.childGrids, isFieldVisible, isSectionVisible, mode, row, sections],
  );
  const activePanel = panels.find((panel) => panel.id === activeTab) ?? panels[0];
  const autoFocusKey = useMemo(() => {
    if (mode === 'read') return undefined;
    const sectionsInPanel = activePanel?.type === 'section' ? [activePanel.section] : [];
    for (const section of sectionsInPanel) {
      const found = section.fields.find((def) => isWritable(def, mode) && isFieldVisible(def));
      if (found) return fieldKey(found.field);
    }
    return undefined;
  }, [activePanel, isFieldVisible, mode]);

  const context = useMemo<SheetContextValue>(
    () => ({
      config,
      mode,
      row,
      uid,
      form,
      baseline,
      errorText,
      isFieldVisible,
      isSectionVisible,
      autoFocusKey,
      requestCancel,
      fieldId,
      clearServerError,
    }),
    [autoFocusKey, baseline, clearServerError, config, errorText, fieldId, form, isFieldVisible, isSectionVisible, mode, requestCancel, row, uid],
  );

  const ext = asExt(config);
  const title =
    mode === 'create'
      ? (ext.form?.createTitle ? resolve(ext.form.createTitle) : '') || t('detail:header.new', { name: resolve(config.name) })
      : row
        ? String(getIn(row, config.displayField) ?? row.id)
        : '';

  const reasonNote = props.mode === 'edit' && mode === 'read' && editBlocked ? editBlocked : undefined;

  return (
    <SheetContext.Provider value={context}>
      <ShortcutScope scope="detail">
        <section
          data-th="sheet"
          data-kx-view="detail"
          data-kx-mode={mode}
          data-kx-presentation={presentation}
          aria-labelledby={titleId}
          aria-busy={saving || undefined}
          onKeyDown={onKeyDown}
          onFocus={rememberFocus}
          ref={sectionRef}
        >
          <SheetHeader
            config={config}
            mode={mode}
            row={row}
            title={title}
            titleId={titleId}
            neighbors={neighbors}
            presentation={presentation}
            editBlocked={editBlocked}
            editHidden={editHidden}
            dirty={dirty}
            commands={headerCommands}
            busyCommand={runner.busy}
            onEdit={startEdit}
            onPrev={() => goTo(neighbors.prev)}
            onNext={() => goTo(neighbors.next)}
            onClose={close}
            onToggleExpand={props.onToggleExpand}
            onCommand={(cmd) => row && runner.run(cmd, row)}
          />
          <div data-kx-part="alerts">
            {reasonNote ? (
              <Feedback tone="info" role="status">
                {reasonNote}
              </Feedback>
            ) : null}
            {feedback.length > 0 ? (
              <Feedback tone="error" role="alert" data-kx-part="sheet-feedback">
                {feedback.length === 1 ? (
                  <span data-kx-part="text">{feedback[0]?.text}</span>
                ) : (
                  <ul data-kx-part="feedback-list">
                    {feedback.map((line) => (
                      <li key={line.id}>{line.text}</li>
                    ))}
                  </ul>
                )}
              </Feedback>
            ) : null}
          </div>
          <div data-kx-part="body">
            <SheetBody
              panels={panels}
              tab={activeTab}
              onTabChange={(next) => {
                setLocalTab(next);
                props.onTabChange(next);
              }}
            />
            {row !== undefined && mode !== 'create' ? (
              <div data-kx-part="cards">
                <SheetRelations config={config} row={row} fields={defs} navigate={navigate} />
                <SheetMeta row={row} />
              </div>
            ) : null}
          </div>
          {mode !== 'read' ? (
            <SheetFooter mode={mode} saving={saving} onSave={() => void save()} onCancel={requestCancel} />
          ) : null}
        </section>
        <ConflictDialog
          open={conflict !== null}
          rows={conflict?.rows ?? []}
          server={conflict?.server}
          busy={resolvingConflict}
          onKeepEditing={() => {
            setConflict(null);
            returnFocus();
          }}
          onReload={reloadFromServer}
          onReapply={reapplyMine}
        />
        <ConfirmDialog
          open={discardOpen}
          title={t('detail:discard.title')}
          message={t('detail:discard.cancel')}
          destructive
          confirmLabel={t('detail:discard.confirm')}
          cancelLabel={t('detail:discard.keep')}
          onConfirm={discardAndExit}
          onOpenChange={(open) => {
            setDiscardOpen(open);
            if (!open) returnFocus();
          }}
        />
        <UnsavedGuard isDirty={isDirty} onKeepEditing={returnFocus} />
        {runner.dialogs}
      </ShortcutScope>
    </SheetContext.Provider>
  );
}

