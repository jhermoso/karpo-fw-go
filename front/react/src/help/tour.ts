// Recorrido guiado con driver.js (MIT) SIN su hoja por defecto (análisis 03 §7.4).
//
// driver.js se carga con `import()` al lanzar el recorrido (M-15.2): no entra en el trozo inicial. El botón
// de ayuda lo precarga al pasar el puntero o recibir el foco (`preloadTour`).
//
// Lo que este módulo pone de su parte:
//  - el aspecto lo da el tema: `help.css` (surface.popover.*, text.*) y las acciones del tema
//    (`data-th="action"` en los botones, puestos en `onPopoverRender`);
//  - el velo sale de `--th-surface-scrim` resuelto con la sonda (trae alfa) y se recalcula al cambiar
//    el tema o el modo;
//  - `animate` y `smoothScroll` sólo si el usuario no pidió reducir el movimiento;
//  - teclado propio: flechas según la dirección (en RTL se invierten) y Esc; las flechas no se
//    secuestran dentro de un widget (cuadrícula, árbol, campo...).
import type * as DriverJs from 'driver.js';
import type { Config, DriveStep, Driver, PopoverDOM } from 'driver.js';
import { iconData } from '../ui/icons';
import { escapeHtml, type ResolvedStep } from './steps';

export interface TourLabels {
  next: string;
  prev: string;
  done: string;
  close: string;
  /** Con `{{current}}` y `{{total}}`, que sustituye driver.js. */
  progress: string;
}

export interface TourEnd {
  reason: string | undefined;
  /** Llegó al último paso (o terminó): el recorrido cuenta como visto. */
  completed: boolean;
}

export interface TourOptions {
  steps: readonly ResolvedStep[];
  labels: TourLabels;
  direction: 'ltr' | 'rtl';
  /** Color actual del velo; se vuelve a pedir en `subscribeTheme`. */
  scrim(): string;
  /** Se llama cuando el tema o el modo cambian; devuelve la baja. */
  subscribeTheme?(callback: () => void): () => void;
  /** Holgura y radio del recorte alrededor del elemento destacado, en píxeles (medidos del tema). */
  stage?: { padding: number; radius: number };
  /** Permite animar y desplazar con suavidad (prefers-reduced-motion: no-preference). */
  motion: boolean;
  /** Peso del icono de cierre: `fill` si el tema es de iconos rellenos. Por omisión `regular`. */
  iconWeight?: 'regular' | 'fill';
  onEnd(end: TourEnd): void;
}

export interface TourController {
  /** Hay un recorrido en curso o en preparación (driver.js todavía cargándose). */
  isActive(): boolean;
  destroy(): void;
}

/** Carga diferida de driver.js: un único `import()` aunque se pida varias veces. */
let driverModule: Promise<typeof DriverJs> | undefined;

function loadDriver(): Promise<typeof DriverJs> {
  driverModule ??= import('driver.js');
  return driverModule;
}

/** Empieza a descargar driver.js sin lanzar nada (al pasar el puntero o enfocar el botón de ayuda). */
export function preloadTour(): void {
  loadDriver().catch(() => {
    driverModule = undefined; // un fallo de red no se queda en caché: el siguiente intento vuelve a pedirlo
  });
}

/** Clase que lleva el popover del recorrido (ver help.css). */
export const TOUR_CLASS = 'kx-tour';

const COMPOSITE_WIDGET =
  'input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="grid"], [role="treegrid"], ' +
  '[role="tree"], [role="listbox"], [role="menu"], [role="menubar"], [role="tablist"], [role="slider"], ' +
  '[role="spinbutton"], [role="textbox"], [role="combobox"], [role="radiogroup"]';

export type TourKeyAction = 'next' | 'prev' | 'close';

/** Qué hace una tecla durante el recorrido. Función pura para poder probarla sin navegador. */
export function tourKeyAction(
  key: string,
  direction: 'ltr' | 'rtl',
  context: { insidePopover: boolean; inWidget: boolean },
): TourKeyAction | null {
  if (key === 'Escape') return 'close';
  if (key !== 'ArrowRight' && key !== 'ArrowLeft') return null;
  if (context.inWidget && !context.insidePopover) return null;
  const forward = direction === 'rtl' ? 'ArrowLeft' : 'ArrowRight';
  return key === forward ? 'next' : 'prev';
}

const SVG_NS = 'http://www.w3.org/2000/svg';

/**
 * El icono `x` del marco como SVG (síncrono: `x` está en el núcleo de iconos, R3-1). El tamaño y el color los
 * pone el tema (`svg[data-th-icon]`) y `help.css`; aquí sólo el dibujo y la marca de decorativo.
 */
export function closeIcon(weight: 'regular' | 'fill' = 'regular'): SVGSVGElement | undefined {
  const data = iconData('x');
  if (!data) return undefined;
  const svg = document.createElementNS(SVG_NS, 'svg');
  svg.setAttribute('viewBox', '0 0 256 256');
  svg.setAttribute('width', '1em');
  svg.setAttribute('height', '1em');
  svg.setAttribute('fill', 'currentColor');
  svg.setAttribute('data-th-icon', 'x');
  svg.setAttribute('data-kx-weight', weight);
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('focusable', 'false');
  for (const d of weight === 'fill' ? data.fill : data.regular) {
    const path = document.createElementNS(SVG_NS, 'path');
    path.setAttribute('d', d);
    svg.append(path);
  }
  return svg;
}

function decoratePopover(popover: PopoverDOM, options: TourOptions): void {
  const { wrapper, nextButton, previousButton, closeButton } = popover;
  wrapper.classList.add(TOUR_CLASS);
  wrapper.setAttribute('data-th-elevation', 'modal');
  wrapper.setAttribute('dir', options.direction);
  nextButton.setAttribute('data-th', 'action');
  nextButton.setAttribute('data-th-kind', 'primary');
  previousButton.setAttribute('data-th', 'action');
  previousButton.setAttribute('data-th-kind', 'secondary');
  closeButton.setAttribute('data-th', 'action');
  closeButton.setAttribute('data-th-kind', 'quiet');
  closeButton.setAttribute('aria-label', options.labels.close);
  closeButton.setAttribute('title', options.labels.close);
  // El «×» de texto de driver.js se cambia por el icono del marco (una sola vez: se decora en cada paso).
  if (!closeButton.querySelector('svg')) {
    const icon = closeIcon(options.iconWeight);
    if (icon) closeButton.replaceChildren(icon);
  }
}

/**
 * Arranca el recorrido. `steps` no puede estar vacío (el llamante decide qué enseñar en ese caso).
 * Devuelve el controlador de inmediato; driver.js se carga a continuación (o ya estaba cargado) y, si
 * el recorrido se cierra mientras tanto, no llega a abrirse. Si la carga falla, el recorrido termina
 * como `reason: 'error'` para que quien lo lanzó recupere el foco.
 */
export function startTour(options: TourOptions): TourController {
  const { steps, labels } = options;
  const driveSteps: DriveStep[] = steps.map((step) => ({
    element: step.element,
    popover: {
      title: escapeHtml(step.title),
      description: escapeHtml(step.body),
    },
  }));

  let reachedLast = false;
  let ended = false;
  let tour: Driver | undefined;
  const cleanups: Array<() => void> = [];

  const finish = (reason: string | undefined) => {
    if (ended) return;
    ended = true;
    for (const cleanup of cleanups.splice(0)) cleanup();
    options.onEnd({ reason, completed: reachedLast || reason === 'done' });
  };

  const begin = ({ driver }: typeof DriverJs) => {
    if (ended) return; // se cerró (o se lanzó otro recorrido) mientras se cargaba driver.js
    const config: Config = {
      steps: driveSteps,
      popoverClass: TOUR_CLASS,
      overlayColor: options.scrim(),
      overlayOpacity: 1, // la opacidad ya viene en el alfa del color del tema
      animate: options.motion,
      smoothScroll: options.motion,
      showProgress: true,
      progressText: labels.progress,
      nextBtnText: labels.next,
      prevBtnText: labels.prev,
      doneBtnText: labels.done,
      closeBtnLabel: labels.close,
      showButtons: ['next', 'previous', 'close'],
      allowClose: true,
      allowKeyboardControl: false, // teclado propio, con dirección (más abajo)
      ...(options.stage ? { stagePadding: options.stage.padding, stageRadius: options.stage.radius } : {}),
      onPopoverRender: (popover) => decoratePopover(popover, options),
      onHighlighted: (_element, _step, { index }) => {
        if (index === driveSteps.length - 1) reachedLast = true;
      },
      onDestroyed: (_element, _step, { reason, index }) => {
        // `onHighlighted` llega un fotograma después: si se cierra antes, el índice activo lo suple
        if (index === driveSteps.length - 1) reachedLast = true;
        finish(reason);
      },
    };

    const instance: Driver = driver(config);
    tour = instance;

    const onKeyUp = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
      const target = event.target instanceof Element ? event.target : null;
      const action = tourKeyAction(event.key, options.direction, {
        insidePopover: target?.closest('.driver-popover') != null,
        inWidget: target?.closest(COMPOSITE_WIDGET) != null,
      });
      if (action === null || !instance.isActive()) return;
      if (action === 'close') instance.destroy();
      else if (action === 'prev') instance.movePrevious();
      else if (instance.hasNextStep()) instance.moveNext();
      else instance.destroy();
    };
    window.addEventListener('keyup', onKeyUp);
    cleanups.push(() => window.removeEventListener('keyup', onKeyUp));

    if (options.subscribeTheme) {
      const unsubscribe = options.subscribeTheme(() => {
        if (!instance.isActive()) return;
        instance.setConfig({ ...instance.getConfig(), overlayColor: options.scrim() });
        instance.refresh();
      });
      cleanups.push(unsubscribe);
    }

    instance.drive();
  };

  loadDriver()
    .then(begin)
    .catch(() => {
      driverModule = undefined;
      finish('error');
    });

  return {
    isActive: () => (tour ? tour.isActive() : !ended),
    destroy: () => {
      if (tour?.isActive()) tour.destroy();
      finish('api');
    },
  };
}
