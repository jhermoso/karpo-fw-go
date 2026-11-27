// Esquema zod PERMISIVO del EntityConfig de Angular (A1 §3). Describe los tipos de las claves que
// se leen y deja pasar todo lo demás: lo que no case produce un ConfigWarning, no un error.
// Sólo `id` y `apiEndpoint` son imprescindibles (sin ellos la entidad no se puede usar).
// Usa `zod/mini` (M-15.3, D-R26): mismo paquete y misma licencia que `zod`, sin los métodos encadenables,
// así el árbol de módulos que entra con la configuración pesa menos.
import * as z from 'zod/mini';
import type { ConfigWarning } from '../contracts';

const str = z.string();
const flag = z.boolean();
const num = z.number();
/** `x?` de zod clásico: `z.optional(x)`. */
const opt = z.optional;
/** Texto: cadena u objeto por idioma. */
const text = z.union([z.string(), z.record(z.string(), z.unknown())]);
/** Regla de visibilidad: expresión o cláusulas. */
const rule = z.union([z.string(), z.looseObject({})]);
const option = z.union([z.string(), z.number(), z.boolean(), z.looseObject({})]);

const field = z.looseObject({
  field: opt(str),
  dataField: opt(str),
  label: opt(text),
  header: opt(text),
  type: opt(str),
  required: opt(flag),
  editable: opt(flag),
  readonly: opt(flag),
  disabled: opt(flag),
  hidden: opt(flag),
  width: opt(z.union([num, str])),
  priority: opt(z.union([num, str])),
  hint: opt(text),
  placeholder: opt(text),
  options: opt(z.array(option)),
  enumValues: opt(z.array(z.looseObject({}))),
  badgeMap: opt(z.record(z.string(), z.unknown())),
  visibleWhen: opt(rule),
  min: opt(z.union([num, str])),
  max: opt(z.union([num, str])),
  minLength: opt(z.union([num, str])),
  maxLength: opt(z.union([num, str])),
  pattern: opt(str),
  sortable: opt(flag),
  lookupEntity: opt(str),
  lookupDisplayField: opt(str),
  lookupValueField: opt(str),
  lookupFilter: opt(z.record(z.string(), z.unknown())),
  validators: opt(z.array(z.looseObject({}))),
});

const section = z.looseObject({
  id: opt(str),
  title: opt(text),
  label: opt(text),
  columns: opt(z.union([num, str])),
  collapsible: opt(flag),
  collapsed: opt(flag),
  fields: opt(z.array(field)),
  visibleWhen: opt(rule),
});

const filter = z.looseObject({
  field: opt(str),
  serverField: opt(str),
  label: opt(text),
  type: opt(str),
  width: opt(z.union([num, str])),
  options: opt(z.array(option)),
  visibleWhen: opt(rule),
});

const rowAction = z.looseObject({
  action: opt(str),
  label: opt(text),
  icon: opt(str),
  color: opt(str),
  visibleWhen: opt(rule),
  confirm: opt(z.union([flag, text])),
  confirmMessage: opt(text),
});

const command = z.looseObject({
  id: opt(str),
  label: opt(text),
  surfaces: opt(z.array(str)),
  method: opt(str),
  endpoint: opt(str),
  sendVersion: opt(flag),
  prompt: opt(z.looseObject({ field: opt(str), required: opt(flag) })),
});

const childGrid = z.looseObject({
  id: opt(str),
  title: opt(text),
  label: opt(text),
  apiEndpoint: opt(str),
  foreignKey: opt(str),
  columns: opt(z.array(field)),
});

/** Esquema de la raíz de una entidad. */
export const entitySchema = z.looseObject({
  id: str,
  name: opt(text),
  namePlural: opt(text),
  description: opt(text),
  icon: opt(str),
  color: opt(str),
  apiEndpoint: str,
  apiVersioned: opt(flag),
  aggregateEndpoint: opt(str),
  editable: opt(flag),
  editType: opt(str),
  saveMode: opt(str),
  presentation: opt(str),
  permissions: opt(z.looseObject({ read: opt(str), write: opt(str) })),
  primaryKey: opt(str),
  displayField: opt(str),
  companyScoped: opt(flag),
  availableViews: opt(z.array(str)),
  defaultView: opt(str),
  searchable: opt(flag),
  searchFields: opt(z.array(str)),
  dataSource: opt(
    z.looseObject({
      type: opt(str),
      paginationStrategy: opt(str),
      baseFilter: opt(z.record(z.string(), z.unknown())),
      serverFiltering: opt(z.looseObject({ companyFilterField: opt(str) })),
    }),
  ),
  grid: opt(
    z.looseObject({
      columns: opt(z.array(field)),
      defaultSort: opt(z.looseObject({ field: opt(str), direction: opt(str) })),
      pageSize: opt(z.union([num, str])),
      pageSizeOptions: opt(z.array(z.union([num, str]))),
      selectable: opt(flag),
      selectionMode: opt(str),
      rowActions: opt(z.array(rowAction)),
    }),
  ),
  filters: opt(z.array(filter)),
  form: opt(
    z.looseObject({
      sections: opt(z.array(section)),
      groups: opt(z.array(section)),
      create: opt(flag),
      edit: opt(flag),
    }),
  ),
  detail: opt(
    z.looseObject({
      sections: opt(z.array(section)),
      childGrids: opt(z.array(childGrid)),
    }),
  ),
  commands: opt(z.array(command)),
  views: opt(z.array(z.looseObject({ kind: opt(str) }))),
  help: opt(z.union([z.array(z.looseObject({})), z.looseObject({})])),
  serverSortFields: opt(z.union([z.record(z.string(), z.string()), z.array(str)])),
});

function formatPath(path: readonly PropertyKey[]): string {
  let out = '';
  for (const part of path) {
    if (typeof part === 'number') out += `[${part}]`;
    else out += out ? `.${String(part)}` : String(part);
  }
  return out;
}

export interface ShapeReport {
  warnings: ConfigWarning[];
  /** Incidencias que impiden usar la entidad (`id` o `apiEndpoint` ausentes o no textuales). */
  fatal: ConfigWarning[];
}

const FATAL_PATHS = new Set(['id', 'apiEndpoint']);

/** Valida la forma del JSON con el esquema permisivo y la reparte en avisos y errores fatales. */
export function validateEntityShape(raw: unknown): ShapeReport {
  const result = entitySchema.safeParse(raw);
  const report: ShapeReport = { warnings: [], fatal: [] };
  if (result.success) return report;
  for (const issue of result.error.issues) {
    const path = formatPath(issue.path);
    const entry: ConfigWarning = {
      path: path || '$',
      message: `valor con tipo no válido (${issue.message})`,
      severity: 'warn',
    };
    if (FATAL_PATHS.has(path)) {
      report.fatal.push({ ...entry, message: `falta \`${path}\` o no es texto` });
    } else {
      report.warnings.push(entry);
    }
  }
  return report;
}
