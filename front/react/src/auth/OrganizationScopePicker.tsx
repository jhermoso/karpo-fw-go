// Selector del ámbito de organizaciones: lo elegido viaja en `X-Organization-Scope` (A1 §8). Sin
// selección se ven todas las organizaciones a las que el usuario tiene acceso. El servidor
// intersecta la cabecera con las concesiones del token: no es una vía de escalada.
import { useQuery } from '@tanstack/react-query';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { isApiError } from '../api';
import { useHttpClient, useSessionActions, useSessionState } from './SessionProvider';
import './auth.css';

export interface OrganizationOption {
  id: string;
  name: string;
}

export interface OrganizationScopePickerProps {
  /** Tras cambiar la selección (el contexto ya está recargado). */
  onChange?(ids: string[]): void;
}

function toOptions(data: unknown): OrganizationOption[] {
  const list = Array.isArray(data)
    ? data
    : typeof data === 'object' && data !== null && Array.isArray((data as { items?: unknown }).items)
      ? ((data as { items: unknown[] }).items)
      : [];
  const out: OrganizationOption[] = [];
  for (const item of list) {
    if (typeof item !== 'object' || item === null) continue;
    const { id, ID, name, Name } = item as Record<string, unknown>;
    const realId = id ?? ID;
    if (realId === undefined || realId === null || realId === '') continue;
    out.push({ id: String(realId), name: String(name ?? Name ?? realId) });
  }
  return out.sort((a, b) => a.name.localeCompare(b.name));
}

export function OrganizationScopePicker({ onChange }: OrganizationScopePickerProps = {}) {
  const { t } = useTranslation('auth');
  const http = useHttpClient();
  const { setScope } = useSessionActions();
  const { scope, session } = useSessionState();
  const uid = useId();
  const authenticated = session.status === 'authenticated';
  const query = useQuery({
    queryKey: ['auth', 'scope-options'],
    queryFn: ({ signal }) => http.request<unknown>('/internal-organizations/names', { signal }),
    enabled: authenticated,
    staleTime: 5 * 60_000,
    select: toOptions,
  });

  if (!authenticated) return null;

  const context = session.context;
  const allowed = context && !context.isGlobalAdmin && context.effectiveOrganizationIds.length > 0
    ? new Set(context.effectiveOrganizationIds)
    : undefined;
  const options = (query.data ?? []).filter((o) => !allowed || allowed.has(o.id));
  const selected = new Set(scope);

  async function apply(next: string[]) {
    await setScope(next);
    onChange?.(next);
  }

  function toggle(id: string, checked: boolean) {
    const next = new Set(selected);
    if (checked) next.add(id);
    else next.delete(id);
    void apply([...next]);
  }

  const hintId = `${uid}-hint`;
  return (
    <fieldset data-kx-scope aria-describedby={hintId} aria-busy={query.isPending || undefined}>
      <legend>{t('scope.label')}</legend>
      <p data-th="hint" id={hintId}>
        {t('scope.hint')}
      </p>
      {query.isPending ? <p role="status">{t('scope.loading')}</p> : null}
      {query.isError ? (
        <p data-th="feedback" data-th-tone="error" role="alert">
          {isApiError(query.error) && query.error.kind === 'forbidden' ? t('scope.forbidden') : t('scope.error')}
        </p>
      ) : null}
      {query.isSuccess && options.length === 0 ? <p>{t('scope.empty')}</p> : null}
      {options.map((option) => (
        <label key={option.id}>
          <input
            type="checkbox"
            checked={selected.has(option.id)}
            onChange={(event) => toggle(option.id, event.target.checked)}
          />
          <span>{option.name}</span>
        </label>
      ))}
      <div data-kx-actions>
        <button
          type="button"
          data-th="action"
          data-th-kind="quiet"
          aria-disabled={scope.length === 0 || undefined}
          data-th-reason={scope.length === 0 ? t('scope.allSelected') : undefined}
          onClick={() => {
            if (scope.length > 0) void apply([]);
          }}
        >
          {t('scope.all')}
        </button>
      </div>
    </fieldset>
  );
}
