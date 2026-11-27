// Proveedor de sesión: crea el almacén, arranca la renovación y publica `SessionContext`.
import { QueryClientContext } from '@tanstack/react-query';
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type PropsWithChildren,
} from 'react';
import { SessionContext, type Session } from '../contracts';
import type { HttpClient } from '../api';
import { SessionStore, type SessionSnapshot } from './session-store';

export interface SessionProviderProps {
  /** Base de la API de autenticación y datos (por omisión `/api`). */
  apiBaseUrl?: string;
  /** Por omisión `sessionStorage`; `null` = sólo memoria. */
  storage?: Storage | null;
  fetch?: typeof fetch;
  /** Idioma de `Accept-Language`. */
  getLanguage?(): string | undefined;
  /** Se llama tras cambiar el ámbito (además de invalidar las cachés de react-query si hay cliente). */
  onScopeChange?(): void;
  /** Se llama cuando la sesión se cierra sin que el usuario lo pida. */
  onSignedOut?(): void;
  /** Un almacén ya creado (pruebas). */
  store?: SessionStore;
}

export const SessionStoreContext = createContext<SessionStore | null>(null);

export interface SessionActions {
  login(username: string, password: string): Promise<Session['status']>;
  changePassword(currentPassword: string, newPassword: string): Promise<Session['status']>;
  logout(): Promise<void>;
  setScope(ids: string[]): Promise<void>;
  reloadContext(): Promise<void>;
}

export function SessionProvider({
  children,
  apiBaseUrl,
  storage,
  fetch: fetchImpl,
  getLanguage,
  onScopeChange,
  onSignedOut,
  store: injected,
}: PropsWithChildren<SessionProviderProps>) {
  const queryClient = useContext(QueryClientContext);
  const [store] = useState(
    () =>
      injected ??
      new SessionStore({ baseUrl: apiBaseUrl, storage, fetch: fetchImpl, getLanguage }),
  );

  useEffect(() => {
    store.setHooks({
      onScopeChange: () => {
        onScopeChange?.();
        void queryClient?.invalidateQueries();
      },
      onSignedOut: (unrequested) => {
        queryClient?.clear();
        if (unrequested) onSignedOut?.();
      },
    });
  }, [store, onScopeChange, onSignedOut, queryClient]);

  useEffect(() => {
    store.start();
    return () => store.dispose();
  }, [store]);

  const snapshot = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
  return (
    <SessionStoreContext.Provider value={store}>
      <SessionContext.Provider value={snapshot.session}>{children}</SessionContext.Provider>
    </SessionStoreContext.Provider>
  );
}

function useStore(): SessionStore {
  const store = useContext(SessionStoreContext);
  if (!store) throw new Error('Falta <SessionProvider> por encima de este componente.');
  return store;
}

/** La sesión del contrato: `status`, `context` y `can(permiso)`. */
export function useSession(): Session {
  const session = useContext(SessionContext);
  if (!session) throw new Error('Falta <SessionProvider> por encima de este componente.');
  return session;
}

/** La sesión con los datos de usuario, el ámbito y los indicadores de carga del contexto. */
export function useSessionState(): SessionSnapshot {
  const store = useStore();
  return useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
}

/** Operaciones de sesión (estables entre renders). */
export function useSessionActions(): SessionActions {
  const store = useStore();
  return useMemo<SessionActions>(
    () => ({
      login: (username, password) => store.login(username, password),
      changePassword: (current, next) => store.changePassword(current, next),
      logout: () => store.logout(),
      setScope: (ids) => store.setScope(ids),
      reloadContext: () => store.loadContext(),
    }),
    [store],
  );
}

/** Cliente HTTP con la sesión ya cableada (token, refresco único, ámbito, 403). */
export function useHttpClient(): HttpClient {
  return useStore().client;
}
