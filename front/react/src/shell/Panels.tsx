// Paneles redimensionables de la pantalla de una entidad: la vista a un lado y, si hay registro abierto,
// su detalle al otro. Es el único sitio que importa `react-resizable-panels`, y se carga de forma diferida
// (`lazy.ts`) para que la librería no pese en el trozo inicial (M-15).
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Group, Panel, Separator, useDefaultLayout } from 'react-resizable-panels';
import { readStored, writeStored } from '../theme/storage';

const layoutStorage = {
  getItem: (key: string) => readStored(key),
  setItem: (key: string, value: string) => writeStored(key, value),
};
const PANEL_IDS = ['list', 'detail'];

/**
 * Reparto por omisión con el detalle abierto: la lista 60 % y la ficha 40 % (D-R34), con la lista siempre por
 * encima del 40 % (un mínimo menor deja la rejilla sin ancho para sus columnas). El usuario lo cambia
 * arrastrando el separador y se recuerda por entidad.
 */
export const LIST_WITH_DETAIL = '60';
export const DETAIL_DEFAULT = '40';
export const LIST_MIN = '40';

export interface PanelsProps {
  /** Entidad: la disposición elegida por el usuario se recuerda por entidad. */
  entity: string;
  /** Vista que se pinta en el panel principal (para `data-kx-view`). */
  view: string;
  list: ReactNode;
  /** Detalle abierto; sin él, la vista ocupa todo el ancho. */
  detail: ReactNode | null;
}

export function Panels({ entity, view, list, detail }: PanelsProps) {
  const { t } = useTranslation('core');
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: `karpo.panel.${entity}`,
    panelIds: PANEL_IDS,
    storage: layoutStorage,
  });
  const detailOpen = detail !== null;
  return (
    <Group
      id={`karpo.group.${entity}`}
      orientation="horizontal"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
      data-kx-part="panels"
    >
      <Panel id="list" minSize={LIST_MIN} defaultSize={detailOpen ? LIST_WITH_DETAIL : '100'}>
        <div data-kx-part="list" data-kx-view={view}>
          {list}
        </div>
      </Panel>
      {detail ? (
        <>
          <Separator data-kx-part="separator" aria-label={t('panel.resize')} />
          <Panel id="detail" minSize="28" defaultSize={DETAIL_DEFAULT}>
            <div data-kx-part="detail">{detail}</div>
          </Panel>
        </>
      ) : null}
    </Group>
  );
}
