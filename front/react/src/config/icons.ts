// Avisos de iconos desconocidos (B-1, D-R20). Tras normalizar, un recorrido genérico del menú y de cada
// entidad avisa de todo `icon` que no sea un nombre del vocabulario (`ICON_NAMES`): el icono cae en un
// círculo y hasta ahora nadie lo decía. Sólo importa `contracts/icons` (datos puros): la capa de
// configuración valida sin cargar los dibujos de Phosphor.
import type { ConfigWarning, EntityConfig, MenuNode } from '../contracts';
import { isIconName } from '../contracts/icons';

/** Ruta de origen de cada nodo de menú (con los índices del JSON, no los del menú ya filtrado). */
const MENU_ORIGINS = new WeakMap<MenuNode, string>();

/** Anota de dónde viene un nodo para que el aviso apunte al JSON del autor (lo usan los cargadores). */
export function rememberMenuOrigin(node: MenuNode, path: string): void {
  MENU_ORIGINS.set(node, path);
}

export function unknownIconMessage(icon: string): string {
  return `icono \`${icon}\` desconocido: se usa un círculo`;
}

/** Propiedades que llevan datos del servidor o del usuario, no configuración de la interfaz. */
const DATA_KEYS = new Set(['warnings', 'dataSource', 'defaultValue', 'filter', 'baseFilter']);

function iconWarning(path: string, icon: string): ConfigWarning {
  return { path, message: unknownIconMessage(icon), severity: 'warn' };
}

/**
 * Avisos de icono de un árbol de menú. `entities` sirve para no repetir lo que ya avisa la entidad: un
 * ítem que hereda el icono de su entidad no se avisa dos veces. La ruta es la del JSON
 * (`menu[3].children[1].icon`); si el nodo no la trae anotada, la del árbol recibido.
 */
export function menuIconWarnings(
  menu: readonly MenuNode[],
  entities: Readonly<Record<string, Pick<EntityConfig, 'icon'>>> = {},
  basePath = 'menu',
): ConfigWarning[] {
  const out: ConfigWarning[] = [];
  const walk = (nodes: readonly MenuNode[], path: string) => {
    nodes.forEach((node, i) => {
      const here = MENU_ORIGINS.get(node) ?? `${path}[${i}]`;
      const inherited = node.entity !== undefined && entities[node.entity]?.icon === node.icon;
      if (node.icon !== undefined && !inherited && !isIconName(node.icon)) out.push(iconWarning(`${here}.icon`, node.icon));
      if (node.children) walk(node.children, `${here}.children`);
    });
  };
  walk(menu, basePath);
  return out;
}

/**
 * Avisos de icono de una entidad normalizada: recorre el modelo entero (icono de la entidad, de sus
 * opciones, secciones, filtros, órdenes y estado vacío) y avisa de cada propiedad `icon` que no es un
 * nombre del vocabulario. Las rutas siguen el modelo normalizado (`columns[2].options[1].icon`).
 */
export function entityIconWarnings(config: Omit<EntityConfig, 'warnings'> & { warnings?: unknown }): ConfigWarning[] {
  const out: ConfigWarning[] = [];
  const seen = new Set<object>();
  const walk = (value: unknown, path: string) => {
    if (Array.isArray(value)) {
      value.forEach((item, i) => walk(item, `${path}[${i}]`));
      return;
    }
    if (typeof value !== 'object' || value === null || seen.has(value)) return;
    seen.add(value);
    for (const [key, child] of Object.entries(value)) {
      if (DATA_KEYS.has(key)) continue;
      const here = path ? `${path}.${key}` : key;
      if (key === 'icon' && typeof child === 'string') {
        if (!isIconName(child)) out.push(iconWarning(here, child));
      } else walk(child, here);
    }
  };
  walk(config, '');
  return out;
}
