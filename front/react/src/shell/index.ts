// Marco de la aplicación (T6): KarpoApp, rutas, menú, barra superior, paleta de órdenes y atajos.
export { KarpoApp, createKarpoQueryClient } from './KarpoApp';
export type { KarpoAppAllProps, KarpoAppExtraProps } from './KarpoApp';
export { publishSelection, clearSelection, useSelection } from './selection';
export { buildEntityTarget, parseEntityRoute, pickView, targetToString } from './route';
export type { BuildContext, BuiltTarget, ParsedLocation, ViewKind } from './route';
export { canReadEntity, canWriteEntity, filterMenu } from './menu';
