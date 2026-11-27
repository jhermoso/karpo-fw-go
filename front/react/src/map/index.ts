// Mapa por niveles (T7): MapLibre con estilo construido en ejecución, geometría propia (GeoJSON o
// PMTiles del propio despliegue) y la tabla del nivel como alternativa accesible.
import './map.css';

export { MapView } from './MapView';
export { aggregateRows, binOf, boundaryCode, chainOf, computeBins, type Bin, type ChainLink } from './aggregate';
export { buildMapStyle, collectUrls, resolveMapColors, type MapColors, type MapDataSource } from './style';
export { mapLayoutOf, navParams, parseNav, type MapLayout, type MapNav } from './nav';
export { pickLabels, type LabelCandidate } from './labels';
export { setMapWorkerUrl } from './engine';
