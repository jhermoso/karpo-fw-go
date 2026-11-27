// Búsqueda rápida y filtros de la configuración (`config.filters`): chips con su valor y un panel con el
// editor de cada tipo (texto, número, sí/no, lista, varias, fecha). El estado vive en la URL (url-state.ts).
import * as Popover from '@radix-ui/react-popover';
import { useEffect, useId, useRef, useState, type FormEvent, type RefObject } from 'react';
import type { FilterDef } from '../contracts';
import { Action, Chip, Field, Icon } from '../ui';
import type { GridEnv } from './types';
import { chipFilters, quickSearchFilter, type QueryState } from './url-state';

const SEARCH_DELAY = 250;

interface FilterBarProps {
  env: GridEnv;
  query: QueryState;
  onChange(next: QueryState): void;
  searchRef: RefObject<HTMLInputElement | null>;
}

/** Texto de un valor de filtro para el chip. */
export function filterValueLabel(env: GridEnv, filter: FilterDef, value: string): string {
  if (filter.kind === 'boolean') return value === 'true' ? env.t('filters.yes') : env.t('filters.no');
  if (filter.kind === 'select' || filter.kind === 'multiselect') {
    const labels = value.split(',').map((part) => {
      const option = filter.options?.find((candidate) => candidate.value === part);
      return option ? env.text(option.label) : part;
    });
    return labels.join(', ');
  }
  return value;
}

export function FilterBar({ env, query, onChange, searchRef }: FilterBarProps) {
  const { config, t } = env;
  const search = quickSearchFilter(config);
  const filters = chipFilters(config);
  const active = filters.filter((filter) => (query.filters[filter.field] ?? '') !== '');

  return (
    <>
      {search ? <SearchBox env={env} filter={search} query={query} onChange={onChange} inputRef={searchRef} /> : null}
      {filters.length > 0 ? (
        <div data-kx-part="filters" data-kx-help="grid.filters" role="group" aria-label={t('filters.label')}>
          <FiltersPanel env={env} filters={filters} query={query} onChange={onChange} activeCount={active.length} />
          {active.map((filter) => (
            <Chip
              key={filter.field}
              tone="info"
              removeLabel={t('filters.remove', { label: env.text(filter.label) })}
              onRemove={() => {
                const next = { ...query.filters };
                delete next[filter.field];
                onChange({ ...query, filters: next });
              }}
            >
              {t('filters.chip', {
                label: env.text(filter.label),
                value: filterValueLabel(env, filter, query.filters[filter.field] ?? ''),
              })}
            </Chip>
          ))}
        </div>
      ) : null}
    </>
  );
}

function SearchBox({
  env,
  filter,
  query,
  onChange,
  inputRef,
}: {
  env: GridEnv;
  filter: FilterDef;
  query: QueryState;
  onChange(next: QueryState): void;
  inputRef: RefObject<HTMLInputElement | null>;
}) {
  const { t } = env;
  const [draft, setDraft] = useState(query.search);
  const [seen, setSeen] = useState(query.search);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const latest = useRef({ query, onChange });
  useEffect(() => {
    latest.current = { query, onChange };
  });
  // La URL manda: si la búsqueda cambia desde fuera (atrás, enlace), el cuadro la sigue.
  if (query.search !== seen) {
    setSeen(query.search);
    setDraft(query.search);
  }
  useEffect(() => () => clearTimeout(timer.current), []);

  function commit(text: string): void {
    clearTimeout(timer.current);
    const current = latest.current;
    if (text !== current.query.search) current.onChange({ ...current.query, search: text });
  }

  const label = env.text(filter.label) || t('search.label');
  return (
    <div role="search" data-kx-part="search" data-kx-help="grid.search" aria-label={label}>
      <Icon name="search" />
      <input
        ref={inputRef}
        type="search"
        value={draft}
        aria-label={label}
        placeholder={t('search.placeholder')}
        autoComplete="off"
        onChange={(event) => {
          const text = event.target.value;
          setDraft(text);
          clearTimeout(timer.current);
          timer.current = setTimeout(() => commit(text), SEARCH_DELAY);
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter') {
            event.preventDefault();
            commit(draft);
          } else if (event.key === 'Escape') {
            setDraft('');
            commit('');
          }
        }}
      />
    </div>
  );
}

function FiltersPanel({
  env,
  filters,
  query,
  onChange,
  activeCount,
}: {
  env: GridEnv;
  filters: FilterDef[];
  query: QueryState;
  onChange(next: QueryState): void;
  activeCount: number;
}) {
  const { t } = env;
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>(query.filters);
  const baseId = useId();

  function openChange(next: boolean): void {
    if (next) setDraft(query.filters);
    setOpen(next);
  }

  function submit(event: FormEvent): void {
    event.preventDefault();
    const cleaned: Record<string, string> = {};
    for (const [field, value] of Object.entries(draft)) if (value !== '') cleaned[field] = value;
    onChange({ ...query, filters: cleaned });
    setOpen(false);
  }

  return (
    <Popover.Root open={open} onOpenChange={openChange}>
      <Popover.Trigger asChild>
        <Action kind="secondary" icon="filter">
          {activeCount > 0 ? t('filters.buttonActive', { count: activeCount }) : t('filters.add')}
        </Action>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          data-kx-ui="filters-panel"
          data-th-elevation="overlay"
          aria-label={t('filters.label')}
          align="start"
          sideOffset={4}
        >
          <form onSubmit={submit} data-kx-part="filters-form">
            {filters.map((filter, index) => (
              <FilterEditor
                key={filter.field}
                env={env}
                filter={filter}
                id={`${baseId}-${index}`}
                value={draft[filter.field] ?? ''}
                onValue={(value) => setDraft((current) => ({ ...current, [filter.field]: value }))}
              />
            ))}
            <div data-kx-part="filters-footer">
              <Action
                kind="quiet"
                onClick={() => {
                  setDraft({});
                }}
              >
                {t('filters.clear')}
              </Action>
              <Action kind="primary" type="submit">
                {t('filters.apply')}
              </Action>
            </div>
          </form>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

function FilterEditor({
  env,
  filter,
  id,
  value,
  onValue,
}: {
  env: GridEnv;
  filter: FilterDef;
  id: string;
  value: string;
  onValue(value: string): void;
}) {
  const { t } = env;
  const label = env.text(filter.label);
  const hint = filter.hint ? env.text(filter.hint) : undefined;

  if (filter.kind === 'multiselect') {
    const chosen = new Set(value.split(',').filter(Boolean));
    return (
      <fieldset data-th="field" data-kx-part="filter-group">
        <legend>{label}</legend>
        {(filter.options ?? []).map((option) => (
          <label key={option.value} data-kx-part="filter-option">
            <input
              type="checkbox"
              checked={chosen.has(option.value)}
              onChange={(event) => {
                const next = new Set(chosen);
                if (event.target.checked) next.add(option.value);
                else next.delete(option.value);
                onValue([...next].join(','));
              }}
            />
            {env.text(option.label)}
          </label>
        ))}
      </fieldset>
    );
  }

  return (
    <Field id={id} label={label} hint={hint}>
      {(control) => {
        if (filter.kind === 'boolean') {
          return (
            <select {...control} value={value} onChange={(event) => onValue(event.target.value)}>
              <option value="">{t('filters.all')}</option>
              <option value="true">{t('filters.yes')}</option>
              <option value="false">{t('filters.no')}</option>
            </select>
          );
        }
        if (filter.kind === 'select') {
          return (
            <select {...control} value={value} onChange={(event) => onValue(event.target.value)}>
              <option value="">{t('filters.all')}</option>
              {(filter.options ?? []).map((option) => (
                <option key={option.value} value={option.value}>
                  {env.text(option.label)}
                </option>
              ))}
            </select>
          );
        }
        return (
          <input
            {...control}
            type={filter.kind === 'number' ? 'number' : filter.kind === 'date' ? 'date' : 'text'}
            inputMode={filter.kind === 'number' ? 'decimal' : undefined}
            value={value}
            placeholder={filter.kind === 'text' ? t('filters.textPlaceholder') : undefined}
            onChange={(event) => onValue(event.target.value)}
          />
        );
      }}
    </Field>
  );
}
