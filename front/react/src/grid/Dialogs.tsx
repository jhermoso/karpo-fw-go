// Diálogos de la cuadrícula: vista previa del pegado y elección ante un conflicto de edición.
import type { ColumnDef, Row } from '../contracts';
import { getFieldType, useFieldErrorMessage } from '../fields';
import { Action, Dialog } from '../ui';
import type { PastePlan } from './paste';
import type { GridEnv } from './types';

interface PastePreviewProps {
  env: GridEnv;
  plan: PastePlan | null;
  onApply(): void;
  onCancel(): void;
}

/** Vista previa del pegado: la matriz con cada celda correcta, con error o saltada, antes de aplicar. */
export function PastePreview({ env, plan, onApply, onCancel }: PastePreviewProps) {
  const { t } = env;
  const fieldMessage = useFieldErrorMessage();
  const defs = new Map<string, ColumnDef>((plan?.columns ?? []).map((column) => [column.field, column]));
  return (
    <Dialog
      open={plan !== null}
      title={t('paste.title')}
      description={plan ? t('paste.summary', { ok: plan.ok, errors: plan.errors, skipped: plan.skipped }) : undefined}
      size="large"
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
      footer={
        <>
          <Action kind="secondary" onClick={onCancel}>
            {t('paste.cancel')}
          </Action>
          <Action
            kind="primary"
            autoFocus
            disabledReason={plan && plan.ok === 0 ? t('paste.nothing') : undefined}
            onClick={onApply}
          >
            {t('paste.apply', { count: plan?.ok ?? 0 })}
          </Action>
        </>
      }
    >
      {plan ? (
        <>
          <div data-kx-part="paste-scroll">
            <table data-th="collection" data-kx-part="paste-table">
              <caption data-kx-part="sr-only">{t('paste.caption')}</caption>
              <thead>
                <tr>
                  {plan.columns.map((column) => (
                    <th key={column.field} scope="col" role="columnheader">
                      {env.text(column.label)}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {plan.rows.map((cells, rowIndex) => (
                  <tr key={rowIndex} data-th="record">
                    {cells.map((cell) => {
                      const def = defs.get(cell.field);
                      const message =
                        cell.status === 'error' && def
                          ? fieldMessage(cell.code ?? 'format', def)
                          : cell.status === 'skipped'
                            ? t('paste.readonly')
                            : undefined;
                      return (
                        <td key={cell.field} data-kx-paste={cell.status} aria-invalid={cell.status === 'error' ? true : undefined}>
                          <span data-kx-part="paste-text">{cell.text}</span>
                          {message ? (
                            <span data-th="field-error" data-kx-part="issue">
                              {message}
                            </span>
                          ) : null}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {plan.droppedRows > 0 || plan.droppedColumns > 0 ? (
            <p data-th="hint">{t('paste.dropped', { rows: plan.droppedRows, columns: plan.droppedColumns })}</p>
          ) : null}
        </>
      ) : null}
    </Dialog>
  );
}

interface ConflictDialogProps {
  env: GridEnv;
  data: { rowId: string; label: string; common: string[]; mine: Record<string, unknown>; fresh: Row } | null;
  onChoose(choice: 'mine' | 'theirs'): void;
  onCancel(): void;
}

/** Ante un 409 con cambios en los mismos campos: mis valores frente a los del servidor, y elegir. */
export function ConflictDialog({ env, data, onChoose, onCancel }: ConflictDialogProps) {
  const { t } = env;
  const byField = new Map(env.config.columns.map((column) => [column.field, column]));
  const show = (field: string, value: unknown): string => {
    const def = byField.get(field);
    return def ? getFieldType(def.kind).format(value, def, env.locale) || t('conflict.empty') : String(value ?? '');
  };
  return (
    <Dialog
      open={data !== null}
      title={t('conflict.dialog.title')}
      description={data ? t('conflict.dialog.description', { label: data.label }) : undefined}
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
      footer={
        <>
          <Action kind="secondary" onClick={onCancel}>
            {t('conflict.dialog.cancel')}
          </Action>
          <Action kind="secondary" onClick={() => onChoose('theirs')}>
            {t('conflict.dialog.theirs')}
          </Action>
          <Action kind="primary" autoFocus onClick={() => onChoose('mine')}>
            {t('conflict.dialog.mine')}
          </Action>
        </>
      }
    >
      {data ? (
        <table data-th="collection" data-kx-part="conflict-table">
          <thead>
            <tr>
              <th scope="col" role="columnheader">
                {t('conflict.dialog.field')}
              </th>
              <th scope="col" role="columnheader">
                {t('conflict.dialog.mineColumn')}
              </th>
              <th scope="col" role="columnheader">
                {t('conflict.dialog.theirsColumn')}
              </th>
            </tr>
          </thead>
          <tbody>
            {data.common.map((field) => (
              <tr key={field} data-th="record">
                <th scope="row">{byField.get(field) ? env.text(byField.get(field)?.label) : field}</th>
                <td>{show(field, data.mine[field])}</td>
                <td>{show(field, data.fresh[field])}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </Dialog>
  );
}
