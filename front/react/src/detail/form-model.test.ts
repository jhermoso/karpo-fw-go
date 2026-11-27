// Modelo del formulario: valores iniciales, esquema generado (mismos códigos que `validate`), cambios y cuerpos.
import { describe, expect, it } from 'vitest';
import type { FieldDef, FieldKind, Row } from '../contracts';
import { getFieldType } from '../fields';
import {
  buildCreateBody,
  buildFieldSchema,
  buildFormSchema,
  buildPatch,
  changedFields,
  fieldKey,
  fieldSpan,
  getIn,
  initialValues,
  isWritable,
  serializeValue,
  setIn,
  uniqueFields,
} from './form-model';

function def(field: string, kind: FieldKind = 'text', extra: Partial<FieldDef> = {}): FieldDef {
  return { field, label: { literal: field }, kind, required: false, editable: true, ...extra };
}

const row = (extra: Record<string, unknown> = {}): Row => ({ id: 'r1', version: 3, ...extra }) as Row;

/** Mensajes (códigos) que da el esquema generado para un valor. */
function codes(fieldDef: FieldDef, value: unknown): string[] {
  const result = buildFieldSchema(fieldDef).safeParse(value);
  return result.success ? [] : result.error.issues.map((issue) => issue.message);
}

describe('claves y rutas', () => {
  it('la clave de formulario no lleva puntos (react-hook-form los trata como anidamiento)', () => {
    expect(fieldKey('name')).toBe('name');
    expect(fieldKey('location.country')).toBe('location__country');
  });

  it('getIn lee rutas con punto y setIn copia sin mutar', () => {
    const source = { location: { country: 'ES', city: 'Madrid' }, name: 'A' };
    expect(getIn(source, 'location.country')).toBe('ES');
    expect(getIn(source, 'location.zip')).toBeUndefined();
    expect(getIn(source, 'name.x.y')).toBeUndefined();
    const next = setIn(source, 'location.country', 'FR');
    expect(next).toEqual({ location: { country: 'FR', city: 'Madrid' }, name: 'A' });
    expect(source.location.country).toBe('ES');
    expect(setIn({}, 'a.b.c', 1)).toEqual({ a: { b: { c: 1 } } });
  });
});

describe('valores iniciales', () => {
  it('lee del registro; los booleanos vacíos son falsos y las listas, vacías', () => {
    const defs = [def('name'), def('active', 'boolean'), def('tags', 'multiselect'), def('note')];
    expect(initialValues(defs, row({ name: 'Sede', tags: ['a'] }))).toEqual({
      name: 'Sede',
      active: false,
      tags: ['a'],
      note: null,
    });
  });

  it('sin registro usa defaultValue de la configuración (el alta)', () => {
    const defs = [def('country', 'text', { defaultValue: 'ES' } as Partial<FieldDef>), def('active', 'boolean')];
    expect(initialValues(defs, undefined)).toEqual({ country: 'ES', active: false });
  });

  it('un campo con punto se lee anidado y se guarda con clave plana', () => {
    expect(initialValues([def('location.country')], row({ location: { country: 'ES' } }))).toEqual({
      location__country: 'ES',
    });
  });
});

describe('escritura', () => {
  it('los immutable sólo se escriben en el alta; editable:false nunca; en lectura nada', () => {
    const immutable = def('type', 'text', { immutable: true });
    expect(isWritable(immutable, 'create')).toBe(true);
    expect(isWritable(immutable, 'edit')).toBe(false);
    expect(isWritable(def('x', 'text', { editable: false }), 'create')).toBe(false);
    expect(isWritable(def('x'), 'read')).toBe(false);
    expect(isWritable(def('x'), 'edit')).toBe(true);
  });
});

describe('esquema generado de los campos', () => {
  it('da los mismos códigos que FieldTypeDef.validate', () => {
    const cases: [FieldDef, unknown][] = [
      [def('a', 'text', { required: true }), ''],
      [def('a', 'text', { maxLength: 3 }), 'abcd'],
      [def('a', 'text', { minLength: 3 }), 'ab'],
      [def('a', 'number', { min: 5 }), 4],
      [def('a', 'number', { max: 5 }), 6],
      [def('a', 'email'), 'no-es-correo'],
      [def('a', 'date'), 'ayer'],
      [def('a', 'boolean', { required: true }), null],
    ];
    for (const [fieldDef, value] of cases) {
      expect(codes(fieldDef, value)).toEqual(getFieldType(fieldDef.kind).validate(value, fieldDef));
    }
  });

  it('un valor correcto no da errores y el formulario completo valida cada campo por su clave', () => {
    const schema = buildFormSchema([def('name', 'text', { required: true }), def('size', 'number', { max: 10 })]);
    expect(schema.safeParse({ name: 'Sede', size: 4 }).success).toBe(true);
    const bad = schema.safeParse({ name: '', size: 40 });
    expect(bad.success).toBe(false);
    if (!bad.success) {
      const byPath = Object.fromEntries(bad.error.issues.map((issue) => [issue.path.join('.'), issue.message]));
      expect(byPath).toEqual({ name: 'required', size: 'max' });
    }
  });

  it('un campo ausente del valor no rompe el esquema (se valida como vacío)', () => {
    const schema = buildFormSchema([def('name', 'text', { required: true })]);
    const result = schema.safeParse({});
    expect(result.success).toBe(false);
  });
});

describe('cambios y cuerpos', () => {
  const defs = [def('name'), def('size', 'number'), def('note', 'textarea'), def('active', 'boolean')];
  const baseline = initialValues(defs, row({ name: 'Sede', size: 10, note: 'hola', active: true }));

  it('sólo cuenta lo que difiere, con tolerancia a vacíos y a número/cadena', () => {
    expect(changedFields(defs, baseline, { ...baseline })).toEqual([]);
    expect(changedFields(defs, baseline, { ...baseline, size: '10' })).toEqual([]);
    const changes = changedFields(defs, baseline, { ...baseline, name: 'Otra', active: false });
    expect(changes.map((change) => change.def.field)).toEqual(['name', 'active']);
    expect(changes[0]).toMatchObject({ key: 'name', value: 'Otra', before: 'Sede' });
  });

  it('el PUT lleva sólo los campos cambiados; vaciar un texto envía cadena vacía y un número, null', () => {
    const changes = changedFields(defs, baseline, { ...baseline, note: '', size: null, name: 'Nueva' });
    expect(buildPatch(changes, row())).toEqual({ name: 'Nueva', size: null, note: '' });
  });

  it('un campo con punto envía el objeto de primer nivel completo (Go mezcla al nivel de la raíz)', () => {
    const nested = [def('location.country'), def('location.city')];
    const start = initialValues(nested, row({ location: { country: 'ES', city: 'Madrid' } }));
    const changes = changedFields(nested, start, { ...start, location__city: 'Sevilla' });
    expect(buildPatch(changes, row({ location: { country: 'ES', city: 'Madrid' } }))).toEqual({
      location: { country: 'ES', city: 'Sevilla' },
    });
  });

  it('serializeValue distingue los textos de los demás tipos', () => {
    expect(serializeValue(def('a'), '  ')).toBe('');
    expect(serializeValue(def('a', 'number'), '')).toBeNull();
    expect(serializeValue(def('a', 'multiselect'), [])).toEqual([]);
    expect(serializeValue(def('a', 'number'), 0)).toBe(0);
  });

  it('el POST omite lo vacío pero conserva false y cero', () => {
    const body = buildCreateBody(defs, { name: 'Sede', size: 0, note: '', active: false });
    expect(body).toEqual({ name: 'Sede', size: 0, active: false });
  });
});

describe('disposición', () => {
  it('uniqueFields no repite un campo que está en dos secciones', () => {
    expect(uniqueFields([def('a'), def('b'), def('a')]).map((d) => d.field)).toEqual(['a', 'b']);
  });

  it('fieldSpan usa width (1..12) o reparte las 12 columnas entre las de la sección', () => {
    expect(fieldSpan(def('a', 'text', { width: 6 }), 2)).toBe(6);
    expect(fieldSpan(def('a'), 2)).toBe(6);
    expect(fieldSpan(def('a'), 3)).toBe(4);
    expect(fieldSpan(def('a'), 4)).toBe(3);
    expect(fieldSpan(def('a'), 1)).toBe(12);
    expect(fieldSpan(def('a', 'text', { width: 40 }), 2)).toBe(12);
    expect(fieldSpan(def('a', 'text', { width: 0 }), 2)).toBe(6);
    expect(fieldSpan(def('a', 'textarea'), 3)).toBe(12);
  });
});
