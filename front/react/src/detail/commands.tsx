// Órdenes de la configuración (`commands[]`): confirmar, pedir motivo, ejecutar y reflejar el
// resultado (PLAN §4.10). Las usan la cabecera del detalle (`detail-header`) y la paleta de órdenes.
import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { describeApiError, isApiError, patchEntityCaches, useEntityApi } from '../api';
import { useText } from '../config';
import type { CommandDef, EntityConfig, EntityRoute, Row } from '../contracts';
import { ConfirmDialog, PromptDialog, useToast } from '../ui';

export interface CommandRunnerOptions {
  navigate?(to: EntityRoute): void;
  /** Se cerró un diálogo de confirmación o de motivo (para devolver el foco a quien lo pidió). */
  onDialogClosed?(): void;
  /** Tras una orden que se hizo (no las de navegación). `deleted` = baja o cierre del registro. */
  onDone?(cmd: CommandDef, row: Row, outcome: { status: 'done' | 'pending'; row?: Row }): void;
}

export interface CommandRunner {
  /** Ejecuta la orden sobre la fila, pasando antes por la confirmación y el motivo si los declara. */
  run(cmd: CommandDef, row: Row): void;
  /** Los diálogos de confirmación y de motivo: se pintan una vez en la pantalla. */
  dialogs: ReactNode;
  /** Identificador de la orden en curso. */
  busy: string | null;
}

interface Pending {
  cmd: CommandDef;
  row: Row;
  stage: 'confirm' | 'prompt';
}

export function useCommandRunner(config: EntityConfig | undefined, options: CommandRunnerOptions = {}): CommandRunner {
  const { t } = useTranslation(['detail', 'errors']);
  const resolve = useText();
  const api = useEntityApi();
  const queryClient = useQueryClient();
  const toast = useToast();
  const [pending, setPending] = useState<Pending | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const { navigate, onDone, onDialogClosed } = options;

  const execute = useCallback(
    async (cmd: CommandDef, row: Row, reason?: string) => {
      if (!config) return;
      setBusy(cmd.id);
      const body = cmd.prompt && reason !== undefined ? { [cmd.prompt.field]: reason } : undefined;
      try {
        const result = await api.command(config, cmd, row, body);
        if (result.status === 'pending') {
          toast.show({
            tone: 'info',
            message: cmd.acceptedMessage ? resolve(cmd.acceptedMessage) : t('detail:commands.pending'),
          });
        } else {
          if (result.row) patchEntityCaches(queryClient, config.id, result.row);
          else void queryClient.invalidateQueries({ queryKey: ['entity', config.id] });
          toast.show({
            tone: 'success',
            message: cmd.successMessage ? resolve(cmd.successMessage) : t('detail:commands.done', { name: resolve(cmd.label) }),
          });
        }
        onDone?.(cmd, row, result);
      } catch (error) {
        if (isApiError(error)) {
          if (error.kind === 'conflict') void queryClient.invalidateQueries({ queryKey: ['entity', config.id] });
          const text = describeApiError(error);
          toast.show({ tone: 'error', message: t(text.key, { defaultValue: text.fallback }) });
        } else {
          toast.show({ tone: 'error', message: t('errors:server') });
        }
      } finally {
        setBusy(null);
      }
    },
    [api, config, onDone, queryClient, resolve, t, toast],
  );

  const run = useCallback(
    (cmd: CommandDef, row: Row) => {
      if (!config) return;
      if (cmd.kind === 'view') {
        navigate?.({ entity: config.id, id: row.id, mode: 'read' });
        return;
      }
      if (cmd.kind === 'edit') {
        navigate?.({ entity: config.id, id: row.id, mode: 'edit' });
        return;
      }
      if (cmd.confirm) setPending({ cmd, row, stage: 'confirm' });
      else if (cmd.prompt) setPending({ cmd, row, stage: 'prompt' });
      else void execute(cmd, row);
    },
    [config, execute, navigate],
  );

  // Al cerrarse el último diálogo, el foco vuelve a quien lo pidió.
  const wasOpen = useRef(false);
  useEffect(() => {
    if (pending) wasOpen.current = true;
    else if (wasOpen.current) {
      wasOpen.current = false;
      onDialogClosed?.();
    }
  }, [pending, onDialogClosed]);

  /** Cierra el diálogo de la etapa dada (si ya se pasó a otra etapa, ésta se conserva). */
  const closeStage = (stage: Pending['stage']) =>
    setPending((current) => (current && current.stage === stage ? null : current));

  let dialogs: ReactNode = null;
  if (pending && config) {
    const { cmd, row, stage } = pending;
    const destructive = cmd.kind === 'delete' || cmd.tone === 'error';
    if (stage === 'confirm') {
      dialogs = (
        <ConfirmDialog
          open
          title={resolve(cmd.label)}
          message={cmd.confirm ? resolve(cmd.confirm) : t('errors:confirm.default')}
          destructive={destructive}
          confirmLabel={resolve(cmd.label)}
          onOpenChange={(open) => {
            if (!open) closeStage('confirm');
          }}
          onConfirm={() => {
            if (cmd.prompt) setPending({ cmd, row, stage: 'prompt' });
            else {
              closeStage('confirm');
              void execute(cmd, row);
            }
          }}
        />
      );
    } else if (cmd.prompt) {
      dialogs = (
        <PromptDialog
          open
          title={resolve(cmd.label)}
          label={resolve(cmd.prompt.label)}
          required={cmd.prompt.required}
          maxLength={cmd.prompt.maxLength}
          submitLabel={resolve(cmd.label)}
          onOpenChange={(open) => {
            if (!open) closeStage('prompt');
          }}
          onSubmit={(reason) => {
            closeStage('prompt');
            void execute(cmd, row, reason);
          }}
        />
      );
    }
  }

  return { run, dialogs, busy };
}
