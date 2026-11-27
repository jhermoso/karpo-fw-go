// Rejilla hija de la ficha (`detail.childGrids[]`): tabla de sólo lectura con las filas que devuelve el
// endpoint del hijo. Si el endpoint lleva `{id}` no se añade `?foreignKey=` (A1 §5 punto 12): la
// ruta ya filtra.
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { describeApiError, isApiError, useEntityApi } from '../api';
import { humanize, useText } from '../config';
import type { ChildGridDef, EntityConfig, Row } from '../contracts';
import { FieldView } from '../fields';
import { Action, Feedback, Skeleton } from '../ui';

/** Filas que se pintan como máximo (la tabla no se virtualiza: los hijos de una ficha son listas cortas). */
export const CHILD_ROW_LIMIT = 500;

export interface ChildGridTableProps {
  config: EntityConfig;
  grid: ChildGridDef;
  row: Row;
}

export function ChildGridTable({ config, grid, row }: ChildGridTableProps) {
  const { t } = useTranslation(['detail', 'errors']);
  const resolve = useText();
  const api = useEntityApi();
  const query = useQuery({
    queryKey: ['entity', config.id, 'child', row.id, grid.id],
    queryFn: ({ signal }) => api.childList(grid.endpoint, row, signal),
  });
  const title = resolve(grid.title);

  if (query.isPending) {
    return <Skeleton lines={4} label={t('detail:childGrid.loading', { title })} />;
  }
  if (query.isError) {
    const text = isApiError(query.error) ? describeApiError(query.error) : { key: 'errors:server', fallback: '' };
    return (
      <Feedback tone="error" role="alert">
        <span data-kx-part="text">{t(text.key, { defaultValue: text.fallback })}</span>
        <Action kind="secondary" icon="refresh" onClick={() => void query.refetch()}>
          {t('detail:record.retry')}
        </Action>
      </Feedback>
    );
  }

  const rows = query.data;
  if (rows.length === 0) {
    return (
      <p data-th="collection-empty">
        {grid.emptyMessage ? resolve(grid.emptyMessage) : t('detail:childGrid.empty', { title })}
      </p>
    );
  }
  const shown = rows.slice(0, CHILD_ROW_LIMIT);
  return (
    <div data-kx-part="child-grid">
      <table data-th="collection" role="table" aria-label={title} aria-rowcount={rows.length + 1}>
        <thead>
          <tr role="row">
            {grid.columns.map((column) => (
              <th key={column.field} scope="col" role="columnheader">
                {resolve(column.label) || humanize(column.field)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {shown.map((child, index) => (
            <tr key={child.id || index} data-th="record" role="row" aria-rowindex={index + 2}>
              {grid.columns.map((column) => (
                <td key={column.field} role="cell">
                  <FieldView value={child[column.field]} def={column} row={child} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length > shown.length ? (
        <p data-th="hint">{t('detail:childGrid.truncated', { shown: shown.length, total: rows.length })}</p>
      ) : null}
    </div>
  );
}
