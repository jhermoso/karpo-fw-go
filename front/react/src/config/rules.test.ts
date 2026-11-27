import { describe, expect, it } from 'vitest';
import { evaluateRule, hasPermission, looseEquals, permissionMatches } from './rules';

const admin = { can: (p: string) => hasPermission(['*.*.*'], p) };
const reader = { can: (p: string) => hasPermission(['Facilities.Facility.Read', 'Geography.*.*'], p) };

describe('permissionMatches', () => {
  it('compara por segmentos con comodines', () => {
    expect(permissionMatches('*.*.*', 'Facilities.Facility.Read')).toBe(true);
    expect(permissionMatches('Facilities.*.*', 'Facilities.Facility.Read')).toBe(true);
    expect(permissionMatches('Facilities.Facility.*', 'Facilities.Facility.Update')).toBe(true);
    expect(permissionMatches('*.Facility.Read', 'Facilities.Facility.Read')).toBe(true);
    expect(permissionMatches('Facilities.Facility.Read', 'Facilities.Facility.Update')).toBe(false);
    expect(permissionMatches('Products.*.*', 'Facilities.Facility.Read')).toBe(false);
    expect(permissionMatches('facilities.facility.read', 'Facilities.Facility.Read')).toBe(true);
    expect(permissionMatches('Facilities.Facility', 'Facilities.Facility.Read')).toBe(false);
    expect(permissionMatches('Facilities.Facility.Read.Extra', 'Facilities.Facility.Read')).toBe(false);
    expect(permissionMatches('*.*.*.*', 'A.B.C')).toBe(false);
    expect(permissionMatches('*', 'A.B.C')).toBe(true);
  });

  it('hasPermission busca entre los concedidos', () => {
    expect(hasPermission([], 'A.B.C')).toBe(false);
    expect(hasPermission(['X.Y.Z', 'A.*.*'], 'A.B.C')).toBe(true);
  });
});

describe('evaluateRule', () => {
  it('sin regla o regla vacía: visible', () => {
    expect(evaluateRule(undefined, {})).toBe(true);
    expect(evaluateRule({}, {})).toBe(true);
    expect(evaluateRule({})).toBe(true);
  });

  it('`permission` con comodines y fail-closed', () => {
    expect(evaluateRule({ permission: 'Facilities.Facility.Read' }, { session: admin })).toBe(true);
    expect(evaluateRule({ permission: 'Facilities.Facility.Read' }, { session: reader })).toBe(true);
    expect(evaluateRule({ permission: 'Facilities.Facility.Update' }, { session: reader })).toBe(false);
    expect(evaluateRule({ permission: 'Geography.Holiday.Read' }, { session: reader })).toBe(true);
    // sin sesión: cerrado
    expect(evaluateRule({ permission: 'A.B.C' }, {})).toBe(false);
    expect(evaluateRule({ permission: 'A.B.C', expr: 'x == 1' }, { form: { x: 1 } })).toBe(false);
  });

  it('`field`/`equals` sobre la fila, con comparación tolerante y rutas con punto', () => {
    const row = { blocked: false, status: 'Pending', location: { country: 'ES' }, n: 1 };
    expect(evaluateRule({ field: 'blocked', equals: false }, { row })).toBe(true);
    expect(evaluateRule({ field: 'blocked', equals: 'false' }, { row })).toBe(true);
    expect(evaluateRule({ field: 'blocked', equals: true }, { row })).toBe(false);
    expect(evaluateRule({ field: 'status', equals: 'pending' }, { row })).toBe(true);
    expect(evaluateRule({ field: 'location.country', equals: 'ES' }, { row })).toBe(true);
    expect(evaluateRule({ field: 'n', equals: '1' }, { row })).toBe(true);
    // sin `equals`: el campo ha de ser «verdadero»
    expect(evaluateRule({ field: 'n' }, { row })).toBe(true);
    expect(evaluateRule({ field: 'blocked' }, { row })).toBe(false);
    expect(evaluateRule({ field: 'blocked', equals: null }, { row: { blocked: null } })).toBe(true);
    // el formulario manda sobre la fila
    expect(evaluateRule({ field: 'status', equals: 'done' }, { row, form: { status: 'done' } })).toBe(true);
    // sin datos o sin el campo: fail-open
    expect(evaluateRule({ field: 'missing', equals: 1 }, { row })).toBe(true);
    expect(evaluateRule({ field: 'x', equals: 1 }, {})).toBe(true);
    expect(evaluateRule({ field: 'location.nope', equals: 1 }, { row })).toBe(true);
  });

  it('`expr` con `==` y `!=`; lo ilegible es fail-open', () => {
    const form = { tipo: 'X', n: 3, vacio: null, flag: true };
    expect(evaluateRule({ expr: "tipo == 'X'" }, { form })).toBe(true);
    expect(evaluateRule({ expr: 'tipo == "Y"' }, { form })).toBe(false);
    expect(evaluateRule({ expr: 'tipo != X' }, { form })).toBe(false);
    expect(evaluateRule({ expr: 'n == 3' }, { form })).toBe(true);
    expect(evaluateRule({ expr: 'n != 3' }, { form })).toBe(false);
    expect(evaluateRule({ expr: 'flag == true' }, { form })).toBe(true);
    expect(evaluateRule({ expr: 'vacio == null' }, { form })).toBe(true);
    expect(evaluateRule({ expr: 'campo ausente > 3' }, { form })).toBe(true);
    expect(evaluateRule({ expr: 'otro == 1' }, { form })).toBe(true);
    expect(evaluateRule({ expr: '???' }, {})).toBe(true);
  });

  it('`allOf`, `anyOf` y `not`', () => {
    const row = { a: 1, b: 2 };
    const yes = { field: 'a', equals: 1 };
    const no = { field: 'a', equals: 9 };
    expect(evaluateRule({ allOf: [yes, { field: 'b', equals: 2 }] }, { row })).toBe(true);
    expect(evaluateRule({ allOf: [yes, no] }, { row })).toBe(false);
    expect(evaluateRule({ allOf: [] }, { row })).toBe(true);
    expect(evaluateRule({ anyOf: [no, yes] }, { row })).toBe(true);
    expect(evaluateRule({ anyOf: [no, { field: 'b', equals: 9 }] }, { row })).toBe(false);
    expect(evaluateRule({ anyOf: [] }, { row })).toBe(true);
    expect(evaluateRule({ not: no }, { row })).toBe(true);
    expect(evaluateRule({ not: yes }, { row })).toBe(false);
    // fail-open: lo que no se puede evaluar deja visible, también dentro de anyOf / not
    expect(evaluateRule({ anyOf: [no, { expr: '???' }] }, { row })).toBe(true);
    expect(evaluateRule({ not: { expr: '???' } }, { row })).toBe(true);
    expect(evaluateRule({ allOf: [{ expr: '???' }, yes] }, { row })).toBe(true);
    // permission dentro de composiciones
    expect(evaluateRule({ allOf: [{ permission: 'A.B.C' }, yes] }, { row, session: reader })).toBe(false);
    expect(evaluateRule({ anyOf: [{ permission: 'A.B.C' }, { permission: 'Facilities.Facility.Read' }] }, { session: reader })).toBe(true);
    expect(evaluateRule({ not: { permission: 'A.B.C' } }, { session: reader })).toBe(true);
    expect(evaluateRule({ permission: 'Facilities.Facility.Read', not: no, field: 'a', equals: 1 }, { row, session: reader })).toBe(true);
  });

  it('looseEquals ignora mayúsculas, espacios y tipos', () => {
    expect(looseEquals(true, 'TRUE')).toBe(true);
    expect(looseEquals(' a ', 'A')).toBe(true);
    expect(looseEquals(1, '1')).toBe(true);
    expect(looseEquals(null, '')).toBe(true);
    expect(looseEquals(undefined, null)).toBe(true);
    expect(looseEquals('a', 'b')).toBe(false);
  });
});
