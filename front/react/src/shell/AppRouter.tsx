// Rutas de la aplicación (PLAN §4.7) y creación del router (navegador, hash o memoria).
//   /login            entrada
//   /cambiar-clave    cambio de contraseña (obligatorio con `must-change-password`)
//   /                 inicio
//   /403              sin permiso
//   /:entity          vista de la entidad (?view=grid|tree|map)
//   /:entity/nuevo    alta
//   /:entity/:id      detalle (?mode=edit)
//   *                 no encontrada
import { lazy, Suspense, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Navigate,
  createBrowserRouter,
  createHashRouter,
  createMemoryRouter,
  useLocation,
  useNavigate,
  type RouteObject,
} from 'react-router';
import { RouterProvider } from 'react-router/dom';
import { useSessionState } from '../auth';
import type { KarpoAppProps } from '../contracts';
import { Skeleton } from '../ui';
import { EntityScreen } from './EntityScreen';
import { ForbiddenPage, HomePage, NotFoundPage } from './pages';
import { Shell } from './Shell';

// Las pantallas de acceso se descargan sólo al usarse (D-R37): el 99 % de las visitas ya tiene sesión y no las
// necesita en el trozo inicial. Los textos que usan (`auth`, `errors`) son del marco y ya están cargados.
const LoginPage = lazy(() => import('../auth/LoginPage').then((module) => ({ default: module.LoginPage })));
const ChangePasswordPage = lazy(() =>
  import('../auth/ChangePasswordPage').then((module) => ({ default: module.ChangePasswordPage })),
);

/** Esqueleto mientras llega una pantalla diferida (la misma de «cargando» del resto de la aplicación). */
function LoadingPage() {
  const { t } = useTranslation('core');
  return (
    <main data-kx-page="loading">
      <Skeleton lines={4} label={t('app.loading')} />
    </main>
  );
}

/** Sólo rutas internas como destino tras entrar (nada de `//otro.sitio` ni `https://…`). */
export function safeReturnUrl(value: string | null): string {
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\')) return '/';
  return value;
}

function LoginRoute() {
  const { session } = useSessionState();
  const location = useLocation();
  const navigate = useNavigate();
  const returnUrl = safeReturnUrl(new URLSearchParams(location.search).get('returnUrl'));
  if (session.status === 'authenticated') return <Navigate to={returnUrl} replace />;
  if (session.status === 'must-change-password') return <Navigate to="/cambiar-clave" replace />;
  return (
    <Suspense fallback={<LoadingPage />}>
      <LoginPage
        onAuthenticated={() => void navigate(returnUrl, { replace: true })}
        onMustChangePassword={() => void navigate('/cambiar-clave', { replace: true })}
      />
    </Suspense>
  );
}

function ChangePasswordRoute() {
  const { session } = useSessionState();
  const navigate = useNavigate();
  if (session.status === 'anonymous') return <Navigate to="/login" replace />;
  return (
    <Suspense fallback={<LoadingPage />}>
      <ChangePasswordPage
        onChanged={() => void navigate('/', { replace: true })}
        onSignedOut={() => void navigate('/login', { replace: true })}
      />
    </Suspense>
  );
}

/** Exige sesión; mientras se cargan los permisos de una sesión restaurada, muestra un esqueleto. */
function AuthLayout() {
  const { session, contextError } = useSessionState();
  const location = useLocation();
  if (session.status === 'anonymous') {
    const returnUrl = `${location.pathname}${location.search}`;
    const query = returnUrl === '/' ? '' : `?returnUrl=${encodeURIComponent(returnUrl)}`;
    return <Navigate to={`/login${query}`} replace />;
  }
  if (session.status === 'must-change-password') return <Navigate to="/cambiar-clave" replace />;
  if (!session.context && !contextError) return <LoadingPage />;
  return <Shell />;
}

/** Las rutas hijas de una entidad no pintan nada: la pantalla de la entidad lee la URL y pinta el detalle. */
function Nothing(): null {
  return null;
}

export function createRoutes(): RouteObject[] {
  return [
    { path: '/login', element: <LoginRoute /> },
    { path: '/cambiar-clave', element: <ChangePasswordRoute /> },
    {
      element: <AuthLayout />,
      children: [
        { index: true, element: <HomePage /> },
        { path: '403', element: <ForbiddenPage /> },
        {
          path: ':entity',
          element: <EntityScreen />,
          // El detalle lo pinta la propia pantalla de la entidad (así la lista no se desmonta).
          children: [{ index: true, Component: Nothing }, { path: 'nuevo', Component: Nothing }, { path: ':id', Component: Nothing }],
        },
        { path: '*', element: <NotFoundPage /> },
      ],
    },
  ];
}

export interface AppRouterProps {
  kind: NonNullable<KarpoAppProps['router']>;
  basename?: string;
  initialPath?: string;
}

function withBase(path: string, basename: string | undefined): string {
  if (!basename || basename === '/') return path;
  const base = basename.replace(/\/$/, '');
  return path === base || path.startsWith(`${base}/`) ? path : `${base}${path.startsWith('/') ? '' : '/'}${path}`;
}

export function AppRouter({ kind, basename, initialPath }: AppRouterProps) {
  const router = useMemo(() => {
    const routes = createRoutes();
    const options = basename ? { basename } : undefined;
    if (kind === 'memory') {
      return createMemoryRouter(routes, {
        ...options,
        initialEntries: [withBase(initialPath ?? '/', basename)],
      });
    }
    return kind === 'hash' ? createHashRouter(routes, options) : createBrowserRouter(routes, options);
    // El router se crea una vez: cambiar de tipo o de base en caliente no tiene sentido.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return <RouterProvider router={router} />;
}
