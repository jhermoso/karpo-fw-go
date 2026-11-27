// Menú: filtrado por permisos y reglas, lista plana y búsqueda difusa; paleta: filtrado y agrupación.
import { describe, expect, it } from 'vitest';
import { resolveText } from '../config';
import type { AppManifest, EntityConfig, MenuNode, Text } from '../contracts';
import { canReadEntity, canWriteEntity, filterMenu, flattenMenu, matchMenu } from './menu';
import { searchMenu } from './menu-search';
import { filterPalette, groupPalette, type PaletteItem } from './palette-items';

const text = (literal: string): Text => ({ literal });

function entity(id: string, extra: Partial<EntityConfig> = {}): EntityConfig {
  return {
    id,
    name: text(id),
    namePlural: text(`${id}s`),
    permissions: {},
    ...extra,
  } as unknown as EntityConfig;
}

const manifest: Pick<AppManifest, 'entities'> = {
  entities: {
    facility: entity('facility', { name: text('Instalación'), namePlural: text('Instalaciones'), permissions: { read: 'Facilities.Facility.Read', write: 'Facilities.Facility.Update' } }),
    supplier: entity('supplier', { name: text('Proveedor'), namePlural: text('Proveedores'), permissions: { read: 'Purchases.Supplier.Read' } }),
    party: entity('party', { namePlural: text('Terceros') }),
  },
};

const menu: MenuNode[] = [
  {
    id: 'org',
    label: text('Organización'),
    children: [
      { id: 'm-facility', entity: 'facility', label: text('Instalaciones') },
      { id: 'm-ghost', entity: 'no-existe', label: text('Fantasma') },
    ],
  },
  {
    id: 'com',
    label: text('Comercial'),
    children: [{ id: 'm-supplier', entity: 'supplier', label: text('Proveedores') }],
  },
  { id: 'm-party', entity: 'party', label: text('Terceros') },
  { id: 'm-hidden', entity: 'party', label: text('Sólo admin'), visibleWhen: { permission: 'Admin.*.*' } },
];

const sessionWith = (...granted: string[]) => ({
  can: (permission: string) =>
    granted.some((g) => g === '*.*.*' || g === permission),
});

describe('permisos de entidad', () => {
  it('sin permiso declarado se puede leer y escribir; con él, manda can() (falla en cerrado)', () => {
    const cfg = manifest.entities.facility as EntityConfig;
    expect(canReadEntity(entity('x'), sessionWith())).toBe(true);
    expect(canWriteEntity(entity('x'), sessionWith())).toBe(true);
    expect(canReadEntity(cfg, sessionWith())).toBe(false);
    expect(canReadEntity(cfg, sessionWith('Facilities.Facility.Read'))).toBe(true);
    expect(canWriteEntity(cfg, sessionWith('Facilities.Facility.Read'))).toBe(false);
    expect(canWriteEntity(cfg, sessionWith('Facilities.Facility.Update'))).toBe(true);
  });
});

describe('filterMenu', () => {
  it('quita entidades sin permiso o inexistentes, reglas que fallan y grupos que se quedan vacíos', () => {
    const visible = filterMenu(menu, manifest, sessionWith('Facilities.Facility.Read'));
    expect(visible.map((node) => node.id)).toEqual(['org', 'm-party']);
    expect(visible[0]?.children?.map((node) => node.id)).toEqual(['m-facility']);
  });

  it('el administrador ve todo lo que existe y la regla visibleWhen con permiso comodín', () => {
    const visible = filterMenu(menu, manifest, { can: (permission) => permission !== 'nada' });
    expect(visible.map((node) => node.id)).toEqual(['org', 'com', 'm-party', 'm-hidden']);
  });

  it('sin sesión de permisos (todo falso) sólo quedan las entidades sin permiso declarado', () => {
    const visible = filterMenu(menu, manifest, sessionWith());
    expect(visible.map((node) => node.id)).toEqual(['m-party']);
  });
});

describe('buscador del menú', () => {
  const resolve = (value: Text | undefined) => resolveText(value, 'es');
  const leaves = flattenMenu(filterMenu(menu, manifest, { can: () => true }), manifest, resolve);

  it('aplana las hojas con su grupo y las palabras clave de la entidad', () => {
    expect(leaves.map((leaf) => [leaf.entity, leaf.group])).toEqual([
      ['facility', 'Organización'],
      ['supplier', 'Comercial'],
      ['party', ''],
      ['party', ''],
    ]);
  });

  it('busca con erratas, sin acentos y por el grupo', () => {
    expect(searchMenu(leaves, 'provedor').map((leaf) => leaf.entity)).toEqual(['supplier']);
    expect(searchMenu(leaves, 'instalacion').map((leaf) => leaf.entity)).toEqual(['facility']);
    expect(searchMenu(leaves, 'comercial').map((leaf) => leaf.entity)).toContain('supplier');
    expect(searchMenu(leaves, 'zzzzzz')).toEqual([]);
    expect(searchMenu(leaves, '   ')).toHaveLength(leaves.length);
  });

  it('mientras se descarga la búsqueda difusa vale la de palabras: sin acentos ni mayúsculas, sin erratas', () => {
    expect(matchMenu(leaves, 'INSTALACION').map((leaf) => leaf.entity)).toEqual(['facility']);
    expect(matchMenu(leaves, 'comercial proveedores').map((leaf) => leaf.entity)).toEqual(['supplier']);
    expect(matchMenu(leaves, 'provedor')).toEqual([]);
    expect(matchMenu(leaves, '   ')).toHaveLength(leaves.length);
  });
});

describe('paleta de órdenes', () => {
  const item = (id: string, group: PaletteItem['group'], label: string, keywords = ''): PaletteItem => ({
    id,
    group,
    label,
    keywords,
    run: () => undefined,
  });
  const items = [
    item('theme', 'theme', 'Tema: Oscuro'),
    item('go', 'go', 'Ir a Productos', 'articulos'),
    item('create', 'create', 'Nuevo: Producto'),
    item('sel', 'selection', 'Bloquear'),
  ];

  it('sin búsqueda sale todo y los grupos van en su orden', () => {
    expect(filterPalette(items, '')).toHaveLength(4);
    expect(groupPalette(items, false).map((section) => section.group)).toEqual(['selection', 'go', 'create', 'theme']);
  });

  it('con búsqueda casa por etiqueta y palabras clave, sin acentos, y agrupa en el orden de aparición', () => {
    expect(filterPalette(items, 'producto').map((i) => i.id).sort()).toEqual(['create', 'go']);
    expect(filterPalette(items, 'artículos').map((i) => i.id)).toEqual(['go']);
    expect(filterPalette(items, 'nada de nada')).toEqual([]);
    const sections = groupPalette(filterPalette(items, 'producto'), true);
    expect(sections.flatMap((section) => section.items)).toHaveLength(2);
  });
});
