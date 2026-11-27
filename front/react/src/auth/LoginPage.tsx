// Página de entrada. HTML semántico con los ganchos de Theros; textos sólo por clave (ns `auth`).
import { useId, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { describeApiError, describeFieldError, isApiError } from '../api';
import { AuthField } from './AuthField';
import { useSessionActions } from './SessionProvider';

export interface LoginPageProps {
  /** Tras un acceso correcto sin cambio de clave pendiente. */
  onAuthenticated?(): void;
  /** Tras un acceso correcto con `mustChangePassword`. */
  onMustChangePassword?(): void;
}

type FieldName = 'username' | 'password';

export function LoginPage({ onAuthenticated, onMustChangePassword }: LoginPageProps = {}) {
  const { t } = useTranslation(['auth', 'errors']);
  const { login } = useSessionActions();
  const uid = useId();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [errors, setErrors] = useState<Partial<Record<FieldName, string>>>({});
  const [general, setGeneral] = useState<string | undefined>();
  const [pending, setPending] = useState(false);
  const busy = useRef(false);
  const usernameRef = useRef<HTMLInputElement>(null);

  const fieldMessage = (error: { code: string; message: string }) => {
    const text = describeFieldError(error);
    return t(text.key, { defaultValue: text.fallback });
  };

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy.current) return;
    const next: Partial<Record<FieldName, string>> = {};
    if (username.trim() === '') next.username = t('errors:required');
    if (password === '') next.password = t('errors:required');
    setErrors(next);
    setGeneral(undefined);
    if (next.username || next.password) {
      const form = event.currentTarget;
      (form.elements.namedItem(next.username ? 'username' : 'password') as HTMLElement | null)?.focus();
      return;
    }
    busy.current = true;
    setPending(true);
    try {
      const status = await login(username.trim(), password);
      if (status === 'must-change-password') onMustChangePassword?.();
      else onAuthenticated?.();
    } catch (error) {
      if (isApiError(error)) {
        const fields: Partial<Record<FieldName, string>> = {};
        for (const name of ['username', 'password'] as const) {
          const first = error.fieldErrors[name]?.[0];
          if (first) fields[name] = fieldMessage(first);
        }
        setErrors(fields);
        if (error.kind === 'unauthorized') setGeneral(t('auth:login.invalidCredentials'));
        else if (Object.keys(fields).length === 0 || error.kind !== 'validation') {
          const text = describeApiError(error);
          setGeneral(t(text.key, { defaultValue: text.fallback }));
        }
      } else {
        setGeneral(t('errors:server'));
      }
      setPassword('');
      usernameRef.current?.focus();
    } finally {
      busy.current = false;
      setPending(false);
    }
  }

  const headingId = `${uid}-title`;
  const statusId = `${uid}-status`;
  return (
    <main data-kx-page="auth">
      <section data-th="sheet" aria-labelledby={headingId}>
        <header>
          <h1 id={headingId}>{t('auth:login.title')}</h1>
          <p>{t('auth:login.intro')}</p>
        </header>
        <form noValidate aria-labelledby={headingId} aria-busy={pending || undefined} onSubmit={submit}>
          {general ? (
            <div data-th="feedback" data-th-tone="error" role="alert">
              {general}
            </div>
          ) : null}
          <AuthField
            id={`${uid}-username`}
            name="username"
            label={t('auth:login.username')}
            value={username}
            onChange={setUsername}
            error={errors.username}
            required
            autoComplete="username"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            autoFocus
            inputRef={usernameRef}
          />
          <AuthField
            id={`${uid}-password`}
            name="password"
            type="password"
            label={t('auth:login.password')}
            value={password}
            onChange={setPassword}
            error={errors.password}
            required
            autoComplete="current-password"
          />
          <p id={statusId} role="status">
            {pending ? t('auth:pending') : ''}
          </p>
          <div data-kx-actions>
            <button
              type={pending ? 'button' : 'submit'}
              data-th="action"
              data-th-kind="primary"
              aria-disabled={pending || undefined}
              data-th-reason={pending ? t('auth:pending') : undefined}
              aria-describedby={pending ? statusId : undefined}
            >
              {t('auth:login.submit')}
            </button>
          </div>
        </form>
      </section>
    </main>
  );
}
