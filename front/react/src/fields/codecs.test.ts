import { beforeEach, describe, expect, it } from 'vitest';
import type { FieldDef, FieldKind, OptionDef } from '../contracts';
import { getCodec } from './codecs';
import {
  compareDecimalStrings,
  editDecimalString,
  formatDecimalString,
  isDecimalString,
  numberLocale,
  numberToDecimalString,
  parseDecimalText,
  roundDecimalString,
  toDecimalString,
} from './decimal';
import { dateLocale, dateParts, dateTimeParts, parseDateText, parseDateTimeText, toIsoDateTime } from './dates';
import { clearLookupCache, rememberLookupLabel } from './lookup-cache';
import { FIELD_KINDS, fieldTypes, getFieldType, registerFieldType } from './registry';
import { findOption, isEmpty, sameValue, textFor, toStringArray } from './values';

function def(kind: FieldKind, extra: Partial<FieldDef> = {}): FieldDef {
  return { field: 'f', label: { literal: 'Campo' }, kind, required: false, editable: true, ...extra };
}

const OPTIONS: OptionDef[] = [
  { value: 'A', label: { literal: 'Activo', byLang: { en: 'Active' } }, tone: 'success' },
  { value: 'I', label: { literal: 'Inactivo', byLang: { en: 'Inactive' } }, tone: 'neutral' },
];

const NBSP = ' ';

function parseOk(kind: FieldKind, text: string, locale: string, extra: Partial<FieldDef> = {}): unknown {
  const result = getCodec(kind).parse(text, def(kind, extra), locale);
  if (!result.ok) throw new Error(`parse(${kind}, ${text}, ${locale}) falló: ${result.code}`);
  return result.value;
}

describe('registro de tipos de campo', () => {
  it('tiene los 16 tipos con visor, editor y códec', () => {
    expect(FIELD_KINDS).toHaveLength(16);
    for (const kind of FIELD_KINDS) {
      const type = getFieldType(kind);
      expect(type.kind).toBe(kind);
      expect(typeof type.Viewer).toBe('function');
      expect(typeof type.Editor).toBe('function');
      expect(['start', 'end', 'center']).toContain(type.align);
      expect(getCodec(kind).kind).toBe(kind);
    }
  });

  it('alinea números a la derecha y booleanos al centro', () => {
    for (const kind of ['number', 'decimal', 'money', 'percentage'] as const) {
      expect(getFieldType(kind).align).toBe('end');
    }
    expect(getFieldType('boolean').align).toBe('center');
    expect(getFieldType('text').align).toBe('start');
  });

  it('permite registrar o sustituir un tipo y trata un kind desconocido como text', () => {
    const original = fieldTypes.email;
    registerFieldType({ ...original, align: 'center' });
    expect(getFieldType('email').align).toBe('center');
    registerFieldType(original);
    expect(getFieldType('email').align).toBe('start');
    expect(getFieldType('nope' as FieldKind).kind).toBe('text');
    expect(getCodec('nope' as FieldKind).kind).toBe('text');
  });
});

describe('decimales sin pérdida', () => {
  it("'1.234,56' en es y '1,234.56' en en dan la cadena decimal '1234.56'", () => {
    expect(parseOk('decimal', '1.234,56', 'es')).toBe('1234.56');
    expect(parseOk('decimal', '1,234.56', 'en')).toBe('1234.56');
    expect(parseOk('money', '1.234,56', 'es')).toBe('1234.56');
    expect(parseOk('money', '1,234.56', 'en')).toBe('1234.56');
    expect(parseOk('percentage', '12,5 %', 'es')).toBe('12.5');
  });

  it("un importe '1200.10' se muestra y vuelve como cadena sin pérdida", () => {
    const money = def('money', { format: 'EUR' });
    const codec = getCodec('money');
    expect(codec.format('1200.10', money, 'es')).toBe(`1.200,10${NBSP}€`);
    expect(codec.format('1200.10', def('money', { format: 'USD' }), 'en')).toBe('$1,200.10');
    const edit = codec.editText('1200.10', money, 'es');
    expect(edit).toBe('1200,10');
    expect(parseOk('money', edit, 'es')).toBe('1200.10');
    expect(typeof parseOk('money', '1200.10', 'en')).toBe('string');
    // Más decimales que los visibles: nunca se redondea sin que la configuración lo pida.
    expect(codec.format('0.123456789', def('money'), 'en')).toBe('0.123456789');
  });

  it('conserva importes más largos que la precisión de un float', () => {
    const big = '12345678901234567890.123456789';
    expect(parseOk('decimal', '12.345.678.901.234.567.890,123456789', 'es')).toBe(big);
    expect(getCodec('decimal').format(big, def('decimal'), 'en')).toBe('12,345,678,901,234,567,890.123456789');
  });

  it('tolera el separador del otro idioma cuando no hay ambigüedad', () => {
    expect(parseDecimalText('1234,56', 'en')).toEqual({ ok: true, value: '1234.56' });
    expect(parseDecimalText('12.5', 'es')).toEqual({ ok: true, value: '12.5' });
    expect(parseDecimalText('1.234', 'es')).toEqual({ ok: true, value: '1234' });
    expect(parseDecimalText('1,234', 'en')).toEqual({ ok: true, value: '1234' });
    expect(parseDecimalText('1.234', 'en')).toEqual({ ok: true, value: '1.234' });
    expect(parseDecimalText('1.234.567', 'es')).toEqual({ ok: true, value: '1234567' });
    expect(parseDecimalText('0,500', 'es')).toEqual({ ok: true, value: '0.500' });
    expect(parseDecimalText('-12,5 €', 'es')).toEqual({ ok: true, value: '-12.5' });
    expect(parseDecimalText('+7', 'en')).toEqual({ ok: true, value: '7' });
    expect(parseDecimalText('.5', 'en')).toEqual({ ok: true, value: '0.5' });
    expect(parseDecimalText('007,50', 'es')).toEqual({ ok: true, value: '7.50' });
    expect(parseDecimalText('-0,00', 'es')).toEqual({ ok: true, value: '0.00' });
    expect(parseDecimalText('  ', 'es')).toEqual({ ok: true, value: null });
    expect(parseDecimalText('12,', 'es')).toEqual({ ok: true, value: '12' });
    expect(parseDecimalText('USD 1,000.50', 'en')).toEqual({ ok: true, value: '1000.50' });
  });

  it('rechaza lo que no es un número', () => {
    for (const text of ['abc', '1.23.4', '1,2,3', '1.234,56,7', '--1', '1e5', ',', '1,234.5.6']) {
      expect(parseDecimalText(text, 'es').ok, text).toBe(false);
    }
  });

  it('formatea con agrupación por idioma', () => {
    expect(formatDecimalString('1234567.5', 'es')).toBe('1.234.567,5');
    expect(formatDecimalString('1234567.5', 'en')).toBe('1,234,567.5');
    // es no agrupa hasta 5 cifras (regla del idioma) salvo que se pida.
    expect(formatDecimalString('1234', 'es')).toBe('1234');
    expect(formatDecimalString('12345', 'es')).toBe('12.345');
    expect(formatDecimalString('1234', 'es', { minGrouping: 1 })).toBe('1.234');
    expect(formatDecimalString('-1234.5', 'en', { fraction: 2 })).toBe('-1,234.50');
    expect(formatDecimalString('1.005', 'en', { fraction: 2 })).toBe('1.01');
    expect(formatDecimalString('12', 'en', { minFraction: 2, grouping: false })).toBe('12.00');
    expect(formatDecimalString('no es', 'en')).toBe('no es');
    expect(editDecimalString('1234.5', 'es')).toBe('1234,5');
    expect(editDecimalString('x', 'es')).toBe('x');
  });

  it('deriva los separadores de otros idiomas con Intl', () => {
    expect(numberLocale('de').decimal).toBe(',');
    expect(numberLocale('de').group).toBe('.');
    expect(parseDecimalText('1.234,56', 'de')).toEqual({ ok: true, value: '1234.56' });
    expect(formatDecimalString('1234.56', 'de')).toBe('1.234,56');
    expect(numberLocale('es-MX').decimal).toBe('.');
    expect(numberLocale('es_MX').group).toBe(',');
    expect(numberLocale('xx-INVALID-TAG-!!').decimal).toBeDefined();
  });

  it('redondea y compara cadenas decimales exactas', () => {
    expect(roundDecimalString('2.5', 0)).toBe('3');
    expect(roundDecimalString('-0.001', 2)).toBe('0.00');
    expect(roundDecimalString('9.999', 2)).toBe('10.00');
    expect(roundDecimalString('1.2', 3)).toBe('1.2');
    expect(roundDecimalString('x', 2)).toBe('x');
    expect(compareDecimalStrings('1200.10', '1200.1')).toBe(0);
    expect(compareDecimalStrings('-1', '1')).toBeLessThan(0);
    expect(compareDecimalStrings('2', '10')).toBeLessThan(0);
    expect(compareDecimalStrings('0.30', '0.3')).toBe(0);
    expect(compareDecimalStrings('0', '-0.0')).toBe(0);
    expect(compareDecimalStrings('-5', '-10')).toBeGreaterThan(0);
    expect(compareDecimalStrings('1.25', '1.3')).toBeLessThan(0);
    expect(isDecimalString('12.50')).toBe(true);
    expect(isDecimalString('12,50')).toBe(false);
    expect(toDecimalString(12.5)).toBe('12.5');
    expect(toDecimalString(' 7 ')).toBe('7');
    expect(toDecimalString(Number.NaN)).toBeUndefined();
    expect(toDecimalString(true)).toBeUndefined();
    expect(numberToDecimalString(1e21)).toBe('1000000000000000000000');
    expect(numberToDecimalString(Number.POSITIVE_INFINITY)).toBe('');
  });
});

describe.each(['es', 'en'] as const)('format y parse en %s', (locale) => {
  const es = locale === 'es';

  it('text, textarea, email, phone y url', () => {
    for (const kind of ['text', 'textarea', 'email', 'phone', 'url'] as const) {
      const codec = getCodec(kind);
      expect(codec.format('  hola ', def(kind), locale)).toBe('  hola ');
      expect(codec.format(null, def(kind), locale)).toBe('');
      expect(codec.format(42, def(kind), locale)).toBe('42');
    }
    expect(parseOk('text', '  hola  ', locale)).toBe('hola');
    expect(parseOk('textarea', 'a\r\nb', locale)).toBe('a\nb');
    expect(parseOk('email', ' a@b.com ', locale)).toBe('a@b.com');
  });

  it('number', () => {
    const codec = getCodec('number');
    expect(codec.format(1234567.5, def('number'), locale)).toBe(es ? '1.234.567,5' : '1,234,567.5');
    expect(codec.format(1234, def('number'), locale)).toBe(es ? '1234' : '1,234');
    expect(codec.format('7', def('number'), locale)).toBe('7');
    expect(codec.format(12.3456, def('number', { format: '0.00' }), locale)).toBe(es ? '12,35' : '12.35');
    expect(codec.format('abc', def('number'), locale)).toBe('abc');
    expect(codec.format(null, def('number'), locale)).toBe('');
    expect(parseOk('number', es ? '1.234,5' : '1,234.5', locale)).toBe(1234.5);
    expect(parseOk('number', '', locale)).toBeNull();
    expect(codec.parse('x', def('number'), locale)).toEqual({ ok: false, code: 'format' });
    expect(codec.editText(1234.5, def('number'), locale)).toBe(es ? '1234,5' : '1234.5');
    expect(codec.editText('x', def('number'), locale)).toBe('x');
    expect(codec.editText(null, def('number'), locale)).toBe('');
  });

  it('decimal, money y percentage', () => {
    expect(getCodec('decimal').format('1234.50', def('decimal'), locale)).toBe(es ? '1.234,50' : '1,234.50');
    expect(getCodec('money').format('5', def('money'), locale)).toBe(es ? '5,00' : '5.00');
    expect(getCodec('money').format('1200.1', def('money', { format: 'n3' }), locale)).toBe(
      es ? '1.200,100' : '1,200.100',
    );
    expect(getCodec('percentage').format('12.5', def('percentage'), locale)).toBe(es ? `12,5${NBSP}%` : '12.5%');
    expect(getCodec('money').format('1200.10', def('money', { format: 'currency:EUR' }), locale)).toBe(
      es ? `1.200,10${NBSP}€` : '€1,200.10',
    );
    expect(getCodec('money').format('1', def('money', { format: 'XXXX' }), locale)).toBe(es ? '1,00' : '1.00');
    expect(getCodec('money').format('1', def('money', { format: 'ZZZ' }), locale).includes('ZZZ')).toBe(true);
    expect(parseOk('decimal', es ? '1.234,56' : '1,234.56', locale)).toBe('1234.56');
    expect(parseOk('money', '', locale)).toBeNull();
  });

  it('date', () => {
    const codec = getCodec('date');
    expect(codec.format('2025-03-14', def('date'), locale)).toBe(es ? '14/03/2025' : '03/14/2025');
    expect(codec.format('2025-03-14T00:00:00Z', def('date'), locale)).toBe(es ? '14/03/2025' : '03/14/2025');
    expect(codec.format('no-fecha', def('date'), locale)).toBe('no-fecha');
    expect(codec.format('', def('date'), locale)).toBe('');
    expect(parseOk('date', es ? '14/03/2025' : '03/14/2025', locale)).toBe('2025-03-14');
    expect(parseOk('date', '2025-03-14', locale)).toBe('2025-03-14');
    expect(parseOk('date', es ? '14-3-25' : '3-14-25', locale)).toBe('2025-03-14');
    expect(parseOk('date', es ? '14.03.1985' : '03.14.1985', locale)).toBe('1985-03-14');
    expect(parseOk('date', es ? '14032025' : '03142025', locale)).toBe('2025-03-14');
    expect(parseOk('date', '', locale)).toBeNull();
    expect(codec.parse(es ? '31/02/2025' : '02/31/2025', def('date'), locale)).toEqual({ ok: false, code: 'format' });
    expect(codec.parse('mañana', def('date'), locale)).toEqual({ ok: false, code: 'format' });
    expect(codec.editText('2025-03-14', def('date'), locale)).toBe(es ? '14/03/2025' : '03/14/2025');
  });

  it('datetime', () => {
    const codec = getCodec('datetime');
    expect(codec.format('2025-03-14T10:30:00', def('datetime'), locale)).toBe(
      es ? '14/03/2025 10:30' : '03/14/2025 10:30 AM',
    );
    expect(codec.format('2025-03-14T15:05:00', def('datetime'), locale)).toBe(
      es ? '14/03/2025 15:05' : '03/14/2025 3:05 PM',
    );
    expect(codec.format('2025-03-14T00:00:00', def('datetime'), locale)).toBe(
      es ? '14/03/2025 00:00' : '03/14/2025 12:00 AM',
    );
    expect(codec.format('2025-03-14', def('datetime'), locale)).toContain(es ? '14/03/2025' : '03/14/2025');
    const text = es ? '14/03/2025 10:30' : '03/14/2025 10:30 AM';
    const parsed = parseOk('datetime', text, locale);
    expect(parsed).toBe(new Date(2025, 2, 14, 10, 30, 0).toISOString());
    // Ida y vuelta con la hora local de pared, sea cual sea la zona de la máquina.
    expect(codec.format(parsed, def('datetime'), locale)).toBe(text);
    expect(parseOk('datetime', es ? '14/03/2025 22:30' : '03/14/2025 10:30 PM', locale)).toBe(
      new Date(2025, 2, 14, 22, 30, 0).toISOString(),
    );
    expect(parseOk('datetime', es ? '14/03/2025' : '03/14/2025', locale)).toBe(new Date(2025, 2, 14).toISOString());
    expect(parseOk('datetime', '2025-03-14T10:30', locale)).toBe(new Date(2025, 2, 14, 10, 30).toISOString());
    expect(parseOk('datetime', '', locale)).toBeNull();
    for (const bad of ['14/03/2025 25:00', '14/03/2025 10:99', '14/03/2025 hola', '99/99/2025 10:00', '03/14/2025 13:00 PM']) {
      expect(codec.parse(bad, def('datetime'), locale).ok, bad).toBe(false);
    }
    expect(codec.editText(parsed, def('datetime'), locale)).toBe(text);
  });

  it('boolean tolerante', () => {
    const codec = getCodec('boolean');
    for (const truthy of [true, 'true', 'TRUE', 'sí', 'Sí', 'si', 'yes', 'Yes', 'y', '1', 1, 'x']) {
      expect(codec.format(truthy, def('boolean'), locale), String(truthy)).toBe(es ? 'Sí' : 'Yes');
    }
    for (const falsy of [false, 'false', 'no', 'No', '0', 0, 'n']) {
      expect(codec.format(falsy, def('boolean'), locale), String(falsy)).toBe('No');
    }
    expect(codec.format(null, def('boolean'), locale)).toBe('');
    expect(codec.format('quizá', def('boolean'), locale)).toBe('');
    expect(parseOk('boolean', 'sí', locale)).toBe(true);
    expect(parseOk('boolean', 'Yes', locale)).toBe(true);
    expect(parseOk('boolean', 'no', locale)).toBe(false);
    expect(parseOk('boolean', '', locale)).toBeNull();
    expect(codec.parse('quizá', def('boolean'), locale)).toEqual({ ok: false, code: 'format' });
    const custom = def('boolean', {
      trueLabel: { literal: 'Activo', byLang: { en: 'Active' } },
      falseLabel: { literal: 'Baja' },
    });
    expect(codec.format(true, custom, locale)).toBe(es ? 'Activo' : 'Active');
    expect(codec.format(false, custom, locale)).toBe('Baja');
    expect(parseOk('boolean', es ? 'activo' : 'Active', locale, custom)).toBe(true);
    expect(codec.editText(true, def('boolean'), locale)).toBe(es ? 'Sí' : 'Yes');
  });

  it('select y badge por valor o por etiqueta', () => {
    for (const kind of ['select', 'badge'] as const) {
      const codec = getCodec(kind);
      const field = def(kind, { options: OPTIONS });
      expect(codec.format('A', field, locale)).toBe(es ? 'Activo' : 'Active');
      expect(codec.format('Z', field, locale)).toBe('Z');
      expect(codec.format(null, field, locale)).toBe('');
      expect(parseOk(kind, 'A', locale, { options: OPTIONS })).toBe('A');
      expect(parseOk(kind, 'activo', locale, { options: OPTIONS })).toBe('A');
      expect(parseOk(kind, es ? 'Inactivo' : 'Inactive', locale, { options: OPTIONS })).toBe('I');
      expect(parseOk(kind, ' i ', locale, { options: OPTIONS })).toBe('I');
      expect(parseOk(kind, '', locale, { options: OPTIONS })).toBeNull();
      expect(parseOk(kind, 'cualquiera', locale)).toBe('cualquiera');
      expect(codec.parse('zzz', field, locale)).toEqual({ ok: false, code: 'format' });
    }
    // Sin tildes ni mayúsculas.
    expect(parseOk('select', 'ÁCTIVO'.replace('Á', 'A'), locale, { options: OPTIONS })).toBe('A');
  });

  it('multiselect', () => {
    const codec = getCodec('multiselect');
    const field = def('multiselect', { options: OPTIONS });
    expect(codec.format(['A', 'I'], field, locale)).toBe(es ? 'Activo, Inactivo' : 'Active, Inactive');
    expect(codec.format('A,I', field, locale)).toBe(es ? 'Activo, Inactivo' : 'Active, Inactive');
    expect(codec.format(null, field, locale)).toBe('');
    expect(parseOk('multiselect', es ? 'Activo; Inactivo' : 'Active, Inactive', locale, { options: OPTIONS })).toEqual([
      'A',
      'I',
    ]);
    expect(parseOk('multiselect', 'A, A', locale, { options: OPTIONS })).toEqual(['A']);
    expect(parseOk('multiselect', 'x, y', locale)).toEqual(['x', 'y']);
    expect(parseOk('multiselect', '', locale)).toBeNull();
    expect(codec.parse('A, nada', field, locale)).toEqual({ ok: false, code: 'format' });
    expect(codec.editText(['A'], field, locale)).toBe(es ? 'Activo' : 'Active');
  });

  it('lookup', () => {
    clearLookupCache();
    const field = def('lookup', { lookup: { entity: 'company', valueField: 'id', displayField: 'name' } });
    const codec = getCodec('lookup');
    expect(codec.format('c-1', field, locale)).toBe('c-1');
    rememberLookupLabel(field.lookup!, 'c-1', 'Acme');
    expect(codec.format('c-1', field, locale)).toBe('Acme');
    expect(codec.format(null, field, locale)).toBe('');
    expect(parseOk('lookup', 'acme', locale, { lookup: field.lookup })).toBe('c-1');
    expect(parseOk('lookup', 'c-9', locale, { lookup: field.lookup })).toBe('c-9');
    expect(parseOk('lookup', ' ', locale)).toBeNull();
    expect(codec.editText('c-1', field, locale)).toBe('Acme');
    expect(codec.format('c-1', def('lookup'), locale)).toBe('c-1');
  });
});

describe('validate devuelve exactamente los códigos', () => {
  it('required en todos los tipos', () => {
    for (const kind of FIELD_KINDS) {
      const field = def(kind, { required: true });
      const type = getFieldType(kind);
      for (const empty of [null, undefined, '']) {
        expect(type.validate(empty, field), `${kind} ${String(empty)}`).toEqual(['required']);
      }
      expect(type.validate(null, def(kind)), kind).toEqual([]);
    }
    expect(getFieldType('text').validate('   ', def('text', { required: true }))).toEqual(['required']);
    expect(getFieldType('multiselect').validate([], def('multiselect', { required: true }))).toEqual(['required']);
    // false es un valor válido para un booleano obligatorio.
    expect(getFieldType('boolean').validate(false, def('boolean', { required: true }))).toEqual([]);
  });

  it('maxLength, minLength y pattern', () => {
    const validate = getFieldType('text').validate;
    expect(validate('abcdef', def('text', { maxLength: 5 }))).toEqual(['maxLength']);
    expect(validate('abcde', def('text', { maxLength: 5 }))).toEqual([]);
    expect(validate('ab', def('text', { minLength: 3 }))).toEqual(['minLength']);
    expect(validate('abc', def('text', { minLength: 3 }))).toEqual([]);
    expect(validate('abc12', def('text', { pattern: '[A-Z]{3}\\d+' }))).toEqual(['pattern']);
    expect(validate('ABC12', def('text', { pattern: '[A-Z]{3}\\d+' }))).toEqual([]);
    // El patrón debe cubrir todo el texto (semántica de HTML).
    expect(validate('ABC12x', def('text', { pattern: '[A-Z]{3}\\d+' }))).toEqual(['pattern']);
    expect(validate('ab', def('text', { minLength: 3, maxLength: 1, pattern: '\\d+' }))).toEqual([
      'maxLength',
      'minLength',
      'pattern',
    ]);
    // Un patrón mal escrito en la configuración no bloquea a nadie.
    expect(validate('x', def('text', { pattern: '(' }))).toEqual([]);
    expect(getFieldType('textarea').validate('abcdef', def('textarea', { maxLength: 3 }))).toEqual(['maxLength']);
    expect(validate(42, def('text', { maxLength: 1 }))).toEqual(['maxLength']);
    expect(validate({}, def('text'))).toEqual(['format']);
  });

  it('min y max en numéricos, exactos sobre cadenas decimales', () => {
    expect(getFieldType('number').validate(5, def('number', { min: 10 }))).toEqual(['min']);
    expect(getFieldType('number').validate(50, def('number', { max: 10 }))).toEqual(['max']);
    expect(getFieldType('number').validate(10, def('number', { min: 10, max: 10 }))).toEqual([]);
    expect(getFieldType('money').validate('12.50', def('money', { min: 12.5 }))).toEqual([]);
    expect(getFieldType('money').validate('12.49', def('money', { min: 12.5 }))).toEqual(['min']);
    expect(getFieldType('decimal').validate('100.01', def('decimal', { max: 100 }))).toEqual(['max']);
    expect(getFieldType('percentage').validate('101', def('percentage', { max: 100 }))).toEqual(['max']);
    expect(getFieldType('number').validate(0, def('number', { required: true }))).toEqual([]);
  });

  it('min y max en multiselect cuentan opciones', () => {
    const field = def('multiselect', { options: OPTIONS, min: 2 });
    expect(getFieldType('multiselect').validate(['A'], field)).toEqual(['min']);
    expect(getFieldType('multiselect').validate(['A', 'I'], { ...field, max: 1 })).toEqual(['max']);
    expect(getFieldType('multiselect').validate(['A', 'Z'], def('multiselect', { options: OPTIONS }))).toEqual(['format']);
  });

  it('format en los tipos con sintaxis propia', () => {
    for (const kind of ['number', 'decimal', 'money', 'percentage'] as const) {
      expect(getFieldType(kind).validate('abc', def(kind)), kind).toEqual(['format']);
      expect(getFieldType(kind).validate('12,5,6', def(kind)), kind).toEqual(['format']);
    }
    expect(getFieldType('date').validate('2025-02-30', def('date'))).toEqual(['format']);
    expect(getFieldType('date').validate('14/03/2025', def('date'))).toEqual(['format']);
    expect(getFieldType('date').validate('2025-03-14', def('date'))).toEqual([]);
    expect(getFieldType('datetime').validate('ayer', def('datetime'))).toEqual(['format']);
    expect(getFieldType('datetime').validate('2025-03-14T10:30:00Z', def('datetime'))).toEqual([]);
    expect(getFieldType('boolean').validate('quizá', def('boolean'))).toEqual(['format']);
    expect(getFieldType('boolean').validate('sí', def('boolean'))).toEqual([]);
    expect(getFieldType('select').validate('Z', def('select', { options: OPTIONS }))).toEqual(['format']);
    expect(getFieldType('select').validate('A', def('select', { options: OPTIONS }))).toEqual([]);
    expect(getFieldType('select').validate('lo que sea', def('select'))).toEqual([]);
    expect(getFieldType('badge').validate('Z', def('badge', { options: OPTIONS }))).toEqual(['format']);
    expect(getFieldType('lookup').validate({}, def('lookup'))).toEqual(['format']);
    expect(getFieldType('lookup').validate('c-1', def('lookup'))).toEqual([]);
    expect(getFieldType('lookup').validate(7, def('lookup'))).toEqual([]);
    expect(getFieldType('email').validate('a@b', def('email'))).toEqual(['format']);
    expect(getFieldType('email').validate('a@b.com', def('email'))).toEqual([]);
    expect(getFieldType('phone').validate('12', def('phone'))).toEqual(['format']);
    expect(getFieldType('phone').validate('+34 600 123 456', def('phone'))).toEqual([]);
    expect(getFieldType('phone').validate('(91) 555-1234 ext 12', def('phone'))).toEqual([]);
    expect(getFieldType('url').validate('x.com', def('url'))).toEqual(['format']);
    expect(getFieldType('url').validate('javascript:alert(1)', def('url'))).toEqual(['format']);
    expect(getFieldType('url').validate('https://x.com/a?b=1', def('url'))).toEqual([]);
    expect(getFieldType('email').validate('mal', def('email', { maxLength: 2 }))).toEqual(['maxLength', 'format']);
  });
});

describe('fechas', () => {
  it('fechas reales y partes', () => {
    expect(dateParts('2024-02-29')).toEqual({ year: 2024, month: 2, day: 29 });
    expect(dateParts('2025-02-29')).toBeUndefined();
    expect(dateParts(new Date(2025, 2, 14))).toEqual({ year: 2025, month: 3, day: 14 });
    expect(dateParts(new Date('x'))).toBeUndefined();
    expect(dateParts(5)).toBeUndefined();
    expect(dateTimeParts(new Date(2025, 2, 14, 8, 5, 3))).toEqual({
      year: 2025,
      month: 3,
      day: 14,
      hour: 8,
      minute: 5,
      second: 3,
    });
    expect(dateTimeParts(new Date('x'))).toBeUndefined();
    expect(dateTimeParts(3)).toBeUndefined();
    expect(dateTimeParts('2025-03-14 10:30')).toMatchObject({ hour: 10, minute: 30 });
    expect(dateTimeParts('2025-03-14T25:30')).toBeUndefined();
    expect(dateTimeParts('2025-03-14T10:30:00+01:00')).toBeDefined();
    expect(dateTimeParts('2025-03-14T10:30:00+0100')).toBeDefined();
    expect(dateTimeParts('2025-03-14T10:30:00.123Z')).toBeDefined();
    expect(toIsoDateTime({ year: 2025, month: 3, day: 14, hour: 10, minute: 30, second: 0 })).toBe(
      new Date(2025, 2, 14, 10, 30).toISOString(),
    );
  });

  it('un instante con zona se muestra en hora local', () => {
    const instant = new Date(Date.UTC(2025, 5, 1, 12, 0, 0));
    const parts = dateTimeParts('2025-06-01T12:00:00Z');
    expect(parts).toEqual(dateTimeParts(instant));
  });

  it('deriva el orden de otros idiomas con Intl', () => {
    expect(dateLocale('en-GB').order).toBe('dmy');
    expect(dateLocale('ja').order).toBe('ymd');
    expect(dateLocale('en_US').order).toBe('mdy');
    expect(dateLocale('de').separator).toBe('.');
    expect(parseDateText('14.03.2025', 'de')).toEqual({ ok: true, value: { year: 2025, month: 3, day: 14 } });
    expect(parseDateTimeText('14.03.2025 10:30', 'de').ok).toBe(true);
    expect(parseDateTimeText('   ', 'es').ok).toBe(false);
  });
});

describe('valores', () => {
  it('isEmpty, sameValue y toStringArray', () => {
    expect(isEmpty(null)).toBe(true);
    expect(isEmpty(' ')).toBe(true);
    expect(isEmpty([])).toBe(true);
    expect(isEmpty(0)).toBe(false);
    expect(isEmpty(false)).toBe(false);
    expect(sameValue(null, '')).toBe(true);
    expect(sameValue('12.5', 12.5)).toBe(true);
    expect(sameValue(['a', 'b'], ['a', 'b'])).toBe(true);
    expect(sameValue(['a'], ['a', 'b'])).toBe(false);
    expect(sameValue('a', 'b')).toBe(false);
    expect(sameValue({}, {})).toBe(false);
    expect(toStringArray('a, b;c')).toEqual(['a', 'b', 'c']);
    expect(toStringArray([1, 2])).toEqual(['1', '2']);
    expect(toStringArray(null)).toEqual([]);
  });

  it('textFor y findOption', () => {
    expect(textFor({ literal: 'Hola', byLang: { en: 'Hello' } }, 'en-GB')).toBe('Hello');
    expect(textFor({ literal: 'Hola' }, 'en')).toBe('Hola');
    expect(textFor(undefined, 'es')).toBe('');
    expect(findOption(OPTIONS, 'activo', 'es')?.value).toBe('A');
    expect(findOption(OPTIONS, 'Active', 'en')?.value).toBe('A');
    expect(findOption(OPTIONS, 'Active', 'es')?.value).toBeUndefined();
    expect(findOption(OPTIONS, '  ', 'es')).toBeUndefined();
    expect(findOption(undefined, 'A', 'es')).toBeUndefined();
    expect(findOption(OPTIONS, null, 'es')).toBeUndefined();
  });
});

beforeEach(() => {
  clearLookupCache();
});
