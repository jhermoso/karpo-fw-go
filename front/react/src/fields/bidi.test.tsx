// M-12: el texto libre (nombres de usuario o de servidor) se pinta en <bdi> para que un nombre en otro sentido
// de escritura no reordene lo que lo rodea.
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { FieldDef } from '../contracts';
import { FieldView } from './FieldView';

function def(kind: FieldDef['kind'], extra: Partial<FieldDef> = {}): FieldDef {
  return { field: 'f', label: { literal: 'Campo' }, kind, required: false, editable: true, ...extra };
}

describe('FieldView pinta el texto libre en <bdi>', () => {
  it('texto y área de texto', () => {
    render(
      <>
        <FieldView value="Oficina de مدريد" def={def('text')} />
        <FieldView value={'línea 1\nlínea 2'} def={def('textarea')} />
      </>,
    );
    const name = screen.getByText('Oficina de مدريد');
    expect(name.tagName).toBe('BDI');
    expect(name).toHaveAttribute('data-kx-ui', 'value');
    expect(name).toHaveAttribute('data-kx-kind', 'text');
    expect(screen.getByText(/línea 1/).tagName).toBe('BDI');
  });

  it('conserva los ganchos de siempre: data-th-format=code sigue en el mismo elemento', () => {
    render(<FieldView value="FAC-001" def={def('text', { format: 'code' })} />);
    const code = screen.getByText('FAC-001');
    expect(code.tagName).toBe('BDI');
    expect(code).toHaveAttribute('data-th-format', 'code');
  });

  it('un valor vacío sigue sin pintar nada (ni un <bdi> vacío)', () => {
    const { container } = render(<FieldView value="" def={def('text')} />);
    expect(container.querySelector('bdi')).toBeNull();
  });

  it('los importes, las fechas y las insignias no llevan <bdi> (no son texto libre)', () => {
    const { container } = render(
      <>
        <FieldView value="12.50" def={def('money', { format: 'EUR' })} />
        <FieldView value="2026-11-27" def={def('date')} />
      </>,
    );
    expect(container.querySelector('bdi')).toBeNull();
  });
});
