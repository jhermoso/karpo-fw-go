// Formatos con Intl: importes en cadena sin pérdida, fechas YYYY-MM-DD sin desfase de zona horaria.
import { describe, expect, it } from 'vitest';
import { createFormatters, formatDecimalString, intlLocale } from './formatters';

const nbsp = new RegExp(`[${String.fromCharCode(0xa0)}${String.fromCharCode(0x202f)}]`, 'g');
/** Normaliza los espacios duros de Intl para comparar con texto legible. */
const plain = (text: string) => text.replace(nbsp, ' ');

describe('números', () => {
  it('usa los separadores del idioma', () => {
    expect(plain(createFormatters('es').number(1234567.891))).toBe('1.234.567,891');
    expect(createFormatters('en').number(1234567.891)).toBe('1,234,567.891');
  });

  it('es no agrupa los números de 4 cifras (regla de Intl) y en sí', () => {
    expect(createFormatters('es').number(1234)).toBe('1234');
    expect(createFormatters('en').number(1234)).toBe('1,234');
  });

  it('acepta opciones de Intl', () => {
    expect(createFormatters('en').number(0.5, { style: 'percent' })).toBe('50%');
    expect(createFormatters('en').number(3, { minimumFractionDigits: 2 })).toBe('3.00');
  });
});

describe('importes en cadena sin pérdida de cifras', () => {
  const big = '1234567890123456789.12';

  it('no pasa por un double: 19 cifras enteras salen enteras', () => {
    expect(plain(createFormatters('es').money(big, 'EUR'))).toBe('1.234.567.890.123.456.789,12 €');
    expect(createFormatters('en').money(big, 'USD')).toBe('$1,234,567,890,123,456,789.12');
    // el mismo valor como number SÍ pierde cifras: por eso el valor viaja como cadena
    expect(Number(big).toFixed(2)).not.toBe(big);
  });

  it('respeta el signo y los céntimos', () => {
    expect(plain(createFormatters('es').money('-1234.50', 'EUR'))).toBe('-1234,50 €');
    expect(createFormatters('en').money('-1234.5', 'EUR')).toBe('-€1,234.50');
    expect(createFormatters('en').money('0.1', 'EUR')).toBe('€0.10');
  });

  it('un number sigue funcionando', () => {
    expect(createFormatters('en').money(1234.5, 'EUR')).toBe('€1,234.50');
  });

  it('una cadena que no es decimal se formatea como number', () => {
    expect(createFormatters('en').number('1e3')).toBe('1,000');
  });

  it('porcentaje: el valor ya es un porcentaje, no una fracción', () => {
    expect(plain(createFormatters('es').percent(12.5))).toBe('12,5 %');
    expect(createFormatters('en').percent('21.00')).toBe('21%');
  });
});

describe('formatDecimalString con cálculo manual (motores sin cadenas exactas)', () => {
  const manual = (value: string, locale: string, options: Intl.NumberFormatOptions = {}) =>
    plain(formatDecimalString(value, new Intl.NumberFormat(locale, options), { manual: true }));

  it('coincide con Intl en números normales', () => {
    expect(manual('1234567.891', 'en')).toBe('1,234,567.891');
    expect(manual('1234567.891', 'es')).toBe('1.234.567,891');
    expect(manual('1234', 'es')).toBe('1234');
    expect(manual('12345', 'es')).toBe('12.345');
    expect(manual('1234', 'en')).toBe('1,234');
  });

  it('no pierde cifras con 19 enteros', () => {
    expect(manual('1234567890123456789.12', 'en', { style: 'currency', currency: 'USD' })).toBe(
      '$1,234,567,890,123,456,789.12',
    );
    expect(manual('1234567890123456789.12', 'es', { style: 'currency', currency: 'EUR' })).toBe(
      '1.234.567.890.123.456.789,12 €',
    );
  });

  it('redondea al máximo de decimales con acarreo', () => {
    expect(manual('0.9995', 'en')).toBe('1');
    expect(manual('999.9995', 'en')).toBe('1,000');
    expect(manual('1.235', 'en', { maximumFractionDigits: 2 })).toBe('1.24');
    expect(manual('1.2349', 'en', { maximumFractionDigits: 2 })).toBe('1.23');
  });

  it('rellena hasta el mínimo y quita los ceros de más', () => {
    expect(manual('5', 'en', { minimumFractionDigits: 2 })).toBe('5.00');
    expect(manual('5.1000', 'en')).toBe('5.1');
    expect(manual('5.1', 'en', { style: 'currency', currency: 'EUR' })).toBe('€5.10');
  });

  it('negativos, y menos cero no sale con signo', () => {
    expect(manual('-1234.5', 'en')).toBe('-1,234.5');
    expect(manual('-0.0004', 'en', { maximumFractionDigits: 2 })).toBe('0');
    expect(manual('-12.5', 'en', { style: 'currency', currency: 'USD' })).toBe('-$12.50');
  });

  it('sin agrupación (useGrouping: false)', () => {
    expect(manual('1234567.5', 'en', { useGrouping: false })).toBe('1234567.5');
  });

  it('un texto que no es decimal pasa por Number', () => {
    expect(manual('abc', 'en')).toBe('NaN');
  });
});

describe('fechas', () => {
  it('YYYY-MM-DD se muestra como esa fecha de calendario, sea cual sea la zona horaria', () => {
    const f = createFormatters('es');
    expect(f.date('2026-11-27', { dateStyle: 'short' })).toBe('27/11/26');
    expect(createFormatters('en').date('2026-11-27', { dateStyle: 'short' })).toBe('11/27/26');
    expect(createFormatters('en').date('2026-01-01', { dateStyle: 'long' })).toBe('January 1, 2026');
    expect(f.date('2024-02-29', { dateStyle: 'medium' })).toContain('2024');
  });

  it('una fecha imposible o que no es fecha devuelve vacío en lugar de inventar otra', () => {
    expect(createFormatters('en').date('2026-02-30')).toBe('');
    expect(createFormatters('en').date('no es fecha')).toBe('');
    expect(createFormatters('en').dateTime('no es fecha')).toBe('');
  });

  it('un instante ISO con zona respeta el instante', () => {
    const out = createFormatters('en').dateTime('2026-11-27T12:00:00Z', { timeZone: 'UTC', timeStyle: 'short' });
    expect(plain(out)).toContain('12:00');
  });

  it('acepta Date y milisegundos', () => {
    const date = new Date(Date.UTC(2026, 10, 27, 12));
    expect(createFormatters('en').date(date, { timeZone: 'UTC', dateStyle: 'short' })).toBe('11/27/26');
    expect(createFormatters('en').date(date.getTime(), { timeZone: 'UTC', dateStyle: 'short' })).toBe('11/27/26');
  });

  it('dateTime de una fecha sin hora no inventa la hora', () => {
    expect(plain(createFormatters('en').dateTime('2026-11-27', { timeStyle: undefined, dateStyle: 'short' }))).toBe(
      '11/27/26',
    );
  });
});

describe('idioma', () => {
  it('el pseudoidioma xx y el vacío usan es', () => {
    expect(intlLocale('xx')).toBe('es');
    expect(intlLocale('')).toBe('es');
    expect(intlLocale('en')).toBe('en');
    expect(createFormatters('xx').locale).toBe('es');
  });
});

describe('cifras latinas en todos los idiomas (D-R38)', () => {
  const ARABIC_INDIC = /[٠-٩۰-۹]/;

  it('el árabe formatea con dígitos latinos: números, importes, porcentajes, rangos y fechas', () => {
    const ar = createFormatters('ar');
    const outputs = [
      ar.number(1234567.891),
      ar.money('1234.5', 'EUR'),
      ar.percent(12.5),
      ar.range(4, 12),
      ar.date('2026-11-27'),
      ar.dateTime(new Date(Date.UTC(2026, 10, 27, 15, 30))),
    ];
    for (const out of outputs) {
      expect(out, out).not.toMatch(ARABIC_INDIC);
      expect(out, out).toMatch(/\d/);
    }
  });

  it('también con ar-EG (cuyo sistema numérico por omisión no es latino) y en cadenas decimales largas', () => {
    expect(createFormatters('ar-EG').number(2026)).not.toMatch(ARABIC_INDIC);
    expect(createFormatters('ar').number('9007199254740993', { useGrouping: false })).toBe('9007199254740993');
    expect(formatDecimalString('1234567.5', new Intl.NumberFormat('ar', { numberingSystem: 'latn' }), { manual: true })).not.toMatch(
      ARABIC_INDIC,
    );
  });

  it('quien lo pide en las opciones obtiene otro sistema numérico', () => {
    expect(createFormatters('ar').number(123, { numberingSystem: 'arab' })).toMatch(ARABIC_INDIC);
  });

  it('no cambia es ni en', () => {
    expect(createFormatters('en').number(1234567.891)).toBe('1,234,567.891');
    expect(plain(createFormatters('es').number(1234567.891))).toBe('1.234.567,891');
  });
});
