// errors.json y auth.json: mismas claves en es y en, sin textos vacíos, y todas las claves que el
// código de T1 usa existen.
import { describe, expect, it } from 'vitest';
import authEn from '../locales/en/auth.json';
import authEs from '../locales/es/auth.json';
import errorsEn from '../locales/en/errors.json';
import errorsEs from '../locales/es/errors.json';
import { normalizeEntityConfig } from '../config';
import { allReal } from '../config/__fixtures__/real-configs';

function flatKeys(value: unknown, prefix = ''): string[] {
  if (typeof value === 'string') return [prefix];
  if (typeof value === 'object' && value !== null) {
    return Object.entries(value).flatMap(([key, child]) => flatKeys(child, prefix ? `${prefix}.${key}` : key));
  }
  throw new Error(`Valor no válido en ${prefix}: sólo se admiten cadenas y objetos`);
}

function flatValues(value: unknown): string[] {
  if (typeof value === 'string') return [value];
  return Object.values(value as object).flatMap(flatValues);
}

describe('locales de T1', () => {
  it.each([
    ['errors', errorsEs, errorsEn],
    ['auth', authEs, authEn],
  ])('%s: mismas claves en es y en', (_name, es, en) => {
    expect(flatKeys(es).sort()).toEqual(flatKeys(en).sort());
  });

  it.each([
    ['errors es', errorsEs],
    ['errors en', errorsEn],
    ['auth es', authEs],
    ['auth en', authEn],
  ])('%s: ningún texto vacío', (_name, dictionary) => {
    expect(flatValues(dictionary).filter((v) => v.trim() === '')).toEqual([]);
  });

  it('errors.json trae las claves pedidas y los códigos de negocio conocidos', () => {
    const keys = new Set(flatKeys(errorsEs));
    for (const key of [
      'required', 'format', 'range', 'enum', 'immutable', 'unsupported', 'maxLength', 'minLength', 'min', 'max',
      'pattern', 'conflict', 'forbidden', 'unauthorized', 'notfound', 'network', 'server', 'too-large', 'pending',
      'hr.reporting_cycle', 'hr.work_center_in_use', 'approval.self_resolution', 'approval.already_resolved',
      'approval.comment_required', 'approval.not_requester',
    ]) {
      expect(keys.has(key), key).toBe(true);
    }
  });

  it('las órdenes por omisión y la pregunta de confirmación del normalizador existen', () => {
    const keys = new Set(flatKeys(errorsEs));
    const used = new Set<string>();
    for (const { json } of allReal()) {
      const cfg = normalizeEntityConfig({ ...json, grid: { ...(json.grid as object), rowActions: [{ action: 'view' }, { action: 'edit' }, { action: 'delete', confirm: true }] }, editable: true });
      for (const cmd of cfg.commands) {
        for (const text of [cmd.label, cmd.confirm]) if (text?.key?.startsWith('errors:')) used.add(text.key.slice('errors:'.length));
      }
    }
    expect([...used].sort()).toEqual(['command.delete', 'command.edit', 'command.view', 'confirm.default']);
    for (const key of used) expect(keys.has(key), key).toBe(true);
  });
});
