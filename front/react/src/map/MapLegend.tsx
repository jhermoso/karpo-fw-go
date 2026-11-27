import { useId, type Ref } from 'react';
import type { LegendItem } from './aggregate';
import { useMapT } from './i18n';

export type { LegendItem } from './aggregate';

export interface MapLegendProps {
  title: string;
  items: readonly LegendItem[];
  /** El elemento de la leyenda: el encuadre del mapa le deja su sitio (M-3). */
  figureRef?: Ref<HTMLElement>;
}

/**
 * Leyenda del mapa. Es texto: cada color lleva su intervalo escrito y el cuadro de color es
 * decorativo (`aria-hidden`), así que no depende de ver el color. «Sin dato» lleva además la trama de
 * rayas que se pinta en el mapa (M-2): el color por sí solo no la distingue del primer paso.
 */
export function MapLegend({ title, items, figureRef }: MapLegendProps) {
  const { t } = useMapT();
  const id = useId();
  return (
    <figure ref={figureRef} data-kx-part="legend" data-kx-help="map.legend" aria-labelledby={id}>
      <figcaption id={id}>{title}</figcaption>
      <ul aria-label={t('legend.label')}>
        {items.map((item) => (
          <li key={item.step}>
            <span data-kx-part="swatch" data-kx-step={item.step} aria-hidden="true" />
            <bdi>{item.text}</bdi>
          </li>
        ))}
        <li>
          <span data-kx-part="swatch" data-kx-step="none" data-kx-pattern="hatch" aria-hidden="true" />
          {t('legend.noData')}
        </li>
      </ul>
    </figure>
  );
}
