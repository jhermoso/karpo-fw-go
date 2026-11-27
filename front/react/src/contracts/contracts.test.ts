import { createContext } from 'react';
import { describe, expect, expectTypeOf, it } from 'vitest';
import {
  ApiContext,
  HelpContext,
  ManifestContext,
  SessionContext,
  ThemeContext,
  queryKeys,
  type AppManifest,
  type EntityApi,
  type EntityConfig,
  type EntityViewProps,
  type FieldKind,
  type KarpoAppProps,
  type ListQuery,
  type Session,
  type ShortcutDef,
  type ThemeApi,
} from './index';

describe('contratos (PLAN §4.2)', () => {
  const query: ListQuery = { page: 1, pageSize: 50, filter: { active: true } };

  it('queryKeys fija las claves de la caché', () => {
    expect(queryKeys.list('facility', query)).toEqual(['entity', 'facility', 'list', query]);
    expect(queryKeys.record('facility', 'f1')).toEqual(['entity', 'facility', 'record', 'f1']);
    expect(queryKeys.label('facility', 'f1')).toEqual(['entity', 'facility', 'label', 'f1']);
  });

  it('los contextos se crean aquí y arrancan en null', () => {
    const probe = createContext<null>(null);
    for (const context of [ManifestContext, ApiContext, SessionContext, ThemeContext, HelpContext]) {
      expect(context.constructor).toBe(probe.constructor);
      expect(context.Provider).toBeDefined();
      expect(context.Consumer).toBeDefined();
    }
  });

  it('los tipos tienen la forma acordada', () => {
    expectTypeOf<Session['status']>().toEqualTypeOf<'anonymous' | 'must-change-password' | 'authenticated'>();
    expectTypeOf<ThemeApi['mode']>().toEqualTypeOf<'light' | 'dark' | 'system'>();
    expectTypeOf<EntityViewProps>().toHaveProperty('navigate');
    expectTypeOf<EntityApi>().toHaveProperty('childList');
    expectTypeOf<EntityConfig['saveMode']>().toEqualTypeOf<'optimistic' | 'batch' | 'form'>();
    expectTypeOf<AppManifest['schemaVersion']>().toEqualTypeOf<1>();
    expectTypeOf<ShortcutDef['scope']>().toEqualTypeOf<'global' | 'grid' | 'detail' | 'tree' | 'map'>();
    expectTypeOf<KarpoAppProps['router']>().toEqualTypeOf<'browser' | 'hash' | 'memory' | undefined>();
    expectTypeOf<FieldKind>().toEqualTypeOf<
      | 'text'
      | 'textarea'
      | 'number'
      | 'decimal'
      | 'money'
      | 'percentage'
      | 'date'
      | 'datetime'
      | 'boolean'
      | 'select'
      | 'multiselect'
      | 'badge'
      | 'lookup'
      | 'email'
      | 'phone'
      | 'url'
    >();
  });
});
