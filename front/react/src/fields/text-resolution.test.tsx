// B-2 (b): las etiquetas de la configuración (`labelKey`) se traducen con el diccionario de la propia
// configuración, también dentro de los campos (insignias, selects, booleanos), y se pueden escribir o
// pegar traducidas. La prueba de la ronda 1 simulaba i18n y por eso no lo veía: aquí hay un diccionario
// real registrado con `registerConfigDictionary`, igual que hace el manifiesto.
import { render, renderHook, screen } from '@testing-library/react';
import { createInstance } from 'i18next';
import type { ReactElement } from 'react';
import { I18nextProvider } from 'react-i18next';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { clearConfigDictionaries, registerConfigDictionary } from '../config/text';
import type { FieldDef, OptionDef, Text } from '../contracts';
import enFields from '../locales/en/fields.json';
import enUi from '../locales/en/ui.json';
import esFields from '../locales/es/fields.json';
import esUi from '../locales/es/ui.json';
import { getCodec } from './codecs';
import { FieldView } from './FieldView';
import { useFieldText } from './hooks';
import { parsePaste, toTsv } from './paste';
import { findOption, optionLabel } from './values';

const filled: Text = { literal: 'Cubierto', key: 'entities.position.status.filled' };
const open: Text = { literal: 'Abierto', key: 'entities.position.status.open' };
const OPTIONS: OptionDef[] = [
  { value: 'FIL', label: filled, tone: 'success' },
  { value: 'OPN', label: open, tone: 'warning' },
];
const status: FieldDef = {
  field: 'status',
  label: { literal: 'Estado', key: 'entities.position.columns.status.header' },
  kind: 'badge',
  required: false,
  editable: true,
  options: OPTIONS,
};

function i18nFor(lng: 'es' | 'en') {
  const i18n = createInstance();
  void i18n.init({
    lng,
    fallbackLng: 'es',
    initAsync: false,
    defaultNS: 'ui',
    ns: ['ui', 'fields'],
    resources: { es: { ui: esUi, fields: esFields }, en: { ui: enUi, fields: enFields } },
    interpolation: { escapeValue: false },
  });
  return i18n;
}

function inLocale(ui: ReactElement, lng: 'es' | 'en') {
  return <I18nextProvider i18n={i18nFor(lng)}>{ui}</I18nextProvider>;
}

beforeEach(() => {
  registerConfigDictionary('es', {
    entities: { position: { status: { filled: 'Cubierto', open: 'Abierto' }, columns: { status: { header: 'Estado' } } } },
  });
  registerConfigDictionary('en', {
    entities: { position: { status: { filled: 'Filled', open: 'Open' }, columns: { status: { header: 'Status' } } } },
  });
});

afterEach(() => clearConfigDictionaries());

describe('useFieldText().resolve usa la misma cadena que useText()', () => {
  it('clave en el diccionario de la configuración en el idioma activo', () => {
    const en = renderHook(() => useFieldText(), { wrapper: ({ children }) => inLocale(<>{children}</>, 'en') });
    expect(en.result.current.resolve(filled)).toBe('Filled');
    expect(en.result.current.resolve(status.label)).toBe('Status');
    const es = renderHook(() => useFieldText(), { wrapper: ({ children }) => inLocale(<>{children}</>, 'es') });
    expect(es.result.current.resolve(filled)).toBe('Cubierto');
  });

  it('una clave de la interfaz sigue ganando al diccionario y sin clave sale el objeto por idioma o el literal', () => {
    const { result } = renderHook(() => useFieldText(), {
      wrapper: ({ children }) => inLocale(<>{children}</>, 'en'),
    });
    expect(result.current.resolve({ literal: 'x', key: 'fields:errors.required' })).toBe('This field is required.');
    expect(result.current.resolve({ literal: 'Activo', byLang: { en: 'Active' } })).toBe('Active');
    expect(result.current.resolve({ literal: 'Sin clave' })).toBe('Sin clave');
    expect(result.current.resolve({ literal: 'Clave rara', key: 'no.existe.en.ningun.sitio' })).toBe('Clave rara');
    expect(result.current.resolve(undefined)).toBe('');
  });
});

describe('BadgeViewer y compañía en inglés', () => {
  it('una insignia con labelKey entities.position.status.filled muestra «Filled» con lng=en', () => {
    render(inLocale(<FieldView value="FIL" def={status} />, 'en'));
    expect(screen.getByText('Filled')).toHaveAttribute('data-th', 'badge');
    expect(screen.queryByText('Cubierto')).toBeNull();
  });

  it('y «Cubierto» con lng=es', () => {
    render(inLocale(<FieldView value="FIL" def={status} />, 'es'));
    expect(screen.getByText('Cubierto')).toHaveAttribute('data-th', 'badge');
  });

  it('select, multiselect y booleano traducen sus etiquetas', () => {
    const select: FieldDef = { ...status, kind: 'select' };
    const multi: FieldDef = { ...status, kind: 'multiselect' };
    const flag: FieldDef = { ...status, kind: 'boolean', trueLabel: filled, falseLabel: open };
    render(
      inLocale(
        <>
          <FieldView value="OPN" def={select} />
          <FieldView value={['FIL', 'OPN']} def={multi} />
          <FieldView value={false} def={flag} />
        </>,
        'en',
      ),
    );
    expect(screen.getAllByText('Open')).toHaveLength(3); // select, una del multiselect y el booleano
    expect(screen.getByText('Filled')).toBeInTheDocument();
    expect(screen.queryByText('Abierto')).toBeNull();
  });
});

describe('los códecs usan el resolvedor opcional', () => {
  const resolve = (text: Text) => (text.key === filled.key ? 'Filled' : text.key === open.key ? 'Open' : (text.byLang?.en ?? 'literal'));

  it('format escribe la etiqueta traducida y sin resolvedor, el literal', () => {
    expect(getCodec('badge').format('FIL', status, 'en', resolve)).toBe('Filled');
    expect(getCodec('badge').format('FIL', status, 'en')).toBe('Cubierto');
    expect(getCodec('select').editText('OPN', status, 'en', resolve)).toBe('Open');
    expect(getCodec('multiselect').format(['FIL', 'OPN'], status, 'en', resolve)).toBe('Filled, Open');
    const flag: FieldDef = { ...status, kind: 'boolean', trueLabel: filled, falseLabel: open };
    expect(getCodec('boolean').format(true, flag, 'en', resolve)).toBe('Filled');
    expect(getCodec('boolean').format(false, flag, 'en')).toBe('Abierto');
  });

  it('parse reconoce la etiqueta traducida (y la del idioma de referencia)', () => {
    expect(getCodec('badge').parse('Filled', status, 'en', resolve)).toEqual({ ok: true, value: 'FIL' });
    expect(getCodec('badge').parse('  filled ', status, 'en', resolve)).toEqual({ ok: true, value: 'FIL' });
    expect(getCodec('badge').parse('  filled ', status, 'en')).toEqual({ ok: false, code: 'format' });
    expect(getCodec('badge').parse('Cubierto', status, 'en', resolve)).toEqual({ ok: true, value: 'FIL' });
    expect(getCodec('badge').parse('Filled', status, 'en')).toEqual({ ok: false, code: 'format' });
    expect(getCodec('multiselect').parse('Filled; opn', status, 'en', resolve)).toEqual({
      ok: true,
      value: ['FIL', 'OPN'],
    });
    const flag: FieldDef = { ...status, kind: 'boolean', trueLabel: filled, falseLabel: open };
    expect(getCodec('boolean').parse('Open', flag, 'en', resolve)).toEqual({ ok: true, value: false });
  });

  it('findOption encuentra la opción por su etiqueta inglesa y optionLabel la devuelve traducida', () => {
    expect(findOption(OPTIONS, 'Filled', 'en', resolve)?.value).toBe('FIL');
    expect(findOption(OPTIONS, 'fil', 'en', resolve)?.value).toBe('FIL');
    expect(findOption(OPTIONS, 'FILLED', 'en', resolve)?.value).toBe('FIL');
    expect(findOption(OPTIONS, 'Filled', 'en')).toBeUndefined();
    expect(optionLabel(OPTIONS[1] as OptionDef, 'en', resolve)).toBe('Open');
    expect(optionLabel(OPTIONS[1] as OptionDef, 'en')).toBe('Abierto');
  });

  it('pegar y copiar con el resolvedor: «Filled» entra y la copia sale traducida', () => {
    const pasted = parsePaste('Filled\nOpen\nNope', [status], 'en', resolve);
    expect(pasted.rows.map((row) => row[0]?.value)).toEqual(['FIL', 'OPN', undefined]);
    expect(pasted.errors).toBe(1);
    expect(toTsv([['FIL'], ['OPN']], [status], 'en', resolve)).toBe('Filled\nOpen');
  });
});
