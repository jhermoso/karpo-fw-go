// Carga diferida de lo que no hace falta para pintar la primera pantalla (M-15): la ficha de detalle, la paleta
// de órdenes (con cmdk y fuse.js) y los paneles redimensionables. Cada carga se puede adelantar con
// `preload*` (al pasar o enfocar un control que la va a necesitar): el módulo se pide una sola vez.
//
// Textos por demanda (D-R37): cada carga pide, junto al módulo, los espacios de nombres de i18n que sus
// pantallas usan (`ensureNamespaces`), así que ningún componente se monta antes de tener sus textos.
import { lazy, type ComponentType } from 'react';
import type { DetailViewProps } from '../detail';
import { ensureNamespaces } from '../i18n';
import type { PaletteHostProps } from './CommandPalette';
import type { PanelsProps } from './Panels';

type Loader<T> = () => Promise<T>;

function once<T>(load: Loader<T>): Loader<T> {
  let promise: Promise<T> | undefined;
  return () => (promise ??= load());
}

/** Espacios de nombres que usa cada trozo diferido, además de los del marco (`FRAME_NAMESPACES`). */
export const DETAIL_NAMESPACES = ['detail'] as const;
export const PALETTE_NAMESPACES = [] as const;
export const PANELS_NAMESPACES = [] as const;

/** El módulo y sus textos, a la vez: la promesa no se resuelve hasta tener los dos. */
function withTexts<T>(namespaces: readonly string[], load: Loader<T>): Loader<T> {
  return () => Promise.all([load(), ensureNamespaces(namespaces)]).then(([module]) => module);
}

const loadDetail = once(withTexts(DETAIL_NAMESPACES, () => import('../detail')));
const loadPalette = once(withTexts(PALETTE_NAMESPACES, () => import('./CommandPalette')));
const loadPanels = once(withTexts(PANELS_NAMESPACES, () => import('./Panels')));

/** Adelanta la descarga de la ficha de detalle (al pasar o enfocar una fila o «Nuevo»). */
export function preloadDetail(): void {
  void loadDetail();
}

/** Adelanta la descarga de la paleta (al pasar o enfocar su botón y al pulsar Ctrl+K). */
export function preloadPalette(): void {
  void loadPalette();
}

/** Adelanta la descarga de los paneles redimensionables (al entrar en la pantalla de una entidad). */
export function preloadPanels(): void {
  void loadPanels();
}

export const DetailViewLazy = lazy<ComponentType<DetailViewProps>>(() =>
  loadDetail().then((module) => ({ default: module.DetailView })),
);

export const PaletteHostLazy = lazy<ComponentType<PaletteHostProps>>(() =>
  loadPalette().then((module) => ({ default: module.PaletteHost })),
);

export const PanelsLazy = lazy<ComponentType<PanelsProps>>(() =>
  loadPanels().then((module) => ({ default: module.Panels })),
);
