// La ficha por dentro (M-8): pistas enlazadas con el campo, reparto en columnas según el ancho de la ficha y
// pie de acciones fijo. jsdom no evalúa consultas de contenedor: el reparto se prueba por lo que escribe el
// código (variables de disposición) y por el CSS, y el comportamiento real lo comprueba e2e/detalle.e2e.ts.
import { screen, waitFor, within } from '@testing-library/react';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import type { EntityConfig, FieldDef, Row } from '../contracts';
import { facilityRows, installMockBackend, renderDetail } from './test-utils';

installMockBackend();

const CSS = readFileSync(path.resolve(import.meta.dirname, 'detail.css'), 'utf8');

function allFields(config: EntityConfig): FieldDef[] {
  return config.detail.sections.flatMap((section) => section.fields);
}

/** Pone una pista en el campo `name` de la ficha, como hace una configuración con `hint` (o la hereda del formulario). */
function withNameHint(config: EntityConfig): void {
  const name = allFields(config).find((field) => field.field === 'name') as FieldDef;
  name.hint = { literal: 'Nombre oficial de la instalación.' };
}

describe('DetailView · pistas (hint)', () => {
  it('en edición pinta la pista junto al campo y la enlaza con aria-describedby', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({ id: record.id, mode: 'edit', configure: withNameHint });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    const hint = screen.getByText('Nombre oficial de la instalación.');
    expect(hint).toHaveAttribute('data-th', 'hint');
    expect(hint.id).not.toBe('');
    expect(name.getAttribute('aria-describedby')?.split(' ')).toContain(hint.id);
    // Y está dentro del mismo campo, bajo su etiqueta.
    expect(name.closest('[data-th="field"]')).toContainElement(hint);
  });

  it('en lectura la pista también se pinta y describe al valor (grupo etiquetado)', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({ id: record.id, configure: withNameHint });
    const hint = await screen.findByText('Nombre oficial de la instalación.');
    const group = hint.closest('[role="group"]') as HTMLElement;
    expect(group.getAttribute('aria-describedby')).toBe(hint.id);
  });

  it('un campo sin pista no lleva texto de ayuda', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({ id: record.id, mode: 'edit' });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    expect(name.closest('[data-th="field"]')?.querySelector('[data-th="hint"]')).toBeNull();
  });
});

describe('DetailView · columnas', () => {
  it('cada campo lleva su ancho en columnas de 12 y el mínimo de 2 columnas para fichas de ancho medio', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({
      id: record.id,
      mode: 'edit',
      configure: (config) => {
        const section = config.detail.sections[0];
        if (section) section.columns = 3;
      },
    });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    const cell = name.closest('[data-kx-part="cell"]') as HTMLElement;
    // Tres columnas = 4 de 12; con la ficha a 30em sólo caben dos = 6 de 12.
    expect(cell.style.getPropertyValue('--kx-span')).toBe('4');
    expect(cell.style.getPropertyValue('--kx-span-mid')).toBe('6');
  });

  it('con 2 columnas el ancho medio es el mismo reparto, y nunca baja de 6 de 12', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({
      id: record.id,
      mode: 'edit',
      configure: (config) => {
        const section = config.detail.sections[0];
        if (section) section.columns = 2;
      },
    });
    const name = await screen.findByRole('textbox', { name: 'Nombre' });
    const cell = name.closest('[data-kx-part="cell"]') as HTMLElement;
    expect(cell.style.getPropertyValue('--kx-span')).toBe('6');
    expect(cell.style.getPropertyValue('--kx-span-mid')).toBe('6');
    for (const other of document.querySelectorAll<HTMLElement>('[data-kx-part="cell"]')) {
      const span = Number(other.style.getPropertyValue('--kx-span'));
      expect(Number(other.style.getPropertyValue('--kx-span-mid'))).toBe(Math.max(span, 6));
    }
  });

  it('el CSS reparte en 1 columna por omisión, 2 desde 28em de ficha y las pedidas desde 44em', () => {
    expect(CSS).toMatch(/@container kx-sheet \(min-width: 28em\)\s*\{[^}]*\{[^}]*grid-column: span var\(--kx-span-mid, 12\)/);
    expect(CSS).toMatch(/@container kx-sheet \(min-width: 44em\)\s*\{[^}]*\{[^}]*grid-column: span var\(--kx-span, 12\)/);
    expect(CSS).not.toMatch(/36em/);
    // Sin consulta, la celda ocupa toda la fila.
    expect(CSS).toMatch(/\[data-kx-part="cell"\]\s*\{[^}]*grid-column: 1 \/ -1/);
  });
});

describe('DetailView · pie de acciones', () => {
  it('en edición Guardar y Cancelar están en un pie fijo abajo, no en la cabecera', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({ id: record.id, mode: 'edit' });
    await screen.findByRole('textbox', { name: 'Nombre' });
    const footer = document.querySelector<HTMLElement>('[data-kx-part="sheet-footer"]') as HTMLElement;
    expect(footer).not.toBeNull();
    expect(within(footer).getByRole('button', { name: 'Guardar' })).toHaveAttribute('data-th-kind', 'primary');
    expect(within(footer).getByRole('button', { name: 'Cancelar' })).toBeInTheDocument();
    const header = document.querySelector<HTMLElement>('[data-kx-part="sheet-header"]') as HTMLElement;
    expect(within(header).queryByRole('button', { name: 'Guardar' })).toBeNull();
    expect(header.compareDocumentPosition(footer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(CSS).toMatch(/\[data-kx-part="sheet-footer"\]\s*\{[^}]*position: sticky;[^}]*inset-block-end: 0/);
  });

  it('en lectura no hay pie y el botón principal es Editar', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({ id: record.id });
    await screen.findByRole('heading', { level: 2, name: String(record.name) });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Editar' })).toBeInTheDocument());
    expect(document.querySelector('[data-kx-part="sheet-footer"]')).toBeNull();
  });

  it('en el alta el pie lleva Crear', async () => {
    await renderDetail({ create: true });
    await screen.findByRole('textbox', { name: /Nombre/ });
    const footer = document.querySelector<HTMLElement>('[data-kx-part="sheet-footer"]') as HTMLElement;
    expect(within(footer).getByRole('button', { name: 'Crear' })).toBeInTheDocument();
  });
});
