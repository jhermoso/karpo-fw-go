import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ComponentPropsWithoutRef,
  type PropsWithChildren,
} from 'react';
import type { ShortcutDef } from '../contracts';
import { useT } from './i18n';
import {
  detectPlatform,
  findShortcutConflicts,
  matchesEvent,
  parseKeys,
  type Platform,
  type ShortcutConflict,
} from './shortcut-keys';

interface ShortcutStore {
  platform: Platform;
  setReporter(report: (conflict: ShortcutConflict) => void): void;
  register(def: ShortcutDef): () => void;
  subscribe(listener: () => void): () => void;
  getDefs(): readonly ShortcutDef[];
  getConflicts(): readonly ShortcutConflict[];
}

const EMPTY_DEFS: readonly ShortcutDef[] = [];
const EMPTY_CONFLICTS: readonly ShortcutConflict[] = [];

const ShortcutContext = createContext<ShortcutStore | null>(null);

function conflictKey(conflict: ShortcutConflict): string {
  return `${conflict.kind}|${conflict.scope}|${conflict.keys}|${[...conflict.ids].sort().join(',')}`;
}

function createStore(platform: Platform): ShortcutStore {
  let reporter: ((conflict: ShortcutConflict) => void) | undefined;
  const defs = new Map<string, ShortcutDef>();
  const listeners = new Set<() => void>();
  const reported = new Set<string>();
  let snapshot: readonly ShortcutDef[] = EMPTY_DEFS;
  let conflicts: readonly ShortcutConflict[] = EMPTY_CONFLICTS;

  function refresh(): void {
    snapshot = [...defs.values()];
    conflicts = findShortcutConflicts(snapshot, platform);
    report();
    listeners.forEach((listener) => listener());
  }

  /** Avisa de cada conflicto una sola vez, en cuanto hay a quién avisar. */
  function report(): void {
    if (!reporter) return;
    for (const conflict of conflicts) {
      const key = conflictKey(conflict);
      if (!reported.has(key)) {
        reported.add(key);
        reporter(conflict);
      }
    }
  }

  return {
    platform,
    setReporter(report_) {
      reporter = report_;
      report();
    },
    register(def) {
      defs.set(def.id, def);
      refresh();
      return () => {
        // Sólo retira el registro propio: un atajo con el mismo id registrado después se conserva.
        if (defs.get(def.id) === def) {
          defs.delete(def.id);
          refresh();
        }
      };
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    getDefs: () => snapshot,
    getConflicts: () => conflicts,
  };
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  if (tag === 'TEXTAREA' || tag === 'SELECT' || tag === 'INPUT') return true;
  const role = target.getAttribute('role');
  return role === 'textbox' || role === 'combobox' || role === 'searchbox';
}

/** Ámbitos activos para un evento: los `data-kx-scope` de los ancestros (el más cercano primero) y `global`. */
function activeScopes(target: EventTarget | null): string[] {
  const scopes: string[] = [];
  let element = target instanceof Element ? target.closest('[data-kx-scope]') : null;
  while (element) {
    const scope = element.getAttribute('data-kx-scope');
    if (scope) scopes.push(scope);
    element = element.parentElement?.closest('[data-kx-scope]') ?? null;
  }
  scopes.push('global');
  return scopes;
}

export interface ShortcutProviderProps {
  /** Plataforma para resolver `Mod` (Ctrl o Cmd); por defecto, la detectada. */
  platform?: Platform;
  /** Aviso de conflicto de teclas (por defecto, `console.warn` sólo en desarrollo). */
  onConflict?(conflict: ShortcutConflict): void;
}

/**
 * Registro de atajos con ámbitos. Un atajo `global` actúa siempre; los demás, cuando el foco está
 * dentro de un elemento `data-kx-scope="<ámbito>"` (el más cercano gana). `Mod` es Ctrl (Cmd en Mac).
 * Dentro de campos de texto sólo valen las combinaciones con `Mod`, y nunca si otro manejador ya
 * llamó a `preventDefault`.
 */
export function ShortcutProvider({ platform, onConflict, children }: PropsWithChildren<ShortcutProviderProps>) {
  const t = useT('ui');
  const [store] = useState(() => createStore(platform ?? detectPlatform()));

  useEffect(() => {
    store.setReporter((conflict) => {
      if (onConflict) {
        onConflict(conflict);
      } else if (import.meta.env.DEV) {
        console.warn(
          t(conflict.kind === 'duplicate' ? 'shortcuts.conflict' : 'shortcuts.shadow', {
            keys: conflict.keys,
            scope: conflict.scope,
            ids: conflict.ids.join(', '),
          }),
        );
      }
    });
  }, [store, onConflict, t]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent): void {
      if (event.defaultPrevented || event.isComposing) return;
      if (isTypingTarget(event.target) && !(event.ctrlKey || event.metaKey)) return;
      const scopes = activeScopes(event.target);
      let best: { def: ShortcutDef; rank: number } | undefined;
      for (const def of store.getDefs()) {
        const rank = scopes.indexOf(def.scope);
        if (rank < 0) continue;
        if (best && best.rank <= rank) continue;
        if (matchesEvent(parseKeys(def.keys), event, store.platform)) best = { def, rank };
      }
      if (best) {
        event.preventDefault();
        best.def.run();
      }
    }
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [store]);

  return <ShortcutContext.Provider value={store}>{children}</ShortcutContext.Provider>;
}

/** Registra un atajo mientras el componente esté montado. Sin `ShortcutProvider` no hace nada. */
export function useShortcut(def: ShortcutDef): void {
  const store = useContext(ShortcutContext);
  const runRef = useRef(def.run);
  useEffect(() => {
    runRef.current = def.run;
  });
  const { id, scope, keys, labelKey } = def;
  useEffect(() => {
    if (!store) return undefined;
    return store.register({ id, scope, keys, labelKey, run: () => runRef.current() });
  }, [store, id, scope, keys, labelKey]);
}

function noopSubscribe(): () => void {
  return () => undefined;
}
function getEmptyDefs(): readonly ShortcutDef[] {
  return EMPTY_DEFS;
}
function getEmptyConflicts(): readonly ShortcutConflict[] {
  return EMPTY_CONFLICTS;
}

/** Atajos registrados, en orden de registro (el panel de ayuda `Shift+?` se genera de aquí). */
export function useShortcutRegistry(): ShortcutDef[] {
  const store = useContext(ShortcutContext);
  const defs = useSyncExternalStore(
    store ? store.subscribe : noopSubscribe,
    store ? store.getDefs : getEmptyDefs,
    getEmptyDefs,
  );
  return defs as ShortcutDef[];
}

/** Conflictos de teclas del registro actual. */
export function useShortcutConflicts(): ShortcutConflict[] {
  const store = useContext(ShortcutContext);
  const conflicts = useSyncExternalStore(
    store ? store.subscribe : noopSubscribe,
    store ? store.getConflicts : getEmptyConflicts,
    getEmptyConflicts,
  );
  return conflicts as ShortcutConflict[];
}

export interface ShortcutScopeProps extends ComponentPropsWithoutRef<'div'> {
  scope: Exclude<ShortcutDef['scope'], 'global'>;
}

/** Marca una zona como ámbito de atajos (`display: contents`: no altera la maquetación). */
export function ShortcutScope({ scope, children, ...rest }: ShortcutScopeProps) {
  return (
    <div {...rest} data-kx-ui="shortcut-scope" data-kx-scope={scope}>
      {children}
    </div>
  );
}
