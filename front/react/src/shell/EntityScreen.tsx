// Pantalla de una entidad: su cabecera (título, recuento, control de vista y «Nuevo»), la vista elegida
// (listado, árbol, diagrama o mapa) y, si la ruta trae un registro o `nuevo`, su detalle. Con
// `presentation: 'panel'` el detalle es un panel redimensionable junto a la vista y la vista no se
// desmonta; con `page` (o en una pantalla estrecha, ≤ 64em) ocupa la pantalla.
//
// El detalle y los paneles se cargan de forma diferida (M-15): la primera pantalla no paga el formulario
// ni la librería de paneles, y se adelantan al pasar o enfocar lo que los va a pedir.
import { Suspense, useCallback, useEffect, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useSession } from '../auth';
import { useText } from '../config';
import type { EntityViewProps, ViewStatus } from '../contracts';
import { Skeleton } from '../ui';
import { EntityHeader } from './EntityHeader';
import { useEntityLocation, useEntityNavigate, useEntityRoute, useListForbidden, useManifest, useNarrow } from './hooks';
import { DetailViewLazy, PanelsLazy, preloadDetail, preloadPanels } from './lazy';
import { canReadEntity } from './menu';
import { pickView } from './route';
import { ForbiddenNotice, ForbiddenPage, NotFoundPage } from './pages';
import { VIEW_COMPONENTS, preloadView } from './views';

export function EntityScreen() {
  const manifest = useManifest();
  const session = useSession();
  const { entity, search } = useEntityLocation();
  const route = useEntityRoute(entity);
  const navigate = useEntityNavigate();
  const config = entity ? manifest.entities[entity] : undefined;
  if (!config || !route) return <NotFoundPage />;
  if (!canReadEntity(config, session)) return <ForbiddenPage entity={config.id} />;
  return <EntityContent config={config} route={route} navigate={navigate} asPageParam={search.get('page') === '1'} />;
}

interface ContentProps extends EntityViewProps {
  asPageParam: boolean;
}

function sameStatus(a: ViewStatus, b: ViewStatus): boolean {
  return a.total === b.total && a.matched === b.matched && a.loading === b.loading;
}

function EntityContent({ config, route, navigate, asPageParam }: ContentProps) {
  const { t } = useTranslation('core');
  const resolve = useText();
  const narrow = useNarrow();
  const forbidden = useListForbidden(config.id);
  const view = pickView(config, route);
  const detailOpen = route.id !== undefined || route.mode === 'edit';
  // En una pantalla estrecha no hay sitio para dos paneles: el detalle va a página completa.
  const asPage = config.presentation === 'page' || asPageParam || narrow;

  // Lo que la vista cuenta (recuento y filtro) se olvida al cambiar de entidad o de vista.
  const statusKey = `${config.id}:${view}`;
  const [status, setStatus] = useState<{ key: string; value: ViewStatus } | undefined>(undefined);
  const onStatus = useCallback(
    (value: ViewStatus) =>
      setStatus((current) => (current?.key === statusKey && sameStatus(current.value, value) ? current : { key: statusKey, value })),
    [statusKey],
  );
  const currentStatus = status?.key === statusKey ? status.value : undefined;

  // Los trozos que esta pantalla va a necesitar se piden ya, en paralelo (el de la vista, el de los paneles).
  useEffect(() => {
    preloadPanels();
    preloadView(view);
  }, [view]);

  const viewProps: EntityViewProps = { config, route, navigate, onStatus };
  const detailProps: EntityViewProps = { config, route, navigate };
  const close = useCallback(() => navigate({ entity: config.id }), [config.id, navigate]);
  const expand = useCallback(
    () =>
      navigate({
        entity: config.id,
        id: route.id,
        mode: route.mode,
        params: { ...route.params, page: '1' },
      }),
    [config.id, navigate, route.id, route.mode, route.params],
  );
  const collapse = useCallback(() => {
    // `page` es un parámetro reservado: al navegar sin él, vuelve el panel.
    navigate({ entity: config.id, id: route.id, mode: route.mode, params: route.params });
  }, [config.id, navigate, route.id, route.mode, route.params]);

  const ViewComponent = VIEW_COMPONENTS[view];
  const list: ReactNode = forbidden ? (
    <ForbiddenNotice entity={config.id} />
  ) : (
    <Suspense fallback={<Skeleton lines={6} label={t('app.loadingView')} />}>
      <ViewComponent key={view} {...viewProps} />
    </Suspense>
  );

  const detail = detailOpen ? (
    <Suspense fallback={<Skeleton lines={6} label={t('app.loadingView')} />}>
      <DetailViewLazy
        key={route.id ?? 'new'}
        {...detailProps}
        presentation={asPage ? 'page' : 'panel'}
        onClose={close}
        onToggleExpand={narrow ? undefined : asPage && config.presentation !== 'page' ? collapse : expand}
      />
    </Suspense>
  ) : null;

  const label = resolve(config.namePlural);

  if (asPage && detailOpen) {
    return (
      <div data-kx-screen="entity" data-kx-entity={config.id} data-kx-layout="page" aria-label={label}>
        {detail}
      </div>
    );
  }

  return (
    <div
      data-kx-screen="entity"
      data-kx-entity={config.id}
      data-kx-layout="panel"
      aria-label={label}
      onPointerOver={preloadDetail}
      onFocusCapture={preloadDetail}
    >
      <EntityHeader config={config} route={route} navigate={navigate} status={currentStatus} />
      <div data-kx-part="panels-host">
        <Suspense fallback={<Skeleton lines={6} label={t('app.loadingView')} />}>
          <PanelsLazy entity={config.id} view={view} list={list} detail={detail} />
        </Suspense>
      </div>
    </div>
  );
}
