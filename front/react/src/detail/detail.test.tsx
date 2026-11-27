// DetailView contra el backend falso de la demo (MSW) y el manifiesto real: lectura, edición con foco,
// guardado con `version`, conflicto 409, errores por campo, hint, alta, anterior/siguiente y rejilla hija.
import { screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { describe, expect, it, vi } from 'vitest';
import { server, mock } from '../../demo/mocks/node';
import { assertTherosHooks } from '../theme';
import { queryKeys, type Row } from '../contracts';
import { facilityRows, renderDetail, installMockBackend, waitForSheet } from './test-utils';

installMockBackend();

const FACILITY_URL = '*/api/v1/facilities/:id';

describe('DetailView · lectura y edición', () => {
  it('abre en edición con ?mode=edit y el foco en el primer campo editable', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({ id: record.id, mode: 'edit' });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    await waitFor(() => expect(name).toHaveFocus());
    expect(name).toHaveValue(String(record.name));
    expect(screen.getByRole('button', { name: 'Guardar' })).toBeInTheDocument();
  });

  it('en lectura enseña los valores con el visor del campo y un botón Editar', async () => {
    const record = facilityRows()[0] as Row;
    const { user, navigated } = await renderDetail({ id: record.id });
    expect(await screen.findByRole('heading', { level: 2, name: String(record.name) })).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: 'Nombre' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Editar' }));
    expect(navigated).toHaveBeenCalledWith(expect.objectContaining({ id: record.id, mode: 'edit' }));
    expect(await screen.findByRole('textbox', { name: 'Nombre' })).toBeInTheDocument();
  });

  it('pinta el hint de la configuración junto al campo', async () => {
    await renderDetail({ create: true });
    await screen.findByRole('textbox', { name: /Nombre/ });
    expect(screen.getByText('Identidad de la instalación que la contiene.')).toBeInTheDocument();
    const hint = screen.getByText('Identidad de la instalación que la contiene.');
    expect(hint).toHaveAttribute('data-th', 'hint');
  });
});

describe('DetailView · guardar', () => {
  it('envía PUT sólo con lo cambiado y la version leída, y escribe la respuesta en las cachés sin releer', async () => {
    const record = facilityRows()[0] as Row;
    const puts: unknown[] = [];
    let reads = 0;
    server.use(
      // Cuenta las lecturas y deja pasar la petición al manejador de la demo.
      http.get(FACILITY_URL, () => {
        reads += 1;
      }),
    );
    const list = facilityRows().slice(0, 5);
    const { user, queryClient, navigated } = await renderDetail({ id: record.id, mode: 'edit', list });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    const readsBeforeSave = reads;

    server.use(
      http.put(FACILITY_URL, async ({ request, params }) => {
        const body = (await request.json()) as Record<string, unknown>;
        puts.push(body);
        return HttpResponse.json({ ...record, ...body, id: params.id, version: (record.version ?? 1) + 1 });
      }),
    );
    await user.clear(name);
    await user.type(name, 'Sede renombrada');
    await user.keyboard('{Control>}{Enter}{/Control}');

    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0]).toEqual({ name: 'Sede renombrada', version: record.version });
    await waitFor(() => expect(navigated).toHaveBeenCalledWith(expect.objectContaining({ id: record.id, mode: 'read' })));

    const cachedRecord = queryClient.getQueryData<Row>(queryKeys.record('facility', record.id));
    expect(cachedRecord?.name).toBe('Sede renombrada');
    expect(cachedRecord?.version).toBe((record.version ?? 1) + 1);
    const cachedList = queryClient.getQueriesData<{ pages: { items: Row[] }[] }>({ queryKey: ['entity', 'facility', 'list'] });
    const inList = cachedList[0]?.[1]?.pages[0]?.items.find((row) => row.id === record.id);
    expect(inList?.name).toBe('Sede renombrada');
    expect(reads).toBe(readsBeforeSave);
    expect(await screen.findByRole('heading', { level: 2, name: 'Sede renombrada' })).toBeInTheDocument();
  });

  it('con un 409 abre el diálogo de conflicto con las diferencias y las opciones Recargar y Reaplicar', async () => {
    const record = facilityRows()[0] as Row;
    const serverVersion = { ...record, name: 'Nombre del servidor', version: (record.version ?? 1) + 1 };
    const { user } = await renderDetail({ id: record.id, mode: 'edit' });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    server.use(
      http.put(FACILITY_URL, () =>
        HttpResponse.json(
          { status: 409, title: 'Conflict', detail: 'the facility changed after it was read: read it again' },
          { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
      http.get(FACILITY_URL, () => HttpResponse.json(serverVersion)),
    );
    await user.clear(name);
    await user.type(name, 'Mi nombre');
    await user.keyboard('{Control>}{Enter}{/Control}');

    const dialog = await screen.findByRole('dialog', { name: 'El registro ha cambiado' });
    const table = within(dialog).getByRole('table', { name: 'Diferencias entre tus cambios y el servidor' });
    const row = within(table).getByRole('row', { name: /Nombre/ });
    expect(within(row).getByText('Mi nombre')).toBeInTheDocument();
    expect(within(row).getByText('Nombre del servidor')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Recargar' })).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Reaplicar mis cambios' })).toBeInTheDocument();
  });

  it('Reaplicar mis cambios deja lo escrito sobre la versión del servidor y guarda con su version', async () => {
    const record = facilityRows()[0] as Row;
    const serverVersion = { ...record, name: 'Nombre del servidor', version: (record.version ?? 1) + 1 };
    const puts: Record<string, unknown>[] = [];
    const { user } = await renderDetail({ id: record.id, mode: 'edit' });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    server.use(
      http.put(FACILITY_URL, async ({ request }) => {
        puts.push((await request.json()) as Record<string, unknown>);
        if (puts.length === 1) {
          return HttpResponse.json(
            { status: 409, title: 'Conflict' },
            { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
          );
        }
        return HttpResponse.json({ ...serverVersion, name: 'Mi nombre', version: serverVersion.version + 1 });
      }),
      http.get(FACILITY_URL, () => HttpResponse.json(serverVersion)),
    );
    await user.clear(name);
    await user.type(name, 'Mi nombre');
    await user.keyboard('{Control>}{Enter}{/Control}');
    const dialog = await screen.findByRole('dialog', { name: 'El registro ha cambiado' });
    await user.click(within(dialog).getByRole('button', { name: 'Reaplicar mis cambios' }));

    const reapplied = await screen.findByRole('textbox', { name: 'Nombre' });
    expect(reapplied).toHaveValue('Mi nombre');
    await user.keyboard('{Control>}{Enter}{/Control}');
    await waitFor(() => expect(puts).toHaveLength(2));
    expect(puts[1]).toEqual({ name: 'Mi nombre', version: serverVersion.version });
  });

  it('un 400 con errors.name y errorCodes.name marca el campo con aria-invalid y el texto traducido', async () => {
    const record = facilityRows()[0] as Row;
    const { user } = await renderDetail({ id: record.id, mode: 'edit' });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    server.use(
      http.put(FACILITY_URL, () =>
        HttpResponse.json(
          {
            status: 400,
            title: 'Validation Failed',
            errors: { name: ['the name is required'] },
            errorCodes: { name: ['required'] },
          },
          { status: 400, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    );
    await user.type(name, ' bis');
    await user.keyboard('{Control>}{Enter}{/Control}');
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'Nombre' })).toHaveAttribute('aria-invalid', 'true'));
    expect(screen.getByText('Este dato es obligatorio.')).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'Nombre' })).toHaveAccessibleDescription(/Este dato es obligatorio\./);
  });

  it('los errores que no casan con ningún campo van al aviso de la ficha', async () => {
    const record = facilityRows()[0] as Row;
    const { user } = await renderDetail({ id: record.id, mode: 'edit' });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    server.use(
      http.put(FACILITY_URL, () =>
        HttpResponse.json(
          { status: 422, title: 'Unprocessable', code: 'facilities.hierarchy_cycle', detail: 'cycle' },
          { status: 422, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    );
    await user.type(name, 'x');
    await user.keyboard('{Control>}{Enter}{/Control}');
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Esa instalación no puede formar parte de sí misma ni de sus descendientes.');
  });

  it('la validación local usa los mismos códigos que el registro de campos y no envía nada', async () => {
    const record = facilityRows()[0] as Row;
    const puts: unknown[] = [];
    server.use(
      http.put(FACILITY_URL, async ({ request }) => {
        puts.push(await request.json());
        return HttpResponse.json(record);
      }),
    );
    const { user } = await renderDetail({ id: record.id, mode: 'edit' });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    await user.clear(name);
    await user.keyboard('{Control>}{Enter}{/Control}');
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'Nombre' })).toHaveAttribute('aria-invalid', 'true'));
    expect(puts).toHaveLength(0);
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'Nombre' })).toHaveFocus());
  });
});

describe('DetailView · cancelar y alta', () => {
  it('Esc sin cambios cancela la edición directamente', async () => {
    const record = facilityRows()[0] as Row;
    const { user, navigated } = await renderDetail({ id: record.id, mode: 'edit' });
    await screen.findByRole('textbox', { name: 'Nombre' });
    await user.keyboard('{Escape}');
    await waitFor(() => expect(navigated).toHaveBeenCalledWith(expect.objectContaining({ id: record.id, mode: 'read' })));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('Esc con cambios pide confirmación y sólo descarta si se confirma', async () => {
    const record = facilityRows()[0] as Row;
    const { user, navigated } = await renderDetail({ id: record.id, mode: 'edit' });
    await user.type(await screen.findByRole('textbox', { name: 'Nombre' }), '!');
    expect(await screen.findByText('Cambios sin guardar')).toBeInTheDocument();
    await user.keyboard('{Escape}');
    const confirm = await screen.findByRole('dialog', { name: 'Cambios sin guardar' });
    expect(navigated).not.toHaveBeenCalled();
    // Seguir editando conserva lo escrito.
    await user.click(within(confirm).getByRole('button', { name: 'Seguir editando' }));
    expect((screen.getByRole('textbox', { name: 'Nombre' }) as HTMLInputElement).value).toContain('!');
    // El foco vuelve al campo que se estaba editando.
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'Nombre' })).toHaveFocus());
    await user.keyboard('{Escape}');
    await user.click(within(await screen.findByRole('dialog', { name: 'Cambios sin guardar' })).getByRole('button', { name: 'Descartar los cambios' }));
    await waitFor(() => expect(navigated).toHaveBeenCalledWith(expect.objectContaining({ id: record.id, mode: 'read' })));
  });

  it('el alta usa form.sections, hace POST y navega al registro creado', async () => {
    const bodies: Record<string, unknown>[] = [];
    server.use(
      http.post('*/api/v1/facilities', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ ...facilityRows()[0], id: 'nuevo-id-1', name: 'Almacén nuevo', version: 1 }, { status: 201 });
      }),
    );
    const { user, navigated } = await renderDetail({ create: true });
    const name = await screen.findByRole('textbox', { name: /Nombre/ });
    await waitFor(() => expect(name).toHaveFocus());
    await user.type(name, 'Almacén nuevo');
    await user.click(screen.getByRole('combobox', { name: /Tipo/ }));
    await user.click((await screen.findAllByRole('option'))[0] as HTMLElement);
    await user.click(screen.getByRole('button', { name: 'Crear' }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({ name: 'Almacén nuevo' });
    expect(bodies[0]).not.toHaveProperty('version');
    await waitFor(() =>
      expect(navigated).toHaveBeenCalledWith(expect.objectContaining({ entity: 'facility', id: 'nuevo-id-1', mode: 'read' })),
    );
  });
});

describe('DetailView · cabecera y rejillas hijas', () => {
  it('Cerrar el detalle y Esc en lectura llaman a onClose', async () => {
    const record = facilityRows()[0] as Row;
    const { user, onClose } = await renderDetail({ id: record.id });
    await screen.findByRole('heading', { level: 2, name: String(record.name) });
    await user.click(screen.getByRole('button', { name: 'Cerrar el detalle' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    screen.getByRole('button', { name: 'Editar' }).focus();
    await user.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('el botón de pantalla completa sólo existe si el anfitrión ofrece el cambio', async () => {
    const record = facilityRows()[0] as Row;
    const first = await renderDetail({ id: record.id });
    await screen.findByRole('heading', { level: 2, name: String(record.name) });
    expect(screen.queryByRole('button', { name: 'Abrir a pantalla completa' })).not.toBeInTheDocument();
    first.view.unmount();

    const onToggleExpand = vi.fn();
    const { user } = await renderDetail({ id: record.id, onToggleExpand });
    await user.click(await screen.findByRole('button', { name: 'Abrir a pantalla completa' }));
    expect(onToggleExpand).toHaveBeenCalledTimes(1);
  });

  it('E pasa a edición desde la ficha en lectura', async () => {
    const record = facilityRows()[0] as Row;
    const { user, navigated } = await renderDetail({ id: record.id });
    await screen.findByRole('heading', { level: 2, name: String(record.name) });
    screen.getByRole('button', { name: 'Editar' }).focus();
    await user.keyboard('e');
    expect(navigated).toHaveBeenCalledWith(expect.objectContaining({ id: record.id, mode: 'edit' }));
  });

  it('una rejilla hija sin filas enseña el mensaje de «vacío» y no una tabla', async () => {
    const supplier = (mock.data.suppliers as unknown as Row[])[0] as Row;
    server.use(http.get('*/api/v1/suppliers/:id/block-history', () => HttpResponse.json([])));
    const { user } = await renderDetail({ entity: 'supplier', id: supplier.id });
    await user.click(await screen.findByRole('tab', { name: 'Historial de bloqueos' }));
    const empty = await waitFor(() => {
      const found = document.querySelector('[data-th="collection-empty"]');
      if (!found) throw new Error('aún no');
      return found;
    });
    // El mensaje sale de la configuración (emptyMessage del hijo).
    expect(empty).toHaveTextContent('Sin bloqueos ni desbloqueos registrados.');
    expect(screen.queryByRole('table', { name: 'Historial de bloqueos' })).not.toBeInTheDocument();
  });

  it('una rejilla hija que falla enseña el error traducido y permite reintentar', async () => {
    const supplier = (mock.data.suppliers as unknown as Row[])[0] as Row;
    let calls = 0;
    server.use(
      http.get('*/api/v1/suppliers/:id/block-history', () => {
        calls += 1;
        if (calls === 1) {
          return HttpResponse.json({ status: 500, title: 'boom' }, { status: 500, headers: { 'Content-Type': 'application/problem+json' } });
        }
        return HttpResponse.json([{ id: 'e1', at: '2026-11-01T10:00:00Z', blocked: true, reason: 'Tras reintentar' }]);
      }),
    );
    const { user } = await renderDetail({ entity: 'supplier', id: supplier.id });
    await user.click(await screen.findByRole('tab', { name: 'Historial de bloqueos' }));
    const alert = await screen.findByRole('alert', {}, { timeout: 8000 });
    expect(alert).toHaveTextContent('El servidor no ha podido atender la petición');
    await user.click(within(alert).getByRole('button', { name: 'Reintentar' }));
    expect(await screen.findByText('Tras reintentar')).toBeInTheDocument();
  }, 15_000);

  it('las secciones y las rejillas hijas son pestañas navegables con las flechas', async () => {
    const supplier = (mock.data.suppliers as unknown as Row[])[0] as Row;
    server.use(
      http.get('*/api/v1/suppliers/:id/block-history', () =>
        HttpResponse.json([{ id: 'e1', at: '2026-11-01T10:00:00Z', blocked: true, reason: 'Motivo' }]),
      ),
    );
    const { user, view } = await renderDetail({ entity: 'supplier', id: supplier.id });
    const tabs = await screen.findAllByRole('tab');
    expect(tabs.length).toBeGreaterThanOrEqual(2);
    expect(tabs[0]).toHaveAttribute('aria-selected', 'true');
    tabs[0]?.focus();
    await user.keyboard('{ArrowRight}');
    expect(tabs[1]).toHaveFocus();
    // Con una pestaña de rejilla abierta, el DOM sigue cumpliendo el contrato de Theros.
    await waitFor(() => expect(tabs[1]).toHaveAttribute('aria-selected', 'true'));
    await screen.findByRole('table');
    assertTherosHooks(view.container);
  });

  it('los campos de sólo lectura de la configuración no se editan aunque la ficha esté en edición', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({ id: record.id, mode: 'edit' });
    await screen.findByRole('textbox', { name: 'Nombre' });
    // «Tipo» (typeName) es editable:false en la configuración: se ve su valor, sin editor.
    expect(screen.queryByRole('textbox', { name: 'Tipo' })).not.toBeInTheDocument();
    const field = document.querySelector('[data-th="field"][aria-labelledby$="-label"][data-th-readonly]');
    expect(field).not.toBeNull();
    // El valor sale del catálogo en el idioma activo (el servidor traduce `typeName`): se ve, no es un hueco.
    const shown = field?.querySelector('[data-kx-part="value"]')?.textContent?.trim() ?? '';
    expect(shown).not.toBe('');
    expect(shown).not.toBe('—');
    expect([String(record.typeName), 'Edificio', 'Planta', 'Sala', 'Terreno']).toContain(shown);
  });
});

describe('DetailView · lista, hijas y contrato', () => {
  it('Alt+↓ y Alt+↑ pasan al registro siguiente y anterior de la lista en caché', async () => {
    const rows = facilityRows().slice(0, 4);
    const second = rows[1] as Row;
    const { user, navigated } = await renderDetail({ id: second.id, list: rows });
    await screen.findByRole('heading', { level: 2, name: String(second.name) });
    expect(screen.getByRole('button', { name: 'Registro anterior' })).not.toHaveAttribute('aria-disabled');
    // Los atajos del ámbito de la ficha valen con el foco dentro de ella.
    screen.getByRole('button', { name: 'Editar' }).focus();
    await user.keyboard('{Alt>}{ArrowDown}{/Alt}');
    expect(navigated).toHaveBeenLastCalledWith(expect.objectContaining({ id: (rows[2] as Row).id }));
    // La ficha nueva recupera el foco y los atajos siguen valiendo.
    await screen.findByRole('heading', { level: 2, name: String((rows[2] as Row).name) });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Editar' })).toHaveFocus());
    await user.keyboard('{Alt>}{ArrowUp}{/Alt}');
    expect(navigated).toHaveBeenLastCalledWith(expect.objectContaining({ id: second.id }));
  });

  it('el primer registro de la lista no tiene anterior', async () => {
    const rows = facilityRows().slice(0, 3);
    await renderDetail({ id: (rows[0] as Row).id, list: rows });
    await waitForSheet();
    expect(await screen.findByRole('button', { name: 'Registro anterior' })).toHaveAttribute('aria-disabled', 'true');
  });

  it('la rejilla hija de proveedores pide /suppliers/{id}/block-history sin ?supplierId=', async () => {
    const supplier = (mock.data.suppliers as unknown as Row[])[0] as Row;
    const urls: string[] = [];
    server.use(
      http.get('*/api/v1/suppliers/:id/block-history', ({ request }) => {
        urls.push(request.url);
        return HttpResponse.json([
          { id: 'e1', at: '2026-11-01T10:00:00Z', blocked: true, reason: 'Impago de facturas' },
          { id: 'e2', at: '2026-11-10T09:30:00Z', blocked: false, reason: 'Regularizado' },
        ]);
      }),
    );
    const { user } = await renderDetail({ entity: 'supplier', id: supplier.id });
    await user.click(await screen.findByRole('tab', { name: 'Historial de bloqueos' }));
    const table = await screen.findByRole('table', { name: 'Historial de bloqueos' });
    expect(urls).toHaveLength(1);
    const url = new URL(urls[0] as string);
    expect(url.pathname).toBe(`/api/v1/suppliers/${supplier.id}/block-history`);
    expect(url.search).toBe('');
    expect(within(table).getAllByRole('row')).toHaveLength(3);
    expect(within(table).getByText('Impago de facturas')).toBeInTheDocument();
    // Las etiquetas del booleano vienen de la configuración (trueLabel/falseLabel).
    expect(within(table).getByText('Bloqueo')).toBeInTheDocument();
    expect(within(table).getByText('Desbloqueo')).toBeInTheDocument();
  });

  it('el DOM de la ficha cumple el contrato de ganchos de Theros', async () => {
    const record = facilityRows()[0] as Row;
    const { view } = await renderDetail({ id: record.id, mode: 'edit' });
    await screen.findByRole('textbox', { name: 'Nombre' });
    assertTherosHooks(view.container);
    assertTherosHooks(document.body, { inlineStyle: 'ignore' });
  });

  it('una entidad sin permiso de escritura no se edita aunque la ruta lo pida', async () => {
    const country = (mock.data.countries as unknown as Row[])[0] as Row;
    await renderDetail({ entity: 'country', id: country.id, mode: 'edit' });
    await waitForSheet();
    expect(screen.queryByRole('button', { name: 'Guardar' })).not.toBeInTheDocument();
  });
});

