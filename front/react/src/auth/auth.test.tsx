// Páginas de entrada y cambio de clave, selector de ámbito y proveedor: por rol y nombre accesible.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, renderHook, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import i18next, { type i18n } from 'i18next';
import { http, HttpResponse } from 'msw';
import type { PropsWithChildren, ReactElement } from 'react';
import { I18nextProvider } from 'react-i18next';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { API, createGoMock } from '../api/__fixtures__/go-server';
import authEn from '../locales/en/auth.json';
import authEs from '../locales/es/auth.json';
import errorsEn from '../locales/en/errors.json';
import errorsEs from '../locales/es/errors.json';
import { ChangePasswordPage } from './ChangePasswordPage';
import { LoginPage } from './LoginPage';
import { OrganizationScopePicker } from './OrganizationScopePicker';
import { SessionProvider, useHttpClient, useSession, useSessionActions, useSessionState } from './SessionProvider';
import { SessionStore } from './session-store';

const mock = createGoMock();
const { server, state } = mock;

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
afterAll(() => server.close());
afterEach(() => server.resetHandlers());
beforeEach(() => {
  mock.calls.length = 0;
  state.validAccess = 'access-1';
  state.validRefresh = 'refresh-1';
  state.refreshCount = 0;
  state.contextCalls = 0;
  state.mustChangePassword = false;
  state.permissions = ['*.*.*'];
});

async function makeI18n(lng: 'es' | 'en' = 'es'): Promise<i18n> {
  const instance = i18next.createInstance();
  await instance.init({
    lng,
    fallbackLng: 'es',
    ns: ['auth', 'errors'],
    defaultNS: 'auth',
    resources: {
      es: { auth: authEs, errors: errorsEs },
      en: { auth: authEn, errors: errorsEn },
    },
    interpolation: { escapeValue: false },
  });
  return instance;
}

async function setup(ui: ReactElement, options: { lng?: 'es' | 'en'; store?: SessionStore } = {}) {
  const i18n = await makeI18n(options.lng);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const store = options.store ?? new SessionStore({ baseUrl: API, storage: null });
  const user = userEvent.setup();
  const view = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <SessionProvider store={store}>{ui}</SessionProvider>
      </QueryClientProvider>
    </I18nextProvider>,
  );
  return { user, store, queryClient, i18n, ...view };
}

async function signedInStore(): Promise<SessionStore> {
  const store = new SessionStore({ baseUrl: API, storage: null });
  await store.login('admin', 'Correct-pass-1');
  mock.calls.length = 0;
  return store;
}

describe('LoginPage', () => {
  it('se localiza por rol y nombre accesible y usa los ganchos de Theros', async () => {
    await setup(<LoginPage />);
    const form = screen.getByRole('form', { name: 'Iniciar sesión' });
    expect(screen.getByRole('heading', { level: 1, name: 'Iniciar sesión' })).toBeInTheDocument();
    expect(form).toHaveAttribute('novalidate');
    const username = within(form).getByRole('textbox', { name: 'Usuario' });
    expect(username).toBeRequired();
    expect(username).toHaveAttribute('autocomplete', 'username');
    const password = within(form).getByLabelText('Contraseña');
    expect(password).toHaveAttribute('type', 'password');
    expect(password).toHaveAttribute('autocomplete', 'current-password');
    const submit = within(form).getByRole('button', { name: 'Entrar' });
    expect(submit).toHaveAttribute('type', 'submit');
    expect(submit).toHaveAttribute('data-th', 'action');
    expect(submit).toHaveAttribute('data-th-kind', 'primary');
    expect(form.querySelectorAll('[data-th="field"]')).toHaveLength(2);
    expect(username.closest('[data-th="field"]')).toHaveAttribute('data-th-required');
    expect(screen.getByRole('main')).toBeInTheDocument();
  });

  it('muestra el error de cada campo con aria-describedby y mueve el foco al primero inválido', async () => {
    const { user } = await setup(<LoginPage />);
    await user.click(screen.getByRole('button', { name: 'Entrar' }));
    const username = screen.getByRole('textbox', { name: 'Usuario' });
    const password = screen.getByLabelText('Contraseña');
    for (const input of [username, password]) {
      expect(input).toHaveAttribute('aria-invalid', 'true');
      const error = document.getElementById(input.getAttribute('aria-describedby')!);
      expect(error).toHaveTextContent('Este dato es obligatorio.');
      expect(error).toHaveAttribute('data-th', 'field-error');
      expect(error).toHaveAttribute('data-th-tone', 'error');
    }
    expect(username).toHaveFocus();
    expect(mock.calls).toHaveLength(0);
    // al escribir y reenviar sólo queda el de la contraseña
    await user.type(username, 'admin');
    await user.click(screen.getByRole('button', { name: 'Entrar' }));
    expect(screen.getByRole('textbox', { name: 'Usuario' })).not.toHaveAttribute('aria-invalid');
    expect(screen.getByLabelText('Contraseña')).toHaveFocus();
  });

  it('credenciales incorrectas: aviso general (role=alert), campo de clave vacío y foco en el usuario', async () => {
    const onAuthenticated = vi.fn();
    const { user } = await setup(<LoginPage onAuthenticated={onAuthenticated} />);
    await user.type(screen.getByRole('textbox', { name: 'Usuario' }), 'admin');
    await user.type(screen.getByLabelText('Contraseña'), 'mala');
    await user.click(screen.getByRole('button', { name: 'Entrar' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Usuario o contraseña incorrectos.');
    expect(screen.getByLabelText('Contraseña')).toHaveValue('');
    expect(screen.getByRole('textbox', { name: 'Usuario' })).toHaveFocus();
    expect(onAuthenticated).not.toHaveBeenCalled();
  });

  it('un 400 con errores de campo los pinta en su campo, traducidos por código', async () => {
    server.use(
      http.post(`${API}/auth/login`, () =>
        HttpResponse.json(
          { title: 'Validation Failed', status: 400, errors: [{ field: 'username', code: 'format', message: 'bad format' }] },
          { status: 400 },
        ),
      ),
    );
    const { user } = await setup(<LoginPage />);
    await user.type(screen.getByRole('textbox', { name: 'Usuario' }), 'a b');
    await user.type(screen.getByLabelText('Contraseña'), 'x');
    await user.click(screen.getByRole('button', { name: 'Entrar' }));
    const input = screen.getByRole('textbox', { name: 'Usuario' });
    await waitFor(() => expect(input).toHaveAttribute('aria-invalid', 'true'));
    expect(document.getElementById(input.getAttribute('aria-describedby')!)).toHaveTextContent('El formato no es válido.');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('errores de red y de servidor, y fallos que no son de la API, dan su aviso general', async () => {
    server.use(http.post(`${API}/auth/login`, () => HttpResponse.error()));
    const { user } = await setup(<LoginPage />);
    await user.type(screen.getByRole('textbox', { name: 'Usuario' }), 'admin');
    await user.type(screen.getByLabelText('Contraseña'), 'x');
    await user.click(screen.getByRole('button', { name: 'Entrar' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('No hay conexión con el servidor');
    server.use(http.post(`${API}/auth/login`, () => HttpResponse.json({ ok: true })));
    await user.type(screen.getByLabelText('Contraseña'), 'x');
    await user.click(screen.getByRole('button', { name: 'Entrar' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('El servidor no ha podido atender la petición'));
  });

  it('un acceso correcto llama a onAuthenticated; con cambio de clave pendiente, a onMustChangePassword', async () => {
    const onAuthenticated = vi.fn();
    const onMustChangePassword = vi.fn();
    const first = await setup(<LoginPage onAuthenticated={onAuthenticated} onMustChangePassword={onMustChangePassword} />);
    await first.user.type(screen.getByRole('textbox', { name: 'Usuario' }), ' admin ');
    await first.user.type(screen.getByLabelText('Contraseña'), 'Correct-pass-1');
    await first.user.keyboard('{Enter}');
    await waitFor(() => expect(onAuthenticated).toHaveBeenCalledTimes(1));
    expect(onMustChangePassword).not.toHaveBeenCalled();
    expect(mock.callsTo('POST', '/api/auth/login')[0]!.body).toEqual({ username: 'admin', password: 'Correct-pass-1' });
    first.unmount();

    state.mustChangePassword = true;
    const second = await setup(<LoginPage onAuthenticated={onAuthenticated} onMustChangePassword={onMustChangePassword} />);
    await second.user.type(screen.getByRole('textbox', { name: 'Usuario' }), 'admin');
    await second.user.type(screen.getByLabelText('Contraseña'), 'Correct-pass-1');
    await second.user.click(screen.getByRole('button', { name: 'Entrar' }));
    await waitFor(() => expect(onMustChangePassword).toHaveBeenCalledTimes(1));
    expect(onAuthenticated).toHaveBeenCalledTimes(1);
  });

  it('mientras envía, la acción queda no disponible (aria-disabled, type=button, motivo) y no reenvía', async () => {
    let release: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    let hits = 0;
    server.use(
      http.post(`${API}/auth/login`, async () => {
        hits += 1;
        await gate;
        return HttpResponse.json({ title: 'Unauthorized' }, { status: 401 });
      }),
    );
    const { user } = await setup(<LoginPage />);
    await user.type(screen.getByRole('textbox', { name: 'Usuario' }), 'admin');
    await user.type(screen.getByLabelText('Contraseña'), 'x');
    await user.click(screen.getByRole('button', { name: 'Entrar' }));
    const busy = await screen.findByRole('button', { name: 'Entrar' });
    await waitFor(() => expect(busy).toHaveAttribute('aria-disabled', 'true'));
    expect(busy).toHaveAttribute('type', 'button');
    expect(busy).toHaveAttribute('data-th-reason', 'Un momento…');
    expect(screen.getByRole('status')).toHaveTextContent('Un momento…');
    expect(screen.getByRole('form')).toHaveAttribute('aria-busy', 'true');
    await user.click(busy);
    release();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Entrar' })).toHaveAttribute('type', 'submit'));
    expect(hits).toBe(1);
  });

  it('en inglés usa las claves en inglés', async () => {
    await setup(<LoginPage />, { lng: 'en' });
    expect(screen.getByRole('form', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'Username' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument();
  });
});

describe('ChangePasswordPage', () => {
  async function mustChangeStore() {
    state.mustChangePassword = true;
    const store = new SessionStore({ baseUrl: API, storage: null });
    await store.login('admin', 'Correct-pass-1');
    mock.calls.length = 0;
    return store;
  }

  it('se localiza por rol y nombre accesible, avisa de que es obligatoria y ofrece cerrar sesión', async () => {
    await setup(<ChangePasswordPage />, { store: await mustChangeStore() });
    const form = screen.getByRole('form', { name: 'Cambiar la contraseña' });
    expect(screen.getByText('Debes cambiar la contraseña antes de continuar.')).toBeInTheDocument();
    expect(within(form).getByLabelText('Contraseña actual')).toHaveAttribute('autocomplete', 'current-password');
    const next = within(form).getByLabelText('Contraseña nueva');
    expect(next).toHaveAttribute('autocomplete', 'new-password');
    expect(next).toHaveAccessibleDescription('La política de contraseñas la comprueba el servidor.');
    expect(within(form).getByLabelText('Repite la contraseña nueva')).toBeRequired();
    expect(within(form).getByRole('button', { name: 'Cambiar la contraseña' })).toHaveAttribute('data-th-kind', 'primary');
    const signOut = within(form).getByRole('button', { name: 'Cerrar sesión' });
    expect(signOut).toHaveAttribute('type', 'button');
    expect(signOut).toHaveAttribute('data-th-kind', 'quiet');
  });

  it('valida en el cliente: obligatorios, distinta de la actual y repetida igual', async () => {
    const { user } = await setup(<ChangePasswordPage />, { store: await mustChangeStore() });
    await user.click(screen.getByRole('button', { name: 'Cambiar la contraseña' }));
    for (const label of ['Contraseña actual', 'Contraseña nueva', 'Repite la contraseña nueva']) {
      const input = screen.getByLabelText(label);
      expect(input).toHaveAttribute('aria-invalid', 'true');
      expect(input).toHaveAccessibleDescription(expect.stringContaining('Este dato es obligatorio.'));
    }
    expect(screen.getByLabelText('Contraseña actual')).toHaveFocus();
    await user.type(screen.getByLabelText('Contraseña actual'), 'Old-pass-1');
    await user.type(screen.getByLabelText('Contraseña nueva'), 'Old-pass-1');
    await user.type(screen.getByLabelText('Repite la contraseña nueva'), 'otra');
    await user.click(screen.getByRole('button', { name: 'Cambiar la contraseña' }));
    expect(screen.getByLabelText('Contraseña nueva')).toHaveAccessibleDescription(expect.stringContaining('distinta de la actual'));
    expect(screen.getByLabelText('Repite la contraseña nueva')).toHaveAccessibleDescription('Las contraseñas no coinciden.');
    expect(mock.callsTo('POST', '/api/auth/change-password')).toHaveLength(0);
  });

  it('los errores del servidor van a su campo (dos formas de código) y el resto a un aviso', async () => {
    const { user } = await setup(<ChangePasswordPage />, { store: await mustChangeStore() });
    await user.type(screen.getByLabelText('Contraseña actual'), 'mala');
    await user.type(screen.getByLabelText('Contraseña nueva'), 'otra-larga-1');
    await user.type(screen.getByLabelText('Repite la contraseña nueva'), 'otra-larga-1');
    await user.click(screen.getByRole('button', { name: 'Cambiar la contraseña' }));
    const current = screen.getByLabelText('Contraseña actual');
    await waitFor(() => expect(current).toHaveAttribute('aria-invalid', 'true'));
    expect(current).toHaveAccessibleDescription('Hay datos que no son válidos. Revisa el formulario.');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    await user.clear(current);
    await user.type(current, 'Old-pass-1');
    await user.clear(screen.getByLabelText('Contraseña nueva'));
    await user.type(screen.getByLabelText('Contraseña nueva'), 'corta');
    await user.clear(screen.getByLabelText('Repite la contraseña nueva'));
    await user.type(screen.getByLabelText('Repite la contraseña nueva'), 'corta');
    await user.click(screen.getByRole('button', { name: 'Cambiar la contraseña' }));
    await waitFor(() =>
      expect(screen.getByLabelText('Contraseña nueva')).toHaveAccessibleDescription(expect.stringContaining('El texto es demasiado corto.')),
    );

    server.use(http.post(`${API}/auth/change-password`, () => HttpResponse.json({ title: 'Boom', status: 500 }, { status: 500 })));
    await user.click(screen.getByRole('button', { name: 'Cambiar la contraseña' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('El servidor no ha podido atender la petición');
    server.use(http.post(`${API}/auth/change-password`, () => HttpResponse.json({ nada: true })));
    await user.click(screen.getByRole('button', { name: 'Cambiar la contraseña' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('El servidor no ha podido atender la petición'));
  });

  it('al cambiarla llama a onChanged y la sesión pasa a `authenticated` con contexto', async () => {
    const onChanged = vi.fn();
    const store = await mustChangeStore();
    const { user } = await setup(<ChangePasswordPage onChanged={onChanged} />, { store });
    await user.type(screen.getByLabelText('Contraseña actual'), 'Old-pass-1');
    await user.type(screen.getByLabelText('Contraseña nueva'), 'Brand-new-pass-1');
    await user.type(screen.getByLabelText('Repite la contraseña nueva'), 'Brand-new-pass-1');
    await user.click(screen.getByRole('button', { name: 'Cambiar la contraseña' }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(store.getSnapshot().session.status).toBe('authenticated');
    expect(store.getSnapshot().session.can('Facilities.Facility.Read')).toBe(true);
  });

  it('cerrar sesión desde la página la cierra y avisa', async () => {
    const onSignedOut = vi.fn();
    const store = await mustChangeStore();
    const { user } = await setup(<ChangePasswordPage onSignedOut={onSignedOut} />, { store });
    await user.click(screen.getByRole('button', { name: 'Cerrar sesión' }));
    await waitFor(() => expect(onSignedOut).toHaveBeenCalled());
    expect(store.getSnapshot().session.status).toBe('anonymous');
  });

  it('cuando es voluntaria enseña la introducción y no la advertencia', async () => {
    await setup(<ChangePasswordPage />, { store: await signedInStore() });
    expect(screen.getByText('Elige una contraseña nueva.')).toBeInTheDocument();
    expect(screen.queryByText(/antes de continuar/)).not.toBeInTheDocument();
  });
});

describe('OrganizationScopePicker', () => {
  it('no pinta nada sin sesión', async () => {
    const { container } = await setup(<OrganizationScopePicker />);
    expect(container).toBeEmptyDOMElement();
  });

  it('lista las organizaciones por nombre y cambiar la selección fija X-Organization-Scope', async () => {
    const store = await signedInStore();
    const onChange = vi.fn();
    const { user } = await setup(<OrganizationScopePicker onChange={onChange} />, { store });
    const group = await screen.findByRole('group', { name: 'Ámbito de organizaciones' });
    expect(group).toHaveAccessibleDescription('Sin selección se ven todas las organizaciones a las que tienes acceso.');
    const boxes = await within(group).findAllByRole('checkbox');
    expect(boxes.map((b) => b.closest('label')!.textContent)).toEqual(['Acme SA', 'Otra SA', 'Zeta SL']);
    const all = within(group).getByRole('button', { name: 'Todas las organizaciones' });
    expect(all).toHaveAttribute('aria-disabled', 'true');
    expect(all).toHaveAttribute('data-th-reason', 'Ya se ven todas las organizaciones.');

    await user.click(screen.getByRole('checkbox', { name: 'Zeta SL' }));
    await waitFor(() => expect(store.getSnapshot().scope).toEqual(['org-2']));
    expect(onChange).toHaveBeenLastCalledWith(['org-2']);
    expect(screen.getByRole('checkbox', { name: 'Zeta SL' })).toBeChecked();
    expect(mock.callsTo('GET', '/api/auth/context').at(-1)!.headers['x-organization-scope']).toBe('org-2');
    await user.click(screen.getByRole('checkbox', { name: 'Acme SA' }));
    await waitFor(() => expect(store.getSnapshot().scope).toEqual(['org-2', 'org-1']));
    await user.click(screen.getByRole('checkbox', { name: 'Zeta SL' }));
    await waitFor(() => expect(store.getSnapshot().scope).toEqual(['org-1']));

    await user.click(within(group).getByRole('button', { name: 'Todas las organizaciones' }));
    await waitFor(() => expect(store.getSnapshot().scope).toEqual([]));
    expect(onChange).toHaveBeenLastCalledWith([]);
    // pulsar «todas» sin selección no hace nada
    const callsBefore = mock.calls.length;
    await user.click(within(group).getByRole('button', { name: 'Todas las organizaciones' }));
    expect(mock.calls.length).toBe(callsBefore);
  });

  it('un usuario que no es administrador global sólo ve las organizaciones de su contexto', async () => {
    state.permissions = ['Facilities.Facility.Read']; // no es administrador global; efectivas: org-1 y org-2
    const store = await signedInStore();
    await setup(<OrganizationScopePicker />, { store });
    const boxes = await screen.findAllByRole('checkbox');
    expect(boxes.map((b) => b.closest('label')!.textContent)).toEqual(['Acme SA', 'Zeta SL']);
  });

  it('muestra la carga, el vacío y los errores', async () => {
    server.use(http.get(`${API}/internal-organizations/names`, () => HttpResponse.json({ items: [{ ID: 'x', Name: 'Mayúsculas' }, { name: 'sin id' }, 5] })));
    const store = await signedInStore();
    const view = await setup(<OrganizationScopePicker />, { store });
    expect(screen.getByRole('status')).toHaveTextContent('Cargando organizaciones…');
    expect(await screen.findByRole('checkbox', { name: 'Mayúsculas' })).toBeInTheDocument();
    view.unmount();

    server.use(http.get(`${API}/internal-organizations/names`, () => HttpResponse.json([])));
    const empty = await setup(<OrganizationScopePicker />, { store });
    expect(await screen.findByText('No hay organizaciones que elegir.')).toBeInTheDocument();
    empty.unmount();

    server.use(http.get(`${API}/internal-organizations/names`, () => HttpResponse.json({ title: 'Forbidden', status: 403 }, { status: 403 })));
    const forbidden = await setup(<OrganizationScopePicker />, { store });
    expect(await screen.findByRole('alert')).toHaveTextContent('No tienes permiso para ver la lista de organizaciones.');
    forbidden.unmount();

    server.use(http.get(`${API}/internal-organizations/names`, () => HttpResponse.json({ title: 'x' }, { status: 500 })));
    await setup(<OrganizationScopePicker />, { store });
    expect(await screen.findByRole('alert')).toHaveTextContent('No se han podido cargar las organizaciones.');
  });
});

describe('SessionProvider y ganchos', () => {
  it('sin proveedor los ganchos fallan con un mensaje claro', () => {
    expect(() => renderHook(() => useSession())).toThrow(/SessionProvider/);
    expect(() => renderHook(() => useSessionActions())).toThrow(/SessionProvider/);
    expect(() => renderHook(() => useSessionState())).toThrow(/SessionProvider/);
    expect(() => renderHook(() => useHttpClient())).toThrow(/SessionProvider/);
  });

  it('publica la sesión del contrato, el estado y las acciones', async () => {
    const wrapper = ({ children }: PropsWithChildren) => (
      <SessionProvider apiBaseUrl={API} storage={null}>
        {children}
      </SessionProvider>
    );
    const { result } = renderHook(() => ({ session: useSession(), state: useSessionState(), actions: useSessionActions(), http: useHttpClient() }), { wrapper });
    expect(result.current.session.status).toBe('anonymous');
    expect(result.current.session.can('A.B.C')).toBe(false);
    await act(async () => {
      await result.current.actions.login('admin', 'Correct-pass-1');
    });
    expect(result.current.session.status).toBe('authenticated');
    expect(result.current.state.username).toBe('admin');
    expect(result.current.session.can('Cualquier.Cosa.Mas')).toBe(true);
    await expect(result.current.http.request('/v1/facilities/f1')).resolves.toMatchObject({ id: 'f1' });
    await act(async () => {
      await result.current.actions.reloadContext();
    });
    expect(state.contextCalls).toBe(2);
    await act(async () => {
      await result.current.actions.logout();
    });
    expect(result.current.session.status).toBe('anonymous');
  });

  it('crea el almacén con las opciones, cambiar el ámbito invalida las consultas y cerrar sesión las vacía', async () => {
    const onScopeChange = vi.fn();
    const onSignedOut = vi.fn();
    const queryClient = new QueryClient();
    queryClient.setQueryData(['entity', 'x'], 1);
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries');
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={queryClient}>
        <SessionProvider apiBaseUrl={API} storage={null} onScopeChange={onScopeChange} onSignedOut={onSignedOut} getLanguage={() => 'en'}>
          {children}
        </SessionProvider>
      </QueryClientProvider>
    );
    const { result } = renderHook(() => ({ actions: useSessionActions(), http: useHttpClient() }), { wrapper });
    await act(async () => {
      await result.current.actions.login('admin', 'Correct-pass-1');
    });
    expect(mock.callsTo('POST', '/api/auth/login')[0]!.headers['accept-language']).toBe('en');
    await act(async () => {
      await result.current.actions.setScope(['org-1']);
    });
    expect(onScopeChange).toHaveBeenCalledTimes(1);
    expect(invalidate).toHaveBeenCalled();
    // el cierre pedido por el usuario vacía la caché pero no avisa como si hubiera caducado
    await act(async () => {
      await result.current.actions.logout();
    });
    expect(queryClient.getQueryData(['entity', 'x'])).toBeUndefined();
    expect(onSignedOut).not.toHaveBeenCalled();
    // un refresco rechazado cierra la sesión sin que el usuario lo pida
    await act(async () => {
      await result.current.actions.login('admin', 'Correct-pass-1');
    });
    state.validAccess = 'rotado';
    state.validRefresh = 'otro';
    await act(async () => {
      await result.current.http.request('/v1/facilities/f1').catch(() => undefined);
    });
    expect(onSignedOut).toHaveBeenCalledTimes(1);
  });
});
