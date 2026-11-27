// Conflicto de versión (409): el registro cambió en el servidor mientras se editaba. Diferencias campo a
// campo (lo mío / lo del servidor) y dos salidas: recargar (descartar lo mío) o reaplicar mis cambios.
import { useTranslation } from 'react-i18next';
import { humanize, useText } from '../config';
import type { FieldDef, Row } from '../contracts';
import { FieldView } from '../fields';
import { Action, Badge, Dialog } from '../ui';
import { getIn } from './form-model';

export interface ConflictRow {
  def: FieldDef;
  mine: unknown;
  theirs: unknown;
  /** Lo tocaron los dos (mi valor y el del servidor difieren del original). */
  both: boolean;
  /** Lo cambié yo. */
  changedByMe: boolean;
}

export interface ConflictDialogProps {
  open: boolean;
  rows: readonly ConflictRow[];
  /** Registro del servidor (para dar contexto a los visores). */
  server: Row | undefined;
  busy?: boolean;
  onKeepEditing(): void;
  onReload(): void;
  onReapply(): void;
}

export function ConflictDialog({ open, rows, server, busy, onKeepEditing, onReload, onReapply }: ConflictDialogProps) {
  const { t } = useTranslation('detail');
  const resolve = useText();
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onKeepEditing();
      }}
      title={t('conflict.title')}
      description={t('conflict.description')}
      size="large"
      footer={
        <>
          <Action kind="quiet" onClick={onKeepEditing} disabled={busy}>
            {t('conflict.keepEditing')}
          </Action>
          <Action kind="secondary" icon="refresh" onClick={onReload} busy={busy}>
            {t('conflict.reload')}
          </Action>
          <Action kind="primary" icon="redo" onClick={onReapply} busy={busy}>
            {t('conflict.reapply')}
          </Action>
        </>
      }
    >
      {rows.length === 0 ? (
        <p>{t('conflict.noDifferences')}</p>
      ) : (
        <table data-th="collection" role="table" aria-label={t('conflict.table')} data-kx-part="conflict-table">
          <thead>
            <tr role="row">
              <th scope="col" role="columnheader">
                {t('conflict.field')}
              </th>
              <th scope="col" role="columnheader">
                {t('conflict.mine')}
              </th>
              <th scope="col" role="columnheader">
                {t('conflict.theirs')}
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map(({ def, mine, theirs, both, changedByMe }) => (
              <tr key={def.field} data-th="record" role="row" data-kx-conflict={both ? '' : undefined}>
                <th scope="row" role="rowheader">
                  {resolve(def.label) || humanize(def.field)}
                  {both ? (
                    <Badge tone="warning">{t('conflict.both')}</Badge>
                  ) : changedByMe ? null : (
                    <Badge tone="info">{t('conflict.onlyServer')}</Badge>
                  )}
                </th>
                <td role="cell" data-kx-part="mine">
                  <FieldView value={mine} def={def} row={server} />
                </td>
                <td role="cell" data-kx-part="theirs">
                  <FieldView value={theirs} def={def} row={server} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Dialog>
  );
}

/** Diferencias de un conflicto: mis cambios y los del servidor sobre el original. */
export function conflictRows(
  defs: readonly FieldDef[],
  keyOf: (def: FieldDef) => string,
  baseline: Record<string, unknown>,
  mine: Record<string, unknown>,
  server: Row,
  same: (a: unknown, b: unknown) => boolean,
): ConflictRow[] {
  const rows: ConflictRow[] = [];
  for (const def of defs) {
    const key = keyOf(def);
    const before = baseline[key];
    const mineValue = mine[key];
    const theirs = getIn(server, def.field);
    const changedByMe = !same(before, mineValue);
    const changedByServer = !same(before, theirs ?? null);
    if (!changedByMe && !changedByServer) continue;
    if (changedByMe && !changedByServer && same(mineValue, theirs ?? null)) continue;
    rows.push({ def, mine: mineValue, theirs, changedByMe, both: changedByMe && changedByServer && !same(mineValue, theirs ?? null) });
  }
  return rows;
}
