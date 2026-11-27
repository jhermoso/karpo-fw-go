// Ampliaciones del modelo normalizado que viven en el módulo de configuración (docs/notas/T1.md).
// Los contratos de `src/contracts` no se tocan: lo que el normalizador produce de más va aquí y
// es estructuralmente compatible con ellos (un `EntityConfigExt` es un `EntityConfig`).
import type {
  ColumnDef,
  ConfigWarning,
  EntityConfig,
  FieldDef,
  FilterDef,
  SectionDef,
  Text,
  VisibilityRule,
} from '../contracts';

export interface GridOptions {
  pageSize: number;
  pageSizeOptions: number[];
  defaultSort?: { field: string; direction: 'asc' | 'desc' };
  selectable: boolean;
  selectionMode: 'single' | 'multiple';
}

export interface DataSourceExt {
  pagination: 'server' | 'client';
  listStyle: 'maintenance' | 'number-size';
  /** `search` = POST {ep}/search; `get` = GET con parámetros. */
  listMethod: 'search' | 'get';
  baseFilter: Record<string, unknown>;
  companyFilterField?: string;
}

export interface FieldDefExt extends FieldDef {
  placeholder?: Text;
  defaultValue?: unknown;
  /** Código de divisa para `money`. */
  currency?: string;
}

export interface ColumnDefExt extends ColumnDef, FieldDefExt {}

export interface FilterDefExt extends FilterDef {
  placeholder?: Text;
  /** Columnas de 1 a 12 del panel de filtros. */
  width?: number;
  icon?: string;
  visibleWhen?: VisibilityRule;
}

export interface SectionDefExt extends SectionDef {
  icon?: string;
  description?: Text;
}

export interface EntityConfigExt extends EntityConfig {
  dataSource: DataSourceExt;
  grid: GridOptions;
  searchable: boolean;
  searchFields: string[];
  companyScoped: boolean;
  /** Con `{id}`: el detalle carga este agregado en lugar de `{endpoint}/{id}`. */
  aggregateEndpoint?: string;
  emptyState?: { icon?: string; title?: Text; message?: Text };
  form: (NonNullable<EntityConfig['form']> & { createTitle?: Text; editTitle?: Text }) | null;
}

/** Vista del modelo normalizado con las ampliaciones (con valores por omisión si faltan). */
export function asExt(config: EntityConfig): EntityConfigExt {
  const c = config as Partial<EntityConfigExt> & EntityConfig;
  return {
    ...c,
    dataSource: {
      ...c.dataSource,
      listMethod:
        (c.dataSource as Partial<DataSourceExt>).listMethod ??
        (c.dataSource.listStyle === 'number-size' ? 'get' : 'search'),
    },
    grid: c.grid ?? {
      pageSize: 25,
      pageSizeOptions: [10, 25, 50, 100],
      selectable: true,
      selectionMode: 'multiple',
    },
    searchable: c.searchable ?? false,
    searchFields: c.searchFields ?? [],
    companyScoped: c.companyScoped ?? true,
  } as EntityConfigExt;
}

export type { ConfigWarning };

/** Error de carga de configuración: el JSON no sirve ni degradado. */
export class ConfigError extends Error {
  readonly issues: ConfigWarning[];
  constructor(message: string, issues: ConfigWarning[] = []) {
    super(message);
    this.name = 'ConfigError';
    this.issues = issues;
  }
}
