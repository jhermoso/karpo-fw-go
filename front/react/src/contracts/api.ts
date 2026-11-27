// Contrato del adaptador HTTP (PLAN §4.2, §4.4).
import type { CommandDef, EntityConfig } from './config';

export type Row = Record<string, unknown> & { id: string; version?: number };

export interface ListQuery {
  page: number;
  pageSize: number;
  sort?: { field: string; direction: 'asc' | 'desc' };
  filter: Record<string, string | number | boolean>;
}

export interface Page<T = Row> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

export type ApiErrorKind =
  | 'validation'
  | 'conflict'
  | 'forbidden'
  | 'unauthorized'
  | 'notfound'
  | 'business'
  | 'too-large'
  | 'network'
  | 'server';

export interface ApiErrorShape {
  kind: ApiErrorKind;
  status: number;
  title: string;
  detail?: string;
  code?: string;
  fieldErrors: Record<string, { code: string; message: string }[]>;
}

export interface CommandResult {
  status: 'done' | 'pending';
  row?: Row;
}

export interface EntityApi {
  list(cfg: EntityConfig, q: ListQuery, signal?: AbortSignal): Promise<Page>;
  get(cfg: EntityConfig, id: string, signal?: AbortSignal): Promise<Row>;
  create(cfg: EntityConfig, body: Record<string, unknown>): Promise<Row>;
  update(
    cfg: EntityConfig,
    id: string,
    patch: Record<string, unknown>,
    version: number,
  ): Promise<Row>;
  remove(cfg: EntityConfig, id: string, body?: Record<string, unknown>): Promise<CommandResult>;
  command(
    cfg: EntityConfig,
    cmd: CommandDef,
    row: Row,
    body?: Record<string, unknown>,
  ): Promise<CommandResult>;
  childList(endpoint: string, row: Row, signal?: AbortSignal): Promise<Row[]>;
  raw<T>(path: string, init?: RequestInit): Promise<T>;
}

export const queryKeys = {
  list: (entity: string, q: ListQuery) => ['entity', entity, 'list', q] as const,
  record: (entity: string, id: string) => ['entity', entity, 'record', id] as const,
  label: (entity: string, id: string) => ['entity', entity, 'label', id] as const,
};
