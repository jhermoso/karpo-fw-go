// Barras de la cuadrícula: la de cambios, flotante y compacta («● N cambios · Descartar · Guardar»), la de selección con las
// órdenes masivas y el aviso de conflictos 409.
import { forwardRef } from 'react';
import type { CommandDef } from '../contracts';
import { Action, Feedback, Icon, ShortcutKeys } from '../ui';
import type { SaveReport } from './save';
import type { GridEnv } from './types';

interface BatchBarProps {
  env: GridEnv;
  /** Celdas con cambios pendientes. */
  count: number;
  /** Filas con cambios que no se pueden guardar solas (inválidas, con error o en conflicto). */
  blocked: number;
  saving: boolean;
  report: SaveReport | null;
  hidden: boolean;
  onSave(): void;
  onDiscard(): void;
  onDismissReport(): void;
}

export const BatchBar = forwardRef<HTMLDivElement, BatchBarProps>(function BatchBar(
  { env, count, blocked, saving, report, hidden, onSave, onDiscard, onDismissReport },
  ref,
) {
  const { t } = env;
  const visible = !hidden;
  return (
    <div
      ref={ref}
      data-kx-ui="batch-bar"
      data-kx-help="grid.batch-bar"
      data-kx-help-fallback="grid.toolbar"
      data-th="toolbar"
      data-th-elevation="overlay"
      hidden={!visible}
      aria-hidden={visible ? undefined : true}
    >
      {count > 0 ? (
        <>
          <span data-kx-part="dirty-dot" aria-hidden="true" />
          <span data-kx-part="count">{t('batch.count', { count })}</span>
          {blocked > 0 ? (
            <span data-kx-part="blocked" data-kx-tone="warning">
              {t('batch.blocked', { count: blocked })}
            </span>
          ) : null}
          <Action kind="secondary" icon="reset" onClick={onDiscard}>
            {t('batch.discard')}
          </Action>
          <Action kind="primary" icon="save" busy={saving} onClick={onSave}>
            {t('batch.save')}
            <ShortcutKeys keys="Mod+S" />
          </Action>
        </>
      ) : null}
      {report ? (
        <span data-kx-part="report" role="status">
          {t('batch.report', { saved: report.saved, failed: report.failed + report.conflicts + report.skipped })}
          <Action kind="quiet" icon="x" aria-label={t('batch.dismiss')} onClick={onDismissReport} />
        </span>
      ) : null}
    </div>
  );
});

interface SelectionBarProps {
  env: GridEnv;
  count: number;
  commands: CommandDef[];
  busy: boolean;
  onRun(command: CommandDef): void;
  onClear(): void;
}

/** Aparece con filas seleccionadas: las órdenes de servidor de la configuración se aplican a todas. */
export function SelectionBar({ env, count, commands, busy, onRun, onClear }: SelectionBarProps) {
  const { t } = env;
  if (count === 0) return null;
  return (
    <div data-kx-ui="selection-bar" data-th="toolbar" role="group" aria-label={t('selection.label')}>
      <span data-kx-part="count">{t('selection.count', { count })}</span>
      {commands.map((command) => (
        <Action
          key={command.id}
          kind={command.kind === 'delete' || command.tone === 'error' ? 'danger' : 'secondary'}
          icon={command.icon}
          busy={busy}
          onClick={() => onRun(command)}
        >
          {env.text(command.label)}
        </Action>
      ))}
      <Action kind="quiet" icon="x" onClick={onClear}>
        {t('selection.clear')}
      </Action>
    </div>
  );
}

interface ConflictNoticeProps {
  env: GridEnv;
  rows: { id: string; label: string }[];
  busyId: string | null;
  onReapply(rowId: string): void;
}

/** Una fila por cada registro que cambió en el servidor mientras se editaba (409). */
export function ConflictNotice({ env, rows, busyId, onReapply }: ConflictNoticeProps) {
  const { t } = env;
  if (rows.length === 0) return null;
  return (
    <div data-kx-ui="conflicts">
      {rows.map((row) => (
        <Feedback key={row.id} tone="warning" data-kx-part="conflict" data-kx-row-ref={row.id}>
          <Icon name="alert-triangle" />
          <span>{t('conflict.message', { label: row.label })}</span>
          <Action
            kind="secondary"
            icon="refresh"
            busy={busyId === row.id}
            aria-label={t('conflict.reloadFor', { label: row.label })}
            onClick={() => onReapply(row.id)}
          >
            {t('conflict.reload')}
          </Action>
        </Feedback>
      ))}
    </div>
  );
}
