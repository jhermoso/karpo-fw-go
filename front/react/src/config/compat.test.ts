// La tabla de compatibilidad cubre todas las claves de A1 §3 y el documento está al día.
// Para regenerar el bloque del documento: `UPDATE_COMPAT_DOC=1 npx vitest run src/config/compat.test.ts`.
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { COMPAT, checkKeys, keyStatus, renderCompatTable } from './compat';

const DOC = fileURLToPath(new URL('../../docs/COMPATIBILIDAD-CONFIG.md', import.meta.url));
const START = '<!-- compat:inicio (generado por src/config/compat.ts; no editar a mano) -->';
const END = '<!-- compat:fin -->';

/** Claves de A1 §3 por ámbito (todas las que cita el análisis, vivas, muertas y de los JSON). */
const A1_KEYS: Record<string, string[]> = {
  root: [
    'id', 'name', 'namePlural', 'description', 'icon', 'color', 'apiEndpoint', 'apiBaseUrl',
    'aggregateEndpoint', 'editable', 'editType', 'primaryKey', 'displayField', 'companyScoped',
    'suppressCompanyColumn', 'createViewType', 'availableViews', 'defaultView', 'searchable',
    'searchFields', 'useInfiniteScroll', 'actions', 'relations', 'hierarchy', 'emptyState',
    'mobileView', 'metadata', 'childGridConfig', 'dataSource', 'grid', 'filters', 'form', 'detail',
    'charts',
  ],
  dataSource: [
    'type', 'sources', 'enrichment', 'paginationStrategy', 'serverFiltering', 'fieldMapping',
    'baseFilter', 'enrichedSearchEndpoint', 'include', 'cursorSearchEndpoint', 'lookups',
    'pairSearch', 'rowMapping', 'chartSampleSize',
  ],
  serverFiltering: ['enabled', 'fieldMapping', 'clientOnlyFields', 'companyFilterField'],
  grid: [
    'columns', 'defaultSort', 'pageSize', 'pageSizeOptions', 'selectable', 'selectionMode',
    'rowActions', 'multiSort', 'editable', 'showRowNumbers', 'resizableColumns',
    'reorderableColumns', 'exportable', 'exportFormats', 'height', 'virtualScroll', 'emptyMessage',
  ],
  column: [
    'field', 'header', 'type', 'width', 'minWidth', 'maxWidth', 'sortable', 'filterable',
    'editable', 'visible', 'sticky', 'format', 'currency', 'currencyCode', 'enumValues',
    'linkConfig', 'align', 'tooltip', 'cssClass', 'template', 'emptyValue', 'tooltipField',
    'trueLabel', 'falseLabel', 'progressConfig', 'trendConfig', 'ratingConfig', 'sparklineConfig',
    'badgeMap', 'flex',
  ],
  enumValue: ['value', 'label', 'color', 'icon', 'cssClass'],
  rowAction: ['action', 'label', 'icon', 'color', 'visibleWhen', 'confirm', 'confirmMessage'],
  filter: [
    'field', 'label', 'type', 'placeholder', 'icon', 'options', 'optionsEndpoint',
    'optionsValueField', 'optionsLabelField', 'defaultValue', 'required', 'width', 'operator',
    'dynamic', 'searchFields', 'dependsOn', 'featureFlag', 'visibleWhen', 'serverField',
    'serverValueFormat', 'headerButton', 'hint', 'queryParam',
  ],
  option: ['value', 'label', 'icon', 'disabled', 'color', 'count', 'parentValue'],
  form: [
    'groups', 'sections', 'generated', 'create', 'edit', 'createVisibleWhen', 'createRouting',
    'createRoute', 'modalSize', 'createTitle', 'editTitle', 'saveButtonText', 'cancelButtonText',
    'showCancel',
  ],
  section: [
    'id', 'title', 'description', 'icon', 'collapsed', 'collapsible', 'fields', 'columns',
    'visibleWhen', 'apiEndpoint', 'upsertMethod', 'upsertParentKey',
  ],
  formField: [
    'field', 'label', 'type', 'placeholder', 'defaultValue', 'required', 'readonly', 'disabled',
    'disabledWhen', 'visibleWhen', 'width', 'options', 'optionsEndpoint', 'optionsSearch',
    'requiredWhenMultipleOptions', 'createOnly', 'validators', 'hint', 'prefix', 'suffix', 'min',
    'max', 'minLength', 'maxLength', 'pattern', 'rows', 'editable', 'hidden', 'readonlyOnEdit',
    'optionsValueField', 'optionsLabelField', 'optionsExtraQueryParams', 'lookupEntity',
    'lookupDisplayField', 'lookupValueField', 'lookupFilter', 'trueLabel', 'falseLabel', 'default',
  ],
  detail: [
    'showHistory', 'showRelations', 'showTags', 'sections', 'tabs', 'childGrids', 'headerActions',
    'redirectToSource',
  ],
  detailField: [
    'field', 'label', 'type', 'options', 'icon', 'copyable', 'required', 'editable', 'badgeType',
    'placeholder', 'format', 'maxLength', 'minLength', 'visibleWhen', 'hint', 'min', 'max',
    'trueLabel', 'falseLabel', 'hidden', 'lookupEntity', 'lookupDisplayField', 'lookupValueField',
    'lookupFilter', 'optionsEndpoint', 'validation',
  ],
  tab: ['id', 'label', 'icon', 'sections', 'component', 'childGrid'],
  childGrid: [
    'id', 'label', 'title', 'icon', 'entityId', 'apiEndpoint', 'foreignKey', 'dataField', 'field',
    'columns', 'emptyMessage', 'pageSize', 'visibleWhen', 'rowActions',
  ],
  childColumn: ['field', 'header', 'type', 'width', 'minWidth', 'emptyValue', 'trueLabel', 'falseLabel', 'format'],
  hierarchy: ['type', 'parentField', 'typeField', 'multiSelect', 'showCounts', 'levels'],
  emptyState: ['icon', 'title', 'message'],
  visibleWhen: [
    'hasRole', 'capability', 'sector', 'userRole', 'permission', 'requiredModule', 'featureFlag',
    'expr', 'allOf', 'anyOf', 'not',
  ],
  sidebarContext: ['id', 'label', 'icon', 'items'],
  sidebarItem: [
    'id', 'label', 'icon', 'children', 'entityId', 'viewType', 'type', 'selectable',
    'requiresCompanySelection', 'externalUrl', 'embedSupported', 'badge', 'roles', 'sector',
    'capability', 'visibleWhen', 'featureFlag', 'quickAccess',
  ],
  index: ['version', 'description', 'entities', 'domains'],
  runtimeConfig: ['apiUrl', 'authUrl', 'apiVersion', 'featureFlags'],
};

describe('tabla de compatibilidad', () => {
  it('cubre todas las claves de A1 §3', () => {
    const missing: string[] = [];
    for (const [scope, keys] of Object.entries(A1_KEYS)) {
      for (const key of keys) if (!keyStatus(scope, key)) missing.push(`${scope}.${key}`);
    }
    expect(missing).toEqual([]);
  });

  it('cada clave aparece una sola vez por ámbito y todo ámbito tiene título', () => {
    for (const table of COMPAT) {
      const keys = table.keys.map((k) => k[0]);
      expect(new Set(keys).size, table.scope).toBe(keys.length);
      expect(table.title.length).toBeGreaterThan(0);
    }
    expect(new Set(COMPAT.map((t) => t.scope)).size).toBe(COMPAT.length);
  });

  it('las claves de texto aceptan `<prop>Key` y los comentarios no avisan', () => {
    expect(keyStatus('root', 'nameKey')?.[1]).toBe('supported');
    expect(keyStatus('column', 'headerKey')?.[1]).toBe('supported');
    expect(keyStatus('root', 'zzzKey')).toBeUndefined();
    expect(checkKeys('root', { _comment: 'x', 'x-nota': 1, '//': 2, $schema: 3 })).toEqual([]);
    expect(checkKeys('root', { zzz: 1 })).toEqual([
      { key: 'zzz', severity: 'warn', message: 'clave desconocida `zzz`: se ignora' },
    ]);
    expect(checkKeys('root', { actions: [] })[0]).toMatchObject({ severity: 'info' });
    expect(checkKeys('root', { metadata: {}, name: 'x' })).toEqual([]);
    expect(checkKeys('ámbito-inexistente', { a: 1 })[0]).toMatchObject({ severity: 'warn' });
  });

  it('docs/COMPATIBILIDAD-CONFIG.md contiene la tabla generada y está al día', () => {
    const doc = readFileSync(DOC, 'utf8').replaceAll('\r\n', '\n');
    const table = renderCompatTable();
    const start = doc.indexOf(START);
    const end = doc.indexOf(END);
    expect(start, 'falta el marcador de inicio').toBeGreaterThanOrEqual(0);
    expect(end, 'falta el marcador de fin').toBeGreaterThan(start);
    if (process.env.UPDATE_COMPAT_DOC) {
      writeFileSync(DOC, `${doc.slice(0, start)}${START}\n\n${table}\n\n${doc.slice(end)}`);
      return;
    }
    const block = doc.slice(start + START.length, end).trim();
    expect(block).toBe(table.trim());
    for (const [scope, keys] of Object.entries(A1_KEYS)) {
      for (const key of keys) expect(block, `${scope}.${key}`).toContain(`\`${key}\``);
    }
  });
});
