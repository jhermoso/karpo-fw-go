// DetailView: la ficha de un registro (lectura, edición y alta) para el panel o la página (PLAN §4.10).
// Este componente carga el registro y gobierna lo que sobrevive a un guardado (la base de la que se
// edita, la pestaña, el foco); el formulario y la cabecera viven en `Sheet`.
import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { isApiError, patchEntityCaches, useEntityApi, useEntityRecord, describeApiError } from '../api';
import { useText } from '../config';
import { queryKeys, type EntityConfig, type EntityViewProps, type Row, type SectionDef } from '../contracts';
import { useHelp } from '../help';
import { Action, Feedback, Skeleton } from '../ui';
import { requestSheetFocus } from './focus-memory';
import { Sheet } from './Sheet';
import { useNeighbors } from './neighbors';
import './detail.css';

export interface DetailViewProps extends EntityViewProps {
  /** `panel` junto a la vista (redimensionable) o `page` a pantalla completa. */
  presentation?: 'panel' | 'page';
  /** Cierra el detalle y vuelve a la vista (por omisión, `navigate({ entity })`). */
  onClose?(): void;
  /** Pasa de panel a página y al revés. Sin él no se ofrece el botón. */
  onToggleExpand?(): void;
}

export function DetailView(props: DetailViewProps) {
  if (props.route.id === undefined) return <CreateSheet {...props} />;
  return <RecordSheet {...props} id={props.route.id} />;
}

/**
 * Mientras la ficha no tiene registro (carga, error, alta no permitida) y es la página, no hay cabecera de
 * entidad ni de ficha: el `h1` único de la pantalla (D-R32) lo pone el nombre de la entidad.
 */
function FallbackTitle({ config, presentation }: { config: EntityConfig; presentation: DetailViewProps['presentation'] }) {
  const resolve = useText();
  return presentation === 'page' ? <h1 data-kx-part="fallback-title">{resolve(config.name)}</h1> : null;
}

function sectionsForCreate(config: EntityConfig): SectionDef[] {
  const fromForm = config.form?.sections ?? [];
  return fromForm.length > 0 ? fromForm : config.detail.sections;
}

function CreateSheet(props: DetailViewProps) {
  const { config, navigate } = props;
  const { t } = useTranslation('detail');
  const queryClient = useQueryClient();
  useHelp('detail');
  const allowed = config.editable && config.form !== null && config.form.create;
  const onSaved = useCallback(
    (created: Row) => {
      patchEntityCaches(queryClient, config.id, created);
      void queryClient.invalidateQueries({ queryKey: ['entity', config.id, 'list'] });
      navigate({ entity: config.id, id: created.id, mode: 'read' });
    },
    [config.id, navigate, queryClient],
  );
  if (!allowed) {
    return (
      <section data-th="sheet" data-kx-view="detail">
        <FallbackTitle config={config} presentation={props.presentation} />
        <Feedback tone="warning">{t('create.notAllowed')}</Feedback>
      </section>
    );
  }
  return (
    <Sheet
      key="create"
      {...props}
      mode="create"
      sections={sectionsForCreate(config)}
      row={undefined}
      tab={undefined}
      onTabChange={() => undefined}
      onSaved={onSaved}
      onReload={() => undefined}
      onReapply={() => undefined}
    />
  );
}

function RecordSheet(props: DetailViewProps & { id: string }) {
  const { config, route, navigate, id } = props;
  const { t } = useTranslation(['detail', 'errors']);
  const resolve = useText();
  const api = useEntityApi();
  const queryClient = useQueryClient();
  const query = useEntityRecord(config, id);
  const neighbors = useNeighbors(config.id, id);
  useHelp('detail');

  const editing = route.mode === 'edit';
  const record = query.data;

  // La base de la que se edita no se mueve mientras se edita (otra lectura no pisa lo escrito).
  const [base, setBase] = useState<Row | undefined>(undefined);
  if (record !== undefined && (base === undefined || (!editing && record !== base))) setBase(record);

  const [generation, setGeneration] = useState(0);
  const [reapply, setReapply] = useState<Record<string, unknown> | undefined>(undefined);
  const [tab, setTab] = useState<string | undefined>(undefined);

  // Lee por adelantado el anterior y el siguiente: Alt+↑/↓ cambia de ficha sin esperar.
  const previousId = neighbors.prev?.id;
  const nextId = neighbors.next?.id;
  useEffect(() => {
    for (const neighbor of [previousId, nextId]) {
      if (neighbor === undefined) continue;
      void queryClient.prefetchQuery({
        queryKey: queryKeys.record(config.id, neighbor),
        queryFn: ({ signal }) => api.get(config, neighbor, signal),
        staleTime: 30_000,
      });
    }
  }, [api, config, nextId, previousId, queryClient]);

  const onSaved = useCallback(
    (updated: Row) => {
      patchEntityCaches(queryClient, config.id, updated);
      requestSheetFocus();
      setBase(updated);
      setReapply(undefined);
      setGeneration((n) => n + 1);
      navigate({ entity: config.id, id, mode: 'read' });
    },
    [config.id, id, navigate, queryClient],
  );
  const onReload = useCallback(
    (server: Row) => {
      patchEntityCaches(queryClient, config.id, server);
      setBase(server);
      setReapply(undefined);
      setGeneration((n) => n + 1);
    },
    [config.id, queryClient],
  );
  const onReapply = useCallback(
    (server: Row, mine: Record<string, unknown>) => {
      patchEntityCaches(queryClient, config.id, server);
      setBase(server);
      setReapply(mine);
      setGeneration((n) => n + 1);
    },
    [config.id, queryClient],
  );

  if (base === undefined) {
    return (
      <section data-th="sheet" data-kx-view="detail" aria-busy={query.isPending || undefined}>
        <FallbackTitle config={config} presentation={props.presentation} />
        {query.isError ? (
          <Feedback tone="error" role="alert">
            <span data-kx-part="text">
              {isApiError(query.error) && query.error.kind === 'notfound'
                ? t('detail:record.notFound', { name: resolve(config.name) })
                : (() => {
                    const text = isApiError(query.error) ? describeApiError(query.error) : { key: 'errors:server', fallback: '' };
                    return t(text.key, { defaultValue: text.fallback });
                  })()}
            </span>
            <Action kind="secondary" icon="refresh" onClick={() => void query.refetch()}>
              {t('detail:record.retry')}
            </Action>
          </Feedback>
        ) : (
          <div data-kx-part="loading">
            <Skeleton lines={6} label={t('detail:record.loading')} />
          </div>
        )}
      </section>
    );
  }

  return (
    <Sheet
      key={`${id}:${generation}:${editing ? 'edit' : 'read'}`}
      {...props}
      mode={editing ? 'edit' : 'read'}
      sections={config.detail.sections}
      row={base}
      reapply={reapply}
      tab={tab}
      onTabChange={setTab}
      onSaved={onSaved}
      onReload={onReload}
      onReapply={onReapply}
    />
  );
}
