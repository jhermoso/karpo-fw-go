import { render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { EntityConfig, FieldDef, Row } from '../contracts';
import type { NodeFieldDefs } from './fields';
import { buildFromFlat, countDescendants, flattenVisible } from './model';
import { TreeList, VIRTUALIZE_THRESHOLD } from './TreeList';

const labelDef: FieldDef = { field: 'name', label: { literal: 'Nombre' }, kind: 'text', required: false, editable: false };
const defs: NodeFieldDefs = { label: labelDef };
const config = { id: 'thing', name: { literal: 'Cosa' }, namePlural: { literal: 'Cosas' } } as unknown as EntityConfig;

function renderList(count: number) {
  const rows: Row[] = [{ id: 'root', name: 'Raíz' } as Row];
  for (let i = 1; i <= count; i += 1) rows.push({ id: `n${i}`, name: `Elemento ${i}`, parent: 'root' } as Row);
  const model = buildFromFlat(rows, 'parent');
  const items = flattenVisible(model, new Set(['root']));
  const props = {
    config,
    model,
    items,
    defs,
    counts: countDescendants(model),
    selectedId: null,
    currentId: 'root',
    matches: new Set<string>(),
    query: '',
    branchLoading: new Set<string>(),
    direction: 'ltr' as const,
    locale: 'es',
    reveal: null,
    onCurrent: vi.fn(),
    onSelect: vi.fn(),
    onToggle: vi.fn(),
    onExpandSiblings: vi.fn(),
    onOpen: vi.fn(),
  };
  return { ...render(<TreeList {...props} />), props };
}

describe('TreeList: virtualización', () => {
  it('con pocas filas pinta todas', () => {
    renderList(40);
    expect(within(screen.getByRole('tree')).getAllByRole('treeitem')).toHaveLength(41);
    expect(screen.getByRole('tree')).not.toHaveAttribute('data-kx-virtual');
  });

  it('con muchas filas sólo pinta una ventana y conserva aria-setsize y aria-posinset', () => {
    const total = VIRTUALIZE_THRESHOLD + 200;
    renderList(total);
    const tree = screen.getByRole('tree');
    expect(tree).toHaveAttribute('data-kx-virtual', 'true');
    const rendered = within(tree).getAllByRole('treeitem');
    expect(rendered.length).toBeGreaterThan(0);
    expect(rendered.length).toBeLessThan(total / 4);
    // El primer elemento es la raíz; el siguiente, el primer hijo, con su posición real en el grupo.
    expect(rendered[0]).toHaveAttribute('aria-level', '1');
    const child = rendered.find((item) => item.getAttribute('aria-level') === '2');
    expect(child).toHaveAttribute('aria-setsize', String(total));
    expect(child).toHaveAttribute('aria-posinset', '1');
    // Las filas virtualizadas van dentro de un contenedor sin rol: el árbol sigue siendo su propietario.
    expect(tree.querySelector('[data-kx-part="spacer"]')).toHaveAttribute('role', 'none');
  });
});
