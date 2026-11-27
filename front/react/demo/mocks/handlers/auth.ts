// Autenticación de Go (A2 §1): login, refresh, logout, cambio de contraseña y contexto de
// autorización. Los errores de estas rutas (fuera de /api/v1) llevan la lista `errors`.
import { http, type HttpHandler } from 'msw';
import {
  authorize,
  formOf,
  identify,
  json,
  noContent,
  problem,
  readObject,
  simulateLatency,
  type FieldIssue,
} from '../http';
import { mock, type DemoUser } from '../state';

/** Vida del token de acceso: 15 minutos, como en Go. */
const ACCESS_TTL_MS = 15 * 60 * 1000;

let tokenSequence = 0;

const base64url = (value: unknown) =>
  btoa(JSON.stringify(value)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');

function passwordOf(user: DemoUser): string {
  return mock.passwords.get(user.username) ?? user.password;
}

function issueSession(user: DemoUser) {
  tokenSequence += 1;
  const issuedAt = Date.now();
  const expiresAt = issuedAt + ACCESS_TTL_MS;
  const accessToken = `${base64url({ alg: 'none', typ: 'JWT' })}.${base64url({
    iss: 'erp-security',
    aud: 'erp-api',
    sub: user.userId,
    username: user.username,
    iat: Math.floor(issuedAt / 1000),
    exp: Math.floor(expiresAt / 1000),
    jti: tokenSequence,
  })}.demo`;
  const refreshToken = `demo-refresh-${tokenSequence}-${user.username}`;
  mock.accessTokens.set(accessToken, { username: user.username, expiresAt });
  mock.refreshTokens.set(refreshToken, user.username);
  return {
    accessToken,
    refreshToken,
    expiresAt: new Date(expiresAt).toISOString(),
    userId: user.userId,
    username: user.username,
    mustChangePassword: user.mustChangePassword,
  };
}

function listProblem(request: Request, status: number, issues: FieldIssue[] = [], detail?: string): Response {
  const path = new URL(request.url).pathname;
  return problem({ status, form: formOf(path), instance: path, issues, detail, headers: status === 401 ? { 'WWW-Authenticate': 'Bearer' } : undefined });
}

const invalidBody = (request: Request) =>
  listProblem(request, 400, [{ field: 'body', code: 'json', message: 'the body is not a JSON object of the expected shape' }], 'One or more fields are invalid');

/** Quien llama, o el usuario `demo` cuando la autenticación está desactivada (nodo). */
function callerOrDemo(request: Request): DemoUser | null {
  const { user } = identify(request);
  if (user) return user;
  return mock.options.requireAuth ? null : (mock.users.get('demo') ?? null);
}

/** Política de contraseñas de Go: de 12 a 128 caracteres y distinta del nombre de usuario. */
function passwordIssues(password: string, username: string): FieldIssue[] {
  const issues: FieldIssue[] = [];
  const length = [...password].length;
  if (length < 12) issues.push({ field: 'password', code: 'too_short', message: 'a password has at least 12 characters' });
  if (length > 128) issues.push({ field: 'password', code: 'too_long', message: 'a password has at most 128 characters' });
  if (password.trim().toLowerCase() === username.toLowerCase()) {
    issues.push({ field: 'password', code: 'username', message: 'the password cannot be the user name' });
  }
  return issues;
}

export const handlers: HttpHandler[] = [
  http.post('*/api/auth/login', async ({ request }) => {
    await simulateLatency(request);
    const read = await readObject(request);
    if ('error' in read) return invalidBody(request);
    const username = typeof read.body.username === 'string' ? read.body.username.trim() : '';
    const password = typeof read.body.password === 'string' ? read.body.password : '';
    const issues: FieldIssue[] = [];
    if (username === '') issues.push({ field: 'username', code: 'required', message: 'the username is required' });
    if (password === '') issues.push({ field: 'password', code: 'required', message: 'the password is required' });
    if (issues.length > 0) return listProblem(request, 400, issues, 'One or more fields are invalid');
    const user = mock.users.get(username);
    if (!user || passwordOf(user) !== password) return listProblem(request, 401, [], 'invalid credentials');
    return json(issueSession(user));
  }),

  http.post('*/api/auth/refresh', async ({ request }) => {
    await simulateLatency(request);
    const read = await readObject(request);
    if ('error' in read) return invalidBody(request);
    const token = typeof read.body.refreshToken === 'string' ? read.body.refreshToken : '';
    const username = mock.refreshTokens.get(token);
    const user = username ? mock.users.get(username) : undefined;
    if (!user) return listProblem(request, 401, [], 'the refresh token is not valid');
    // Cada refresco entrega un par nuevo y retira el anterior (rotación).
    mock.refreshTokens.delete(token);
    return json(issueSession(user));
  }),

  http.post('*/api/auth/logout', async ({ request }) => {
    await simulateLatency(request);
    const read = await readObject(request);
    if (!('error' in read) && typeof read.body.refreshToken === 'string') mock.refreshTokens.delete(read.body.refreshToken);
    return noContent();
  }),

  http.post('*/api/auth/change-password', async ({ request }) => {
    await simulateLatency(request);
    const denied = authorize(request);
    if (denied) return denied;
    const user = callerOrDemo(request);
    if (!user) return listProblem(request, 401);
    const read = await readObject(request);
    if ('error' in read) return invalidBody(request);
    const current = typeof read.body.currentPassword === 'string' ? read.body.currentPassword : '';
    const next = typeof read.body.newPassword === 'string' ? read.body.newPassword : '';
    if (current !== passwordOf(user)) {
      return listProblem(request, 400, [{ field: 'currentPassword', code: 'invalid', message: 'the current password is not correct' }], 'One or more fields are invalid');
    }
    const issues = passwordIssues(next, user.username);
    if (issues.length > 0) return listProblem(request, 400, issues, 'One or more fields are invalid');
    mock.passwords.set(user.username, next);
    user.mustChangePassword = false;
    // Una sesión nueva sustituye a las anteriores del usuario.
    for (const [token, session] of mock.accessTokens) if (session.username === user.username) mock.accessTokens.delete(token);
    for (const [token, owner] of mock.refreshTokens) if (owner === user.username) mock.refreshTokens.delete(token);
    return json(issueSession(user));
  }),

  http.get('*/api/auth/context', async ({ request }) => {
    await simulateLatency(request);
    const denied = authorize(request);
    if (denied) return denied;
    const user = callerOrDemo(request);
    if (!user) return listProblem(request, 401);
    const organizations = mock.data.organizations.map((o) => o.id);
    return json({
      outcome: 'allow',
      context: {
        subjectId: user.userId,
        subjectName: user.displayName,
        actorKind: 'human',
        actorPartyId: user.userId,
        isGlobalAdmin: user.isGlobalAdmin,
        grants: organizations.map((organizationId) => ({ organizationId, level: user.isGlobalAdmin ? 'Full' : 'ReadOnly', includeSubsidiaries: false })),
        permissions: user.permissions,
        effectiveOrganizationIds: organizations,
        policyVersion: 'demo-1',
        correlationId: `demo-context-${user.username}`,
      },
    });
  }),
];
