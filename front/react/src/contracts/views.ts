// Contrato de las vistas de entidad (PLAN §4.2).
// Cada módulo de vista exporta un componente con este nombre exacto:
//   grid -> GridView, detail -> DetailView, tree -> TreeView, map -> MapView.
import type { EntityConfig } from './config';
import type { IconName } from './icons';

export interface EntityRoute {
  entity: string;
  id?: string;
  mode?: 'read' | 'edit';
  view?: 'grid' | 'tree' | 'map';
  params?: Record<string, string>;
}

/**
 * Lo que una vista cuenta a la cabecera de la entidad (M-7): cuántos elementos hay y cuántos pasan el
 * filtro, para escribir «248 instalaciones» o «12 de 1.204». Todo es opcional: una vista informa sólo de
 * lo que sabe.
 */
export interface ViewStatus {
  /** Elementos de la entidad (sin filtro). */
  total?: number;
  /** Elementos que pasan el filtro y la búsqueda. */
  matched?: number;
  /** La vista aún está cargando: el recuento no es definitivo. */
  loading?: boolean;
}

export interface EntityViewProps {
  config: EntityConfig;
  route: EntityRoute;
  navigate(to: EntityRoute): void;
  /** La vista informa de su recuento a la cabecera de la entidad (M-7). Se puede llamar muchas veces. */
  onStatus?(status: ViewStatus): void;
}

/** Identificador de una opción del control de vista (M-6, D-R23). */
export type ViewChoiceId = 'table' | 'tree' | 'diagram' | 'map';

/**
 * Una opción del único control de vista de la cabecera de entidad: «Tabla · Árbol · Diagrama · Mapa».
 * `view` y `layout` son lo que se escribe en la ruta (`EntityRoute.view` y `params.layout`).
 */
export interface ViewChoice {
  id: ViewChoiceId;
  view: NonNullable<EntityRoute['view']>;
  /** Sólo en las opciones de la vista de árbol: `list` (Árbol) o `diagram` (Diagrama). */
  layout?: 'list' | 'diagram';
  icon: IconName;
  /** Clave de i18n con su espacio de nombres (`core:view.table`); el texto lo pone R4 en `core.json`. */
  labelKey: `core:view.${ViewChoiceId}`;
}

const TABLE: ViewChoice = { id: 'table', view: 'grid', icon: 'table', labelKey: 'core:view.table' };
const TREE: ViewChoice = { id: 'tree', view: 'tree', layout: 'list', icon: 'list-bullets', labelKey: 'core:view.tree' };
const DIAGRAM: ViewChoice = { id: 'diagram', view: 'tree', layout: 'diagram', icon: 'graph', labelKey: 'core:view.diagram' };
const MAP: ViewChoice = { id: 'map', view: 'map', icon: 'map-trifold', labelKey: 'core:view.map' };

/**
 * Opciones del control de vista de una entidad, en orden fijo (Tabla, Árbol, Diagrama, Mapa), a partir
 * de las vistas que declara: `grid` da Tabla; `tree` da Árbol (`layout: list`) y Diagrama
 * (`layout: diagram`); `map` da Mapa. Con una sola opción, la cabecera no pinta el control.
 */
export function viewChoices(config: Pick<EntityConfig, 'views'>): ViewChoice[] {
  const kinds = new Set(config.views.map((view) => view.kind));
  const choices: ViewChoice[] = [];
  if (kinds.has('grid')) choices.push({ ...TABLE });
  if (kinds.has('tree')) choices.push({ ...TREE }, { ...DIAGRAM });
  if (kinds.has('map')) choices.push({ ...MAP });
  return choices;
}
