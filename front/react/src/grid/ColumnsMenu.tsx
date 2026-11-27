// Menú de columnas: mostrar u ocultar, reordenar (con botones, apto para teclado), fijar al inicio o al
// final y restablecer. El estado vive en `ColumnPrefs` (localStorage por usuario y entidad).
import * as Popover from '@radix-ui/react-popover';
import { useId } from 'react';
import type { ColumnDef } from '../contracts';
import { Action } from '../ui';
import { defaultPrefs, moveColumn, setAutoFit, setHidden, setPinned, type ColumnPrefs } from './prefs';
import type { GridEnv } from './types';

interface ColumnsMenuProps {
  env: GridEnv;
  columns: readonly ColumnDef[];
  prefs: ColumnPrefs;
  /** Columnas que el ajuste al ancho ha ocultado ahora mismo (no están en `prefs.hidden`). */
  fitHidden?: readonly string[];
  onChange(next: ColumnPrefs): void;
}

export function ColumnsMenu({ env, columns, prefs, fitHidden = [], onChange }: ColumnsMenuProps) {
  const { t } = env;
  const squeezed = new Set(fitHidden);
  const idPrefix = useId();
  const byField = new Map(columns.map((column) => [column.field, column]));
  const visibleCount = prefs.order.filter((field) => !prefs.hidden.includes(field)).length;

  return (
    <Popover.Root>
      <Popover.Trigger asChild>
        <Action kind="quiet" icon="columns" data-kx-help="grid.columns">
          {t('columns.button')}
        </Action>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          data-kx-ui="columns-panel"
          data-th-elevation="overlay"
          aria-label={t('columns.title')}
          align="end"
          sideOffset={4}
        >
          <ul data-kx-part="columns-list">
            {prefs.order.map((field, index) => {
              const column = byField.get(field);
              if (!column) return null;
              const label = env.text(column.label);
              const hidden = prefs.hidden.includes(field);
              const pinned = prefs.pinned.start.includes(field) ? 'start' : prefs.pinned.end.includes(field) ? 'end' : false;
              const noRoom = !hidden && squeezed.has(field);
              const noteId = `${idPrefix}-${index}-fit`;
              return (
                <li key={field} data-kx-part="columns-item" data-kx-fit={noRoom ? 'hidden' : undefined}>
                  <label>
                    <input
                      type="checkbox"
                      checked={!hidden}
                      disabled={!hidden && visibleCount <= 1}
                      aria-describedby={noRoom ? noteId : undefined}
                      onChange={(event) => onChange(setHidden(prefs, field, !event.target.checked))}
                    />
                    <span>{label}</span>
                    {noRoom ? (
                      <span id={noteId} data-kx-part="fit-note">
                        {t('columns.noRoom')}
                      </span>
                    ) : null}
                  </label>
                  <Action
                    kind="quiet"
                    icon="arrow-up"
                    aria-label={t('columns.earlier', { label })}
                    disabledReason={index === 0 ? t('columns.firstReason') : undefined}
                    onClick={() => onChange(moveColumn(prefs, field, -1))}
                  />
                  <Action
                    kind="quiet"
                    icon="arrow-down"
                    aria-label={t('columns.later', { label })}
                    disabledReason={index === prefs.order.length - 1 ? t('columns.lastReason') : undefined}
                    onClick={() => onChange(moveColumn(prefs, field, 1))}
                  />
                  <span role="group" aria-label={t('columns.pin', { label })} data-kx-part="pin-group">
                    <Action
                      kind="quiet"
                      aria-label={t('columns.pinStart', { label })}
                      aria-pressed={pinned === 'start'}
                      onClick={() => onChange(setPinned(prefs, field, pinned === 'start' ? false : 'start'))}
                    >
                      {t('columns.pinStartShort')}
                    </Action>
                    <Action
                      kind="quiet"
                      aria-label={t('columns.pinEnd', { label })}
                      aria-pressed={pinned === 'end'}
                      onClick={() => onChange(setPinned(prefs, field, pinned === 'end' ? false : 'end'))}
                    >
                      {t('columns.pinEndShort')}
                    </Action>
                  </span>
                </li>
              );
            })}
          </ul>
          <label data-kx-part="columns-autofit">
            <input type="checkbox" checked={prefs.autoFit !== false} onChange={(event) => onChange(setAutoFit(prefs, event.target.checked))} />
            <span>{t('columns.autoFit')}</span>
          </label>
          <div data-kx-part="columns-footer">
            <Action kind="quiet" icon="reset" onClick={() => onChange(defaultPrefs(columns))}>
              {t('columns.reset')}
            </Action>
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
