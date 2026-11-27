// Menú lateral: filtrado por permisos y reglas de visibilidad, y lista plana para el buscador.
import type { AppManifest, EntityConfig, MenuNode, Session, Text } from '../contracts';
import { evaluateRule } from '../config';

/** ¿Puede el usuario leer la entidad? Sin permiso declarado, sí; con él, `can()` (falla en cerrado). */
export function canReadEntity(config: EntityConfig, session: Pick<Session, 'can'>): boolean {
  const required = config.permissions?.read;
  return required === undefined || required === '' ? true : session.can(required);
}

/** ¿Puede el usuario modificar la entidad? Sin permiso de escritura declarado, sí (manda el servidor). */
export function canWriteEntity(config: EntityConfig, session: Pick<Session, 'can'>): boolean {
  const required = config.permissions?.write;
  return required === undefined || required === '' ? true : session.can(required);
}

/**
 * Menú visible: quita los nodos cuya regla `visibleWhen` falla, las entidades que no existen o que el
 * usuario no puede leer y los grupos que se quedan sin hijos.
 */
export function filterMenu(
  nodes: readonly MenuNode[],
  manifest: Pick<AppManifest, 'entities'>,
  session: Pick<Session, 'can'>,
): MenuNode[] {
  const out: MenuNode[] = [];
  for (const node of nodes) {
    if (node.visibleWhen && !evaluateRule(node.visibleWhen, { session })) continue;
    if (node.entity !== undefined) {
      const config = manifest.entities[node.entity];
      if (!config || !canReadEntity(config, session)) continue;
    }
    if (node.children) {
      const children = filterMenu(node.children, manifest, session);
      if (children.length === 0 && node.entity === undefined) continue;
      out.push({ ...node, children });
    } else {
      out.push(node);
    }
  }
  return out;
}

export interface MenuLeaf {
  id: string;
  entity: string;
  label: string;
  group: string;
  icon?: string;
  keywords: string;
}

/** Hojas del menú con su texto resuelto, para el buscador. */
export function flattenMenu(
  nodes: readonly MenuNode[],
  manifest: Pick<AppManifest, 'entities'>,
  resolve: (text: Text | undefined) => string,
  group = '',
): MenuLeaf[] {
  const leaves: MenuLeaf[] = [];
  for (const node of nodes) {
    const label = resolve(node.label);
    if (node.entity !== undefined) {
      const config = manifest.entities[node.entity];
      leaves.push({
        id: node.id,
        entity: node.entity,
        label: label || resolve(config?.namePlural) || node.entity,
        group,
        icon: node.icon ?? config?.icon,
        keywords: [resolve(config?.name), resolve(config?.description)].filter(Boolean).join(' '),
      });
    }
    if (node.children) leaves.push(...flattenMenu(node.children, manifest, resolve, label || group));
  }
  return leaves;
}

/** Texto sin mayúsculas ni acentos, para comparar. */
function fold(text: string): string {
  return text.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
}

/**
 * Búsqueda inmediata del menú: todas las palabras escritas aparecen (sin acentos ni mayúsculas) en la etiqueta,
 * el grupo o las palabras clave. Es la que se enseña mientras se descarga la difusa (`menu-search.ts`, con
 * fuse.js, fuera del trozo inicial) y la que queda si esa descarga falla.
 */
export function matchMenu(leaves: readonly MenuLeaf[], query: string): MenuLeaf[] {
  const words = fold(query).split(/\s+/).filter(Boolean);
  if (words.length === 0) return [...leaves];
  return leaves.filter((leaf) => {
    const haystack = fold(`${leaf.label} ${leaf.group} ${leaf.keywords}`);
    return words.every((word) => haystack.includes(word));
  });
}
