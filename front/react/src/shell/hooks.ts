// Ganchos del marco: ubicación de entidad, navegación por EntityRoute y avisos de lista.
import { useQueryClient } from '@tanstack/react-query';
import {
  createContext,
  useCallback,
  useContext,
  useLayoutEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type RefObject,
} from 'react';
import { useLocation, useNavigate } from 'react-router';
import { isApiError } from '../api';
import type { AppManifest, EntityConfig, EntityRoute } from '../contracts';
import { ManifestContext } from '../contracts';
import { NEW_SEGMENT, buildEntityTarget, parseEntityRoute, targetToString } from './route';

/** Primer segmento que no es una entidad. */
const NON_ENTITY = new Set(['login', 'cambiar-clave', '403']);

export function useManifest(): AppManifest {
  const manifest = useContext(ManifestContext);
  if (!manifest) throw new Error('Falta el manifiesto: envuelve la app en <KarpoApp> o en un ManifestContext.Provider.');
  return manifest;
}

export interface EntityLocation {
  entity?: string;
  id?: string;
  create: boolean;
  search: URLSearchParams;
}

function decode(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

/** Entidad, registro y consulta de la URL actual (`/:entity`, `/:entity/nuevo`, `/:entity/:id`). */
export function useEntityLocation(): EntityLocation {
  const { pathname, search } = useLocation();
  return useMemo(() => {
    const [first, second, third] = pathname.split('/').filter(Boolean);
    const params = new URLSearchParams(search);
    if (!first || NON_ENTITY.has(first) || third !== undefined) return { create: false, search: params };
    const entity = decode(first);
    if (second === undefined) return { entity, create: false, search: params };
    if (second === NEW_SEGMENT) return { entity, create: true, search: params };
    return { entity, id: decode(second), create: false, search: params };
  }, [pathname, search]);
}

/** La `EntityRoute` de la URL actual para la entidad indicada. */
export function useEntityRoute(entity: string | undefined): EntityRoute | undefined {
  const location = useEntityLocation();
  return useMemo(
    () =>
      entity === undefined || location.entity !== entity
        ? undefined
        : parseEntityRoute({ entity, id: location.id, create: location.create, search: location.search }),
    [entity, location],
  );
}

/** `navigate(EntityRoute)` de las vistas: construye la URL (con lo que conserva) y navega. */
export function useEntityNavigate(): (to: EntityRoute) => void {
  const navigate = useNavigate();
  const manifest = useManifest();
  const current = useEntityLocation();
  return useCallback(
    (to: EntityRoute) => {
      const config: EntityConfig | undefined = manifest.entities[to.entity];
      const target = buildEntityTarget(to, {
        currentEntity: current.entity,
        currentSearch: current.search,
        defaultView: config?.defaultView,
      });
      // Pasar de un registro a otro o cambiar de modo no ensucia el historial: «atrás» vuelve a la lista.
      const replace = to.id !== undefined && current.entity === to.entity && current.id !== undefined;
      void navigate(targetToString(target), { replace });
    },
    [current.entity, current.id, current.search, manifest, navigate],
  );
}

/** ¿Alguna lista de la entidad ha fallado con 403? (la lista la lee la vista; el marco avisa). */
export function useListForbidden(entity: string): boolean {
  const client = useQueryClient();
  return useSyncExternalStore(
    (notify) => client.getQueryCache().subscribe(notify),
    () =>
      client
        .getQueryCache()
        .findAll({ queryKey: ['entity', entity, 'list'] })
        .some((query) => query.state.status === 'error' && isApiError(query.state.error) && query.state.error.kind === 'forbidden'),
    () => false,
  );
}

// ---------------------------------------------------------------------------------------------
// Tamaño del marco: modo compacto y cajón (B-5)
// ---------------------------------------------------------------------------------------------

/** Ancho del marco (en `em`) hasta el que la barra superior es compacta (teléfonos y ventanas estrechas). */
export const COMPACT_MAX_EM = 40;
/** Ancho del marco (en `em`) hasta el que el menú es un cajón y el detalle ocupa la página entera. */
export const NARROW_MAX_EM = 64;

export interface ShellSize {
  /** Marco de ≤ 40em: barra superior con el menú «Más». */
  compact: boolean;
  /** Marco de ≤ 64em: menú como cajón y detalle a página completa. */
  narrow: boolean;
}

const WIDE: ShellSize = { compact: false, narrow: false };

export const ShellSizeContext = createContext<ShellSize>(WIDE);

/** Clasifica un ancho en píxeles según el tamaño de letra del marco. Pura: se prueba sin DOM. */
export function classifyWidth(width: number, emPx: number): ShellSize {
  const em = emPx > 0 ? emPx : 16;
  return { compact: width <= COMPACT_MAX_EM * em, narrow: width <= NARROW_MAX_EM * em };
}

/**
 * Observa el ancho del marco (`[data-kx-shell]`) con un `ResizeObserver`. Lo usa `Shell` una vez y publica el
 * resultado en `ShellSizeContext`: así hay un solo observador y la barra superior, el detalle y las pruebas
 * leen lo mismo. Con CSS puro el árbol de accesibilidad conservaría los controles ocultos; por eso se
 * pinta un árbol u otro.
 */
export function useShellSize(root: RefObject<HTMLElement | null>): ShellSize {
  const [size, setSize] = useState<ShellSize>(WIDE);
  useLayoutEffect(() => {
    const element = root.current;
    if (!element) return undefined;
    const measure = (width: number) => {
      // Sin geometría (jsdom, un marco aún sin pintar) no se sabe: se queda como «ancho».
      if (!(width > 0)) return;
      const next = classifyWidth(width, Number.parseFloat(getComputedStyle(element).fontSize));
      setSize((current) => (current.compact === next.compact && current.narrow === next.narrow ? current : next));
    };
    measure(element.getBoundingClientRect().width);
    if (typeof ResizeObserver === 'undefined') return undefined;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[entries.length - 1];
      if (entry) measure(entry.contentRect.width);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [root]);
  return size;
}

/** ¿El marco es compacto (≤ 40em)? Fuera de `Shell` siempre es `false`. */
export function useCompact(): boolean {
  return useContext(ShellSizeContext).compact;
}

/** ¿El marco es estrecho (≤ 64em: el menú es un cajón y el detalle va a página completa)? */
export function useNarrow(): boolean {
  return useContext(ShellSizeContext).narrow;
}
