// Cambio de contraseña (obligatorio tras `mustChangePassword`, o voluntario). Devuelve una sesión nueva.
import { useId, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { describeApiError, describeFieldError, isApiError } from '../api';
import { AuthField } from './AuthField';
import { useSessionActions, useSessionState } from './SessionProvider';

export interface ChangePasswordPageProps {
  /** Tras cambiar la contraseña (la sesión ya es la nueva y está autenticada). */
  onChanged?(): void;
  /** Tras cerrar la sesión desde esta página. */
  onSignedOut?(): void;
}

type FieldName = 'currentPassword' | 'newPassword' | 'confirmPassword';
const SERVER_FIELDS = ['currentPassword', 'newPassword'] as const;

export function ChangePasswordPage({ onChanged, onSignedOut }: ChangePasswordPageProps = {}) {
  const { t } = useTranslation(['auth', 'errors']);
  const { changePassword, logout } = useSessionActions();
  const { session } = useSessionState();
  const uid = useId();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [errors, setErrors] = useState<Partial<Record<FieldName, string>>>({});
  const [general, setGeneral] = useState<string | undefined>();
  const [pending, setPending] = useState(false);
  const busy = useRef(false);
  const mandatory = session.status === 'must-change-password';

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy.current) return;
    const found: Partial<Record<FieldName, string>> = {};
    if (current === '') found.currentPassword = t('errors:required');
    if (next === '') found.newPassword = t('errors:required');
    else if (next === current) found.newPassword = t('auth:changePassword.same');
    if (confirm === '') found.confirmPassword = t('errors:required');
    else if (confirm !== next) found.confirmPassword = t('auth:changePassword.mismatch');
    setErrors(found);
    setGeneral(undefined);
    const firstInvalid = (['currentPassword', 'newPassword', 'confirmPassword'] as const).find((n) => found[n]);
    if (firstInvalid) {
      (event.currentTarget.elements.namedItem(firstInvalid) as HTMLElement | null)?.focus();
      return;
    }
    busy.current = true;
    setPending(true);
    try {
      await changePassword(current, next);
      setCurrent('');
      setNext('');
      setConfirm('');
      onChanged?.();
    } catch (error) {
      if (isApiError(error)) {
        const fields: Partial<Record<FieldName, string>> = {};
        for (const name of SERVER_FIELDS) {
          const first = error.fieldErrors[name]?.[0];
          if (first) {
            const text = describeFieldError(first);
            fields[name] = t(text.key, { defaultValue: text.fallback });
          }
        }
        setErrors(fields);
        if (error.kind !== 'validation' || Object.keys(fields).length === 0) {
          const text = describeApiError(error);
          setGeneral(t(text.key, { defaultValue: text.fallback }));
        }
      } else {
        setGeneral(t('errors:server'));
      }
    } finally {
      busy.current = false;
      setPending(false);
    }
  }

  async function signOut() {
    await logout();
    onSignedOut?.();
  }

  const headingId = `${uid}-title`;
  const statusId = `${uid}-status`;
  return (
    <main data-kx-page="auth">
      <section data-th="sheet" aria-labelledby={headingId}>
        <header>
          <h1 id={headingId}>{t('auth:changePassword.title')}</h1>
          <p>{mandatory ? t('auth:changePassword.required') : t('auth:changePassword.intro')}</p>
        </header>
        <form noValidate aria-labelledby={headingId} aria-busy={pending || undefined} onSubmit={submit}>
          {general ? (
            <div data-th="feedback" data-th-tone="error" role="alert">
              {general}
            </div>
          ) : null}
          <AuthField
            id={`${uid}-current`}
            name="currentPassword"
            type="password"
            label={t('auth:changePassword.current')}
            value={current}
            onChange={setCurrent}
            error={errors.currentPassword}
            required
            autoComplete="current-password"
            autoFocus
          />
          <AuthField
            id={`${uid}-new`}
            name="newPassword"
            type="password"
            label={t('auth:changePassword.new')}
            value={next}
            onChange={setNext}
            error={errors.newPassword}
            hint={t('auth:changePassword.hint')}
            required
            autoComplete="new-password"
          />
          <AuthField
            id={`${uid}-confirm`}
            name="confirmPassword"
            type="password"
            label={t('auth:changePassword.confirm')}
            value={confirm}
            onChange={setConfirm}
            error={errors.confirmPassword}
            required
            autoComplete="new-password"
          />
          <p id={statusId} role="status">
            {pending ? t('auth:pending') : ''}
          </p>
          <div data-kx-actions>
            <button type="button" data-th="action" data-th-kind="quiet" onClick={() => void signOut()}>
              {t('auth:changePassword.signOut')}
            </button>
            <button
              type={pending ? 'button' : 'submit'}
              data-th="action"
              data-th-kind="primary"
              aria-disabled={pending || undefined}
              data-th-reason={pending ? t('auth:pending') : undefined}
              aria-describedby={pending ? statusId : undefined}
            >
              {t('auth:changePassword.submit')}
            </button>
          </div>
        </form>
      </section>
    </main>
  );
}
