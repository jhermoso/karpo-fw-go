// Registro de iconos (D-R19, D-R20, D-R37, B-1, M-11): nombre de icono -> dibujo. Los dibujos NO se importan
// de Phosphor en ejecución: los genera `scripts/gen-icons.mjs` en dos módulos a partir de la lista única de
// `src/contracts/icons.ts`.
//
//   icon-data-core.ts   los iconos del marco (`CORE_ICON_NAMES`): estático, en el trozo inicial. `iconData`
//                       los lee de forma SÍNCRONA.
//   icon-data-extra.ts  el resto: trozo diferido que se pide con `import()` al arrancar (`loadExtraIcons`).
//                       Mientras no llega, `<Icon>` pinta un `svg` vacío del mismo tamaño y se repinta al
//                       llegar (`useIconSnapshot`, `useSyncExternalStore`), sin saltos de maquetación.
//
// Si `ICON_NAMES` o `CORE_ICON_NAMES` cambian y los ficheros no se regeneran, `npm run icons:gen -- --check`
// falla.
import { useEffect, useSyncExternalStore } from 'react';
import { isIconName, type IconName } from '../contracts/icons';
import { ICON_DATA_CORE, type IconData } from './icon-data-core';

export type { IconData };

/**
 * Dibujos del NÚCLEO, de lectura síncrona. Ya NO trae todos los nombres de `ICON_NAMES`: los demás llegan con
 * `loadExtraIcons()`; para leer cualquiera se usa `iconData(name)`.
 */
export const ICON_DATA: Readonly<Partial<Record<IconName, IconData>>> = ICON_DATA_CORE;

type IconTable = Readonly<Partial<Record<IconName, IconData>>>;

/** Estado del trozo diferido: `idle` (sin pedir), `loading`, `ready` o `failed`. */
type ExtraState = { status: 'idle' | 'loading' | 'failed' } | { status: 'ready'; table: IconTable };

let extra: ExtraState = { status: 'idle' };
let extraPromise: Promise<void> | undefined;
const listeners = new Set<() => void>();

function notify(): void {
  for (const listener of [...listeners]) listener();
}

/**
 * Pide el trozo diferido de iconos. Idempotente: se importa una sola vez y todos esperan la misma promesa.
 * Nunca rechaza: si la descarga falla, los iconos extra quedan como desconocidos (círculo con
 * `data-kx-icon="missing"`) y se avisa en la consola.
 */
export function loadExtraIcons(): Promise<void> {
  if (extraPromise) return extraPromise;
  extra = { status: 'loading' };
  extraPromise = import('./icon-data-extra')
    .then((module) => {
      extra = { status: 'ready', table: module.ICON_DATA_EXTRA };
    })
    .catch((cause: unknown) => {
      extra = { status: 'failed' };
      console.warn('[karpo] no se pudieron cargar los iconos diferidos', cause);
    })
    .finally(notify);
  return extraPromise;
}

/** Sólo para las pruebas: olvida el trozo diferido para volver a medir «antes de cargar». */
export function resetExtraIcons(): void {
  extra = { status: 'idle' };
  extraPromise = undefined;
  notify();
}

/** Marca de «el dibujo está pedido pero aún no ha llegado». */
export const ICON_PENDING = 'pending' as const;

/**
 * Lo que se sabe ahora de un nombre: su dibujo, `'pending'` (es un nombre válido del trozo diferido y éste
 * aún no ha llegado) o `undefined` (no es un nombre de `ICON_NAMES`, o la descarga falló).
 */
export function iconSnapshot(name: string): IconData | typeof ICON_PENDING | undefined {
  if (!isIconName(name)) return undefined;
  const core = ICON_DATA_CORE[name];
  if (core) return core;
  if (extra.status === 'ready') return extra.table[name];
  return extra.status === 'failed' ? undefined : ICON_PENDING;
}

/** Dibujo de un nombre de icono si ya está disponible; `undefined` si no es un nombre válido o aún no llegó. */
export function iconData(name: string): IconData | undefined {
  const snapshot = iconSnapshot(name);
  return snapshot === ICON_PENDING ? undefined : snapshot;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Lo de `iconSnapshot` como gancho de React: se repinta cuando llega el trozo diferido. Un icono extra pide
 * la descarga si nadie lo hizo ya (el entrypoint la adelanta al arrancar).
 */
export function useIconSnapshot(name: string): IconData | typeof ICON_PENDING | undefined {
  const read = () => iconSnapshot(name);
  const snapshot = useSyncExternalStore(subscribe, read, read);
  useEffect(() => {
    if (snapshot === ICON_PENDING) void loadExtraIcons();
  }, [snapshot]);
  return snapshot;
}

// Al arrancar (sólo en un navegador): el trozo diferido se pide tras evaluar el módulo, en paralelo con el
// manifiesto y el resto, para que los iconos del menú aparezcan al instante en la práctica.
if (typeof window !== 'undefined') setTimeout(() => void loadExtraIcons(), 0);
