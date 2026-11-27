// Almacén de sesión, independiente de React (PLAN §4.4, A2 §1).
//  - Login de Go: { accessToken, refreshToken, expiresAt, userId, username, mustChangePassword }
//    (sin `success`). Con `mustChangePassword` la sesión queda en 'must-change-password' y sólo se
//    ofrece el cambio de clave; `change-password` devuelve una sesión nueva.
//  - Identidad y permisos de `GET /api/auth/context`: se recarga tras login, refresco, cambio de
//    ámbito y un 403. Si no se puede cargar, los permisos quedan vacíos (fail-closed).
//  - Un único refresco en vuelo; renovación programada antes de `expiresAt`.
//  - Sesión en `sessionStorage` (envuelto en try/catch: puede no existir o lanzar).
import type { AuthContext, Session } from '../contracts';
import { hasPermission } from '../config/rules';
import { createHttpClient, isApiError, type HttpClient } from '../api';

const SESSION_KEY = 'karpo.session';
const SCOPE_KEY = 'karpo.scope';
/** Tras un 403 no se recarga el contexto más de una vez en este tiempo (evita bucles). */
const FORBIDDEN_RELOAD_GAP_MS = 10_000;
const MAX_TIMER_MS = 2 ** 31 - 1;
/** Vida supuesta del token si el servidor no manda una fecha legible (A2 §1: 15 minutos). */
const DEFAULT_LIFETIME_MS = 15 * 60_000;

export interface SessionTokens {
  accessToken: string;
  refreshToken: string;
  /** Instante de caducidad del token de acceso (ms desde 1970). */
  expiresAt: number;
  userId: string;
  username: string;
  mustChangePassword: boolean;
}

export interface SessionSnapshot {
  /** La sesión del contrato (`status`, `context`, `can`). */
  session: Session;
  userId?: string;
  username?: string;
  /** Organizaciones elegidas para `X-Organization-Scope` (vacío = todas). */
  scope: string[];
  /** `true` si el contexto de permisos no se pudo cargar. */
  contextError: boolean;
  /** `true` mientras se carga el contexto. */
  loadingContext: boolean;
}

export interface SessionStoreOptions {
  /** Base de la API, p. ej. `/api`. */
  baseUrl?: string;
  /** Por omisión `sessionStorage`; `null` = sin almacenamiento. */
  storage?: Storage | null;
  fetch?: typeof fetch;
  now?(): number;
  /** Margen antes de `expiresAt` para renovar (por omisión, 20 % de la vida restante, entre 1 s y 60 s). */
  refreshMarginMs?: number;
  getLanguage?(): string | undefined;
  /** Se llama tras cambiar el ámbito (p. ej. para invalidar las cachés). */
  onScopeChange?(): void;
  /** Se llama al cerrarse la sesión; `unrequested` = no la cerró el usuario (token no renovable). */
  onSignedOut?(unrequested: boolean): void;
}

interface State {
  tokens?: SessionTokens;
  context?: AuthContext;
  contextError: boolean;
  loadingContext: boolean;
  scope: string[];
}

function defaultStorage(): Storage | null {
  try {
    return typeof sessionStorage === 'undefined' ? null : sessionStorage;
  } catch {
    return null;
  }
}

function isObj(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function str(value: unknown): string | undefined {
  return typeof value === 'string' && value !== '' ? value : undefined;
}

/** `expiresAt` de Go: fecha RFC 3339, o número en segundos o milisegundos. */
export function parseExpiry(value: unknown, now: number): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value < 1e12 ? value * 1000 : value;
  if (typeof value === 'string') {
    const parsed = Date.parse(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return now + DEFAULT_LIFETIME_MS;
}

function parseTokens(body: unknown, now: number): SessionTokens {
  if (!isObj(body)) throw new Error('Respuesta de sesión no válida: se esperaba un objeto JSON');
  const accessToken = str(body.accessToken);
  const refreshToken = str(body.refreshToken);
  if (!accessToken || !refreshToken) {
    throw new Error('Respuesta de sesión no válida: faltan `accessToken` o `refreshToken`');
  }
  return {
    accessToken,
    refreshToken,
    expiresAt: parseExpiry(body.expiresAt, now),
    userId: str(body.userId) ?? '',
    username: str(body.username) ?? '',
    mustChangePassword: body.mustChangePassword === true,
  };
}

function parseContext(body: unknown): AuthContext | undefined {
  if (!isObj(body)) return undefined;
  if (body.outcome !== undefined && body.outcome !== 'allow') return undefined;
  const raw = isObj(body.context) ? body.context : body;
  if (!str(raw.subjectId) && !Array.isArray(raw.permissions)) return undefined;
  const strings = (value: unknown) => (Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []);
  return {
    subjectId: str(raw.subjectId) ?? '',
    subjectName: str(raw.subjectName) ?? '',
    isGlobalAdmin: raw.isGlobalAdmin === true,
    permissions: strings(raw.permissions),
    effectiveOrganizationIds: strings(raw.effectiveOrganizationIds),
  };
}

export class SessionStore {
  readonly client: HttpClient;
  private onScopeChange?: () => void;
  private onSignedOut?: (unrequested: boolean) => void;

  private state: State = { contextError: false, loadingContext: false, scope: [] };
  private snapshot: SessionSnapshot;
  private readonly listeners = new Set<() => void>();
  private readonly storage: Storage | null;
  private readonly fetchImpl?: typeof fetch;
  private readonly baseUrl: string;
  private readonly now: () => number;
  private readonly margin?: number;
  private refreshing: Promise<string | undefined> | null = null;
  private loading: Promise<void> | null = null;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private lastForbiddenReload = 0;
  private started = false;

  constructor(options: SessionStoreOptions = {}) {
    this.baseUrl = options.baseUrl ?? '/api';
    this.storage = options.storage === undefined ? defaultStorage() : options.storage;
    this.fetchImpl = options.fetch;
    this.now = options.now ?? Date.now;
    this.margin = options.refreshMarginMs;
    this.onScopeChange = options.onScopeChange;
    this.onSignedOut = options.onSignedOut;
    this.restore();
    this.snapshot = this.buildSnapshot();
    this.client = createHttpClient({
      baseUrl: this.baseUrl,
      fetch: options.fetch,
      getToken: () => this.state.tokens?.accessToken,
      refresh: () => this.refreshTokens(),
      getScope: () => (this.state.scope.length > 0 ? this.state.scope.join(',') : undefined),
      getLanguage: options.getLanguage,
      onUnauthorized: () => this.clear(true),
      onForbidden: () => this.reloadAfterForbidden(),
    });
  }

  /** Cambia los ganchos de aviso (los pone el proveedor tras crear el almacén). */
  setHooks(hooks: { onScopeChange?: () => void; onSignedOut?: (unrequested: boolean) => void }): void {
    this.onScopeChange = hooks.onScopeChange;
    this.onSignedOut = hooks.onSignedOut;
  }

  // -- suscripción (useSyncExternalStore) --------------------------------------------------------

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): SessionSnapshot => this.snapshot;

  private emit(): void {
    this.snapshot = this.buildSnapshot();
    for (const listener of [...this.listeners]) listener();
  }

  private buildSnapshot(): SessionSnapshot {
    const { tokens, context } = this.state;
    const status: Session['status'] = !tokens
      ? 'anonymous'
      : tokens.mustChangePassword
        ? 'must-change-password'
        : 'authenticated';
    const permissions = status === 'authenticated' && context ? context.permissions : [];
    const session: Session = {
      status,
      ...(context ? { context } : {}),
      can: (permission: string) => hasPermission(permissions, permission),
    };
    return {
      session,
      userId: tokens?.userId,
      username: tokens?.username,
      scope: this.state.scope,
      contextError: this.state.contextError,
      loadingContext: this.state.loadingContext,
    };
  }

  // -- almacenamiento ---------------------------------------------------------------------------

  private restore(): void {
    try {
      const raw = this.storage?.getItem(SESSION_KEY);
      if (raw) {
        const parsed: unknown = JSON.parse(raw);
        if (isObj(parsed) && str(parsed.accessToken) && str(parsed.refreshToken)) {
          this.state.tokens = parseTokens(parsed, this.now());
        }
      }
      const scope = this.storage?.getItem(SCOPE_KEY);
      if (scope) {
        const parsed: unknown = JSON.parse(scope);
        if (Array.isArray(parsed)) this.state.scope = parsed.filter((v): v is string => typeof v === 'string');
      }
    } catch {
      // almacenamiento ilegible: se empieza sin sesión
    }
  }

  private persist(): void {
    try {
      if (this.state.tokens) this.storage?.setItem(SESSION_KEY, JSON.stringify(this.state.tokens));
      else this.storage?.removeItem(SESSION_KEY);
      if (this.state.scope.length > 0) this.storage?.setItem(SCOPE_KEY, JSON.stringify(this.state.scope));
      else this.storage?.removeItem(SCOPE_KEY);
    } catch {
      // sin almacenamiento: la sesión vive sólo en memoria
    }
  }

  // -- ciclo de vida ----------------------------------------------------------------------------

  /** Arranca: renueva o carga el contexto de una sesión restaurada y programa la renovación. */
  start(): void {
    if (this.started) return;
    this.started = true;
    const tokens = this.state.tokens;
    if (!tokens) return;
    if (tokens.expiresAt - this.now() <= 1000) void this.refreshTokens();
    else {
      this.schedule();
      void this.loadContext();
    }
  }

  dispose(): void {
    this.started = false;
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
  }

  private schedule(): void {
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
    const tokens = this.state.tokens;
    if (!tokens) return;
    const remaining = tokens.expiresAt - this.now();
    const margin = this.margin ?? Math.min(60_000, Math.max(1000, remaining * 0.2));
    const delay = Math.min(Math.max(remaining - margin, 1000), MAX_TIMER_MS);
    this.timer = setTimeout(() => {
      this.timer = undefined;
      void this.refreshTokens().then((token) => {
        // Un fallo transitorio (sin red) no cierra la sesión: se reintenta mientras el token valga.
        if (!token && this.state.tokens && this.state.tokens.expiresAt > this.now()) {
          this.timer = setTimeout(() => void this.refreshTokens(), 15_000);
        }
      });
    }, delay);
  }

  private apply(tokens: SessionTokens): void {
    this.state = { ...this.state, tokens, contextError: false };
    this.persist();
    this.schedule();
    this.emit();
  }

  /** Cierra la sesión en local (sin avisar al servidor). */
  private clear(unrequested: boolean): void {
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
    const had = this.state.tokens !== undefined;
    this.state = { ...this.state, tokens: undefined, context: undefined, contextError: false, loadingContext: false };
    this.persist();
    this.emit();
    if (had) this.onSignedOut?.(unrequested);
  }

  // -- operaciones ------------------------------------------------------------------------------

  /** Inicia sesión. Devuelve el estado resultante (`authenticated` o `must-change-password`). */
  async login(username: string, password: string): Promise<Session['status']> {
    const body = await this.client.request<unknown>('/auth/login', {
      method: 'POST',
      json: { username, password },
      skipAuth: true,
    });
    this.apply(parseTokens(body, this.now()));
    await this.loadContext();
    return this.snapshot.session.status;
  }

  /** Cambia la contraseña: el servidor devuelve una sesión nueva (ya sin `mustChangePassword`). */
  async changePassword(currentPassword: string, newPassword: string): Promise<Session['status']> {
    const body = await this.client.request<unknown>('/auth/change-password', {
      method: 'POST',
      json: { currentPassword, newPassword },
    });
    this.apply(parseTokens(body, this.now()));
    await this.loadContext();
    return this.snapshot.session.status;
  }

  /** Cierra la sesión (avisa al servidor sin esperar que responda bien). */
  async logout(): Promise<void> {
    const refreshToken = this.state.tokens?.refreshToken;
    this.clear(false);
    if (!refreshToken) return;
    try {
      await this.client.request('/auth/logout', { method: 'POST', json: { refreshToken }, skipAuth: true });
    } catch {
      // la sesión local ya está cerrada
    }
  }

  /** Renueva los tokens (un solo refresco en vuelo). Devuelve el token de acceso nuevo o `undefined`. */
  refreshTokens(): Promise<string | undefined> {
    if (this.refreshing) return this.refreshing;
    const current = this.state.tokens;
    if (!current) return Promise.resolve(undefined);
    this.refreshing = (async () => {
      try {
        const body = await this.client.request<unknown>('/auth/refresh', {
          method: 'POST',
          json: { refreshToken: current.refreshToken },
          skipAuth: true,
        });
        const tokens = parseTokens(body, this.now());
        this.apply(tokens);
        void this.loadContext();
        return tokens.accessToken;
      } catch (error) {
        // El servidor rechazó el refresco: la sesión ya no sirve. Un fallo de red, no.
        if (isApiError(error) && error.kind !== 'network' && error.kind !== 'server') this.clear(true);
        else if (!isApiError(error)) this.clear(true);
        return undefined;
      } finally {
        this.refreshing = null;
      }
    })();
    return this.refreshing;
  }

  /** Recarga identidad y permisos de `GET /api/auth/context` (fail-closed si no se puede). */
  loadContext(): Promise<void> {
    if (this.loading) return this.loading;
    if (!this.state.tokens) return Promise.resolve();
    this.state = { ...this.state, loadingContext: true };
    this.emit();
    this.loading = (async () => {
      try {
        const body = await this.client.request<unknown>('/auth/context');
        const context = parseContext(body);
        this.state = { ...this.state, context, contextError: context === undefined, loadingContext: false };
      } catch {
        this.state = { ...this.state, context: undefined, contextError: true, loadingContext: false };
      } finally {
        this.loading = null;
        this.emit();
      }
    })();
    return this.loading;
  }

  private reloadAfterForbidden(): void {
    const tokens = this.state.tokens;
    if (!tokens || tokens.mustChangePassword) return;
    const now = this.now();
    if (now - this.lastForbiddenReload < FORBIDDEN_RELOAD_GAP_MS) return;
    this.lastForbiddenReload = now;
    void this.loadContext();
  }

  /** Elige las organizaciones del ámbito (`X-Organization-Scope`); vacío = todas. */
  async setScope(ids: string[]): Promise<void> {
    const unique = [...new Set(ids.filter((id) => id !== ''))];
    this.state = { ...this.state, scope: unique };
    this.persist();
    this.emit();
    if (this.loading) await this.loading;
    await this.loadContext();
    this.onScopeChange?.();
  }
}
