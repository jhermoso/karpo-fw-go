import { useContext } from 'react';
import { ThemeContext, isDirectionalIcon } from '../contracts';
import { ICON_PENDING, useIconSnapshot } from './icons';

/** Peso pedido. Los iconos generados llevan `regular` y `fill`; cualquier otro se dibuja como `regular`. */
export type IconWeight = 'regular' | 'fill' | 'bold' | 'light' | 'thin' | 'duotone';

export interface IconProps {
  /** Nombre de la configuración o de la interfaz (`building`, `view`, `edit`, `chevron-right`…). */
  name: string;
  /** Fuerza (o impide) el espejo en RTL; por defecto lo decide el propio icono. */
  mirror?: boolean;
  /** Nombre accesible; sin él el icono es decorativo (`aria-hidden`). */
  label?: string;
  /** Peso a la fuerza; por defecto `fill` si el tema es de iconos rellenos y `regular` si no. */
  weight?: IconWeight;
}

const isDevelopment = (): boolean => {
  try {
    return import.meta.env.DEV === true;
  } catch {
    return false;
  }
};

/** Nombres desconocidos ya avisados: un aviso por nombre, no uno por pintado. */
const warned = new Set<string>();

function warnMissing(name: string): void {
  if (!isDevelopment() || warned.has(name)) return;
  warned.add(name);
  console.warn(
    `[karpo] icono «${name}» desconocido: se pinta un círculo. Los nombres válidos son los de ICON_NAMES ` +
      '(@karpo/react/contracts); si el nombre es nuevo, añádelo allí y ejecuta `npm run icons:gen`.',
  );
}

/** Para las pruebas: olvida los avisos ya emitidos. */
export function resetMissingIconWarnings(): void {
  warned.clear();
}

/**
 * Icono con peso según el tema (`ThemeInfo.iconStyle`, A3 §6.5), `svg[data-th-icon]` (el tema fija
 * tamaño y color) y `data-kx-mirror` en los direccionales (A3 §7.8). Los dibujos son datos generados:
 * los del marco (`icon-data-core.ts`) están siempre; el resto (`icon-data-extra.ts`) llega diferido y,
 * mientras tanto, se pinta un `svg` VACÍO del mismo tamaño con `data-kx-icon="pending"` (sin saltos de
 * maquetación; se repinta al llegar). Un nombre que no está en `ICON_NAMES` pinta un círculo con
 * `data-kx-icon="missing"`.
 */
export function Icon({ name, mirror, label, weight }: IconProps) {
  const theme = useContext(ThemeContext);
  const resolvedWeight: IconWeight = weight ?? (theme?.info.iconStyle === 'filled' ? 'fill' : 'regular');
  const snapshot = useIconSnapshot(name);
  const pending = snapshot === ICON_PENDING;
  const data = pending ? undefined : snapshot;
  if (!data && !pending) warnMissing(name);
  const paths = data ? (resolvedWeight === 'fill' ? data.fill : data.regular) : undefined;
  const mirrored = mirror ?? isDirectionalIcon(name);
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="1em"
      height="1em"
      viewBox="0 0 256 256"
      fill="currentColor"
      data-th-icon={name}
      data-kx-icon={data ? undefined : pending ? 'pending' : 'missing'}
      data-kx-weight={resolvedWeight}
      data-kx-mirror={mirrored ? 'true' : undefined}
      aria-hidden={label ? undefined : true}
      aria-label={label}
      role={label ? 'img' : undefined}
      focusable="false"
    >
      {paths ? (
        paths.map((d, index) => <path key={index} d={d} />)
      ) : pending ? null : (
        // Círculo de reserva: el mismo trazo que el de `circle` de Phosphor, a la vista en el menú.
        <circle cx="128" cy="128" r="96" fill="none" stroke="currentColor" strokeWidth="16" />
      )}
    </svg>
  );
}
