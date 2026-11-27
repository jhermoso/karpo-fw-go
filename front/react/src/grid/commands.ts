// Órdenes de fila y masivas (PLAN §4.9): `view` y `edit` navegan; `delete` y las órdenes HTTP llaman a la
// API con el motivo si la configuración lo pide. Un 202 (o `pendingApproval`) no es un fallo: la fila queda
// «pendiente de aprobación». Sin React.
import type { QueryClient } from '@tanstack/react-query';
import { patchEntityCaches } from '../api';
import type { CommandDef, EntityApi, EntityConfig, Row, Session } from '../contracts';
import { evaluateRule } from '../config';
import type { GridStore } from './store';

export interface CommandContext {
  config: EntityConfig;
  api: EntityApi;
  client: QueryClient;
  store: GridStore;
}

export type CommandOutcome =
  | { status: 'done'; rowId: string; row?: Row }
  | { status: 'pending'; rowId: string }
  | { status: 'failed'; rowId: string; error: unknown };

/** ¿La orden llama a la API? (`view` y `edit` sólo navegan.) */
export function isServerCommand(command: CommandDef): boolean {
  return command.kind === 'delete' || command.kind === 'http';
}

/** Órdenes de una superficie visibles para la fila (y la sesión) dadas. */
export function commandsFor(
  config: EntityConfig,
  surface: 'row' | 'toolbar',
  session: Pick<Session, 'can'> | null,
  row?: Row,
): CommandDef[] {
  return config.commands.filter(
    (command) =>
      command.surfaces.includes(surface) &&
      evaluateRule(command.visibleWhen, { session: session ?? undefined, row }),
  );
}

/** Órdenes masivas: las de servidor de la fila y las de la barra, sin repetir. */
export function bulkCommands(config: EntityConfig, session: Pick<Session, 'can'> | null): CommandDef[] {
  const seen = new Set<string>();
  const out: CommandDef[] = [];
  for (const command of [...commandsFor(config, 'toolbar', session), ...commandsFor(config, 'row', session)]) {
    if (!isServerCommand(command) || seen.has(command.id)) continue;
    seen.add(command.id);
    out.push(command);
  }
  return out;
}

/** Ejecuta una orden de servidor sobre una fila. Nunca lanza. */
export async function runCommand(
  ctx: CommandContext,
  command: CommandDef,
  row: Row,
  body?: Record<string, unknown>,
): Promise<CommandOutcome> {
  const { api, config, client, store } = ctx;
  try {
    const result = await api.command(config, command, row, body);
    if (result.status === 'pending') {
      store.getState().setStatus(row.id, { phase: 'pending' });
      return { status: 'pending', rowId: row.id };
    }
    if (result.row) patchEntityCaches(client, config.id, result.row);
    // Bajas, cierres y bloqueos cambian lo que enseña la lista (o el orden): se vuelve a leer.
    void client.invalidateQueries({ queryKey: ['entity', config.id, 'list'] });
    return { status: 'done', rowId: row.id, ...(result.row ? { row: result.row } : {}) };
  } catch (error) {
    return { status: 'failed', rowId: row.id, error };
  }
}
