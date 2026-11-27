import type { Ref } from 'react';
import { Icon } from '../ui';
import { useMapT } from './i18n';
import type { Crumb } from './nav';

export interface MapBreadcrumbsProps {
  crumbs: readonly Crumb[];
  /** Nombre que se enseña de cada eslabón (países en el idioma activo). */
  nameOf(crumb: Crumb): string;
  onGo(position: number): void;
  /** El eslabón actual: recibe el foco cuando el que lo tenía desaparece al cambiar de nivel. */
  currentRef?: Ref<HTMLSpanElement>;
}

/**
 * Migas de pan: Mundo › España › Comunidad de Madrid › Madrid. El último es la página actual. Un eslabón
 * cuyo nivel se saltó por tener un solo elemento (M-4) nombra el lugar pero no es un botón: no hay nivel
 * al que volver.
 */
export function MapBreadcrumbs({ crumbs, nameOf, onGo, currentRef }: MapBreadcrumbsProps) {
  const { t } = useMapT();
  return (
    <nav aria-label={t('breadcrumbs.label')} data-kx-part="breadcrumbs" data-kx-help="map.breadcrumbs">
      <ol>
        {crumbs.map((crumb, position) => {
          const last = position === crumbs.length - 1;
          return (
            <li key={`${crumb.index}:${crumb.code ?? ''}`}>
              {last ? (
                <span ref={currentRef} aria-current="page" tabIndex={-1}>
                  <bdi>{nameOf(crumb)}</bdi>
                </span>
              ) : crumb.skipped ? (
                <>
                  <span data-kx-part="skipped">
                    <bdi>{nameOf(crumb)}</bdi>
                  </span>
                  <Icon name="chevron-right" />
                </>
              ) : (
                <>
                  <button type="button" onClick={() => onGo(position)}>
                    <bdi>{nameOf(crumb)}</bdi>
                  </button>
                  <Icon name="chevron-right" />
                </>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
