// Medida del ancho que la rejilla tiene para sus columnas de datos (D-R34): el de la zona de desplazamiento sin
// las columnas de selección y de acciones, que van fijas a los lados. Se vuelve a medir con `ResizeObserver`
// (al abrir el detalle, arrastrar el divisor, girar la pantalla...). La decisión de qué se encoge o se oculta
// no está aquí: es `fitColumns` (fit.ts) y la aplica `useGridModel`.
import { useLayoutEffect, useState, type RefObject } from 'react';

const SELECT_SELECTOR = '[data-kx-part="header-row"] > [data-kx-col="__select"]';
const ACTIONS_SELECTOR = '[data-kx-part="header-row"] > [data-kx-col="__actions"]';

/** Ancho disponible para las columnas de datos, o `undefined` mientras no se pueda medir (o esté desactivado). */
export function measureAvailable(viewport: HTMLElement): number | undefined {
  const width = viewport.clientWidth;
  // Sin diseño (oculto o fuera del documento) no hay medida: es mejor no ajustar que ocultarlo todo.
  if (!(width > 0)) return undefined;
  const lead = viewport.querySelector<HTMLElement>(SELECT_SELECTOR)?.getBoundingClientRect().width ?? 0;
  const trail = viewport.querySelector<HTMLElement>(ACTIONS_SELECTOR)?.getBoundingClientRect().width ?? 0;
  return Math.max(0, Math.floor(width - lead - trail));
}

/**
 * `reserve` cambia cuando aparece o desaparece una de las columnas de los lados (para volver a medir sin que
 * haya cambiado el tamaño de la zona de desplazamiento).
 */
export function useFitColumns(ref: RefObject<HTMLElement | null>, enabled: boolean, reserve: string): number | undefined {
  const [available, setAvailable] = useState<number | undefined>(undefined);

  useLayoutEffect(() => {
    const element = ref.current;
    if (!enabled || !element) {
      setAvailable(undefined);
      return undefined;
    }
    const measure = () => {
      const next = measureAvailable(element);
      // Una medida nula (pestaña oculta) conserva la anterior: al volver a verse se mide de nuevo.
      if (next !== undefined) setAvailable((current) => (current === next ? current : next));
    };
    measure();
    if (typeof ResizeObserver === 'undefined') return undefined;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [ref, enabled, reserve]);

  return enabled ? available : undefined;
}
