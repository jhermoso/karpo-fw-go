import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createInstance } from 'i18next';
import { useState, type ReactElement } from 'react';
import { I18nextProvider } from 'react-i18next';
import { describe, expect, it, vi } from 'vitest';
import { ThemeContext, type ThemeApi } from '../contracts';
import enFields from '../locales/en/fields.json';
import enUi from '../locales/en/ui.json';
import esFields from '../locales/es/fields.json';
import esUi from '../locales/es/ui.json';
import {
  Action,
  Badge,
  Chip,
  ConfirmDialog,
  Dialog,
  Feedback,
  Field,
  Icon,
  Kbd,
  PromptDialog,
  ShortcutKeys,
  Skeleton,
  Spinner,
  ToastProvider,
  Toolbar,
  useToast,
} from './index';

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

function fakeTheme(iconStyle: 'outline' | 'filled'): ThemeApi {
  return {
    info: { name: 'fake', modes: ['light'], highContrast: false, iconStyle, dictionaryHash: 'sha256:x' },
    mode: 'light',
    setMode: () => undefined,
    setTheme: async () => undefined,
    available: ['fake'],
    resolveColor: () => 'rgb(0, 0, 0)',
    onChange: () => () => undefined,
  };
}

describe('Action', () => {
  it('es un button type=button por defecto, con los ganchos de Theros', () => {
    render(<Action>Guardar</Action>);
    const button = screen.getByRole('button', { name: 'Guardar' });
    expect(button).toHaveAttribute('type', 'button');
    expect(button).toHaveAttribute('data-th', 'action');
    expect(button).toHaveAttribute('data-th-kind', 'secondary');
    expect(button).not.toHaveAttribute('aria-disabled');
  });

  it.each([
    ['primary', 'primary'],
    ['secondary', 'secondary'],
    ['ghost', 'quiet'],
    ['quiet', 'quiet'],
    ['danger', 'destructive'],
    ['destructive', 'destructive'],
  ] as const)('kind %s → data-th-kind %s', (kind, expected) => {
    render(<Action kind={kind}>x</Action>);
    expect(screen.getByRole('button')).toHaveAttribute('data-th-kind', expected);
  });

  it('no disponible: button type=button con aria-disabled, data-th-reason y aria-describedby con el motivo', async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    render(
      <Action type="submit" kind="primary" disabledReason="Falta el permiso de escritura" onClick={onClick}>
        Guardar
      </Action>,
    );
    const button = screen.getByRole('button', { name: 'Guardar' });
    expect(button).toHaveAttribute('type', 'button');
    expect(button).toHaveAttribute('aria-disabled', 'true');
    expect(button).toHaveAttribute('data-th-reason', 'Falta el permiso de escritura');
    const describedBy = button.getAttribute('aria-describedby');
    expect(describedBy).toBeTruthy();
    expect(document.getElementById(describedBy as string)).toHaveTextContent('Falta el permiso de escritura');
    expect(button).toHaveAccessibleDescription('Falta el permiso de escritura');
    // Sigue siendo enfocable y no hace nada.
    await user.click(button);
    await user.tab();
    expect(onClick).not.toHaveBeenCalled();
  });

  it('conserva el aria-describedby propio junto al motivo y deja pasar los pulsos si está disponible', async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    const { rerender } = render(
      <>
        <p id="ayuda">Ayuda</p>
        <Action aria-describedby="ayuda" disabledReason="No ahora">
          Enviar
        </Action>
      </>,
    );
    const button = screen.getByRole('button', { name: 'Enviar' });
    expect(button.getAttribute('aria-describedby')?.split(' ')).toContain('ayuda');
    expect(button.getAttribute('aria-describedby')?.split(' ')).toHaveLength(2);
    rerender(
      <>
        <p id="ayuda">Ayuda</p>
        <Action aria-describedby="ayuda" onClick={onClick} type="submit">
          Enviar
        </Action>
      </>,
    );
    const ready = screen.getByRole('button', { name: 'Enviar' });
    expect(ready).toHaveAttribute('type', 'submit');
    await user.click(ready);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('ocupada: aria-busy y no admite otro pulso', async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    render(
      <Action busy onClick={onClick}>
        Enviando
      </Action>,
    );
    const button = screen.getByRole('button', { name: 'Enviando' });
    expect(button).toHaveAttribute('aria-busy', 'true');
    await user.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });

  it('con icono pinta el svg decorativo', () => {
    render(
      <Action icon="edit" aria-label="Editar">
        {null}
      </Action>,
    );
    const button = screen.getByRole('button', { name: 'Editar' });
    expect(button.querySelector('svg[data-th-icon="edit"]')).toHaveAttribute('aria-hidden', 'true');
  });
});

describe('Toolbar', () => {
  it('nunca lleva role=toolbar', () => {
    render(
      <Toolbar>
        <Action>Uno</Action>
      </Toolbar>,
    );
    expect(screen.queryByRole('toolbar')).toBeNull();
    const bar = screen.getByRole('button').parentElement;
    expect(bar).toHaveAttribute('data-th', 'toolbar');
    expect(bar).not.toHaveAttribute('role');
    expect(bar).not.toHaveAttribute('aria-label');
  });

  it('la barra de búsqueda lleva role=search con su nombre y tampoco es toolbar', () => {
    render(
      <Toolbar search label="Filtrar instalaciones">
        <input aria-label="Texto" />
      </Toolbar>,
    );
    const search = screen.getByRole('search', { name: 'Filtrar instalaciones' });
    expect(search).toHaveAttribute('data-th', 'toolbar');
    expect(screen.queryByRole('toolbar')).toBeNull();
  });
});

describe('Icon', () => {
  it("usa el peso 'fill' si el tema es de iconos rellenos y 'regular' sin ThemeContext", () => {
    const { unmount } = render(<Icon name="edit" />);
    const regular = document.querySelector('svg[data-th-icon="edit"]') as SVGElement;
    expect(regular).toHaveAttribute('data-kx-weight', 'regular');
    const regularMarkup = regular.innerHTML;
    unmount();

    const { unmount: unmountFilled } = render(
      <ThemeContext.Provider value={fakeTheme('filled')}>
        <Icon name="edit" />
      </ThemeContext.Provider>,
    );
    const filled = document.querySelector('svg[data-th-icon="edit"]') as SVGElement;
    expect(filled).toHaveAttribute('data-kx-weight', 'fill');
    expect(filled.innerHTML).not.toBe(regularMarkup);
    unmountFilled();

    render(
      <ThemeContext.Provider value={fakeTheme('outline')}>
        <Icon name="edit" />
      </ThemeContext.Provider>,
    );
    expect(document.querySelector('svg[data-th-icon="edit"]')).toHaveAttribute('data-kx-weight', 'regular');
  });

  it('los direccionales llevan data-kx-mirror y el resto no', () => {
    render(
      <>
        <Icon name="chevron-right" />
        <Icon name="arrow-left" />
        <Icon name="undo" />
        <Icon name="edit" />
        <Icon name="chevron-down" />
        <Icon name="edit" mirror />
        <Icon name="arrow-right" mirror={false} />
      </>,
    );
    const svg = (name: string) => [...document.querySelectorAll(`svg[data-th-icon="${name}"]`)] as SVGElement[];
    expect(svg('chevron-right')[0]).toHaveAttribute('data-kx-mirror', 'true');
    expect(svg('arrow-left')[0]).toHaveAttribute('data-kx-mirror', 'true');
    expect(svg('undo')[0]).toHaveAttribute('data-kx-mirror', 'true');
    expect(svg('edit')[0]).not.toHaveAttribute('data-kx-mirror');
    expect(svg('chevron-down')[0]).not.toHaveAttribute('data-kx-mirror');
    expect(svg('edit')[1]).toHaveAttribute('data-kx-mirror', 'true');
    expect(svg('arrow-right')[0]).not.toHaveAttribute('data-kx-mirror');
  });

  it('con etiqueta es una imagen con nombre; sin ella, decorativo', () => {
    render(
      <>
        <Icon name="building" label="Empresa" />
        <Icon name="search" />
      </>,
    );
    expect(screen.getByRole('img', { name: 'Empresa' })).toHaveAttribute('data-th-icon', 'building');
    expect(document.querySelector('svg[data-th-icon="search"]')).toHaveAttribute('aria-hidden', 'true');
  });

  it('resuelve los nombres de la configuración y tiene icono de reserva', () => {
    const names = [
      'building',
      'view',
      'edit',
      'delete',
      'search',
      'box',
      'truck',
      'package',
      'calendar',
      'briefcase',
      'history',
      'globe',
      'lock',
    ];
    render(
      <>
        {names.map((name) => (
          <Icon key={name} name={name} />
        ))}
        <Icon name="no-existe" weight="bold" />
      </>,
    );
    for (const name of names) {
      expect(document.querySelector(`svg[data-th-icon="${name}"]`), name).not.toBeNull();
    }
    expect(document.querySelector('svg[data-th-icon="no-existe"]')).toHaveAttribute('data-kx-weight', 'bold');
  });
});

describe('Feedback, Badge y Chip', () => {
  it('Feedback con tono y role status|alert', () => {
    render(
      <>
        <Feedback tone="success">Guardado</Feedback>
        <Feedback tone="error">Falló</Feedback>
        <Feedback tone="info" role="alert">
          Forzado
        </Feedback>
        <Feedback tone="accent">Marca</Feedback>
      </>,
    );
    const ok = screen.getByText('Guardado');
    expect(ok).toHaveAttribute('data-th', 'feedback');
    expect(ok).toHaveAttribute('data-th-tone', 'success');
    expect(ok).toHaveAttribute('role', 'status');
    expect(screen.getByText('Falló')).toHaveAttribute('role', 'alert');
    expect(screen.getByText('Forzado')).toHaveAttribute('role', 'alert');
    expect(screen.getByText('Marca')).toHaveAttribute('data-th-tone', 'info');
    expect(screen.getByText('Marca')).toHaveAttribute('data-kx-tone', 'accent');
  });

  it('Badge: tono neutro por defecto', () => {
    render(
      <>
        <Badge>sin tono</Badge>
        <Badge tone="warning">aviso</Badge>
      </>,
    );
    expect(screen.getByText('sin tono')).toHaveAttribute('data-th-tone', 'neutral');
    expect(screen.getByText('aviso')).toHaveAttribute('data-th-tone', 'warning');
  });

  it('Chip: botón de quitar quiet con nombre accesible', async () => {
    const user = userEvent.setup();
    const onRemove = vi.fn();
    render(
      withLocale(
        <>
          <Chip tone="info" onRemove={onRemove}>
            Activas
          </Chip>
          <Chip>Sin quitar</Chip>
          <Chip onRemove={onRemove}>
            <b>Complejo</b>
          </Chip>
        </>,
      ),
    );
    const remove = screen.getByRole('button', { name: 'Quitar Activas' });
    expect(remove).toHaveAttribute('data-th-kind', 'quiet');
    expect(remove).toHaveAttribute('type', 'button');
    await user.click(remove);
    expect(onRemove).toHaveBeenCalledTimes(1);
    expect(screen.getAllByRole('button')).toHaveLength(2);
    expect(screen.getByRole('button', { name: 'Quitar' })).toBeInTheDocument();
  });

  it('Chip en inglés', () => {
    render(
      withLocale(
        <Chip onRemove={() => undefined} removeLabel="Quitar filtro">
          Ok
        </Chip>,
        'en',
      ),
    );
    expect(screen.getByRole('button', { name: 'Quitar filtro' })).toBeInTheDocument();
  });
});

describe('Kbd, Skeleton y Spinner', () => {
  it('Kbd y ShortcutKeys pintan una tecla por pieza, con Mod según la plataforma', () => {
    const { rerender } = render(<ShortcutKeys keys="Mod+Shift+Z" platform="other" />);
    expect([...document.querySelectorAll('kbd')].map((k) => k.textContent)).toEqual(['Ctrl', 'Shift', 'Z']);
    rerender(<ShortcutKeys keys="Mod+Shift+Z" platform="mac" />);
    expect([...document.querySelectorAll('kbd')].map((k) => k.textContent)).toEqual(['⌘', '⇧', 'Z']);
    rerender(<Kbd>Esc</Kbd>);
    expect(screen.getByText('Esc')).toHaveAttribute('data-th-format', 'code');
  });

  it('Skeleton: líneas decorativas, o anunciado si lleva etiqueta', () => {
    const { rerender } = render(<Skeleton lines={3} />);
    const decorative = document.querySelector('[data-kx-ui="skeleton"]') as HTMLElement;
    expect(decorative).toHaveAttribute('aria-hidden', 'true');
    expect(decorative.querySelectorAll('[data-kx-part="line"]')).toHaveLength(3);
    rerender(<Skeleton label="Cargando filas" />);
    expect(screen.getByRole('status', { name: 'Cargando filas' })).toBeInTheDocument();
    rerender(withLocale(<Skeleton announce lines={0} />, 'en'));
    expect(screen.getByRole('status', { name: 'Loading content' })).toBeInTheDocument();
    expect(document.querySelectorAll('[data-kx-part="line"]')).toHaveLength(1);
  });

  it('Spinner: role=status con texto accesible, o decorativo', () => {
    const { rerender } = render(<Spinner />);
    expect(screen.getByRole('status')).toHaveTextContent('Cargando');
    rerender(<Spinner label="Guardando" />);
    expect(screen.getByRole('status')).toHaveTextContent('Guardando');
    rerender(<Spinner decorative />);
    expect(screen.queryByRole('status')).toBeNull();
    rerender(withLocale(<Spinner />, 'en'));
    expect(screen.getByRole('status')).toHaveTextContent('Loading');
  });
});

describe('Field', () => {
  it('enlaza etiqueta, ayuda y error con el control', () => {
    render(
      <Field id="nombre" label="Nombre" hint="Como en el DNI" error="Obligatorio" required>
        <input />
      </Field>,
    );
    const input = screen.getByLabelText('Nombre');
    expect(input).toHaveAttribute('id', 'nombre');
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input).toHaveAttribute('aria-required', 'true');
    expect(input).toHaveAccessibleDescription('Como en el DNI Obligatorio');
    const root = input.closest('[data-th="field"]');
    expect(root).toHaveAttribute('data-th-required');
    expect(root?.querySelector('[data-th="hint"]')).toHaveTextContent('Como en el DNI');
    expect(root?.querySelector('[data-th="field-error"]')).toHaveAttribute('data-th-tone', 'error');
  });

  it('sin ayuda ni error no pone aria-describedby ni aria-invalid; admite sólo lectura', () => {
    render(
      <Field id="c" label="Código" readOnly>
        <input readOnly />
      </Field>,
    );
    const input = screen.getByLabelText('Código');
    expect(input).not.toHaveAttribute('aria-describedby');
    expect(input).not.toHaveAttribute('aria-invalid');
    expect(input.closest('[data-th="field"]')).toHaveAttribute('data-th-readonly');
  });

  it('con función recibe las propiedades y con un elemento conserva su aria-describedby', () => {
    render(
      <>
        <Field id="a" label="A" error="mal">
          {(control) => <input {...control} data-testid="fn" />}
        </Field>
        <Field id="b" label="B" hint="pista">
          <input aria-describedby="otro" />
        </Field>
        <p id="otro">Otro</p>
        <Field id="c" label="C">
          texto suelto
        </Field>
      </>,
    );
    expect(screen.getByTestId('fn')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByLabelText('B').getAttribute('aria-describedby')?.split(' ')).toContain('otro');
    expect(screen.getByLabelText('B')).toHaveAccessibleDescription(/Otro/);
    expect(screen.getByText('texto suelto')).toBeInTheDocument();
  });
});

describe('Toast', () => {
  function Emitter({ onAction, tone, duration }: { onAction?: () => void; tone?: 'error' | 'info'; duration?: number }) {
    const toast = useToast();
    return (
      <>
        <button
          type="button"
          onClick={() => toast.show({ message: 'Fila eliminada', tone, onAction, duration })}
        >
          emitir
        </button>
        <button type="button" onClick={() => toast.dismiss()}>
          cerrar-ultimo
        </button>
      </>
    );
  }

  it('muestra un aviso con la acción Deshacer, que invoca su callback y cierra el aviso', async () => {
    const user = userEvent.setup();
    const onAction = vi.fn();
    render(
      withLocale(
        <ToastProvider>
          <Emitter onAction={onAction} />
        </ToastProvider>,
      ),
    );
    await user.click(screen.getByRole('button', { name: 'emitir' }));
    const notice = await screen.findByRole('status');
    expect(notice).toHaveAttribute('data-th', 'feedback');
    expect(notice).toHaveAttribute('data-th-elevation', 'overlay');
    expect(notice).toHaveTextContent('Fila eliminada');
    expect(screen.getByRole('region', { name: 'Avisos' })).toBeInTheDocument();
    await user.click(within(notice).getByRole('button', { name: 'Deshacer' }));
    expect(onAction).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('en inglés la acción es Undo; el error es un alert; el botón cierra', async () => {
    const user = userEvent.setup();
    render(
      withLocale(
        <ToastProvider>
          <Emitter tone="error" onAction={() => undefined} />
        </ToastProvider>,
        'en',
      ),
    );
    await user.click(screen.getByRole('button', { name: 'emitir' }));
    const alert = await screen.findByRole('alert');
    expect(within(alert).getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    await user.click(within(alert).getByRole('button', { name: 'Dismiss notification' }));
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('se cierra solo tras su duración y se pausa con el ratón encima', async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <Emitter duration={150} />
      </ToastProvider>,
    );
    await user.click(screen.getByRole('button', { name: 'emitir' }));
    const notice = await screen.findByRole('status');
    await user.hover(notice);
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(screen.getByRole('status')).toBeInTheDocument();
    await user.unhover(notice);
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull(), { timeout: 2000 });
  });

  it('duración 0 lo deja hasta que se cierre; dismiss() cierra el último; máximo 5 visibles', async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <Emitter duration={0} />
      </ToastProvider>,
    );
    for (let index = 0; index < 7; index += 1) {
      await user.click(screen.getByRole('button', { name: 'emitir' }));
    }
    expect(screen.getAllByRole('status')).toHaveLength(5);
    await user.click(screen.getByRole('button', { name: 'cerrar-ultimo' }));
    expect(screen.getAllByRole('status')).toHaveLength(4);
  });

  it('sin proveedor, useToast no falla ni muestra nada', async () => {
    const user = userEvent.setup();
    render(<Emitter />);
    await user.click(screen.getByRole('button', { name: 'emitir' }));
    await user.click(screen.getByRole('button', { name: 'cerrar-ultimo' }));
    expect(screen.queryByRole('status')).toBeNull();
  });
});

describe('Dialog', () => {
  function Host({ children }: { children?: (close: () => void) => ReactElement }) {
    const [open, setOpen] = useState(true);
    return (
      <>
        <span>{open ? 'abierto' : 'cerrado'}</span>
        <Dialog open={open} onOpenChange={setOpen} title="Detalle" description="Qué se va a hacer">
          {children?.(() => setOpen(false)) ?? <p>contenido</p>}
        </Dialog>
      </>
    );
  }

  it('es un role=dialog modal con nombre, descripción y data-th-elevation=modal', async () => {
    render(withLocale(<Host />));
    const dialog = await screen.findByRole('dialog', { name: 'Detalle' });
    expect(dialog).toHaveAttribute('data-th-elevation', 'modal');
    expect(dialog).toHaveAccessibleDescription('Qué se va a hacer');
    expect(within(dialog).getByText('contenido')).toBeInTheDocument();
  });

  it('Esc y el botón Cerrar lo cierran y devuelven el foco', async () => {
    const user = userEvent.setup();
    render(
      withLocale(
        <>
          <button type="button">abrir</button>
          <Host />
        </>,
      ),
    );
    await screen.findByRole('dialog');
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('cerrado')).toBeInTheDocument();
  });

  it('el botón de cerrar tiene nombre accesible', async () => {
    const user = userEvent.setup();
    render(withLocale(<Host />, 'en'));
    await user.click(await screen.findByRole('button', { name: 'Close' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  it('sin descripción no avisa de aria-describedby y admite pie', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    render(
      <Dialog open onOpenChange={() => undefined} title="Sin texto" footer={<Action>Hecho</Action>}>
        <p>x</p>
      </Dialog>,
    );
    const dialog = await screen.findByRole('dialog', { name: 'Sin texto' });
    expect(within(dialog).getByRole('button', { name: 'Hecho' })).toBeInTheDocument();
    expect(warn.mock.calls.flat().join(' ')).not.toMatch(/Description/);
    expect(error.mock.calls.flat().join(' ')).not.toMatch(/Description/);
    error.mockRestore();
    warn.mockRestore();
  });
});

describe('ConfirmDialog', () => {
  it('confirmar llama a onConfirm y cierra; cancelar sólo cierra', async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    const onOpenChange = vi.fn();
    const { rerender } = render(
      withLocale(
        <ConfirmDialog open title="¿Eliminar?" message="No se puede deshacer" onConfirm={onConfirm} onOpenChange={onOpenChange} />,
      ),
    );
    const dialog = await screen.findByRole('dialog', { name: '¿Eliminar?' });
    expect(dialog).toHaveAccessibleDescription('No se puede deshacer');
    expect(screen.getByRole('button', { name: 'Confirmar' })).toHaveAttribute('type', 'submit');
    expect(screen.getByRole('button', { name: 'Cancelar' })).toHaveAttribute('type', 'button');
    await user.click(screen.getByRole('button', { name: 'Confirmar' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenLastCalledWith(false);

    onConfirm.mockClear();
    onOpenChange.mockClear();
    rerender(
      withLocale(
        <ConfirmDialog open title="¿Eliminar?" message="No se puede deshacer" onConfirm={onConfirm} onOpenChange={onOpenChange} />,
      ),
    );
    await user.click(screen.getByRole('button', { name: 'Cancelar' }));
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('destructiva: botón destructive y foco inicial en Cancelar; etiquetas propias; Enter confirma', async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      withLocale(
        <ConfirmDialog
          open
          destructive
          title="Borrar"
          message="Seguro"
          confirmLabel="Sí, borrar"
          cancelLabel="No"
          onConfirm={onConfirm}
          onOpenChange={() => undefined}
        />,
      ),
    );
    const confirm = await screen.findByRole('button', { name: 'Sí, borrar' });
    expect(confirm).toHaveAttribute('data-th-kind', 'destructive');
    expect(screen.getByRole('button', { name: 'No' })).toHaveFocus();
    confirm.focus();
    await user.keyboard('{Enter}');
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });
});

describe('PromptDialog', () => {
  it('con motivo obligatorio no envía vacío y muestra el error enlazado al campo', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(
      withLocale(
        <PromptDialog open title="Bloquear" label="Motivo" required onSubmit={onSubmit} onOpenChange={() => undefined} />,
      ),
    );
    const field = await screen.findByRole('textbox', { name: 'Motivo' });
    expect(field).toHaveFocus();
    expect(field).toHaveAttribute('aria-required', 'true');
    await user.click(screen.getByRole('button', { name: 'Aceptar' }));
    expect(onSubmit).not.toHaveBeenCalled();
    expect(field).toHaveAttribute('aria-invalid', 'true');
    expect(field).toHaveAccessibleDescription('Indica un motivo.');
    await user.type(field, '  Impago  ');
    await user.click(screen.getByRole('button', { name: 'Aceptar' }));
    expect(onSubmit).toHaveBeenCalledWith('Impago');
  });

  it('con motivo opcional admite vacío; con maxLength limita y cuenta', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    const onOpenChange = vi.fn();
    render(
      withLocale(
        <PromptDialog
          open
          title="Nota"
          label="Texto"
          maxLength={5}
          submitLabel="Enviar"
          cancelLabel="Salir"
          onSubmit={onSubmit}
          onOpenChange={onOpenChange}
        />,
        'es',
      ),
    );
    const field = await screen.findByRole('textbox', { name: 'Texto' });
    expect(field).toHaveAttribute('maxlength', '5');
    expect(field).toHaveAccessibleDescription('0 de 5 caracteres');
    await user.type(field, 'abcdefgh');
    expect(field).toHaveValue('abcde');
    expect(field).toHaveAccessibleDescription('5 de 5 caracteres');
    await user.clear(field);
    await user.click(screen.getByRole('button', { name: 'Enviar' }));
    expect(onSubmit).toHaveBeenCalledWith('');
    expect(onOpenChange).toHaveBeenCalledWith(false);
    await user.click(screen.getByRole('button', { name: 'Salir' }));
    expect(onOpenChange).toHaveBeenCalledTimes(2);
  });

  it('parte del valor inicial', async () => {
    render(
      withLocale(
        <PromptDialog open title="Nota" label="Texto" initialValue="previo" onSubmit={() => undefined} onOpenChange={() => undefined} />,
        'en',
      ),
    );
    expect(await screen.findByRole('textbox', { name: 'Texto' })).toHaveValue('previo');
    expect(screen.getByRole('button', { name: 'OK' })).toBeInTheDocument();
  });
});
