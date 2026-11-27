// El valor vacío de un campo de sólo lectura de la ficha se pinta con la misma marca que la rejilla (D-R35):
// «—» decorativo y «Sin valor» para el lector; un texto propio de la configuración se respeta.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { facilityRows, installMockBackend, renderDetail, waitForSheet } from '../detail/test-utils';
import type { EntityConfig, FieldDef, Row } from '../contracts';

installMockBackend();

function readonlyField(field: string, emptyValue?: string): FieldDef {
  return {
    field,
    label: { literal: `Etiqueta ${field}` },
    kind: 'text',
    required: false,
    editable: false,
    ...(emptyValue !== undefined ? { emptyValue } : {}),
  };
}

/** Añade campos de sólo lectura (sin valor en el registro) a la primera sección de la ficha. */
function withEmptyFields(...defs: FieldDef[]) {
  return (config: EntityConfig) => {
    const first = config.detail.sections[0];
    if (first) first.fields.push(...defs);
    const form = config.form?.sections[0];
    if (form) form.fields.push(...defs);
  };
}

function valueOf(field: string): HTMLElement {
  const holder = document.querySelector<HTMLElement>(`[data-kx-field="${field}"] [data-kx-part="value"]`);
  if (!holder) throw new Error(`No hay campo ${field}`);
  return holder;
}

describe('SheetField: valor vacío', () => {
  it('con emptyValue «-» y sin emptyValue pinta «—» con «Sin valor» para el lector de pantalla', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({
      id: record.id,
      configure: withEmptyFields(readonlyField('vacioGuion', '-'), readonlyField('vacioSin')),
    });
    await waitForSheet();
    await screen.findAllByText('Etiqueta vacioGuion');
    for (const field of ['vacioGuion', 'vacioSin']) {
      const holder = valueOf(field);
      expect(holder.querySelector('[aria-hidden="true"]')?.textContent, field).toBe('—');
      expect(holder.querySelector('[data-kx-part="sr-only"]')?.textContent, field).toBe('Sin valor');
    }
  });

  it('un emptyValue con texto («sin fecha») se respeta tal cual', async () => {
    const record = facilityRows()[0] as Row;
    await renderDetail({ id: record.id, configure: withEmptyFields(readonlyField('vacioTexto', 'sin fecha')) });
    await waitForSheet();
    await screen.findAllByText('Etiqueta vacioTexto');
    const holder = valueOf('vacioTexto');
    expect(holder).toHaveTextContent('sin fecha');
    expect(holder.querySelector('[data-kx-part="sr-only"]')).toBeNull();
    expect(holder.querySelector('[aria-hidden="true"]')).toBeNull();
  });
});
