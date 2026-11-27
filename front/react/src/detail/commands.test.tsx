// Órdenes de la superficie `detail-header` (bloquear/desbloquear proveedores): motivo, version, 202 y errores.
import { screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { mock, server, setMockOptions } from '../../demo/mocks/node';
import { queryKeys, type Row } from '../contracts';
import { installMockBackend, renderDetail } from './test-utils';

installMockBackend();

const BLOCK_URL = '*/api/v1/suppliers/:id/block';

function firstSupplier(): Row {
  const supplier = (mock.data.suppliers as unknown as Row[]).find((row) => row.blocked === false);
  if (!supplier) throw new Error('El conjunto de datos no trae proveedores sin bloquear');
  return supplier;
}

describe('órdenes de la cabecera', () => {
  it('Bloquear pide el motivo, envía POST con version y motivo, y cambia por Desbloquear', async () => {
    const supplier = firstSupplier();
    const bodies: Record<string, unknown>[] = [];
    server.use(
      http.post(BLOCK_URL, async ({ request }) => {
        bodies.push((await request.clone().json()) as Record<string, unknown>);
        return undefined;
      }),
    );
    const { user, queryClient } = await renderDetail({ entity: 'supplier', id: supplier.id });
    await user.click(await screen.findByRole('button', { name: 'Bloquear' }));
    // La version que viaja es la del registro que se leyó para pintar la ficha (antes de que cambie).
    const read = queryClient.getQueryData<Row>(queryKeys.record('supplier', supplier.id));
    expect(typeof read?.version).toBe('number');

    const dialog = await screen.findByRole('dialog', { name: 'Bloquear' });
    // El motivo es obligatorio: sin él no se envía nada.
    await user.click(within(dialog).getByRole('button', { name: 'Bloquear' }));
    expect(await within(dialog).findByText('Indica un motivo.')).toBeInTheDocument();
    expect(bodies).toHaveLength(0);

    await user.type(within(dialog).getByRole('textbox', { name: /Motivo/ }), 'Impago de facturas');
    await user.click(within(dialog).getByRole('button', { name: 'Bloquear' }));

    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toEqual({ reason: 'Impago de facturas', version: read?.version });
    const toasts = await screen.findByRole('region', { name: 'Avisos' });
    expect(within(toasts).getByText('Bloqueado')).toBeInTheDocument();
    // La regla visibleWhen (blocked == false) cambia la orden disponible sin releer la lista.
    expect(await screen.findByRole('button', { name: 'Desbloquear' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Bloquear' })).not.toBeInTheDocument();
  });

  it('un 202 se cuenta como «pendiente de aprobación» y la ficha no cambia', async () => {
    setMockOptions({ requireApproval: true });
    const supplier = firstSupplier();
    const { user } = await renderDetail({ entity: 'supplier', id: supplier.id });
    await user.click(await screen.findByRole('button', { name: 'Bloquear' }));
    const dialog = await screen.findByRole('dialog', { name: 'Bloquear' });
    await user.type(within(dialog).getByRole('textbox', { name: /Motivo/ }), 'Revisión');
    await user.click(within(dialog).getByRole('button', { name: 'Bloquear' }));
    const toasts = await screen.findByRole('region', { name: 'Avisos' });
    expect(within(toasts).getByText('Solicitud enviada: queda pendiente de aprobación')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Bloquear' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Desbloquear' })).not.toBeInTheDocument();
  });

  it('un error de negocio de la orden se enseña como aviso con su texto traducido', async () => {
    const supplier = firstSupplier();
    server.use(
      http.post(BLOCK_URL, () =>
        HttpResponse.json(
          { status: 422, title: 'Unprocessable', code: 'approval.already_resolved', detail: 'x' },
          { status: 422, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    );
    const { user } = await renderDetail({ entity: 'supplier', id: supplier.id });
    await user.click(await screen.findByRole('button', { name: 'Bloquear' }));
    const dialog = await screen.findByRole('dialog', { name: 'Bloquear' });
    await user.type(within(dialog).getByRole('textbox', { name: /Motivo/ }), 'Prueba');
    await user.click(within(dialog).getByRole('button', { name: 'Bloquear' }));
    expect(await screen.findByText('La solicitud ya está resuelta.')).toBeInTheDocument();
  });

  it('las órdenes no se ofrecen mientras se edita', async () => {
    const supplier = firstSupplier();
    await renderDetail({ entity: 'supplier', id: supplier.id, mode: 'edit' });
    await screen.findByRole('button', { name: 'Guardar' });
    expect(screen.queryByRole('button', { name: 'Bloquear' })).not.toBeInTheDocument();
  });
});
