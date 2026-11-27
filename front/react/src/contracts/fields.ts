// Contrato del registro de tipos de campo (PLAN §4.2, §4.8).
import type { ComponentType } from 'react';
import type { FieldDef, FieldKind, Text } from './config';
import type { Row } from './api';

/**
 * Resuelve un texto de la configuración (`Text`) al idioma activo. Los códecs lo usan para las
 * etiquetas traducidas (`trueLabel`/`falseLabel`, opciones de `select`, `multiselect` y `badge`):
 * `format` escribe la etiqueta traducida y `parse` la reconoce al pegar. Sin él, se usa el literal del `Text`.
 */
export type TextResolver = (text: Text) => string;

export interface FieldTypeDef {
  kind: FieldKind;
  align: 'start' | 'end' | 'center';
  format(value: unknown, def: FieldDef, locale: string, resolve?: TextResolver): string;
  parse(
    text: string,
    def: FieldDef,
    locale: string,
    resolve?: TextResolver,
  ): { ok: true; value: unknown } | { ok: false; code: string };
  /** Códigos: required, maxLength, min… */
  validate(value: unknown, def: FieldDef): string[];
  Viewer: ComponentType<{ value: unknown; def: FieldDef; row?: Row }>;
  Editor: ComponentType<{
    value: unknown;
    def: FieldDef;
    onChange(v: unknown): void;
    onCommit(move?: 'next' | 'prev' | 'down' | 'up'): void;
    onCancel(): void;
    autoFocus?: boolean;
    invalid?: boolean;
    describedBy?: string;
  }>;
}
