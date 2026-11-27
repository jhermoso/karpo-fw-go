import { useMemo } from 'react';
import { Action } from '../ui';
import type { GeoFeature } from './geo';
import { useMapT } from './i18n';

export interface MapTableProps {
  features: readonly GeoFeature[];
  values: ReadonlyMap<string, number>;
  nameOf(feature: GeoFeature): string;
  formatValue(value: number): string;
  caption: string;
  locale: string;
  /** Hay un nivel inferior al que entrar (si no, no se ofrecen botones). */
  canEnter: boolean;
  onEnter(code: string): void;
  /** Fila enfocada o señalada: se marca en el mapa. */
  onActive(code: string | null): void;
  activeCode: string | null;
}

/**
 * Tabla del nivel que se mira: delimitación y valor. Es la alternativa accesible al mapa y también
 * navega: cada delimitación con detalle es un botón que entra en ella.
 */
export function MapTable(props: MapTableProps) {
  const { t } = useMapT();
  const { features, values, nameOf, locale } = props;
  const rows = useMemo(() => {
    const collator = new Intl.Collator(locale);
    return features
      .map((feature) => ({ feature, name: nameOf(feature), value: values.get(feature.code) }))
      .sort((a, b) => {
        if (a.value !== undefined && b.value !== undefined && a.value !== b.value) return b.value - a.value;
        if ((a.value === undefined) !== (b.value === undefined)) return a.value === undefined ? 1 : -1;
        return collator.compare(a.name, b.name);
      });
  }, [features, values, nameOf, locale]);

  if (rows.length === 0) {
    return (
      <p data-kx-part="empty" role="status">
        {t('table.empty')}
      </p>
    );
  }

  return (
    <div data-kx-part="table-wrap">
      <table data-kx-part="table">
        <caption>{props.caption}</caption>
        <thead>
          <tr>
            <th scope="col">{t('table.name')}</th>
            <th scope="col" data-kx-align="end">
              {t('table.value')}
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map(({ feature, name, value }) => {
            const openable = props.canEnter && feature.hasChildren;
            return (
              <tr
                key={feature.code}
                data-kx-code={feature.code}
                data-kx-active={props.activeCode === feature.code ? 'true' : undefined}
                onMouseEnter={() => props.onActive(feature.code)}
                onMouseLeave={() => props.onActive(null)}
                onFocus={() => props.onActive(feature.code)}
                onBlur={() => props.onActive(null)}
              >
                <th scope="row">
                  {openable ? (
                    <Action kind="quiet" icon="chevron-right" aria-label={t('table.open', { name })} onClick={() => props.onEnter(feature.code)}>
                      <bdi>{name}</bdi>
                    </Action>
                  ) : (
                    <span data-kx-part="name">
                      <bdi>{name}</bdi>
                    </span>
                  )}
                  {props.canEnter && !feature.hasChildren ? <span data-kx-part="note">{t('table.noDetail')}</span> : null}
                </th>
                <td data-kx-align="end">
                  {value !== undefined && value > 0 ? <span data-kx-part="figure">{props.formatValue(value)}</span> : null}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
