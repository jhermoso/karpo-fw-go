// Vistas de entidad cargadas de forma diferida: la cuadrícula, el organigrama y el mapa se descargan
// sólo cuando una pantalla las usa (MapLibre y @xyflow pesan lo suyo). Textos por demanda (D-R37): junto al
// módulo de cada vista se piden sus espacios de nombres de i18n (`ensureNamespaces`), de modo que la vista
// no se monta hasta tener sus textos y nunca enseña una clave cruda.
import { lazy, type ComponentType } from 'react';
import type { EntityViewProps } from '../contracts';
import { ensureNamespaces } from '../i18n';

/** El módulo de una vista y sus textos, a la vez. */
function withTexts<T>(namespaces: readonly string[], load: () => Promise<T>): () => Promise<T> {
  return () => Promise.all([load(), ensureNamespaces(namespaces)]).then(([module]) => module);
}

const loadGrid = withTexts(['grid'], () => import('../grid'));
const loadTree = withTexts(['tree'], () => import('../tree'));
const loadMap = withTexts(['map'], () => import('../map'));

export const GridViewLazy = lazy<ComponentType<EntityViewProps>>(() =>
  loadGrid().then((module) => ({ default: module.GridView })),
);
export const TreeViewLazy = lazy<ComponentType<EntityViewProps>>(() =>
  loadTree().then((module) => ({ default: module.TreeView })),
);
export const MapViewLazy = lazy<ComponentType<EntityViewProps>>(() =>
  loadMap().then((module) => ({ default: module.MapView })),
);

/** Adelanta la descarga de la vista (en paralelo con la de los paneles): el módulo se pide una sola vez. */
export function preloadView(view: 'grid' | 'tree' | 'map'): void {
  void { grid: loadGrid, tree: loadTree, map: loadMap }[view]();
}

export const VIEW_COMPONENTS: Record<'grid' | 'tree' | 'map', ComponentType<EntityViewProps>> = {
  grid: GridViewLazy,
  tree: TreeViewLazy,
  map: MapViewLazy,
};
