// Tarjetas «Registro» y «Relaciones» de la ficha (M-4, D-R40): lo que muestran, lo que omiten y lo que enlazan.
// Componentes sueltos contra el manifiesto real de la demo y el backend falso, y la ficha entera.
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { SessionContext, type AppManifest, type EntityConfig, type EntityRoute, type FieldDef, type Row, type Session } from '../contracts';
import { SheetMeta, actorText, shortId } from './SheetMeta';
import { SheetRelations, childCountOf, relationFields, relationId } from './SheetRelations';
import { TestProviders, demoManifest, facilityRows, installMockBackend, renderDetail, testQueryClient, waitForSheet } from './test-utils';
import { useSession } from '../auth';
import type { PropsWithChildren } from 'react';

installMockBackend();

const UUID = '0192a000-1111-7222-9333-444455556666';

async function renderMeta(row: Row): Promise<ReturnType<typeof userEvent.setup>> {
  const manifest = await demoManifest();
  const user = userEvent.setup();
  render(
    <TestProviders manifest={manifest}>
      <SheetMeta row={row} />
    </TestProviders>,
  );
  await screen.findByRole('heading', { level: 3, name: 'Registro' });
  return user;
}

describe('SheetMeta · tarjeta «Registro»', () => {
  it('muestra el identificador corto y la versión, y omite creado y modificado si la fila no los trae', async () => {
    await renderMeta({ id: UUID, version: 7 });
    const card = screen.getByRole('region', { name: 'Registro' });
    expect(within(card).getByText(shortId(UUID))).toHaveAttribute('title', UUID);
    expect(within(card).getByText('Identificador')).toBeInTheDocument();
    expect(within(card).getByText('Versión')).toBeInTheDocument();
    expect(within(card).getByText('7')).toBeInTheDocument();
    expect(within(card).queryByText('Creado')).not.toBeInTheDocument();
    expect(within(card).queryByText('Modificado')).not.toBeInTheDocument();
  });

  it('sin `version` no hay fila de versión', async () => {
    await renderMeta({ id: UUID });
    expect(screen.queryByText('Versión')).not.toBeInTheDocument();
  });

  it('con sello de auditoría enseña cuándo y quién, y sólo lo que viene', async () => {
    await renderMeta({
      id: UUID,
      version: 2,
      createdAt: '2026-03-04T09:30:00Z',
      createdBy: 'ana',
      updatedAt: '2026-05-06T10:45:00Z',
    });
    const created = document.querySelector('[data-kx-fact="created"]') as HTMLElement;
    expect(created).toHaveTextContent(/2026/);
    expect(created).toHaveTextContent('ana');
    const updated = document.querySelector('[data-kx-fact="updated"]') as HTMLElement;
    expect(updated).toHaveTextContent(/2026/);
    expect(updated).not.toHaveTextContent('ana');
  });

  it('una fecha ilegible se enseña tal cual y un autor ausente no deja huecos', async () => {
    await renderMeta({ id: UUID, createdAt: 'ayer por la tarde' });
    const created = document.querySelector('[data-kx-fact="created"]') as HTMLElement;
    expect(created).toHaveTextContent('ayer por la tarde');
    expect(created.textContent).not.toContain('·');
  });

  it('el botón copia el identificador completo y lo avisa', async () => {
    const user = await renderMeta({ id: UUID, version: 1 });
    await user.click(screen.getByRole('button', { name: 'Copiar el identificador' }));
    expect(await navigator.clipboard.readText()).toBe(UUID);
    expect(await screen.findByText('Identificador copiado.')).toBeInTheDocument();
  });

  it('actorText y shortId', () => {
    expect(actorText('  ana ')).toBe('ana');
    expect(actorText({ displayName: 'Ana P.' })).toBe('Ana P.');
    expect(actorText({ nada: 1 })).toBe('');
    expect(actorText(null)).toBe('');
    expect(shortId('abc')).toBe('abc');
    expect(shortId(UUID)).toBe('0192a000…');
  });
});

// ---------------------------------------------------------------------------------------------

function fieldsOf(config: EntityConfig): FieldDef[] {
  return config.detail.sections.flatMap((section) => section.fields);
}

/** Una instalación con padre (la demo la trae jerárquica: `partOf`). */
function childFacility(): Row {
  const row = facilityRows().find((candidate) => typeof candidate.partOf === 'string' && candidate.partOf !== '');
  if (!row) throw new Error('la demo no tiene una instalación con padre');
  return row;
}

interface RelationsOptions {
  row?: Row;
  /** Permisos que tiene el usuario (por omisión, los del administrador de la demo, que lo puede todo). */
  granted?: readonly string[];
  configure?: (config: EntityConfig, manifest: AppManifest) => void;
}

/** Sustituye `can()` de la sesión por una lista cerrada de permisos (sin lista, la sesión tal cual). */
function Granting({ permissions, children }: PropsWithChildren<{ permissions: readonly string[] | undefined }>) {
  const session = useSession();
  if (!permissions) return <>{children}</>;
  const limited: Session = { ...session, can: (permission) => permissions.includes(permission) };
  return <SessionContext.Provider value={limited}>{children}</SessionContext.Provider>;
}

async function renderRelations(options: RelationsOptions = {}) {
  const manifest = structuredClone(await demoManifest()) as AppManifest;
  const config = manifest.entities.facility as EntityConfig;
  options.configure?.(config, manifest);
  const row = options.row ?? childFacility();
  const navigate = vi.fn<(to: EntityRoute) => void>();
  const user = userEvent.setup();
  const view = render(
    <TestProviders manifest={manifest} queryClient={testQueryClient()}>
      <Granting permissions={options.granted}>
        <SheetRelations config={config} row={row} fields={fieldsOf(config)} navigate={navigate} />
      </Granting>
    </TestProviders>,
  );
  return { view, user, navigate, config, row, manifest };
}

describe('SheetRelations · tarjeta «Relaciones»', () => {
  it('enlaza un campo lookup con valor a la ficha de destino y navega por la ruta de entidad', async () => {
    const { user, navigate, row } = await renderRelations();
    const parent = facilityRows().find((candidate) => candidate.id === row.partOf) as Row;
    const link = await screen.findByRole('link', { name: `Abrir Instalación: ${String(parent.name)}` });
    expect(link).toHaveAttribute('href', `/facility/${String(row.partOf)}`);
    expect(link).toHaveTextContent(String(parent.name));
    await user.click(link);
    expect(navigate).toHaveBeenCalledWith({ entity: 'facility', id: row.partOf, mode: 'read' });
  });

  it('con Ctrl o Mayús el clic no navega por la aplicación (lo resuelve el navegador)', async () => {
    const { user, navigate, row } = await renderRelations();
    const link = await screen.findByRole('link', { name: /^Abrir Instalación:/ });
    // jsdom no navega: el `document` recoge el clic después del manejador de React y evita el aviso.
    const swallow = (event: Event) => event.preventDefault();
    document.addEventListener('click', swallow);
    await user.keyboard('{Control>}');
    await user.click(link);
    await user.keyboard('{/Control}');
    document.removeEventListener('click', swallow);
    expect(navigate).not.toHaveBeenCalled();
    expect(link).toHaveAttribute('href', `/facility/${String(row.partOf)}`);
  });

  it('no enlaza el lookup cuyo destino no puede leer el usuario: queda como texto con su nombre', async () => {
    const { row } = await renderRelations({
      granted: ['Otra.Cosa.Read'],
      configure: (config) => {
        config.permissions = { ...config.permissions, read: 'Facilities.Facility.Read' };
      },
    });
    const parent = facilityRows().find((candidate) => candidate.id === row.partOf) as Row;
    const fact = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('[data-kx-fact="lookup"][data-kx-field="partOf"]');
      if (!found || found.querySelector('[data-kx-ui="skeleton"]')) throw new Error('aún carga');
      return found;
    });
    expect(within(fact).queryByRole('link')).toBeNull();
    expect(fact).toHaveTextContent(String(parent.name));
    expect(screen.queryByRole('link', { name: /^Abrir / })).toBeNull();
  });

  it('con el permiso de lectura de la entidad de destino sí lo enlaza', async () => {
    const { row } = await renderRelations({
      granted: ['Facilities.Facility.Read'],
      configure: (config) => {
        config.permissions = { ...config.permissions, read: 'Facilities.Facility.Read' };
      },
    });
    const link = await screen.findByRole('link', { name: /^Abrir Instalación:/ });
    expect(link).toHaveAttribute('href', `/facility/${String(row.partOf)}`);
  });

  it('un lookup sin valor no es una relación', () => {
    const fields: FieldDef[] = [
      { field: 'a', label: { literal: 'A' }, kind: 'lookup', required: false, editable: true, lookup: { entity: 'x', valueField: 'id', displayField: 'name' } },
      { field: 'b', label: { literal: 'B' }, kind: 'lookup', required: false, editable: true, lookup: { entity: 'x', valueField: 'id', displayField: 'name' } },
      { field: 'c', label: { literal: 'C' }, kind: 'text', required: false, editable: true },
    ];
    const found = relationFields(fields, { id: '1', a: '', b: { id: 'xyz' }, c: 'texto' });
    expect(found.map((entry) => [entry.def.field, entry.id])).toEqual([['b', 'xyz']]);
  });

  it('con vista de árbol ofrece verlo en el árbol, centrado en el registro, y no repite al padre si ya es un campo', async () => {
    const { row, navigate, user } = await renderRelations();
    const tree = await screen.findByRole('link', { name: /en el árbol/ });
    const href = new URL(tree.getAttribute('href') as string, 'http://localhost');
    expect(href.pathname).toBe('/facility');
    expect(href.searchParams.get('view')).toBe('tree');
    expect(href.searchParams.get('focus')).toBe(row.id);
    await user.click(tree);
    expect(navigate).toHaveBeenCalledWith({ entity: 'facility', view: 'tree', params: { focus: row.id } });
    // `partOf` ya sale como lookup: no hay un segundo «Dentro de».
    expect(document.querySelector('[data-kx-fact="parent"]')).toBeNull();
  });

  it('si el padre no es un campo de la ficha, «Dentro de» lo enseña con su nombre y lo enlaza', async () => {
    const { row } = await renderRelations({
      configure: (config) => {
        for (const section of config.detail.sections) section.fields = section.fields.filter((field) => field.field !== 'partOf');
      },
    });
    const parent = facilityRows().find((candidate) => candidate.id === row.partOf) as Row;
    const within_ = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('[data-kx-fact="parent"]');
      if (!found || !found.querySelector('a')) throw new Error('aún no está');
      return found;
    });
    expect(within(within_).getByText('Dentro de')).toBeInTheDocument();
    expect(within(within_).getByRole('link')).toHaveTextContent(String(parent.name));
  });

  it('el recuento de hijos sólo sale si la fila lo trae', async () => {
    const without = await renderRelations();
    await screen.findByRole('link', { name: /en el árbol/ });
    expect(screen.queryByText('Contiene')).not.toBeInTheDocument();
    without.view.unmount();

    await renderRelations({ row: { ...childFacility(), childCount: 3 } });
    expect(await screen.findByText('Contiene')).toBeInTheDocument();
    expect(screen.getByText('3 elementos')).toBeInTheDocument();
  });

  it('una entidad sin árbol ni relaciones no pinta la tarjeta', async () => {
    const manifest = await demoManifest();
    const config = structuredClone(manifest.entities.facility as EntityConfig);
    config.views = config.views.filter((view) => view.kind !== 'tree');
    const { container } = render(
      <TestProviders manifest={manifest}>
        <SheetRelations config={config} row={{ id: 'x', version: 1 }} fields={[]} navigate={() => undefined} />
      </TestProviders>,
    );
    await waitFor(() => expect(document.querySelector('[data-kx-view]')).toBeNull());
    expect(container.querySelector('[data-kx-part="relations"]')).toBeNull();
  });

  it('relationId y childCountOf', () => {
    expect(relationId('a')).toBe('a');
    expect(relationId(5)).toBe('5');
    expect(relationId({ id: 'z' })).toBe('z');
    expect(relationId('')).toBeUndefined();
    expect(relationId(null)).toBeUndefined();
    expect(childCountOf({ id: '1', childCount: 4 })).toBe(4);
    expect(childCountOf({ id: '1', childrenCount: 0 })).toBe(0);
    expect(childCountOf({ id: '1', childCount: 'muchos' })).toBeUndefined();
    expect(childCountOf({ id: '1' })).toBeUndefined();
  });
});

describe('DetailView · las tarjetas en la ficha', () => {
  it('en lectura y en edición salen «Relaciones» y «Registro» bajo el formulario; el pie sigue siendo el último', async () => {
    const row = childFacility();
    await renderDetail({ id: row.id, mode: 'edit' });
    await screen.findByRole('textbox', { name: 'Nombre' });
    const body = document.querySelector('section[data-th="sheet"] > [data-kx-part="body"]') as HTMLElement;
    const cards = body.querySelector('[data-kx-part="cards"]') as HTMLElement;
    expect(within(cards).getByRole('heading', { level: 3, name: 'Relaciones' })).toBeInTheDocument();
    expect(within(cards).getByRole('heading', { level: 3, name: 'Registro' })).toBeInTheDocument();
    expect(cards.compareDocumentPosition(document.querySelector('[data-kx-part="sheet-footer"]') as HTMLElement) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('en lectura también, con el identificador y la versión del registro', async () => {
    const row = childFacility();
    await renderDetail({ id: row.id });
    const sheet = await waitForSheet();
    await within(sheet).findByRole('heading', { level: 3, name: 'Registro' });
    expect(within(sheet).getByText(String(row.version))).toBeInTheDocument();
    expect(within(sheet).getByText(shortId(row.id))).toBeInTheDocument();
  });

  it('en el alta no hay tarjetas: todavía no hay registro', async () => {
    await renderDetail({ create: true });
    await screen.findByRole('textbox', { name: /Nombre/ });
    expect(document.querySelector('[data-kx-part="cards"]')).toBeNull();
  });
});
