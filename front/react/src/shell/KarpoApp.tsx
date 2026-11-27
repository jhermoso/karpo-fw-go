// KarpoApp: monta los proveedores (consultas, manifiesto, API, sesión, tema, idioma, ayuda, avisos y
// atajos) y el router. Todo lo que se pinta sale de la configuración (PLAN §4.7).
import { QueryClient, QueryClientProvider, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useRef, useState, type PropsWithChildren } from 'react';
import { useTranslation } from 'react-i18next';
import { createEntityApi } from '../api';
import { SessionProvider, useHttpClient } from '../auth';
import { loadManifest } from '../config';
import { ApiContext, ManifestContext, type AppManifest, type KarpoAppProps } from '../contracts';
import { HelpProvider } from '../help';
import { I18nProvider } from '../i18n';
import { ThemeProvider } from '../theme';
import { Action, Feedback, Skeleton, ShortcutProvider, ToastProvider } from '../ui';
import { AppRouter } from './AppRouter';
import './shell.css';

/** Ampliaciones opcionales de `KarpoAppProps` (pruebas e integraciones). */
export interface KarpoAppExtraProps {
  /** `fetch` para leer el manifiesto y los temas (por omisión, el global). */
  fetch?: typeof fetch;
  /** Cliente de consultas propio (por omisión se crea uno con `createKarpoQueryClient`). */
  queryClient?: QueryClient;
  /** Almacén de la sesión (por omisión `sessionStorage`; `null` = sólo memoria). */
  sessionStorage?: Storage | null;
}

export type KarpoAppAllProps = KarpoAppProps & KarpoAppExtraProps;

/** Cliente de consultas de la app: datos frescos 30 s, sin reintentos propios (el cliente HTTP ya reintenta los GET). */
export function createKarpoQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        gcTime: 5 * 60_000,
        retry: false,
        refetchOnWindowFocus: false,
      },
      mutations: { retry: false },
    },
  });
}

type ManifestState =
  | { status: 'loading' }
  | { status: 'ready'; manifest: AppManifest }
  | { status: 'error'; message: string };

function useManifestLoader(props: KarpoAppAllProps): ManifestState {
  const { manifest, manifestUrl, fetch: fetchImpl } = props;
  const [loaded, setLoaded] = useState<ManifestState>({ status: 'loading' });
  useEffect(() => {
    if (manifest || !manifestUrl) return undefined;
    const controller = new AbortController();
    loadManifest(manifestUrl, { signal: controller.signal, fetch: fetchImpl })
      .then((value) => {
        if (!controller.signal.aborted) setLoaded({ status: 'ready', manifest: value });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setLoaded({ status: 'error', message: error instanceof Error ? error.message : String(error) });
      });
    return () => controller.abort();
  }, [manifest, manifestUrl, fetchImpl]);
  if (manifest) return { status: 'ready', manifest };
  if (!manifestUrl) return { status: 'error', message: 'KarpoApp necesita «manifest» o «manifestUrl».' };
  return loaded;
}

export function KarpoApp(props: KarpoAppAllProps) {
  const [attempt, setAttempt] = useState(0);
  return (
    <I18nProvider locale={props.locale}>
      <ManifestGate key={attempt} props={props} onRetry={() => setAttempt((n) => n + 1)} />
    </I18nProvider>
  );
}

function ManifestGate({ props, onRetry }: { props: KarpoAppAllProps; onRetry(): void }) {
  const { t } = useTranslation('core');
  const state = useManifestLoader(props);
  if (state.status === 'loading') {
    return (
      <main data-kx-page="loading">
        <Skeleton lines={4} label={t('app.loading')} />
      </main>
    );
  }
  if (state.status === 'error') {
    return (
      <main data-kx-page="loading">
        <Feedback tone="error" role="alert">
          <span data-kx-part="text">
            <strong>{t('app.loadError')}</strong> {state.message}
          </span>
          <Action kind="secondary" icon="refresh" onClick={onRetry}>
            {t('app.retry')}
          </Action>
        </Feedback>
      </main>
    );
  }
  return <AppRuntime manifest={state.manifest} props={props} />;
}

function AppRuntime({ manifest, props }: { manifest: AppManifest; props: KarpoAppAllProps }) {
  const [queryClient] = useState(() => props.queryClient ?? createKarpoQueryClient());
  const themeOrigin = props.themeOrigin ?? manifest.theme.origin;
  const apiBaseUrl = props.apiBaseUrl ?? manifest.api.baseUrl;
  return (
    <QueryClientProvider client={queryClient}>
      <RefetchOnLanguageChange />
      <ManifestContext.Provider value={manifest}>
        <ThemeProvider
          origin={themeOrigin}
          defaultTheme={manifest.theme.default}
          available={manifest.theme.available}
          fetcher={props.fetch}
        >
          <SessionProvider
            apiBaseUrl={apiBaseUrl}
            storage={props.sessionStorage}
            getLanguage={() => (typeof document === 'undefined' ? undefined : document.documentElement.lang || undefined)}
          >
            <ApiBridge manifest={manifest}>
              <ToastProvider>
                <ShortcutProvider>
                  <HelpProvider>
                    <AppRouter kind={props.router ?? 'browser'} basename={props.basename} initialPath={props.initialPath} />
                  </HelpProvider>
                </ShortcutProvider>
              </ToastProvider>
            </ApiBridge>
          </SessionProvider>
        </ThemeProvider>
      </ManifestContext.Provider>
    </QueryClientProvider>
  );
}

/**
 * Al cambiar de idioma, las filas en caché conservan lo que el servidor tradujo (nombres de catálogo,
 * `typeName`) en el idioma anterior: se marcan como caducadas y las que se ven se piden de nuevo con
 * el `Accept-Language` nuevo.
 */
function RefetchOnLanguageChange() {
  const queryClient = useQueryClient();
  const { i18n } = useTranslation();
  const language = i18n.resolvedLanguage ?? i18n.language;
  const previous = useRef(language);
  useEffect(() => {
    if (previous.current === language) return;
    previous.current = language;
    void queryClient.invalidateQueries({ queryKey: ['entity'] });
  }, [language, queryClient]);
  return null;
}

/** `ApiContext` a partir del cliente HTTP con la sesión cableada. */
function ApiBridge({ manifest, children }: PropsWithChildren<{ manifest: AppManifest }>) {
  const client = useHttpClient();
  const api = useMemo(() => createEntityApi(client, manifest), [client, manifest]);
  return <ApiContext.Provider value={api}>{children}</ApiContext.Provider>;
}
