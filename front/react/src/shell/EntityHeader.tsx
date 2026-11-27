// Cabecera de la entidad (M-6, M-7, D-R23, D-R32): el único `h1` de la pantalla, recuento plural, el único control de vista y la acción
// principal «Nuevo». Va encima de los paneles de `EntityScreen`; las vistas le cuentan lo que muestran con
// `onStatus` y la barra superior ya no lleva selector de vista.
import { useCallback, useMemo, useRef, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useSession } from '../auth';
import { useText } from '../config';
import { viewChoices, type EntityConfig, type EntityRoute, type ViewChoice, type ViewStatus } from '../contracts';
import { useLocale } from '../i18n';
import { Action, useShortcut } from '../ui';
import { useCompact } from './hooks';
import { canWriteEntity } from './menu';
import { preloadDetail } from './lazy';
import { pickView } from './route';

export interface EntityHeaderProps {
  config: EntityConfig;
  route: EntityRoute;
  navigate(to: EntityRoute): void;
  /** Lo último que la vista activa ha contado (recuento y filtro). */
  status: ViewStatus | undefined;
}

/** La opción del control que corresponde a la ruta: la vista y, en el árbol, su disposición. */
export function currentChoice(config: EntityConfig, route: EntityRoute): ViewChoice | undefined {
  const choices = viewChoices(config);
  const view = pickView(config, route);
  if (view === 'tree') {
    const wanted = route.params?.layout === 'diagram' ? 'diagram' : 'tree';
    return choices.find((choice) => choice.id === wanted);
  }
  return choices.find((choice) => choice.view === view);
}

/** La ruta a la que lleva una opción: conserva el registro y los parámetros, y fija `layout` sólo en el árbol. */
export function routeForChoice(route: EntityRoute, choice: ViewChoice): EntityRoute {
  const { layout: _previous, ...rest } = route.params ?? {};
  void _previous;
  const params = choice.view === 'tree' && choice.layout ? { ...rest, layout: choice.layout } : rest;
  return { ...route, view: choice.view, params };
}

/** ¿Se puede dar de alta desde la cabecera? Formulario de alta, entidad editable y permiso de escritura. */
export function canCreateEntity(config: EntityConfig, session: Parameters<typeof canWriteEntity>[1]): boolean {
  return Boolean(config.form?.create) && config.editable && canWriteEntity(config, session);
}

export function EntityHeader({ config, route, navigate, status }: EntityHeaderProps) {
  const { t } = useTranslation('core');
  const resolve = useText();
  const { locale } = useLocale();
  const session = useSession();
  const compact = useCompact();
  const choices = useMemo(() => viewChoices(config), [config]);
  const current = currentChoice(config, route);
  const allowed = canCreateEntity(config, session);

  const create = useCallback(() => navigate({ entity: config.id, mode: 'edit' }), [config.id, navigate]);
  const select = useCallback(
    (choice: ViewChoice) => {
      if (choice.id !== current?.id) navigate(routeForChoice(route, choice));
    },
    [current?.id, navigate, route],
  );

  const name = resolve(config.name);
  const plural = resolve(config.namePlural);
  const count = useMemo(
    () => countText((key, vars) => String(t(key, vars ?? {})), locale, status, name, plural),
    [t, locale, status, name, plural],
  );

  return (
    <div data-kx-part="entity-header" data-kx-compact={compact ? '' : undefined}>
      <div data-kx-part="entity-titles">
        <h1 data-kx-part="entity-title">{plural}</h1>
        <p data-kx-part="entity-count" aria-busy={status?.loading ? true : undefined}>
          {count}
        </p>
      </div>
      <div data-kx-part="entity-actions">
        {choices.length > 1 ? <ViewControl choices={choices} current={current} onSelect={select} compact={compact} /> : null}
        {allowed ? (
          <>
            <NewShortcut onRun={create} />
            <Action
              kind="primary"
              icon="plus"
              data-kx-part="new"
              data-kx-help="core.new"
              aria-keyshortcuts="N"
              onClick={create}
              onPointerEnter={preloadDetail}
              onFocus={preloadDetail}
            >
              {t('header.new')}
            </Action>
          </>
        ) : null}
      </div>
    </div>
  );
}

/** El atajo `N` sólo existe (y sólo se documenta en el panel de atajos) si hay permiso para crear. */
function NewShortcut({ onRun }: { onRun(): void }) {
  useShortcut({ id: 'shell.new', scope: 'global', keys: 'N', labelKey: 'core:shortcuts.new', run: onRun });
  return null;
}

/** Idiomas en los que los sustantivos llevan mayúscula inicial: no se les baja. */
const CAPITALISED_NOUNS = new Set(['de', 'lb']);

/** «Instalaciones» → «instalaciones» tras un número; los acrónimos («CRM») y el alemán se dejan como están. */
function lowerNoun(noun: string, locale: string): string {
  const base = locale.split('-')[0]?.toLowerCase() ?? '';
  if (CAPITALISED_NOUNS.has(base) || /\p{Lu}/u.test(noun.slice(1))) return noun;
  return noun.charAt(0).toLocaleLowerCase(locale) + noun.slice(1);
}

/**
 * «248 instalaciones», o «12 de 1.204 instalaciones» con filtro. El nombre es el singular con una sola unidad
 * (según las reglas plurales del idioma) y el plural en los demás casos. Sin recuento de la vista, cadena vacía.
 */
export function countText(
  t: (key: string, options?: Record<string, unknown>) => string,
  locale: string,
  status: ViewStatus | undefined,
  singular: string,
  plural: string,
): string {
  const matched = status?.matched ?? status?.total;
  if (matched === undefined) return '';
  const total = status?.total;
  const numbers = new Intl.NumberFormat(locale, { numberingSystem: 'latn' });
  const rules = new Intl.PluralRules(locale);
  const subject = total !== undefined && total !== matched ? total : matched;
  const noun = lowerNoun(rules.select(subject) === 'one' ? singular || plural : plural || singular, locale);
  if (total !== undefined && total !== matched) {
    return t('header.countOf', { matched: numbers.format(matched), total: numbers.format(total), name: noun });
  }
  return t('header.count', { n: numbers.format(matched), name: noun });
}

interface ViewControlProps {
  choices: readonly ViewChoice[];
  current: ViewChoice | undefined;
  onSelect(choice: ViewChoice): void;
  compact: boolean;
}

/**
 * Control segmentado único: `role="radiogroup"` con un `radio` por opción. Es una sola parada de tabulación
 * (la opción elegida); las flechas cambian de opción y la eligen (patrón APG de grupo de radios), con Inicio y
 * Fin a los extremos. En RTL la flecha derecha va hacia el principio visual, como en el resto de la interfaz.
 */
function ViewControl({ choices, current, onSelect, compact }: ViewControlProps) {
  const { t } = useTranslation('core');
  const group = useRef<HTMLDivElement>(null);
  const focusable = current?.id ?? choices[0]?.id;

  const move = (event: KeyboardEvent<HTMLDivElement>) => {
    const keys = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'];
    if (!keys.includes(event.key) || event.altKey || event.ctrlKey || event.metaKey) return;
    const index = Math.max(0, choices.findIndex((choice) => choice.id === focusable));
    const rtl = getComputedStyle(event.currentTarget).direction === 'rtl';
    const forward = event.key === 'ArrowDown' || event.key === (rtl ? 'ArrowLeft' : 'ArrowRight');
    const next =
      event.key === 'Home' ? 0 : event.key === 'End' ? choices.length - 1 : (index + (forward ? 1 : -1) + choices.length) % choices.length;
    const choice = choices[next];
    if (!choice) return;
    event.preventDefault();
    onSelect(choice);
    group.current?.querySelector<HTMLElement>(`[data-kx-choice="${choice.id}"]`)?.focus();
  };

  return (
    <div
      ref={group}
      role="radiogroup"
      aria-label={t('view.label')}
      data-kx-ui="segmented"
      data-kx-help="core.view-switch"
      data-kx-icons-only={compact ? '' : undefined}
      onKeyDown={move}
    >
      {choices.map((choice) => {
        const checked = choice.id === current?.id;
        const label = t(choice.labelKey.replace('core:', ''));
        return (
          <Action
            key={choice.id}
            kind="quiet"
            icon={choice.icon}
            role="radio"
            aria-checked={checked}
            aria-label={compact ? label : undefined}
            title={compact ? label : undefined}
            tabIndex={choice.id === focusable ? 0 : -1}
            data-kx-choice={choice.id}
            onClick={() => onSelect(choice)}
          >
            <span data-kx-part="choice-label">{label}</span>
          </Action>
        );
      })}
    </div>
  );
}
