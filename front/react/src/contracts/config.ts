// Contrato de configuración: modelo interno normalizado (lo que sale del normalizador).
// Lo escribe sólo la tarea 0 (PLAN §4.2). Quien necesite más, lo amplía en su módulo.

export type Text = { literal: string; key?: string; byLang?: Record<string, string> };
export type Tone = 'success' | 'error' | 'warning' | 'info' | 'neutral' | 'accent';
export type FieldKind =
  | 'text'
  | 'textarea'
  | 'number'
  | 'decimal'
  | 'money'
  | 'percentage'
  | 'date'
  | 'datetime'
  | 'boolean'
  | 'select'
  | 'multiselect'
  | 'badge'
  | 'lookup'
  | 'email'
  | 'phone'
  | 'url';

export interface OptionDef {
  value: string;
  label: Text;
  tone?: Tone;
  icon?: string;
}

export interface LookupDef {
  entity: string;
  valueField: string;
  displayField: string;
  filter?: Record<string, string | number | boolean>;
}

export type VisibilityRule = {
  expr?: string;
  permission?: string;
  field?: string;
  equals?: unknown;
  allOf?: VisibilityRule[];
  anyOf?: VisibilityRule[];
  not?: VisibilityRule;
};

export interface FieldDef {
  field: string;
  label: Text;
  kind: FieldKind;
  required: boolean;
  editable: boolean;
  immutable?: boolean;
  min?: number;
  max?: number;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  options?: OptionDef[];
  lookup?: LookupDef;
  hint?: Text;
  /** 1..12 */
  width?: number;
  format?: string;
  emptyValue?: string;
  visibleWhen?: VisibilityRule;
  trueLabel?: Text;
  falseLabel?: Text;
}

export interface ColumnDef extends FieldDef {
  widthPx?: number;
  sortable: boolean;
  serverSortField?: string;
  pinned?: 'start' | 'end';
  hidden?: boolean;
  /**
   * Importancia de la columna (extensión opcional, D-R34): entero, mayor = más importante. Con poco ancho la
   * rejilla oculta primero las de menor prioridad; sin ella, de derecha a izquierda. La primera columna y las
   * fijadas nunca se ocultan. La configuración de Angular la ignora.
   */
  priority?: number;
}

export interface FilterDef {
  field: string;
  serverField: string;
  label: Text;
  kind: 'text' | 'boolean' | 'number' | 'select' | 'multiselect' | 'date';
  quickSearch: boolean;
  options?: OptionDef[];
  defaultValue?: unknown;
  hint?: Text;
}

export interface CommandDef {
  id: string;
  label: Text;
  icon?: string;
  tone?: Tone;
  surfaces: ('row' | 'detail-header' | 'toolbar')[];
  kind: 'view' | 'edit' | 'delete' | 'http';
  method?: 'POST' | 'PUT' | 'DELETE';
  endpoint?: string;
  confirm?: Text;
  prompt?: { field: string; label: Text; required: boolean; maxLength?: number };
  sendVersion?: boolean;
  successMessage?: Text;
  acceptedMessage?: Text;
  visibleWhen?: VisibilityRule;
}

export interface SectionDef {
  id: string;
  title: Text;
  columns: number;
  collapsible: boolean;
  collapsed: boolean;
  fields: FieldDef[];
  visibleWhen?: VisibilityRule;
}

export interface ChildGridDef {
  id: string;
  title: Text;
  endpoint: string;
  columns: ColumnDef[];
  emptyMessage?: Text;
}

export interface MapLevelDef {
  id: 'world' | 'country' | 'region' | 'province' | 'city';
  label: Text;
  boundaryLevel: string;
  codeProperty: string;
  /** id en manifest.map.sources */
  source: string;
  /** Código de la delimitación que se abre por omisión en este nivel (`null`: la raíz del nivel). */
  defaultCode?: string | null;
}

export type ViewDef =
  | { kind: 'grid' }
  | {
      kind: 'tree';
      parentField: string;
      labelField: string;
      subtitleField?: string;
      badgeField?: string;
      source:
        | { type: 'flat' }
        | {
            type: 'chart';
            endpoint: string;
            rootsFrom: 'list';
            depth: number;
            childrenField: string;
            nodeField: string;
          };
    }
  | {
      kind: 'map';
      boundaryField: string;
      levels: MapLevelDef[];
      aggregate: 'count' | 'sum';
      valueField?: string;
      /**
       * Un nivel con una sola delimitación se salta (Q-10, D-R30). Por omisión `true`; el normalizador
       * rellena el valor y la configuración puede ponerlo a `false` para conservar ese nivel.
       */
      skipSingle?: boolean;
    };

export interface HelpStepDef {
  /** ancla data-kx-help o selector */
  target: string;
  title: Text;
  body: Text;
}

export interface ConfigWarning {
  path: string;
  message: string;
  severity: 'info' | 'warn';
}

export interface EntityConfig {
  id: string;
  name: Text;
  namePlural: Text;
  description?: Text;
  icon?: string;
  tone?: Tone;
  endpoint: string;
  apiVersioned: boolean;
  primaryKey: string;
  displayField: string;
  editable: boolean;
  saveMode: 'optimistic' | 'batch' | 'form';
  presentation: 'panel' | 'page';
  permissions?: { read?: string; write?: string };
  dataSource: {
    pagination: 'server' | 'client';
    listStyle: 'maintenance' | 'number-size';
    baseFilter: Record<string, unknown>;
    companyFilterField?: string;
  };
  columns: ColumnDef[];
  filters: FilterDef[];
  form: { sections: SectionDef[]; create: boolean; edit: boolean } | null;
  detail: { sections: SectionDef[]; childGrids: ChildGridDef[] };
  views: ViewDef[];
  defaultView: 'grid' | 'tree' | 'map';
  commands: CommandDef[];
  help: HelpStepDef[];
  warnings: ConfigWarning[];
}

export interface MenuNode {
  id: string;
  label: Text;
  icon?: string;
  entity?: string;
  children?: MenuNode[];
  visibleWhen?: VisibilityRule;
}

export type MapSourceDef =
  | { type: 'geojson'; url: string; schematic?: boolean }
  | { type: 'pmtiles'; url: string; layer: string; schematic?: boolean };

export interface AppManifest {
  schemaVersion: 1;
  entities: Record<string, EntityConfig>;
  menu: MenuNode[];
  locales: string[];
  defaultLocale: string;
  theme: { origin: string; default: string; available: string[] };
  map: { sources: Record<string, MapSourceDef> };
  api: { baseUrl: string; apiVersion: string };
}
