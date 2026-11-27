// ThemeProvider: carga el tema de Theros Front y lo expone como ThemeApi (PLAN §4.5, análisis 03 §10.5).
//
//  - Inserta `<link rel="stylesheet" href="{origen}{tema}/index.css">` (origen configurable: soberanía,
//    nunca una URL de terceros) y lee `{origen}{tema}/manifest.json` para saber qué modos tiene el tema.
//  - Cambiar de tema = enlazar la hoja nueva, esperar a que cargue (sus cinco @import incluidos) y
//    retirar la anterior: no hay parpadeo sin estilo. Cambiar de modo = `data-th-mode` en <html>,
//    sólo con los modos que el tema declara.
//  - Observa prefers-color-scheme, prefers-contrast, forced-colors y el atributo `data-th-mode`, y
//    avisa a los suscriptores (MapLibre, canvas, driver.js) para que vuelvan a resolver sus colores.
import {
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren,
} from 'react';
import { ThemeContext, type ThemeApi, type ThemeInfo, type ThemeMode } from '../contracts';
import { EXPECTED_DICTIONARY_HASH } from './contract';
import './layers.css';
import './kx-structure.css';
import './kx-ext.css';
import { loadThemeManifest, type ThemeManifestInfo } from './manifest';
import { measureIconStyle, resolveThemeColor } from './probe';
import { readStored, writeStored } from './storage';

/** Información del tema con lo que el manifiesto aporta además del contrato (`ThemeInfo`). */
export interface KarpoThemeInfo extends ThemeInfo {
  density?: string;
  locale?: string;
  trend?: string;
  resolvedHash?: string;
}

export interface ThemeProviderProps {
  /** Origen de los temas de Theros Front (carpeta que contiene `<tema>/index.css`). Por defecto `/themes/`. */
  origin?: string;
  /** Tema inicial si el usuario no eligió otro. Por defecto `modern-flat`. */
  defaultTheme?: string;
  /** Temas que se pueden elegir. Por defecto sólo `defaultTheme`. */
  available?: string[];
  /** Modo inicial si el usuario no eligió otro. Por defecto `system`. */
  defaultMode?: ThemeMode;
  /** Prefijo de las claves de localStorage; `null` desactiva la persistencia. Por defecto `karpo.theme`. */
  storageKey?: string | null;
  /** Hash del diccionario que se espera; si el del tema difiere se avisa en la consola. */
  expectedHash?: string;
  /** Elemento que lleva `data-th-mode`. Por defecto `<html>`. */
  root?: HTMLElement;
  /** `fetch` para el manifiesto (pruebas, cabeceras propias). */
  fetcher?: typeof fetch;
  /** Milisegundos máximos para que cargue una hoja de tema. Por defecto 15000. */
  loadTimeoutMs?: number;
}

const DEFAULT_ORIGIN = '/themes/';
const DEFAULT_THEME = 'modern-flat';
const DEFAULT_LOAD_TIMEOUT_MS = 15_000;
const LINK_ATTRIBUTE = 'data-kx-theme';
const MODE_ATTRIBUTE = 'data-th-mode';
const MEDIA_QUERIES = [
  '(prefers-color-scheme: dark)',
  '(prefers-contrast: more)',
  '(forced-colors: active)',
] as const;
const MODES: readonly ThemeMode[] = ['light', 'dark', 'system'];

function provisionalInfo(name: string): KarpoThemeInfo {
  return { name, modes: ['light'], highContrast: false, iconStyle: 'outline', dictionaryHash: '' };
}

const FALLBACK_THEME: ThemeApi = {
  info: provisionalInfo(''),
  mode: 'light',
  setMode: () => undefined,
  setTheme: () => {
    console.warn('[karpo/theme] setTheme sin ThemeProvider: no hace nada');
    return Promise.resolve();
  },
  available: [],
  resolveColor: (token, el) => resolveThemeColor(token, el),
  onChange: () => () => undefined,
};

/**
 * La API del tema. Sin proveedor devuelve una API inerte (tema desconocido, sólo claro, iconos de
 * contorno) en lugar de lanzar: los componentes que se prueban sueltos siguen funcionando.
 */
export function useTheme(): ThemeApi {
  return useContext(ThemeContext) ?? FALLBACK_THEME;
}

function normalizeOrigin(origin: string): string {
  return origin.endsWith('/') ? origin : `${origin}/`;
}

function tokens(value: string | null): string[] {
  return (value ?? '').split(/\s+/).filter(Boolean);
}

/** Los modos son `light`, `dark` o `system`; uno que el tema no tiene se sustituye por el más cercano. */
export function clampMode(requested: ThemeMode, modes: readonly ThemeMode[]): ThemeMode {
  if (modes.includes(requested)) return requested;
  if (requested === 'system' && modes.includes('light')) return 'light';
  return modes.includes('light') ? 'light' : (modes[0] ?? 'light');
}

/** Escribe `light|dark` en `data-th-mode` conservando los demás modos (p. ej. `high-contrast`). */
function applyModeAttribute(root: HTMLElement, mode: ThemeMode): string {
  const kept = tokens(root.getAttribute(MODE_ATTRIBUTE)).filter((t) => t !== 'light' && t !== 'dark');
  if (mode !== 'system') kept.unshift(mode);
  if (kept.length === 0) root.removeAttribute(MODE_ATTRIBUTE);
  else root.setAttribute(MODE_ATTRIBUTE, kept.join(' '));
  return root.getAttribute(MODE_ATTRIBUTE) ?? '';
}

interface LinkHandle {
  link: HTMLLinkElement;
  /** Se resuelve cuando la hoja (y sus @import) cargó; se rechaza con error o tiempo agotado. */
  loaded: Promise<void>;
  /** `false` si se adoptó un `<link>` que ya estaba en la página (no se retira al desmontar). */
  created: boolean;
}

function ensureLink(doc: Document, href: string, name: string, timeoutMs: number): LinkHandle {
  const existing = Array.from(doc.querySelectorAll<HTMLLinkElement>(`link[${LINK_ATTRIBUTE}]`)).find(
    (l) => l.getAttribute('href') === href,
  );
  const link = existing ?? doc.createElement('link');
  const created = existing === undefined;
  if (created) {
    link.rel = 'stylesheet';
    link.setAttribute(LINK_ATTRIBUTE, name);
    link.setAttribute('href', href);
  }
  const loaded = new Promise<void>((resolve, reject) => {
    if (!created && link.sheet) {
      resolve();
      return;
    }
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error(`Tiempo agotado cargando ${href}`));
    }, timeoutMs);
    const onLoad = () => {
      cleanup();
      resolve();
    };
    const onError = () => {
      cleanup();
      reject(new Error(`No se pudo cargar ${href}`));
    };
    function cleanup() {
      clearTimeout(timer);
      link.removeEventListener('load', onLoad);
      link.removeEventListener('error', onError);
    }
    link.addEventListener('load', onLoad);
    link.addEventListener('error', onError);
  });
  // Un rechazo no atendido por quien no espera (p. ej. tema sustituido) no debe ensuciar la consola.
  loaded.catch(() => undefined);
  if (created) doc.head.appendChild(link);
  return { link, loaded, created };
}

interface LoadedTheme {
  info: KarpoThemeInfo;
  link: HTMLLinkElement;
}

export function ThemeProvider({
  children,
  origin = DEFAULT_ORIGIN,
  defaultTheme = DEFAULT_THEME,
  available,
  defaultMode = 'system',
  storageKey = 'karpo.theme',
  expectedHash = EXPECTED_DICTIONARY_HASH,
  root: rootProp,
  fetcher,
  loadTimeoutMs = DEFAULT_LOAD_TIMEOUT_MS,
}: PropsWithChildren<ThemeProviderProps>) {
  const base = normalizeOrigin(origin);
  const names = useMemo(() => {
    const list = available && available.length > 0 ? [...available] : [defaultTheme];
    if (!list.includes(defaultTheme)) list.unshift(defaultTheme);
    return [...new Set(list)];
  }, [available, defaultTheme]);
  const modeKey = storageKey === null ? null : `${storageKey}.mode`;

  const [theme, setThemeState] = useState(() => {
    const stored = storageKey === null ? null : readStored(storageKey);
    const name = stored !== null && names.includes(stored) ? stored : defaultTheme;
    return { name, info: provisionalInfo(name) as KarpoThemeInfo, ready: false };
  });
  const [requestedMode, setRequestedMode] = useState<ThemeMode>(() => {
    const stored = modeKey === null ? null : readStored(modeKey);
    return stored !== null && (MODES as readonly string[]).includes(stored) ? (stored as ThemeMode) : defaultMode;
  });

  const listeners = useRef(new Set<() => void>());
  const themeRef = useRef(theme);
  const fetcherRef = useRef(fetcher);
  const linkRef = useRef<HTMLLinkElement | null>(null);
  const ownsLinkRef = useRef(false);
  const loadToken = useRef(0);
  const appliedMode = useRef<string | undefined>(undefined);
  const lastWritten = useRef<string | undefined>(undefined);

  useLayoutEffect(() => {
    themeRef.current = theme;
    fetcherRef.current = fetcher;
  }, [theme, fetcher]);

  const getRoot = useCallback(() => rootProp ?? document.documentElement, [rootProp]);

  const notify = useCallback(() => {
    for (const listener of Array.from(listeners.current)) {
      try {
        listener();
      } catch (error) {
        console.error('[karpo/theme] un suscriptor de onChange falló', error);
      }
    }
  }, []);

  const onChange = useCallback((callback: () => void) => {
    listeners.current.add(callback);
    return () => {
      listeners.current.delete(callback);
    };
  }, []);

  /** Enlaza la hoja del tema, espera a que cargue y lee su manifiesto. Rechaza si la hoja falla. */
  const loadTheme = useCallback(
    async (name: string): Promise<LoadedTheme & { created: boolean }> => {
      const doc = getRoot().ownerDocument;
      const handle = ensureLink(doc, `${base}${name}/index.css`, name, loadTimeoutMs);
      const [sheet, manifest] = await Promise.allSettled([
        handle.loaded,
        loadThemeManifest(`${base}${name}/manifest.json`, name, { fetcher: fetcherRef.current }),
      ]);
      if (sheet.status === 'rejected') {
        if (handle.created) handle.link.remove();
        throw sheet.reason instanceof Error ? sheet.reason : new Error(String(sheet.reason));
      }
      let parsed: ThemeManifestInfo | null = null;
      if (manifest.status === 'fulfilled') {
        parsed = manifest.value;
      } else {
        console.warn(
          `[karpo/theme] no se pudo leer el manifiesto del tema «${name}»: se supone sólo modo claro.`,
          manifest.reason,
        );
      }
      if (parsed && parsed.dictionaryHash !== expectedHash) {
        console.warn(
          `[karpo/theme] el tema «${name}» usa el diccionario ${parsed.dictionaryHash || '(sin hash)'} y este ` +
            `front espera ${expectedHash}: puede haber ganchos o variables que no coincidan.`,
        );
      }
      const iconStyle = parsed?.iconStyle ?? measureIconStyle(getRoot());
      const info: KarpoThemeInfo = parsed
        ? { ...parsed, name, iconStyle }
        : { ...provisionalInfo(name), iconStyle };
      return { info, link: handle.link, created: handle.created };
    },
    [base, expectedHash, getRoot, loadTimeoutMs],
  );

  // Carga inicial: la hoja se enlaza antes del primer pintado (layout effect) y el manifiesto llega después.
  useLayoutEffect(() => {
    const token = ++loadToken.current;
    const name = themeRef.current.name;
    let cancelled = false;
    loadTheme(name)
      .then((result) => {
        if (cancelled || token !== loadToken.current) return;
        linkRef.current = result.link;
        ownsLinkRef.current = result.created;
        setThemeState({ name, info: result.info, ready: true });
        notify();
      })
      .catch((error: unknown) => {
        if (cancelled || token !== loadToken.current) return;
        console.error(`[karpo/theme] no se pudo cargar el tema «${name}»`, error);
        setThemeState({ name, info: provisionalInfo(name), ready: true });
      });
    return () => {
      cancelled = true;
      if (linkRef.current && ownsLinkRef.current) linkRef.current.remove();
      linkRef.current = null;
    };
  }, [loadTheme, notify]);

  const effectiveMode: ThemeMode = theme.ready ? clampMode(requestedMode, theme.info.modes) : requestedMode;

  // El modo se escribe antes del pintado para que una preferencia oscura no parpadee en claro.
  useLayoutEffect(() => {
    const root = getRoot();
    const written = applyModeAttribute(root, effectiveMode);
    lastWritten.current = written;
    const changed = appliedMode.current !== undefined && appliedMode.current !== written;
    appliedMode.current = written;
    if (changed) notify();
  }, [effectiveMode, getRoot, notify]);

  // Cambios ajenos: atributo `data-th-mode` tocado por otro código y preferencias del sistema.
  useEffect(() => {
    const root = getRoot();
    const observer = new MutationObserver(() => {
      const current = root.getAttribute(MODE_ATTRIBUTE) ?? '';
      if (current === lastWritten.current) return;
      lastWritten.current = current;
      const list = tokens(current);
      setRequestedMode(list.includes('dark') ? 'dark' : list.includes('light') ? 'light' : 'system');
      notify();
    });
    observer.observe(root, { attributes: true, attributeFilter: [MODE_ATTRIBUTE] });
    const onMedia = () => notify();
    const queries = MEDIA_QUERIES.map((q) => window.matchMedia?.(q)).filter(
      (q): q is MediaQueryList => q !== undefined,
    );
    for (const query of queries) query.addEventListener?.('change', onMedia);
    return () => {
      observer.disconnect();
      for (const query of queries) query.removeEventListener?.('change', onMedia);
    };
  }, [getRoot, notify]);

  const setMode = useCallback(
    (mode: ThemeMode) => {
      const { ready, info } = themeRef.current;
      if (ready && !info.modes.includes(mode)) {
        console.warn(
          `[karpo/theme] el tema «${info.name}» no tiene el modo «${mode}» (tiene: ${info.modes.join(', ')})`,
        );
        return;
      }
      setRequestedMode(mode);
      if (modeKey !== null) writeStored(modeKey, mode);
    },
    [modeKey],
  );

  const setTheme = useCallback(
    async (name: string) => {
      if (!names.includes(name)) {
        throw new Error(`Tema desconocido «${name}». Disponibles: ${names.join(', ')}`);
      }
      const current = themeRef.current;
      if (name === current.name && current.ready) return;
      const token = ++loadToken.current;
      const result = await loadTheme(name);
      if (token !== loadToken.current) {
        // Otra petición se adelantó: esta hoja ya no sirve (salvo que sea la misma).
        if (result.created && result.link !== linkRef.current) result.link.remove();
        return;
      }
      const previous = linkRef.current;
      linkRef.current = result.link;
      ownsLinkRef.current = result.created || ownsLinkRef.current;
      if (previous && previous !== result.link) previous.remove();
      setThemeState({ name, info: result.info, ready: true });
      if (storageKey !== null) writeStored(storageKey, name);
      notify();
    },
    [loadTheme, names, notify, storageKey],
  );

  const resolveColor = useCallback(
    (token: string, el?: Element) => resolveThemeColor(token, el ?? getRoot()),
    [getRoot],
  );

  const api = useMemo<ThemeApi>(
    () => ({
      info: theme.info,
      mode: effectiveMode,
      setMode,
      setTheme,
      available: names,
      resolveColor,
      onChange,
    }),
    [theme.info, effectiveMode, setMode, setTheme, names, resolveColor, onChange],
  );

  return <ThemeContext.Provider value={api}>{children}</ThemeContext.Provider>;
}
