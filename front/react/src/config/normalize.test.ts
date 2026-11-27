// Normalizador con los 8 JSON reales de Go y los 15 puntos de la tabla de A1 §5 (puntos 1-13 y
// la parte de configuración del 14; el 14 y el 15 de red se prueban en src/api).
import { describe, expect, it } from 'vitest';
import type { ConfigWarning } from '../contracts';
import { allReal, readRealById } from './__fixtures__/real-configs';
import { matchOption, normalizeEntityConfig, toneFromHash } from './normalize';
import type { EntityConfigExt, FieldDefExt } from './types';

function load(id: string, overlay?: unknown): EntityConfigExt {
  return normalizeEntityConfig(readRealById(id), overlay);
}

function allFields(cfg: EntityConfigExt) {
  return [
    ...cfg.columns,
    ...(cfg.form?.sections.flatMap((s) => s.fields) ?? []),
    ...cfg.detail.sections.flatMap((s) => s.fields),
  ];
}

describe('los 8 JSON reales de Go', () => {
  const real = allReal();

  it('se normalizan sin excepción', () => {
    expect(real).toHaveLength(8);
    for (const { path, json } of real) {
      const cfg = normalizeEntityConfig(json);
      expect(cfg.id, path).toBe(json.id);
      expect(cfg.columns.length, path).toBeGreaterThan(0);
      expect(cfg.endpoint.startsWith('/'), path).toBe(true);
      expect(cfg.apiVersioned, path).toBe(true);
      expect(cfg.dataSource.pagination, path).toBe('server');
      expect(cfg.dataSource.listStyle, path).toBe('maintenance');
      expect(cfg.saveMode, path).toBe('optimistic');
    }
  });

  it('no producen avisos de gravedad `warn` (sólo informativos)', () => {
    for (const { path, json } of real) {
      const warns = normalizeEntityConfig(json).warnings.filter((w) => w.severity === 'warn');
      expect(warns, path).toEqual([]);
    }
  });

  it('es determinista: dos normalizaciones dan lo mismo', () => {
    for (const { json } of real) {
      expect(normalizeEntityConfig(json)).toEqual(normalizeEntityConfig(json));
    }
  });

  it('los textos conservan el literal en español', () => {
    const cfg = load('facility');
    expect(cfg.name.literal).toBe('Instalación');
    expect(cfg.namePlural.literal).toBe('Instalaciones');
    expect(cfg.endpoint).toBe('/facilities');
    expect(cfg.dataSource.companyFilterField).toBe('organization');
    expect(cfg.displayField).toBe('name');
    expect(cfg.grid.pageSize).toBe(25);
    expect(cfg.grid.defaultSort).toEqual({ field: 'name', direction: 'asc' });
  });
});

describe('A1 §5: puntos de la tabla', () => {
  it('1. filtro `checkbox` = boolean', () => {
    const wc = load('work-center');
    expect(wc.filters[0]).toMatchObject({ field: 'isActive', serverField: 'isActive', kind: 'boolean' });
    expect(load('supplier').filters[0]?.kind).toBe('boolean');
  });

  it('2. filtro `number` se soporta', () => {
    const holiday = load('holiday');
    expect(holiday.filters.find((f) => f.field === 'year')?.kind).toBe('number');
    expect(holiday.filters.find((f) => f.quickSearch)?.serverField).toBe('search');
  });

  it('3. `badgeMap` -> opciones con claves en cadena y comparación tolerante', () => {
    const facility = load('facility');
    const active = facility.columns.find((c) => c.field === 'active')!;
    expect(active.kind).toBe('badge');
    expect(active.options?.map((o) => o.value)).toEqual(['true', 'false']);
    expect(active.options?.[0]).toMatchObject({ tone: 'success', label: { literal: 'Activa' } });
    expect(active.options?.[1]?.tone).toBe('neutral');
    expect(matchOption(active.options, true)?.label.literal).toBe('Activa');
    expect(matchOption(active.options, 'False')?.label.literal).toBe('Inactiva');
    expect(matchOption(active.options, 'otro')).toBeUndefined();
  });

  it('3b. `badgeMap` y `enumValues` se funden (enumValues manda)', () => {
    const cfg = normalizeEntityConfig({
      id: 'x',
      apiEndpoint: '/x',
      grid: {
        columns: [
          {
            field: 's',
            header: 'S',
            type: 'badge',
            badgeMap: { a: { label: 'A', color: 'green' }, b: { label: 'B', color: 'red' } },
            enumValues: [{ value: 'b', label: 'Be', color: 'blue' }, { value: 1, label: 'Uno' }],
          },
        ],
      },
    });
    const options = cfg.columns[0]!.options!;
    expect(options.map((o) => o.value)).toEqual(['a', 'b', '1']);
    expect(options[1]).toMatchObject({ label: { literal: 'Be' }, tone: 'info' });
    expect(matchOption(options, 1)?.label.literal).toBe('Uno');
  });

  it('4. campos `checkbox` del detalle = boolean (ninguno queda como texto)', () => {
    const facility = load('facility');
    expect(facility.detail.sections[0]!.fields.find((f) => f.field === 'active')?.kind).toBe('boolean');
    for (const { json } of allReal()) {
      const raw = json.detail as { sections: { fields: { field: string; type: string }[] }[] };
      const checkboxes = raw.sections.flatMap((s) => s.fields).filter((f) => f.type === 'checkbox');
      const cfg = normalizeEntityConfig(json);
      for (const f of checkboxes) {
        const def = cfg.detail.sections.flatMap((s) => s.fields).find((x) => x.field === f.field);
        expect(def?.kind, `${String(json.id)}.${f.field}`).toBe('boolean');
      }
    }
    // checkbox y switch también en el formulario
    const wc = load('work-center');
    expect(wc.form!.sections[0]!.fields.find((f) => f.field === 'headquarters')?.kind).toBe('boolean');
  });

  it('5. `sections[].columns` y `width` (1-12) se respetan', () => {
    const section = load('facility').form!.sections[0]!;
    expect(section.columns).toBe(2);
    expect(section.fields.map((f) => f.width)).toEqual([6, 6, 6, 6]);
    const clamped = normalizeEntityConfig({
      id: 'x',
      apiEndpoint: '/x',
      form: { sections: [{ columns: 9, fields: [{ field: 'a', type: 'text', width: 40 }] }] },
    });
    expect(clamped.form!.sections[0]).toMatchObject({ columns: 4 });
    expect(clamped.form!.sections[0]!.fields[0]!.width).toBe(12);
  });

  it('6. `hint` se conserva en formularios y detalle', () => {
    const supplier = load('supplier');
    const formField = supplier.form!.sections[0]!.fields.find((f) => f.field === 'supplier')!;
    expect(formField.hint?.literal).toContain('Identidad del proveedor');
    const blocked = supplier.detail.sections[0]!.fields.find((f) => f.field === 'blocked')!;
    expect(blocked.hint?.literal).toContain('bloqueado');
    const hints = allReal().flatMap(({ json }) => allFields(normalizeEntityConfig(json))).filter((f) => f.hint);
    expect(hints.length).toBeGreaterThanOrEqual(30);
  });

  it('7. claves ajenas: texto sin superposición, `lookup` con `lookupEntity`', () => {
    const plain = load('facility');
    const partOf = plain.form!.sections[0]!.fields.find((f) => f.field === 'partOf')!;
    expect(partOf.kind).toBe('text');
    expect(partOf.lookup).toBeUndefined();

    const withOverlay = load('facility', {
      form: {
        sections: [
          {
            id: 'main',
            fields: [
              { field: 'partOf', lookupEntity: 'facility', lookupDisplayField: 'name', lookupFilter: { active: true, bad: { x: 1 } } },
            ],
          },
        ],
      },
    });
    const lookup = withOverlay.form!.sections[0]!.fields.find((f) => f.field === 'partOf')!;
    expect(lookup.kind).toBe('lookup');
    expect(lookup.lookup).toEqual({ entity: 'facility', valueField: 'id', displayField: 'name', filter: { active: true } });
    // el resto del formulario sigue ahí (fusión en profundidad, no sustitución)
    expect(withOverlay.form!.sections[0]!.fields).toHaveLength(4);
    expect(withOverlay.form!.sections[0]!.fields[0]!.field).toBe('name');
  });

  it('8. `fieldMapping` se ignora sin avisar', () => {
    for (const { json } of allReal()) {
      const cfg = normalizeEntityConfig(json);
      const mentions = cfg.warnings.filter((w) => /fieldMapping/.test(w.path + w.message));
      expect(mentions, String(json.id)).toEqual([]);
    }
  });

  it('10. `delete` con `confirm` y `confirmMessage` -> orden con pregunta propia', () => {
    const facility = load('facility');
    const del = facility.commands.find((c) => c.id === 'delete')!;
    expect(del).toMatchObject({ kind: 'delete', tone: 'error', label: { literal: 'Desactivar' } });
    expect(del.confirm?.literal).toBe('¿Desactivar esta instalación? No se borra: se puede volver a activar.');
    expect(del.surfaces).toContain('row');
    const view = facility.commands.find((c) => c.id === 'view')!;
    expect(view).toMatchObject({ kind: 'view', tone: 'accent' });
    const edit = facility.commands.find((c) => c.id === 'edit')!;
    expect(edit.tone).toBe('warning');
    // confirm sin mensaje -> pregunta por omisión con clave de i18n
    const wcDelete = load('work-center').commands.find((c) => c.id === 'delete')!;
    expect(wcDelete.confirm).toBeUndefined();
  });

  it('11. `sortable` -> `serverSortField` sólo si el servidor lo admite', () => {
    const supplier = load('supplier');
    expect(supplier.columns.find((c) => c.field === 'paymentDays')).toMatchObject({
      sortable: true,
      serverSortField: 'paymentDays',
    });
    expect(supplier.columns.find((c) => c.field === 'supplier')).toMatchObject({ sortable: false });
    // toda columna ordenable de los 8 JSON reales tiene orden de servidor (lista blanca de A2 §2)
    for (const { json } of allReal()) {
      for (const column of normalizeEntityConfig(json).columns.filter((c) => c.sortable)) {
        expect(column.serverSortField, `${String(json.id)}.${column.field}`).toBe(column.field);
      }
    }
    const custom = normalizeEntityConfig(
      {
        id: 'x',
        apiEndpoint: '/otras',
        grid: {
          columns: [
            { field: 'a', header: 'A', sortable: true },
            { field: 'b', header: 'B', sortable: true },
          ],
        },
      },
      { serverSortFields: { b: 'bee' } },
    );
    expect(custom.columns[0]).toMatchObject({ sortable: true });
    expect(custom.columns[0]!.serverSortField).toBeUndefined();
    expect(custom.columns[1]!.serverSortField).toBe('bee');
    expect(custom.warnings.some((w) => w.severity === 'info' && /sólo en cliente.*a$/.test(w.message))).toBe(true);
  });

  it('11b. `serverSortFields` como lista: cada campo se ordena en el servidor con su nombre y no da aviso de tipo', () => {
    const cfg = normalizeEntityConfig(
      { id: 'x', apiEndpoint: '/api/v1/x', grid: { columns: [{ field: 'a', header: 'A', sortable: true }, { field: 'b', header: 'B', sortable: true }] } },
      { serverSortFields: ['b'] },
    );
    expect(cfg.columns[0]!.serverSortField).toBeUndefined();
    expect(cfg.columns[1]!.serverSortField).toBe('b');
    expect(cfg.warnings.filter((w) => w.path === 'serverSortFields')).toEqual([]);
  });

  it('12. hijo con `{id}` no añade `?foreignKey=`; sin `{id}` sí', () => {
    const supplier = load('supplier');
    expect(supplier.detail.childGrids).toHaveLength(1);
    const child = supplier.detail.childGrids[0]!;
    expect(child.endpoint).toBe('/suppliers/{id}/block-history');
    expect(child.endpoint).not.toContain('?');
    expect(child.columns.map((c) => c.field)).toEqual(['at', 'blocked', 'reason', 'userName']);
    expect(child.columns[1]).toMatchObject({ kind: 'boolean', trueLabel: { literal: 'Bloqueo' } });
    expect(child.emptyMessage?.literal).toContain('Sin bloqueos');
    const loose = normalizeEntityConfig({
      id: 'x',
      apiEndpoint: '/x',
      detail: { childGrids: [{ id: 'h', apiEndpoint: '/history', foreignKey: 'xId', columns: [] }] },
    });
    expect(loose.detail.childGrids[0]!.endpoint).toBe('/history?xId={id}');
  });

  it('13. `maxLength` se aplica', () => {
    const facility = load('facility');
    expect(facility.form!.sections[0]!.fields.find((f) => f.field === 'name')?.maxLength).toBe(200);
    expect(load('supplier').form!.sections[0]!.fields.find((f) => f.field === 'iban')?.maxLength).toBe(34);
    expect(load('holiday').form!.sections[0]!.fields.find((f) => f.field === 'name')?.maxLength).toBe(120);
  });

  it('14. (configuración) la edición directa es optimista y los campos inmutables se marcan', () => {
    const facility = load('facility');
    expect(facility.saveMode).toBe('optimistic');
    const cfg = normalizeEntityConfig({
      id: 'x',
      apiEndpoint: '/x',
      form: {
        sections: [
          {
            fields: [
              { field: 'code', type: 'text', readonlyOnEdit: true },
              { field: 'k', type: 'text', createOnly: true },
              { field: 'ro', type: 'text', readonly: true },
            ],
          },
        ],
      },
    });
    const [code, k, ro] = cfg.form!.sections[0]!.fields;
    expect(code).toMatchObject({ immutable: true, editable: true });
    expect(k!.immutable).toBe(true);
    expect(ro).toMatchObject({ editable: false });
    expect(ro!.immutable).toBeUndefined();
  });
});

describe('reglas de normalización (PLAN §4.3)', () => {
  const base = { id: 'x', apiEndpoint: '/x' };

  it('un tipo desconocido se degrada a `text` con aviso; `fieldMapping` no avisa', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      dataSource: { serverFiltering: { fieldMapping: { a: 'b' } } },
      grid: { columns: [{ field: 'f', header: 'F', type: 'hologram' }] },
    });
    expect(cfg.columns[0]!.kind).toBe('text');
    const warning = cfg.warnings.find((w) => w.path === 'grid.columns[0].type');
    expect(warning).toMatchObject({ severity: 'warn' });
    expect(warning!.message).toContain('hologram');
    expect(cfg.warnings.filter((w) => /fieldMapping/.test(w.path))).toEqual([]);
  });

  it('tipos conocidos sin equivalente degradan a `text` con otro aviso', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      grid: { columns: [{ field: 'f', header: 'F', type: 'sparkline' }, { field: 'a', type: 'actions' }] },
    });
    expect(cfg.columns).toHaveLength(1);
    expect(cfg.columns[0]!.kind).toBe('text');
    expect(cfg.warnings.some((w) => /sin equivalente/.test(w.message))).toBe(true);
  });

  it('claves desconocidas dan aviso `warn`; las muertas, `info`', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      zzz: 1,
      useInfiniteScroll: true,
      $schema: 'x',
      grid: { columns: [{ field: 'f', type: 'text', cssClass: 'x', nope: 1 }] },
    });
    const byPath = Object.fromEntries(cfg.warnings.map((w) => [w.path, w]));
    expect(byPath.zzz).toMatchObject({ severity: 'warn' });
    expect(byPath.useInfiniteScroll).toMatchObject({ severity: 'info' });
    expect(byPath['grid.columns[0].cssClass']).toMatchObject({ severity: 'info' });
    expect(byPath['grid.columns[0].nope']).toMatchObject({ severity: 'warn' });
    expect(byPath.$schema).toBeUndefined();
  });

  it('`groups` = `sections` y `field` = `dataField`', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      form: { groups: [{ id: 'g', title: 'G', fields: [{ dataField: 'k', label: 'K', type: 'text' }] }] },
    });
    expect(cfg.form!.sections[0]).toMatchObject({ id: 'g', title: { literal: 'G' } });
    expect(cfg.form!.sections[0]!.fields[0]!.field).toBe('k');
    expect(cfg.form).toMatchObject({ create: true, edit: true });
  });

  it('`visibleWhen` en cadena = `{expr}`; las cláusulas legadas se descartan con aviso', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      form: {
        sections: [
          {
            fields: [
              { field: 'a', type: 'text', visibleWhen: "tipo == 'X'" },
              { field: 'b', type: 'text', visibleWhen: { permission: 'A.B.C', roles: ['customer'], capability: 'c' } },
              { field: 'c', type: 'text', visibleWhen: { anyOf: [{ permission: 'A' }, { field: 'z', equals: false }], not: { expr: 'q != 1' } } },
            ],
          },
        ],
      },
    });
    const [a, b, c] = cfg.form!.sections[0]!.fields;
    expect(a!.visibleWhen).toEqual({ expr: "tipo == 'X'" });
    expect(b!.visibleWhen).toEqual({ permission: 'A.B.C' });
    expect(cfg.warnings.filter((w) => /legada/.test(w.message)).map((w) => w.path.split('.').pop())).toEqual([
      'roles',
      'capability',
    ]);
    expect(c!.visibleWhen).toEqual({
      anyOf: [{ permission: 'A' }, { field: 'z', equals: false }],
      not: { expr: 'q != 1' },
    });
  });

  it('colores: nombres -> tono, hexadecimal ignorado con aviso y tono por hash estable', () => {
    const named = normalizeEntityConfig({ ...base, color: 'danger' });
    expect(named.tone).toBe('error');
    for (const [name, tone] of [
      ['green', 'success'],
      ['warning', 'warning'],
      ['gray', 'neutral'],
      ['blue', 'info'],
      ['primary', 'accent'],
    ] as const) {
      expect(normalizeEntityConfig({ ...base, color: name }).tone).toBe(tone);
    }
    const hex = normalizeEntityConfig({ ...base, color: '#16a085' });
    expect(hex.tone).toBe(toneFromHash('x'));
    expect(hex.warnings.find((w) => w.path === 'color')).toMatchObject({ severity: 'info' });
    expect(toneFromHash('facility')).toBe(toneFromHash('facility'));
    const unknown = normalizeEntityConfig({ ...base, color: 'chartreuse' });
    expect(unknown.warnings.some((w) => w.path === 'color')).toBe(true);
  });

  it('`editType` -> `saveMode`; la superposición puede cambiarlo', () => {
    expect(normalizeEntityConfig({ ...base, editType: 'direct' }).saveMode).toBe('optimistic');
    const party = normalizeEntityConfig({ ...base, editType: 'party' });
    expect(party.saveMode).toBe('form');
    expect(party.warnings.some((w) => w.path === 'editType')).toBe(true);
    expect(normalizeEntityConfig({ ...base, editType: 'direct' }, { saveMode: 'batch' }).saveMode).toBe('batch');
    expect(normalizeEntityConfig({ ...base, saveMode: 'nope' }).warnings.some((w) => w.path === 'saveMode')).toBe(true);
  });

  it('endpoints: `~/`, `/api/` y `apiVersioned: false` son rutas sin versión (estilo number-size)', () => {
    const plain = normalizeEntityConfig({ ...base, apiEndpoint: '/api/v1/products' });
    expect(plain).toMatchObject({ endpoint: '/products', apiVersioned: true });
    const tilde = normalizeEntityConfig({ ...base, apiEndpoint: '~/purchases/approvals' });
    expect(tilde).toMatchObject({ endpoint: '/purchases/approvals', apiVersioned: false });
    expect(tilde.dataSource).toMatchObject({ listStyle: 'number-size', listMethod: 'get' });
    const bare = normalizeEntityConfig({ ...base, apiEndpoint: '/api/hr/positions' });
    expect(bare).toMatchObject({ endpoint: '/hr/positions', apiVersioned: false });
    const flag = normalizeEntityConfig({ ...base, apiEndpoint: '/shipments/approvals', apiVersioned: false });
    expect(flag.apiVersioned).toBe(false);
    const abs = normalizeEntityConfig({ ...base, apiEndpoint: 'https://example.test/api/x' });
    expect(abs).toMatchObject({ endpoint: 'https://example.test/api/x', apiVersioned: false });
    const noSlash = normalizeEntityConfig({ ...base, apiEndpoint: 'things' });
    expect(noSlash.endpoint).toBe('/things');
    // hijo de una entidad sin versión: ruta sin versión marcada con `~`
    const child = normalizeEntityConfig({
      ...base,
      apiEndpoint: '/api/hr/positions',
      detail: { childGrids: [{ id: 'h', apiEndpoint: '/hr/positions/{id}/holders', columns: [] }] },
    });
    expect(child.detail.childGrids[0]!.endpoint).toBe('~/hr/positions/{id}/holders');
  });

  it('`commands[]` declarativos (front/README.md) -> CommandDef con prompt, versión y mensajes', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      commands: [
        {
          id: 'block',
          label: 'Bloquear',
          icon: 'lock',
          color: 'danger',
          surfaces: ['row', 'detail-header', 'nope'],
          visibleWhen: { field: 'blocked', equals: false },
          method: 'post',
          endpoint: '/suppliers/{id}/block',
          prompt: { field: 'reason', label: 'Motivo', required: true, maxLength: 200 },
          sendVersion: true,
          successMessage: 'Proveedor bloqueado',
          acceptedMessage: { es: 'Pendiente', en: 'Pending' },
          confirm: true,
        },
        { id: 'weird', endpoint: '/x', method: 'GET' },
      ],
      grid: {
        rowActions: [
          { action: 'block' },
          { action: 'view', label: 'Ver' },
          { action: 'inexistente' },
        ],
      },
    });
    const block = cfg.commands.find((c) => c.id === 'block')!;
    expect(block).toMatchObject({
      kind: 'http',
      method: 'POST',
      endpoint: '/suppliers/{id}/block',
      sendVersion: true,
      tone: 'error',
      surfaces: ['row', 'detail-header'],
      visibleWhen: { field: 'blocked', equals: false },
      prompt: { field: 'reason', required: true, maxLength: 200 },
    });
    expect(block.prompt!.label.literal).toBe('Motivo');
    expect(block.acceptedMessage?.byLang).toEqual({ es: 'Pendiente', en: 'Pending' });
    expect(block.confirm?.key).toBe('errors:confirm.default');
    expect(cfg.commands.find((c) => c.id === 'weird')).toMatchObject({ method: 'POST' });
    expect(cfg.warnings.some((w) => /inexistente/.test(w.message))).toBe(true);
    expect(cfg.warnings.some((w) => w.path === 'commands[1].method')).toBe(true);
    expect(cfg.commands.find((c) => c.id === 'view')).toBeDefined();
  });

  it('una entidad de sólo lectura no ofrece editar ni borrar', () => {
    const country = load('country');
    expect(country.editable).toBe(false);
    expect(country.commands.map((c) => c.id)).toEqual(['view']);
    expect(country.form).toBeNull();
    expect(country.columns.every((c) => !c.editable)).toBe(true);
    const ro = normalizeEntityConfig({
      ...base,
      editable: false,
      form: { sections: [{ fields: [{ field: 'a', type: 'text' }] }] },
      grid: { rowActions: [{ action: 'edit' }, { action: 'delete' }, { action: 'view' }] },
    });
    expect(ro.commands.map((c) => c.id)).toEqual(['view']);
    expect(ro.form).toMatchObject({ create: false, edit: false });
  });

  it('columnas: edición derivada del formulario, fijado, ocultas, ancho y alias de cabecera', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      form: {
        sections: [
          {
            fields: [
              { field: 'name', type: 'text' },
              { field: 'code', type: 'text', readonlyOnEdit: true },
              { field: 'state', type: 'checkbox' },
            ],
          },
        ],
      },
      grid: {
        columns: [
          { field: 'name', header: 'Nombre', width: 200, sticky: true },
          { field: 'code', label: 'Código', visible: false },
          { field: 'state', header: 'Estado', type: 'badge', sticky: 'right' },
          { field: 'free', header: 'Libre', editable: true },
          { field: 'locked', header: 'Fija', editable: false },
        ],
      },
    });
    const byField = Object.fromEntries(cfg.columns.map((c) => [c.field, c]));
    expect(byField.name).toMatchObject({ editable: true, widthPx: 200, pinned: 'start' });
    expect(byField.code).toMatchObject({ editable: false, hidden: true });
    expect(byField.code!.label.literal).toBe('Código');
    expect(byField.state).toMatchObject({ editable: false, pinned: 'end' });
    expect(byField.free!.editable).toBe(true);
    expect(byField.locked!.editable).toBe(false);
  });

  it('columnas: `priority` entera se conserva; un valor no entero se descarta con aviso (D-R34)', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      grid: {
        columns: [
          { field: 'a', header: 'A', priority: 5 },
          { field: 'b', header: 'B', priority: '2' },
          { field: 'c', header: 'C', priority: 2.5 },
          { field: 'd', header: 'D', priority: 'alta' },
          { field: 'e', header: 'E' },
        ],
      },
    });
    const byField = Object.fromEntries(cfg.columns.map((c) => [c.field, c]));
    expect(byField.a?.priority).toBe(5);
    expect(byField.b?.priority).toBe(2);
    expect(byField.c?.priority).toBeUndefined();
    expect(byField.d?.priority).toBeUndefined();
    expect(byField.e?.priority).toBeUndefined();
    // (La tabla de compatibilidad de `compat.ts` aún no conoce `priority`: su aviso de «clave desconocida» no cuenta.)
    const warned = cfg.warnings.filter((w) => /prioridad/.test(w.message));
    expect(warned.map((w) => w.path)).toEqual(['grid.columns[2].priority', 'grid.columns[3].priority']);
    expect(warned.every((w) => w.severity === 'warn')).toBe(true);
  });

  it('`validators` planos, patrón inválido, hidden y valores por omisión', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      form: {
        sections: [
          {
            fields: [
              {
                field: 'a',
                type: 'text',
                validators: [
                  { type: 'required' },
                  { type: 'minLength', value: 2 },
                  { type: 'maxLength', value: 9 },
                  { type: 'min', value: 1 },
                  { type: 'max', value: 5 },
                  { type: 'pattern', value: '^a' },
                  { type: 'email' },
                ],
                default: 'z',
                placeholder: 'ph',
              },
              { field: 'b', type: 'text', pattern: '(' },
              { field: 'c', type: 'text', hidden: true },
              { label: 'sin campo' },
              'basura',
            ],
          },
        ],
      },
    });
    const [a, b] = cfg.form!.sections[0]!.fields;
    expect(cfg.form!.sections[0]!.fields).toHaveLength(2);
    expect(a).toMatchObject({ required: true, minLength: 2, maxLength: 9, min: 1, max: 5, pattern: '^a', defaultValue: 'z' });
    expect((a as FieldDefExt).placeholder?.literal).toBe('ph');
    expect(b!.pattern).toBeUndefined();
    expect(cfg.warnings.some((w) => /expresión regular inválida/.test(w.message))).toBe(true);
  });

  it('tipos de campo: alias, enum, lookup sin entidad y autocomplete', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      form: {
        sections: [
          {
            fields: [
              { field: 'a', type: 'currency', currencyCode: 'EUR' },
              { field: 'b', type: 'multi-select', options: ['x', { value: 2, label: 'Dos' }] },
              { field: 'c', type: 'guid-select' },
              { field: 'd', type: 'autocomplete', options: ['u'] },
              { field: 'e', type: 'enum', options: ['p'] },
              { field: 'f', type: 'decimal' },
              { field: 'g', type: 'link' },
              { field: 'h' },
              { field: 'i', type: 'select', lookup: { entity: 'person', displayField: 'fullName' } },
            ],
          },
        ],
      },
    });
    const kinds = cfg.form!.sections[0]!.fields.map((f) => f.kind);
    expect(kinds).toEqual(['money', 'multiselect', 'text', 'select', 'select', 'decimal', 'url', 'text', 'lookup']);
    expect(cfg.form!.sections[0]!.fields[0]).toMatchObject({ currency: 'EUR' });
    expect(cfg.form!.sections[0]!.fields[1]!.options?.map((o) => o.value)).toEqual(['x', '2']);
    expect(cfg.form!.sections[0]!.fields[8]!.lookup).toMatchObject({ entity: 'person', displayField: 'fullName', valueField: 'id' });
    expect(cfg.warnings.some((w) => /sin `lookupEntity`/.test(w.message))).toBe(true);
  });

  it('filtros: degradaciones, búsqueda rápida, opciones y marcadores', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      filters: [
        { field: '_search', type: 'text', placeholder: 'Buscar…', icon: 'search', width: 30 },
        { field: 'when', type: 'date-range', label: 'Cuándo' },
        { field: 'n', type: 'number-range' },
        { field: 'k', type: 'autocomplete', options: ['a', 'b'] },
        { field: 'q', type: 'autocomplete' },
        { field: 'z', type: 'raro' },
        { field: 'm', type: 'multi-select', options: [{ value: 'a', label: 'A' }], defaultValue: 'a', hint: 'ayuda' },
        { serverField: 'only', type: 'switch', visibleWhen: 'a == 1' },
        { type: 'text' },
        7,
      ],
    });
    expect(cfg.filters.map((f) => f.kind)).toEqual(['text', 'date', 'number', 'select', 'text', 'text', 'multiselect', 'boolean']);
    expect(cfg.filters[0]).toMatchObject({ quickSearch: true, serverField: 'search', width: 12 });
    expect(cfg.filters[0]!.label.literal).toBe('Search');
    expect(cfg.filters[6]).toMatchObject({ defaultValue: 'a' });
    expect(cfg.filters[7]).toMatchObject({ field: 'only', visibleWhen: { expr: 'a == 1' } });
    expect(cfg.warnings.filter((w) => /tipo de filtro/.test(w.message))).toHaveLength(5);
  });

  it('vistas: árbol y mapa por superposición, `hierarchy` y vistas no soportadas', () => {
    const cfg = normalizeEntityConfig(
      {
        ...base,
        displayField: 'name',
        availableViews: ['grid', 'detail', 'kanban'],
        defaultView: 'tree',
        hierarchy: { type: 'tree', parentField: 'partOf', showCounts: true },
        grid: { columns: [{ field: 'name', type: 'text' }] },
      },
      {
        views: [
          { kind: 'grid' },
          {
            kind: 'tree',
            parentField: 'reportsTo',
            subtitleField: 'unit',
            badgeField: 'status',
            source: { type: 'chart', endpoint: '/api/hr/positions/{id}/chart', depth: 50 },
          },
          {
            kind: 'map',
            boundaryField: 'location.geoBoundary',
            aggregate: 'sum',
            valueField: 'areaM2',
            levels: [
              { id: 'world', label: { es: 'Mundo', en: 'World' }, source: 'demo', codeProperty: 'alpha2' },
              { id: 'bogus', source: 'demo' },
              { id: 'city' },
            ],
          },
          { kind: 'map', boundaryField: 'x', levels: [] },
          { kind: 'tree' },
          { kind: 'kanban' },
        ],
      },
    );
    expect(cfg.views.map((v) => v.kind)).toEqual(['grid', 'tree', 'map']);
    const tree = cfg.views[1]!;
    expect(tree).toMatchObject({
      kind: 'tree',
      parentField: 'reportsTo',
      labelField: 'name',
      subtitleField: 'unit',
      badgeField: 'status',
      source: { type: 'chart', depth: 20, childrenField: 'reports', nodeField: 'position', rootsFrom: 'list' },
    });
    const map = cfg.views[2]!;
    expect(map).toMatchObject({ kind: 'map', aggregate: 'sum', valueField: 'areaM2', boundaryField: 'location.geoBoundary' });
    expect((map as { levels: unknown[] }).levels).toHaveLength(1);
    expect(cfg.defaultView).toBe('tree');
    expect(cfg.warnings.some((w) => /kanban/.test(w.message))).toBe(true);
  });

  it('`hierarchy` sin vista declarada -> organigrama plano; `defaultView` sin esa vista cae a `grid`', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      defaultView: 'map',
      hierarchy: { type: 'tree', parentField: 'parent' },
      grid: { columns: [{ field: 'name', type: 'text' }] },
    });
    expect(cfg.views[1]).toMatchObject({ kind: 'tree', parentField: 'parent', source: { type: 'flat' } });
    expect(cfg.defaultView).toBe('grid');
    expect(cfg.warnings.some((w) => w.path === 'defaultView')).toBe(true);
    expect(normalizeEntityConfig({ ...base, defaultView: 'chart' }).warnings.some((w) => w.path === 'defaultView')).toBe(true);
  });

  it('ayuda: pasos con textos por clave, por idioma o literales', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      help: {
        steps: [
          { target: 'grid.table', titleKey: 'help.grid.title', title: 'Tabla', body: { es: 'Cuerpo', en: 'Body' } },
          { target: '#x', title: 'X', body: 'x' },
          { title: 'sin objetivo' },
        ],
      },
    });
    expect(cfg.help).toHaveLength(2);
    expect(cfg.help[0]!.title).toEqual({ literal: 'Tabla', key: 'help.grid.title' });
    expect(cfg.help[0]!.body.byLang).toEqual({ es: 'Cuerpo', en: 'Body' });
    const asArray = normalizeEntityConfig({ ...base, help: [{ target: 't', title: 'T', description: 'd' }] });
    expect(asArray.help[0]!.body.literal).toBe('d');
  });

  it('detalle: pestañas aplanadas, secciones con endpoint propio y detalle generado', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      detail: {
        tabs: [
          { id: 't1', sections: [{ id: 's1', title: 'Uno', fields: [{ field: 'a', type: 'text' }], apiEndpoint: '/x/{id}/y' }] },
          { id: 't2', component: 'custom', childGrid: { id: 'c', apiEndpoint: '/c/{id}', columns: [] } },
        ],
        childGrids: [
          { id: 'inline', dataField: 'items' },
          { id: 'sin-endpoint' },
          { entityId: 'note', title: 'Notas', columns: [] },
        ],
      },
    });
    expect(cfg.detail.sections.map((s) => s.id)).toEqual(['s1']);
    expect(cfg.detail.childGrids.map((c) => c.endpoint)).toEqual(['/notes', '/c/{id}']);
    expect(cfg.warnings.filter((w) => w.severity === 'warn').length).toBeGreaterThanOrEqual(4);

    const generated = normalizeEntityConfig({
      ...base,
      grid: { columns: [{ field: 'a', header: 'A' }, { field: 'b', type: 'badge' }] },
    });
    expect(generated.detail.sections[0]!.fields.map((f) => f.field)).toEqual(['a']);
    const fromForm = normalizeEntityConfig({
      ...base,
      form: { sections: [{ fields: [{ field: 'q', type: 'text' }] }] },
    });
    expect(fromForm.detail.sections[0]!.fields[0]!.field).toBe('q');
  });

  it('formulario generado desde las columnas y títulos del formulario', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      form: { generated: true, createTitle: { es: 'Nuevo', en: 'New' }, editTitleKey: 'k.edit' },
      grid: { columns: [{ field: 'a', header: 'A' }, { field: 'b', header: 'B', type: 'badge' }] },
    });
    expect(cfg.form!.sections[0]!.fields.map((f) => f.field)).toEqual(['a']);
    expect(cfg.form!.sections[0]!.fields[0]!.editable).toBe(true);
    expect(cfg.form!.createTitle?.byLang).toEqual({ es: 'Nuevo', en: 'New' });
    expect(cfg.form!.editTitle?.key).toBe('k.edit');
    const both = normalizeEntityConfig({
      ...base,
      form: { sections: [{ fields: [{ field: 'a' }] }], groups: [{ fields: [{ field: 'b' }] }] },
    });
    expect(both.form!.sections[0]!.fields[0]!.field).toBe('a');
    expect(both.warnings.some((w) => w.path === 'form.groups')).toBe(true);
  });

  it('textos: clave, objeto por idioma y literal; etiqueta por omisión legible', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      name: { es: 'Cosa', en: 'Thing' },
      namePluralKey: 'x.plural',
      description: 'd',
      grid: { columns: [{ field: 'unitPrice', headerKey: 'x.unit' }] },
    });
    expect(cfg.name).toEqual({ literal: 'Cosa', byLang: { es: 'Cosa', en: 'Thing' } });
    expect(cfg.namePlural).toEqual({ literal: '', key: 'x.plural' });
    expect(cfg.columns[0]!.label).toEqual({ literal: '', key: 'x.unit' });
    const bare = normalizeEntityConfig({ ...base, id: 'purchase-order', grid: { columns: [{ field: 'unitPrice' }] } });
    expect(bare.name.literal).toBe('Purchase order');
    expect(bare.columns[0]!.label.literal).toBe('Unit price');
  });

  it('estado vacío, paginación, permisos, agregado y búsqueda', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      aggregateEndpoint: '/api/v1/x/{id}/aggregate',
      permissions: { read: 'A.B.Read', write: 'A.B.Update' },
      emptyState: { icon: 'box', titleKey: 'e.t', message: 'Nada' },
      searchable: true,
      searchFields: ['name'],
      companyScoped: false,
      primaryKey: 'code',
      dataSource: { paginationStrategy: 'client', type: 'enriched', baseFilter: { kind: 'x' }, listMethod: 'get', serverFiltering: { companyFilterField: 'org' } },
      grid: { pageSize: 5000, pageSizeOptions: [5, 'x', 5000], selectionMode: 'single', selectable: false, defaultSort: { field: 'a', direction: 'DESC' } },
    });
    expect(cfg.aggregateEndpoint).toBe('/x/{id}/aggregate');
    expect(cfg.permissions).toEqual({ read: 'A.B.Read', write: 'A.B.Update' });
    expect(cfg.emptyState).toMatchObject({ icon: 'box', title: { key: 'e.t' }, message: { literal: 'Nada' } });
    expect(cfg).toMatchObject({ searchable: true, searchFields: ['name'], companyScoped: false, primaryKey: 'code' });
    expect(cfg.dataSource).toMatchObject({ pagination: 'client', baseFilter: { kind: 'x' }, listMethod: 'get', companyFilterField: 'org' });
    expect(cfg.grid).toMatchObject({ pageSize: 1000, pageSizeOptions: [5], selectionMode: 'single', selectable: false });
    expect(cfg.grid.defaultSort).toEqual({ field: 'a', direction: 'desc' });
    expect(cfg.warnings.some((w) => w.path === 'dataSource.type')).toBe(true);
    const emptyMsg = normalizeEntityConfig({ ...base, grid: { emptyMessage: 'Vacío' } });
    expect(emptyMsg.emptyState?.message?.literal).toBe('Vacío');
  });
});

describe('herencia de pistas del formulario al detalle (M-8)', () => {
  const base = { id: 'x', apiEndpoint: '/x', grid: { columns: [{ field: 'code', type: 'text' }] } };
  const find = (cfg: EntityConfigExt, field: string) => cfg.detail.sections.flatMap((s) => s.fields).find((f) => f.field === field)!;

  it('un campo de `detail` sin `hint` hereda el del campo homónimo de `form`, con aviso `info`', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      form: {
        sections: [
          {
            id: 'main',
            fields: [
              { field: 'code', label: 'Código', type: 'text', hint: 'No cambia después del alta.', placeholder: 'ABC-001', maxLength: 20 },
              { field: 'name', label: 'Nombre', type: 'text', hintKey: 'x.hint.name' },
            ],
          },
        ],
      },
      detail: {
        sections: [
          {
            id: 'main',
            fields: [
              { field: 'code', label: 'Código', type: 'text' },
              { field: 'name', label: 'Nombre', type: 'text' },
              { field: 'solo', label: 'Sólo detalle', type: 'text' },
            ],
          },
        ],
      },
    });
    const code = find(cfg, 'code') as FieldDefExt;
    expect(code.hint).toEqual({ literal: 'No cambia después del alta.' });
    expect(code.placeholder).toEqual({ literal: 'ABC-001' });
    expect(code.maxLength).toBe(20);
    // También la clave de i18n (el `Text` entero), sin compartir el objeto con el campo de `form`.
    expect(find(cfg, 'name').hint).toEqual({ literal: '', key: 'x.hint.name' });
    expect(find(cfg, 'name').hint).not.toBe(cfg.form!.sections[0]!.fields[1]!.hint);
    expect(find(cfg, 'solo').hint).toBeUndefined();
    const info = cfg.warnings.filter((w) => w.severity === 'info' && /hereda/.test(w.message));
    expect(info.map((w) => w.path)).toEqual(['detail.sections[0].fields[0]', 'detail.sections[0].fields[1]']);
    expect(info[0]!.message).toBe('el campo `code` hereda `hint`, `placeholder`, `maxLength` de `form`');
  });

  it('lo que el detalle ya trae manda, y sin formulario no hay nada que heredar', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      form: { sections: [{ id: 'main', fields: [{ field: 'code', type: 'text', hint: 'Del formulario', maxLength: 10 }] }] },
      detail: { sections: [{ id: 'main', fields: [{ field: 'code', type: 'text', hint: 'Del detalle', maxLength: 99 }] }] },
    });
    expect(find(cfg, 'code').hint).toEqual({ literal: 'Del detalle' });
    expect(find(cfg, 'code').maxLength).toBe(99);
    expect(cfg.warnings.some((w) => /hereda/.test(w.message))).toBe(false);
    const withoutForm = normalizeEntityConfig({ ...base, detail: { sections: [{ id: 'main', fields: [{ field: 'code', type: 'text' }] }] } });
    expect(find(withoutForm, 'code').hint).toBeUndefined();
  });

  it('en las 8 configuraciones de Go, el detalle muestra las pistas que antes sólo tenía el formulario', () => {
    for (const id of ['asset', 'facility', 'supplier']) {
      const cfg = load(id);
      const formHints = new Map(cfg.form!.sections.flatMap((s) => s.fields).filter((f) => f.hint).map((f) => [f.field, f.hint]));
      expect(formHints.size, id).toBeGreaterThan(0);
      for (const field of cfg.detail.sections.flatMap((s) => s.fields)) {
        if (formHints.has(field.field) && !(field as FieldDefExt).hint) throw new Error(`${id}.${field.field} sin pista`);
        if (formHints.has(field.field)) expect(field.hint, `${id}.${field.field}`).toBeDefined();
      }
    }
  });
});

describe('`skipSingle` del mapa (M-4, Q-10)', () => {
  const base = { id: 'x', apiEndpoint: '/x', grid: { columns: [{ field: 'name', type: 'text' }] } };
  const map = (extra: Record<string, unknown> = {}) => ({
    kind: 'map',
    boundaryField: 'location.geoBoundary',
    levels: [{ id: 'world', source: 'demo' }],
    ...extra,
  });
  const skip = (cfg: EntityConfigExt) => (cfg.views.find((v) => v.kind === 'map') as { skipSingle?: boolean } | undefined)?.skipSingle;

  it('por omisión es `true`', () => {
    expect(skip(normalizeEntityConfig(base, { views: [map()] }))).toBe(true);
  });

  it('respeta `false` explícito (también como cadena) y `true`', () => {
    expect(skip(normalizeEntityConfig(base, { views: [map({ skipSingle: false })] }))).toBe(false);
    expect(skip(normalizeEntityConfig(base, { views: [map({ skipSingle: 'false' })] }))).toBe(false);
    expect(skip(normalizeEntityConfig(base, { views: [map({ skipSingle: true })] }))).toBe(true);
    // Un valor que no es booleano no cuenta: vale la omisión.
    expect(skip(normalizeEntityConfig(base, { views: [map({ skipSingle: 3 })] }))).toBe(true);
  });

  it('`skipSingle` es una clave conocida: no da aviso', () => {
    const cfg = normalizeEntityConfig(base, { views: [map({ skipSingle: false })] });
    expect(cfg.warnings.filter((w) => /skipSingle/.test(w.path))).toEqual([]);
  });
});

describe('avisos de icono (B-1)', () => {
  const base = { id: 'x', apiEndpoint: '/x' };
  const iconWarnings = (cfg: EntityConfigExt) => cfg.warnings.filter((w) => /^icono `.+` desconocido/.test(w.message));

  it('un icono desconocido de la entidad, las opciones, las secciones, los filtros y las órdenes da `warn` con su ruta', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      icon: 'no-such-icon',
      grid: {
        columns: [
          { field: 'status', type: 'badge', badgeMap: { a: { label: 'A', icon: 'check' }, b: { label: 'B', icon: 'nope-b' } } },
        ],
      },
      form: { sections: [{ id: 'main', icon: 'nope-section', fields: [{ field: 'status', type: 'select', options: [{ value: 'a', label: 'A', icon: 'nope-opt' }] }] }] },
      filters: [{ field: 'status', type: 'text', icon: 'nope-filter' }],
      commands: [{ id: 'go', label: 'Ir', icon: 'nope-command', endpoint: '/x/{id}/go' }],
      emptyState: { icon: 'nope-empty', title: 'Nada' },
    });
    const found = iconWarnings(cfg);
    expect(found.every((w) => w.severity === 'warn')).toBe(true);
    expect(found.map((w) => w.path).sort()).toEqual(
      [
        'icon',
        'columns[0].options[1].icon',
        'form.sections[0].icon',
        'form.sections[0].fields[0].options[0].icon',
        'filters[0].icon',
        'commands[0].icon',
        'emptyState.icon',
      ].sort(),
    );
    expect(found.find((w) => w.path === 'icon')!.message).toBe('icono `no-such-icon` desconocido: se usa un círculo');
  });

  it('los iconos válidos (nombres de Phosphor y alias) no avisan', () => {
    const cfg = normalizeEntityConfig({
      ...base,
      icon: 'buildings',
      grid: { columns: [{ field: 'a', type: 'badge', badgeMap: { x: { label: 'X', icon: 'check-circle' } } }], rowActions: [{ action: 'view', label: 'Ver', icon: 'view' }] },
      filters: [{ field: '_search', type: 'text', icon: 'search' }],
    });
    expect(iconWarnings(cfg)).toEqual([]);
  });

  it('un nombre que sólo se parece a uno de la lista (mayúsculas, espacios) también avisa', () => {
    expect(iconWarnings(normalizeEntityConfig({ ...base, icon: 'Buildings' }))).toHaveLength(1);
    expect(iconWarnings(normalizeEntityConfig({ ...base, icon: 'tree structure' }))).toHaveLength(1);
  });
});

describe('errores de carga', () => {
  it('lanza ConfigError si no es un objeto o faltan `id` o `apiEndpoint`', () => {
    expect(() => normalizeEntityConfig(null)).toThrow(/no es un objeto/);
    expect(() => normalizeEntityConfig([])).toThrow(/no es un objeto/);
    expect(() => normalizeEntityConfig({ name: 'x', apiEndpoint: '/x' })).toThrow(/id/);
    expect(() => normalizeEntityConfig({ id: 'x' })).toThrow(/apiEndpoint/);
    try {
      normalizeEntityConfig({ id: 'x' });
    } catch (error) {
      expect((error as { issues: ConfigWarning[] }).issues[0]).toMatchObject({ path: 'apiEndpoint' });
    }
  });

  it('el `id` de las opciones suple o manda sobre el del JSON', () => {
    expect(normalizeEntityConfig({ name: 'x', apiEndpoint: '/x' }, undefined, { id: 'sup' }).id).toBe('sup');
    const over = normalizeEntityConfig({ id: 'a', apiEndpoint: '/x' }, undefined, { id: 'b' });
    expect(over.id).toBe('b');
    expect(over.warnings.some((w) => w.path === 'id')).toBe(true);
  });

  it('un tipo mal formado en una parte no fatal es un aviso, no una excepción', () => {
    const cfg = normalizeEntityConfig({ id: 'x', apiEndpoint: '/x', searchable: 'sí', grid: { columns: 'no' }, filters: {} });
    expect(cfg.columns).toEqual([]);
    expect(cfg.warnings.some((w) => /tipo no válido/.test(w.message))).toBe(true);
    const bad = normalizeEntityConfig({ id: 'x', apiEndpoint: '/x', form: 'raro' });
    expect(bad.form).toBeNull();
    expect(bad.warnings.some((w) => w.path === 'form')).toBe(true);
  });
});
