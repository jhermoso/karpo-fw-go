// Cabecera de la ficha: título (`displayField`), estado, órdenes de la superficie `detail-header`,
// navegación anterior/siguiente y Editar. Guardar y Cancelar viven en el pie fijo (`SheetFooter`).
import { forwardRef, useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useText } from '../config';
import type { CommandDef, EntityConfig, Row } from '../contracts';
import { FieldView } from '../fields';
import { Action } from '../ui';
import { takeSheetFocus } from './focus-memory';
import type { FormMode } from './form-model';
import type { Neighbors } from './neighbors';

export interface SheetHeaderProps {
  config: EntityConfig;
  mode: FormMode;
  row: Row | undefined;
  title: string;
  titleId: string;
  neighbors: Neighbors;
  presentation: 'panel' | 'page';
  /** `undefined` = se puede editar; si no, el motivo (texto ya traducido) que se enseña en el botón. */
  editBlocked: string | undefined;
  /** La entidad no se edita en absoluto: no se ofrece el botón. */
  editHidden: boolean;
  dirty: boolean;
  commands: readonly CommandDef[];
  busyCommand: string | null;
  onEdit(): void;
  onPrev(): void;
  onNext(): void;
  onClose(): void;
  onToggleExpand?(): void;
  onCommand(cmd: CommandDef): void;
}

/**
 * Título de la ficha: `h1` cuando la ficha es la página (no hay cabecera de entidad) y `h2` en el panel, que
 * convive con el `h1` de la cabecera de la entidad (D-R32). El `id` es el mismo: `aria-labelledby` no cambia.
 */
const Heading = forwardRef<HTMLHeadingElement, { level: 1 | 2; id: string; children: ReactNode }>(function Heading(
  { level, id, children },
  ref,
) {
  const Tag = level === 1 ? 'h1' : 'h2';
  return (
    <Tag id={id} ref={ref} tabIndex={-1}>
      {children}
    </Tag>
  );
});

export function SheetHeader(props: SheetHeaderProps) {
  const { t } = useTranslation('detail');
  const resolve = useText();
  const { config, mode, row, neighbors, presentation, dirty } = props;
  const primary = useRef<HTMLButtonElement>(null);
  const status = config.columns.find((column) => column.kind === 'badge');

  // Tras guardar, cancelar o cambiar de registro la ficha se vuelve a montar: el foco sigue dentro de ella.
  const titleRef = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (mode !== 'read' || !takeSheetFocus()) return;
    (primary.current ?? titleRef.current)?.focus();
  }, [mode]);

  const editing = mode !== 'read';
  const hasRecord = row !== undefined;
  const position =
    neighbors.position !== undefined
      ? neighbors.total !== undefined && neighbors.total >= neighbors.position
        ? t('header.positionOf', { position: neighbors.position, total: neighbors.total })
        : t('header.position', { position: neighbors.position })
      : undefined;

  return (
    <header data-kx-part="sheet-header" data-kx-help="detail.header">
      <div data-kx-part="titles">
        <p data-kx-part="eyebrow">{mode === 'create' ? t('header.creating') : resolve(config.name)}</p>
        <div data-kx-part="title-row">
          <Heading level={presentation === 'page' ? 1 : 2} id={props.titleId} ref={titleRef}>
            {props.title}
          </Heading>
          {status && row ? <FieldView value={row[status.field]} def={status} row={row} /> : null}
          {dirty ? <span data-kx-part="dirty-mark">{t('header.unsaved')}</span> : null}
        </div>
      </div>

      <div data-kx-part="actions" role="group" aria-label={t('header.actions')}>
        {hasRecord ? (
          <div data-kx-part="neighbors" role="group" aria-label={t('header.neighbors')}>
            <Action
              kind="quiet"
              icon="arrow-up"
              aria-label={t('header.prev')}
              aria-keyshortcuts="Alt+ArrowUp"
              disabledReason={neighbors.prev ? undefined : t('header.noPrev')}
              onClick={props.onPrev}
            />
            <Action
              kind="quiet"
              icon="arrow-down"
              aria-label={t('header.next')}
              aria-keyshortcuts="Alt+ArrowDown"
              disabledReason={neighbors.next ? undefined : t('header.noNext')}
              onClick={props.onNext}
            />
            {position ? (
              <span data-kx-part="position" aria-live="polite">
                <bdi>{position}</bdi>
              </span>
            ) : null}
          </div>
        ) : null}

        {!editing
          ? props.commands.map((cmd) => (
              <Action
                key={cmd.id}
                kind={cmd.tone === 'error' || cmd.kind === 'delete' ? 'destructive' : 'secondary'}
                icon={cmd.icon}
                busy={props.busyCommand === cmd.id}
                onClick={() => props.onCommand(cmd)}
              >
                {resolve(cmd.label)}
              </Action>
            ))
          : null}

        {mode === 'read' && !props.editHidden ? (
          <Action
            ref={primary}
            kind="primary"
            icon="edit"
            data-kx-help="detail.save"
            aria-keyshortcuts="E"
            disabledReason={props.editBlocked}
            onClick={props.onEdit}
          >
            {t('actions.edit')}
          </Action>
        ) : null}

        {props.onToggleExpand && hasRecord ? (
          <Action
            kind="quiet"
            icon={presentation === 'page' ? 'collapse' : 'expand'}
            aria-label={presentation === 'page' ? t('actions.toPanel') : t('actions.toPage')}
            title={presentation === 'page' ? t('actions.toPanel') : t('actions.toPage')}
            onClick={props.onToggleExpand}
          />
        ) : null}
        {mode !== 'create' || presentation === 'panel' ? (
          <Action
            kind="quiet"
            icon="x"
            aria-label={presentation === 'page' ? t('actions.backToList') : t('actions.close')}
            title={presentation === 'page' ? t('actions.backToList') : t('actions.close')}
            onClick={props.onClose}
          />
        ) : null}
      </div>
    </header>
  );
}
