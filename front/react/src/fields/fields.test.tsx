import { act, render, screen, waitFor, within } from '@testing-library/react';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import userEvent from '@testing-library/user-event';
import { createInstance } from 'i18next';
import { useState, type ReactElement } from 'react';
import { I18nextProvider } from 'react-i18next';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  ApiContext,
  type EntityApi,
  type FieldDef,
  type FieldKind,
  type OptionDef,
  type Row,
} from '../contracts';
import enFields from '../locales/en/fields.json';
import enUi from '../locales/en/ui.json';
import esFields from '../locales/es/fields.json';
import esUi from '../locales/es/ui.json';
import { FieldEditor } from './FieldEditor';
import { FieldView } from './FieldView';
import type { EditorProps } from './editor-props';
import { clearLookupCache, rememberLookupLabel } from './lookup-cache';
import { EMPTY_MARK, emptyText } from './values';
import { useFieldErrorMessage } from './hooks';
import { getFieldType } from './registry';

function def(kind: FieldKind, extra: Partial<FieldDef> = {}): FieldDef {
  return { field: 'f', label: { literal: 'Campo' }, kind, required: false, editable: true, ...extra };
}

const OPTIONS: OptionDef[] = [
  { value: 'A', label: { literal: 'Activo', byLang: { en: 'Active' } }, tone: 'success' },
  { value: 'I', label: { literal: 'Inactivo', byLang: { en: 'Inactive' } }, tone: 'warning', icon: 'lock' },
  { value: 'B', label: { literal: 'Baja' }, tone: 'accent' },
];

function withLocale(ui: ReactElement, lng: 'es' | 'en' = 'es'): ReactElement {
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
  return <I18nextProvider i18n={i18n}>{ui}</I18nextProvider>;
}

describe('FieldView', () => {
  describe('valor vacío (D-R35)', () => {
    /** La marca visible y el texto para lectores de pantalla de un visor vacío. */
    function mark(container: HTMLElement) {
      const empty = container.querySelector<HTMLElement>('[data-kx-empty="true"]');
      return {
        empty,
        visible: empty?.querySelector('[aria-hidden="true"]')?.textContent,
        spoken: empty?.querySelector('[data-kx-part="sr-only"]')?.textContent,
      };
    }

    it('un emptyValue hecho sólo de guiones se normaliza a la marca única «—»', () => {
      for (const dash of ['-', '–', '—', ' - ', '--']) {
        const { container, unmount } = render(withLocale(<FieldView value={null} def={def('text', { emptyValue: dash })} />, 'es'));
        expect(mark(container)).toMatchObject({ visible: '—', spoken: 'Sin valor' });
        unmount();
      }
    });

    it('sin emptyValue un valor vacío también pinta «—», en todos los tipos de visor', () => {
      for (const kind of ['text', 'number', 'money', 'date', 'datetime', 'boolean', 'select', 'badge', 'multiselect', 'lookup', 'email', 'url'] as const) {
        const { container, unmount } = render(withLocale(<FieldView value="" def={def(kind)} />, 'en'));
        expect(mark(container), kind).toMatchObject({ visible: '—', spoken: 'No value' });
        unmount();
      }
    });

    it('«—» es decorativo (aria-hidden) y el lector oye «Sin valor»', () => {
      render(withLocale(<FieldView value={null} def={def('text', { emptyValue: '-' })} />, 'es'));
      expect(screen.getByText('—')).toHaveAttribute('aria-hidden', 'true');
      expect(screen.getByText('Sin valor')).toHaveAttribute('data-kx-part', 'sr-only');
    });

    it('un emptyValue con texto («sin fecha», «·») se respeta y se lee tal cual', () => {
      const { container, rerender } = render(withLocale(<FieldView value={null} def={def('date', { emptyValue: 'sin fecha' })} />, 'es'));
      const empty = screen.getByText('sin fecha');
      expect(empty).toHaveAttribute('data-kx-empty', 'true');
      expect(empty).not.toHaveAttribute('aria-hidden');
      expect(container.querySelector('[data-kx-part="sr-only"]')).toBeNull();
      rerender(withLocale(<FieldView value={[]} def={def('multiselect', { emptyValue: '·' })} />, 'es'));
      expect(screen.getByText('·')).toHaveAttribute('data-kx-empty', 'true');
    });

    it('emptyText y EMPTY_MARK', () => {
      expect(EMPTY_MARK).toBe('—');
      expect(emptyText(undefined)).toBe('—');
      expect(emptyText('')).toBe('—');
      expect(emptyText('  ')).toBe('—');
      for (const dash of ['-', '–', '—', '‒', '−', '- -']) expect(emptyText(dash), dash).toBe('—');
      expect(emptyText('sin fecha')).toBe('sin fecha');
      expect(emptyText('n/d')).toBe('n/d');
      expect(emptyText('-1')).toBe('-1');
    });
  });

  it('los códigos llevan data-th-format=code', () => {
    render(<FieldView value="FAC-001" def={def('text', { format: 'code' })} />);
    expect(screen.getByText('FAC-001')).toHaveAttribute('data-th-format', 'code');
  });

  it('los importes se formatean por idioma, sin perder la cadena y sin gancho `amount` (D-R35)', () => {
    const money = def('money', { format: 'EUR' });
    const { unmount } = render(withLocale(<FieldView value="1200.10" def={money} />, 'es'));
    const amount = screen.getByText(/1\.200,10/);
    expect(amount).not.toHaveAttribute('data-th-format');
    expect(amount.textContent).toBe('1.200,10\u00a0€');
    unmount();
    render(withLocale(<FieldView value="1200.10" def={def('money', { format: 'USD' })} />, 'en'));
    expect(screen.getByText('$1,200.10')).not.toHaveAttribute('data-th-format');
  });

  it('ningún visor numérico emite data-th-format=amount: se alinean al final y se estilan en fields.css', () => {
    const { container } = render(
      <>
        <FieldView value={1234567.5} def={def('number')} />
        <FieldView value="12.5" def={def('percentage')} />
        <FieldView value="3.14159" def={def('decimal')} />
        <FieldView value="9.99" def={def('money', { format: 'EUR' })} />
      </>,
    );
    expect(screen.getByText('1.234.567,5')).toHaveAttribute('data-kx-align', 'end');
    expect(screen.getByText(/12,5/)).toHaveAttribute('data-kx-align', 'end');
    expect(container.querySelectorAll('[data-kx-align="end"]')).toHaveLength(4);
    expect(container.querySelector('[data-th-format="amount"]')).toBeNull();
  });

  it('fields.css da a las cifras tabular-nums, alineación al final y sin salto de línea, sin cambiar de familia', () => {
    const css = readFileSync(resolve(import.meta.dirname, 'fields.css'), 'utf8');
    const rule = /\[data-kx-ui="value"\]\[data-kx-align="end"\]\s*\{([^}]*)\}/.exec(css)?.[1] ?? '';
    expect(rule).toMatch(/font-variant-numeric:\s*tabular-nums/);
    expect(rule).toMatch(/text-align:\s*end/);
    expect(rule).toMatch(/white-space:\s*nowrap/);
    expect(rule).not.toMatch(/font-family/);
  });

  it('el badge lleva data-th=badge y el tono de la opción (accent se pinta como info)', () => {
    const { rerender } = render(withLocale(<FieldView value="A" def={def('badge', { options: OPTIONS })} />, 'en'));
    const badge = screen.getByText('Active');
    expect(badge).toHaveAttribute('data-th', 'badge');
    expect(badge).toHaveAttribute('data-th-tone', 'success');
    rerender(withLocale(<FieldView value="I" def={def('badge', { options: OPTIONS })} />, 'es'));
    const locked = screen.getByText('Inactivo');
    expect(locked).toHaveAttribute('data-th-tone', 'warning');
    expect(locked.querySelector('svg[data-th-icon="lock"]')).not.toBeNull();
    rerender(withLocale(<FieldView value="B" def={def('badge', { options: OPTIONS })} />, 'es'));
    const accent = screen.getByText('Baja');
    expect(accent).toHaveAttribute('data-th-tone', 'info');
    expect(accent).toHaveAttribute('data-kx-tone', 'accent');
    rerender(withLocale(<FieldView value="ZZ" def={def('badge', { options: OPTIONS })} />, 'es'));
    expect(screen.getByText('ZZ')).toHaveAttribute('data-th-tone', 'neutral');
  });

  it('select muestra la etiqueta; multiselect, una insignia por valor', () => {
    const { rerender } = render(withLocale(<FieldView value="A" def={def('select', { options: OPTIONS })} />, 'en'));
    expect(screen.getByText('Active')).toBeInTheDocument();
    rerender(withLocale(<FieldView value={['A', 'I', 'Q']} def={def('multiselect', { options: OPTIONS })} />, 'es'));
    expect(screen.getByText('Activo')).toHaveAttribute('data-th', 'badge');
    expect(screen.getByText('Inactivo')).toBeInTheDocument();
    expect(screen.getByText('Q')).toBeInTheDocument();
    rerender(withLocale(<FieldView value={[]} def={def('multiselect', { emptyValue: '·' })} />, 'es'));
    expect(screen.getByText('·')).toBeInTheDocument();
  });

  it('fecha y fecha con hora con <time> y el formato del idioma', () => {
    const { rerender } = render(withLocale(<FieldView value="2025-03-14" def={def('date')} />, 'es'));
    const time = screen.getByText('14/03/2025');
    expect(time.tagName).toBe('TIME');
    expect(time).toHaveAttribute('datetime', '2025-03-14');
    rerender(withLocale(<FieldView value="2025-03-14" def={def('date')} />, 'en'));
    expect(screen.getByText('03/14/2025')).toBeInTheDocument();
    rerender(withLocale(<FieldView value="2025-03-14T10:30:00" def={def('datetime')} />, 'en'));
    expect(screen.getByText('03/14/2025 10:30 AM').tagName).toBe('TIME');
    rerender(withLocale(<FieldView value="no es fecha" def={def('datetime')} />, 'en'));
    expect(screen.getByText('no es fecha')).not.toHaveAttribute('datetime');
  });

  it('booleano con icono y etiqueta (no sólo color)', () => {
    const { rerender } = render(withLocale(<FieldView value="true" def={def('boolean')} />, 'es'));
    const yes = screen.getByText('Sí');
    expect(yes).toHaveAttribute('data-kx-value', 'true');
    expect(yes.querySelector('svg[data-th-icon="check"]')).not.toBeNull();
    rerender(withLocale(<FieldView value={false} def={def('boolean')} />, 'en'));
    expect(screen.getByText('No')).toHaveAttribute('data-kx-value', 'false');
    rerender(withLocale(<FieldView value={null} def={def('boolean', { emptyValue: '-' })} />, 'en'));
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('email, teléfono y URL son enlaces; una URL insegura no', () => {
    render(
      <>
        <FieldView value="a@b.com" def={def('email')} />
        <FieldView value="+34 600 123 456" def={def('phone')} />
        <FieldView value="https://karpo.example/x" def={def('url')} />
        <FieldView value="javascript:alert(1)" def={def('url')} />
        <FieldView value="" def={def('url', { emptyValue: '-' })} />
      </>,
    );
    expect(screen.getByRole('link', { name: 'a@b.com' })).toHaveAttribute('href', 'mailto:a@b.com');
    expect(screen.getByRole('link', { name: '+34 600 123 456' })).toHaveAttribute('href', 'tel:+34600123456');
    const external = screen.getByRole('link', { name: 'https://karpo.example/x' });
    expect(external).toHaveAttribute('target', '_blank');
    expect(external).toHaveAttribute('rel', 'noopener noreferrer');
    expect(screen.getAllByRole('link')).toHaveLength(3);
    expect(screen.getByText('javascript:alert(1)').tagName).toBe('SPAN');
  });

  describe('lookup', () => {
    beforeEach(() => clearLookupCache());
    const lookup = { entity: 'company', valueField: 'id', displayField: 'name' };

    it('muestra la etiqueta resuelta con EntityApi.get y la recuerda', async () => {
      const api = {
        get: vi.fn().mockResolvedValue({ id: 'c-1', name: 'Acme' }),
      } as unknown as EntityApi;
      render(
        <ApiContext.Provider value={api}>
          <FieldView value="c-1" def={def('lookup', { lookup })} />
        </ApiContext.Provider>,
      );
      expect(screen.getByText('', { selector: '[aria-busy="true"]' })).toBeInTheDocument();
      expect(await screen.findByText('Acme')).toHaveAttribute('data-th', 'reference');
      expect(api.get).toHaveBeenCalledTimes(1);
    });

    it('sin proveedor de API (o con la etiqueta en caché) no pide nada', () => {
      render(<FieldView value="c-7" def={def('lookup', { lookup })} />);
      expect(screen.getByText('c-7')).toBeInTheDocument();
      rememberLookupLabel(lookup, 'c-8', 'Beta');
      render(<FieldView value="c-8" def={def('lookup', { lookup })} />);
      expect(screen.getByText('Beta')).toBeInTheDocument();
    });

    it('si la petición falla, enseña el id', async () => {
      const api = { get: vi.fn().mockRejectedValue(new Error('x')) } as unknown as EntityApi;
      render(
        <ApiContext.Provider value={api}>
          <FieldView value="c-9" def={def('lookup', { lookup })} />
        </ApiContext.Provider>,
      );
      expect(await screen.findByText('c-9')).toBeInTheDocument();
    });
  });
});

/** Editor controlado que anota lo que ocurre. */
function Harness({
  initial = null,
  field,
  variant,
  onEvent,
  id,
}: {
  initial?: unknown;
  field: FieldDef;
  variant?: EditorProps['variant'];
  onEvent: (event: unknown[]) => void;
  id?: string;
}) {
  const [value, setValue] = useState<unknown>(initial);
  return (
    <FieldEditor
      value={value}
      def={field}
      variant={variant}
      id={id}
      autoFocus
      onChange={(next) => {
        onEvent(['change', next]);
        setValue(next);
      }}
      onCommit={(move) => onEvent(['commit', move])}
      onCancel={() => onEvent(['cancel'])}
    />
  );
}

describe('FieldEditor: teclado y valores', () => {
  it('Enter confirma hacia abajo, Shift+Enter hacia arriba, Tab y Shift+Tab, Esc cancela', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(<Harness field={def('text')} onEvent={(e) => events.push(e)} />);
    const input = screen.getByRole('textbox', { name: 'Campo' });
    expect(input).toHaveFocus();
    await user.keyboard('hola{Enter}{Shift>}{Enter}{/Shift}{Tab}{Shift>}{Tab}{/Shift}{Escape}');
    expect(events.filter((e) => e[0] !== 'change')).toEqual([
      ['commit', 'down'],
      ['commit', 'up'],
      ['commit', 'next'],
      ['commit', 'prev'],
      ['cancel'],
    ]);
    expect(events.filter((e) => e[0] === 'change').at(-1)).toEqual(['change', 'hola']);
  });

  it('en el formulario Tab no se intercepta y Enter no se bloquea', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(
      <>
        <Harness field={def('text')} variant="form" onEvent={(e) => events.push(e)} />
        <button type="button">siguiente</button>
      </>,
    );
    await user.keyboard('{Tab}');
    expect(screen.getByRole('button', { name: 'siguiente' })).toHaveFocus();
    expect(events).toEqual([]);
  });

  it('expone aria-invalid, aria-describedby y aria-required', () => {
    render(
      <FieldEditor
        value=""
        def={def('text', { required: true })}
        invalid
        describedBy="err-1"
        onChange={() => undefined}
        onCommit={() => undefined}
        onCancel={() => undefined}
      />,
    );
    const input = screen.getByRole('textbox', { name: 'Campo' });
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input).toHaveAttribute('aria-describedby', 'err-1');
    expect(input).toHaveAttribute('aria-required', 'true');
  });

  it('un id externo sustituye al aria-label (la etiqueta del Field lo nombra)', () => {
    render(
      <>
        <label htmlFor="mi-campo">Importe</label>
        <FieldEditor
          id="mi-campo"
          value=""
          def={def('money')}
          onChange={() => undefined}
          onCommit={() => undefined}
          onCancel={() => undefined}
        />
      </>,
    );
    const input = screen.getByRole('textbox', { name: 'Importe' });
    expect(input).not.toHaveAttribute('aria-label');
  });

  it('número en es: escribe 1.234,5 y sale 1234.5 como número', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(withLocale(<Harness field={def('number')} onEvent={(e) => events.push(e)} />, 'es'));
    await user.type(screen.getByRole('textbox'), '1.234,5');
    expect(events.at(-1)).toEqual(['change', 1234.5]);
  });

  it('importe en es y en: sale la cadena decimal, sin float', async () => {
    const user = userEvent.setup();
    const esEvents: unknown[][] = [];
    const { unmount } = render(withLocale(<Harness field={def('money')} onEvent={(e) => esEvents.push(e)} />, 'es'));
    await user.type(screen.getByRole('textbox'), '1.200,10');
    expect(esEvents.at(-1)).toEqual(['change', '1200.10']);
    unmount();
    const enEvents: unknown[][] = [];
    render(withLocale(<Harness field={def('money')} onEvent={(e) => enEvents.push(e)} />, 'en'));
    await user.type(screen.getByRole('textbox'), '1,200.10');
    expect(enEvents.at(-1)).toEqual(['change', '1200.10']);
  });

  it('el valor inicial se edita sin separador de miles y con la coma del idioma', () => {
    render(withLocale(<Harness initial="1200.10" field={def('money')} onEvent={() => undefined} />, 'es'));
    expect(screen.getByRole('textbox')).toHaveValue('1200,10');
  });

  it('un texto que no se entiende sale tal cual y validate lo marca format', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    const field = def('date');
    render(withLocale(<Harness field={field} onEvent={(e) => events.push(e)} />, 'es'));
    await user.type(screen.getByRole('textbox'), 'foo');
    const last = events.at(-1);
    expect(last).toEqual(['change', 'foo']);
    expect(getFieldType('date').validate(last?.[1], field)).toEqual(['format']);
    await user.clear(screen.getByRole('textbox'));
    await user.type(screen.getByRole('textbox'), '14/03/2025');
    expect(events.at(-1)).toEqual(['change', '2025-03-14']);
  });

  it('relee el valor si el padre lo cambia por su cuenta (deshacer)', () => {
    const field = def('number');
    const noop = () => undefined;
    const { rerender } = render(
      <FieldEditor value={5} def={field} onChange={noop} onCommit={noop} onCancel={noop} />,
    );
    expect(screen.getByRole('textbox')).toHaveValue('5');
    rerender(<FieldEditor value={9} def={field} onChange={noop} onCommit={noop} onCancel={noop} />);
    expect(screen.getByRole('textbox')).toHaveValue('9');
  });

  it('área de texto: Enter escribe un salto y Ctrl+Enter confirma', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(<Harness field={def('textarea')} onEvent={(e) => events.push(e)} />);
    await user.keyboard('a{Enter}b');
    expect(events.at(-1)).toEqual(['change', 'a\nb']);
    expect(events.some((e) => e[0] === 'commit')).toBe(false);
    await user.keyboard('{Control>}{Enter}{/Control}');
    expect(events.at(-1)).toEqual(['commit', 'down']);
  });

  it('no edita nada si el campo es de sólo lectura', () => {
    render(
      <FieldEditor
        value="x"
        def={def('text', { editable: false })}
        onChange={() => undefined}
        onCommit={() => undefined}
        onCancel={() => undefined}
      />,
    );
    expect(screen.getByRole('textbox')).toHaveAttribute('readonly');
  });

  it('casilla: Espacio alterna, Enter confirma y tiene nombre accesible', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(<Harness field={def('boolean')} onEvent={(e) => events.push(e)} />);
    const box = screen.getByRole('checkbox', { name: 'Campo' });
    expect(box).toHaveFocus();
    expect(box).not.toBeChecked();
    await user.keyboard(' ');
    expect(events.at(-1)).toEqual(['change', true]);
    expect(box).toBeChecked();
    await user.keyboard('{Enter}');
    expect(events.at(-1)).toEqual(['commit', 'down']);
    await user.click(box);
    expect(events.at(-1)).toEqual(['change', false]);
  });

  it('select nativo: elige por etiqueta, vacío da null y conserva un valor huérfano', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(withLocale(<Harness initial="ZZ" field={def('select', { options: OPTIONS })} onEvent={(e) => events.push(e)} />));
    const select = screen.getByRole('combobox', { name: 'Campo' });
    expect(select).toHaveValue('ZZ');
    await user.selectOptions(select, 'Inactivo');
    expect(events.at(-1)).toEqual(['change', 'I']);
    await user.selectOptions(select, '—');
    expect(events.at(-1)).toEqual(['change', null]);
    await user.selectOptions(select, 'Activo');
    await user.keyboard('{Enter}');
    expect(events.at(-1)).toEqual(['commit', 'down']);
  });

  it('badge se edita como un select', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(withLocale(<Harness initial="activo" field={def('badge', { options: OPTIONS })} onEvent={(e) => events.push(e)} />));
    expect(screen.getByRole('combobox')).toHaveValue('A');
    await user.selectOptions(screen.getByRole('combobox'), 'Baja');
    expect(events.at(-1)).toEqual(['change', 'B']);
  });
});

describe('FieldEditor: multiselect', () => {
  it('abre un panel de casillas y devuelve la lista en el orden de las opciones', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(withLocale(<Harness initial={['I']} field={def('multiselect', { options: OPTIONS })} onEvent={(e) => events.push(e)} />));
    const trigger = screen.getByRole('button', { name: /Inactivo/ });
    expect(trigger).toHaveAttribute('aria-haspopup', 'dialog');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    await user.click(trigger);
    const panel = await screen.findByRole('dialog', { name: 'Opciones de Campo' });
    const first = within(panel).getByRole('checkbox', { name: 'Activo' });
    await user.click(first);
    expect(events.at(-1)).toEqual(['change', ['A', 'I']]);
    await user.click(within(panel).getByRole('checkbox', { name: 'Inactivo' }));
    expect(events.at(-1)).toEqual(['change', ['A']]);
  });

  it('Esc cierra el panel sin cancelar la edición; Enter en el botón confirma', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(withLocale(<Harness field={def('multiselect', { options: OPTIONS })} onEvent={(e) => events.push(e)} />));
    const trigger = screen.getByRole('button', { name: 'Elige opciones' });
    expect(trigger).toHaveFocus();
    await user.keyboard('{ArrowDown}');
    await screen.findByRole('dialog');
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(events.some((e) => e[0] === 'cancel')).toBe(false);
    trigger.focus();
    await user.keyboard('{Enter}');
    expect(events.at(-1)).toEqual(['commit', 'down']);
    expect(screen.queryByRole('dialog')).toBeNull();
    await user.keyboard('{Escape}');
    expect(events.at(-1)).toEqual(['cancel']);
  });

  it('Tab dentro del panel lo cierra y confirma hacia el siguiente; Enter sólo lo cierra', async () => {
    const user = userEvent.setup();
    const events: unknown[][] = [];
    render(withLocale(<Harness field={def('multiselect', { options: OPTIONS })} onEvent={(e) => events.push(e)} />));
    await user.click(screen.getByRole('button'));
    const panel = await screen.findByRole('dialog');
    within(panel).getByRole('checkbox', { name: 'Activo' }).focus();
    await user.keyboard('{Enter}');
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(events.some((e) => e[0] === 'commit')).toBe(false);
    await user.click(screen.getByRole('button'));
    const second = await screen.findByRole('dialog');
    within(second).getByRole('checkbox', { name: 'Baja' }).focus();
    await user.keyboard('{Tab}');
    expect(events.at(-1)).toEqual(['commit', 'next']);
    await user.click(screen.getByRole('button'));
    within(await screen.findByRole('dialog')).getByRole('checkbox', { name: 'Baja' }).focus();
    await user.keyboard('{Shift>}{Tab}{/Shift}');
    expect(events.at(-1)).toEqual(['commit', 'prev']);
  });
});

describe('FieldEditor: lookup', () => {
  const lookup = { entity: 'company', valueField: 'id', displayField: 'name', filter: { active: true } };
  const companies: Row[] = [
    { id: 'c-1', name: 'Acme' },
    { id: 'c-2', name: 'Acme Logística' },
    { id: 'c-3', name: 'Beta' },
  ];

  beforeEach(() => clearLookupCache());

  function fakeApi(): EntityApi & { list: ReturnType<typeof vi.fn>; get: ReturnType<typeof vi.fn> } {
    return {
      list: vi.fn(async (_cfg: unknown, query: { filter: Record<string, unknown> }) => {
        const search = String(query.filter.search ?? '').toLowerCase();
        const items = companies.filter((c) => String(c.name).toLowerCase().includes(search));
        return { items, total: items.length, page: 1, pageSize: 20 };
      }),
      get: vi.fn(async (_cfg: unknown, id: string) => companies.find((c) => c.id === id) as Row),
    } as unknown as EntityApi & { list: ReturnType<typeof vi.fn>; get: ReturnType<typeof vi.fn> };
  }

  it('busca con EntityApi.list y el filtro search, muestra displayField y devuelve el valueField', async () => {
    const user = userEvent.setup();
    const api = fakeApi();
    const events: unknown[][] = [];
    render(
      withLocale(
        <ApiContext.Provider value={api}>
          <Harness field={def('lookup', { lookup, label: { literal: 'Empresa' } })} onEvent={(e) => events.push(e)} />
        </ApiContext.Provider>,
      ),
    );
    const input = screen.getByRole('combobox', { name: 'Empresa' });
    expect(input).toHaveFocus();
    // Al enfocar se pide la primera página sin búsqueda.
    await screen.findByRole('option', { name: 'Beta' });
    expect(api.list.mock.calls[0]?.[1]).toMatchObject({
      page: 1,
      pageSize: 20,
      filter: { active: true },
    });
    expect(api.list.mock.calls[0]?.[1].filter).not.toHaveProperty('search');

    await user.type(input, 'acme');
    await waitFor(() => expect(screen.queryByRole('option', { name: 'Beta' })).toBeNull());
    const last = api.list.mock.calls.at(-1)?.[1] as { filter: Record<string, unknown> };
    expect(last.filter).toEqual({ active: true, search: 'acme' });
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Acme', 'Acme Logística']);

    await user.click(screen.getByRole('option', { name: 'Acme Logística' }));
    expect(events.at(-1)).toEqual(['change', 'c-2']);
    await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
    // Una vez elegida, el campo enseña la etiqueta, no el id.
    expect(screen.getByRole('combobox', { name: 'Empresa' })).toHaveValue('Acme Logística');
  });

  it('muestra la etiqueta resuelta de un valor existente (EntityApi.get)', async () => {
    const api = fakeApi();
    render(
      withLocale(
        <ApiContext.Provider value={api}>
          <Harness initial="c-3" field={def('lookup', { lookup })} onEvent={() => undefined} />
        </ApiContext.Provider>,
      ),
    );
    await waitFor(() => expect(screen.getByRole('combobox')).toHaveValue('Beta'));
    expect(api.get).toHaveBeenCalledTimes(1);
  });

  it('teclado: con búsqueda, flecha y Enter eligen; sin búsqueda, Enter confirma; Esc cierra y luego cancela', async () => {
    const user = userEvent.setup();
    const api = fakeApi();
    const events: unknown[][] = [];
    render(
      withLocale(
        <ApiContext.Provider value={api}>
          <Harness field={def('lookup', { lookup })} onEvent={(e) => events.push(e)} />
        </ApiContext.Provider>,
      ),
    );
    const input = screen.getByRole('combobox');
    await screen.findByRole('option', { name: 'Beta' });
    expect(input).toHaveAttribute('aria-expanded', 'true');
    expect(input).toHaveAttribute('aria-controls');

    // Sin escribir ni recorrer, Enter confirma el valor actual.
    await user.keyboard('{Enter}');
    expect(events.at(-1)).toEqual(['commit', 'down']);

    await user.keyboard('be');
    await waitFor(() => expect(screen.getAllByRole('option')).toHaveLength(1));
    await user.keyboard('{ArrowDown}{Enter}');
    expect(events.at(-1)).toEqual(['change', 'c-3']);

    // Esc con la lista abierta la cierra; con la lista cerrada cancela.
    await user.keyboard('x');
    await screen.findByRole('listbox');
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
    expect(events.some((e) => e[0] === 'cancel')).toBe(false);
    expect(input).toHaveAttribute('aria-expanded', 'false');
    await user.keyboard('{Escape}');
    expect(events.at(-1)).toEqual(['cancel']);
    await user.keyboard('{Tab}');
    expect(events.at(-1)).toEqual(['commit', 'next']);
  });

  it('ArrowDown abre una lista cerrada y borrar el texto quita la selección', async () => {
    const user = userEvent.setup();
    const api = fakeApi();
    const events: unknown[][] = [];
    render(
      withLocale(
        <ApiContext.Provider value={api}>
          <Harness initial="c-1" field={def('lookup', { lookup })} onEvent={(e) => events.push(e)} />
        </ApiContext.Provider>,
      ),
    );
    const input = screen.getByRole('combobox');
    await waitFor(() => expect(input).toHaveValue('Acme'));
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
    await user.keyboard('{ArrowDown}');
    await screen.findByRole('listbox');
    await user.clear(input);
    expect(events.at(-1)).toEqual(['change', null]);
  });

  it('avisa si la búsqueda falla y si no hay resultados', async () => {
    const user = userEvent.setup();
    const api = fakeApi();
    const events: unknown[][] = [];
    render(
      withLocale(
        <ApiContext.Provider value={api}>
          <Harness field={def('lookup', { lookup })} onEvent={(e) => events.push(e)} />
        </ApiContext.Provider>,
      ),
    );
    await screen.findByRole('option', { name: 'Beta' });
    await user.keyboard('zzzz');
    expect(await screen.findByText('Sin resultados')).toBeInTheDocument();
    api.list.mockRejectedValueOnce(new Error('caída'));
    await user.keyboard('q');
    expect(await screen.findByRole('alert')).toHaveTextContent('No se pudo buscar');
  });

  it('sin proveedor de API no falla: lista vacía', async () => {
    render(withLocale(<Harness field={def('lookup', { lookup })} onEvent={() => undefined} />));
    const input = screen.getByRole('combobox');
    await act(async () => {
      input.focus();
    });
    expect(input).toHaveAttribute('aria-expanded', 'true');
    expect(screen.queryAllByRole('option')).toHaveLength(0);
  });
});

describe('mensajes de validación', () => {
  function Message({ code, field }: { code: string; field: FieldDef }) {
    const message = useFieldErrorMessage();
    return <p>{message(code, field)}</p>;
  }

  it('traduce cada código con los límites de la definición, en es y en', () => {
    const field = def('text', { maxLength: 10, minLength: 3, min: 1, max: 9 });
    const { rerender } = render(withLocale(<Message code="maxLength" field={field} />, 'es'));
    expect(screen.getByText('Máximo 10 caracteres.')).toBeInTheDocument();
    rerender(withLocale(<Message code="minLength" field={field} />, 'en'));
    expect(screen.getByText('At least 3 characters.')).toBeInTheDocument();
    rerender(withLocale(<Message code="max" field={def('number', { max: 9 })} />, 'es'));
    expect(screen.getByText('Debe ser como máximo 9.')).toBeInTheDocument();
    rerender(withLocale(<Message code="required" field={field} />, 'en'));
    expect(screen.getByText('This field is required.')).toBeInTheDocument();
  });

  it('sin i18next de la app usa los textos empaquetados', () => {
    render(<Message code="format" field={def('text')} />);
    expect(screen.getByText('El valor no tiene un formato válido.')).toBeInTheDocument();
  });
});
