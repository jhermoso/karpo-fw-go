// Tarjeta «Relaciones» de la ficha (M-4, D-R40): lo que la configuración ya dice de los vínculos del registro,
// sin código por pantalla. Cada campo `lookup` con valor es un enlace a la ficha de destino (sólo si el usuario
// puede leer esa entidad; si no, el valor queda como texto). Si la entidad tiene vista de árbol (`parentField`)
// se añade «Dentro de» (el padre, salvo que ya salga como campo `lookup`), el recuento de hijos (sólo si la fila
// lo trae: la API de mantenimiento no lo da hoy) y un enlace a la vista de árbol centrada en el registro.
// No importa nada de `src/tree`: la vista de árbol se identifica por la configuración (`ViewDef`).
import { useContext, useId, type MouseEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useHref, useInRouterContext } from 'react-router';
import { useEntityRecord } from '../api';
import { useSession } from '../auth';
import { useText } from '../config';
import { ManifestContext, type EntityConfig, type EntityRoute, type FieldDef, type Row } from '../contracts';
import { useLookupLabel } from '../fields/hooks';
import { Icon, Skeleton } from '../ui';
import { canReadEntity } from '../shell/menu';
import { buildEntityTarget, targetToString } from '../shell/route';
import { getIn } from './form-model';

export interface SheetRelationsProps {
  config: EntityConfig;
  row: Row;
  /** Campos de las secciones de la ficha (los `lookup` con valor son relaciones). */
  fields: readonly FieldDef[];
  /** Navegación por ruta de entidad (la de la pantalla): un clic normal en un enlace la usa. */
  navigate(to: EntityRoute): void;
}

/** Identificador al que apunta un valor de clave ajena: texto, número u objeto `{ id }`. */
export function relationId(value: unknown): string | undefined {
  if (typeof value === 'object' && value !== null) return relationId((value as Record<string, unknown>).id);
  if (typeof value === 'string') return value.trim() === '' ? undefined : value;
  if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  return undefined;
}

/** Recuento de hijos directos si la fila lo trae (`childCount`, `childrenCount` o `childrenTotal`). */
export function childCountOf(row: Row): number | undefined {
  for (const key of ['childCount', 'childrenCount', 'childrenTotal']) {
    const value = row[key];
    if (typeof value === 'number' && Number.isInteger(value) && value >= 0) return value;
  }
  return undefined;
}

/** Los campos que son relaciones: `lookup` con valor en la fila y destino conocido. */
export function relationFields(fields: readonly FieldDef[], row: Row): { def: FieldDef; id: string }[] {
  const out: { def: FieldDef; id: string }[] = [];
  const seen = new Set<string>();
  for (const def of fields) {
    if (def.kind !== 'lookup' || !def.lookup || seen.has(def.field)) continue;
    const id = relationId(getIn(row, def.field));
    if (id === undefined) continue;
    seen.add(def.field);
    out.push({ def, id });
  }
  return out;
}

export function SheetRelations({ config, row, fields, navigate }: SheetRelationsProps) {
  const { t } = useTranslation('detail');
  const headingId = useId();
  const treeView = config.views.find((view) => view.kind === 'tree');
  const parentField = treeView?.kind === 'tree' ? treeView.parentField : undefined;
  const parentId = parentField === undefined ? undefined : relationId(getIn(row, parentField));
  const relations = relationFields(fields, row);
  // «Dentro de» no repite al padre cuando éste ya sale como campo `lookup` de la propia entidad.
  const parentShown = parentField !== undefined && relations.some(({ def }) => def.field === parentField);
  const children = treeView ? childCountOf(row) : undefined;

  if (relations.length === 0 && !treeView) return null;

  return (
    <section data-th="section" data-kx-part="relations" aria-labelledby={headingId} data-kx-help="detail.relations">
      <header data-kx-part="section-header">
        <h3 id={headingId}>{t('relations.title')}</h3>
        {treeView ? (
          <RelationAnchor
            route={{ entity: config.id, view: 'tree', params: { focus: row.id } }}
            navigate={navigate}
            label={t('relations.treeLabel', { name: String(getIn(row, config.displayField) ?? row.id) })}
          >
            <Icon name="tree-structure" />
            <span>{t('relations.tree')}</span>
          </RelationAnchor>
        ) : null}
      </header>
      <dl data-kx-part="facts">
        {relations.map(({ def, id }) => (
          <LookupRelation key={def.field} def={def} id={id} navigate={navigate} />
        ))}
        {treeView && parentId !== undefined && !parentShown ? <ParentRelation config={config} parentId={parentId} navigate={navigate} /> : null}
        {children !== undefined ? (
          <div data-kx-part="fact" data-kx-fact="children">
            <dt>{t('relations.contains')}</dt>
            <dd>
              <bdi>{t('relations.children', { count: children })}</bdi>
            </dd>
          </div>
        ) : null}
      </dl>
    </section>
  );
}

/** Un campo `lookup` con valor: enlace a la ficha de destino si hay permiso de lectura; si no, texto. */
function LookupRelation({ def, id, navigate }: { def: FieldDef; id: string; navigate(to: EntityRoute): void }) {
  const { t } = useTranslation('detail');
  const resolve = useText();
  const session = useSession();
  const manifest = useContext(ManifestContext);
  const { label, loading } = useLookupLabel(def, id);
  const lookup = def.lookup;
  const target = lookup ? manifest?.entities[lookup.entity] : undefined;
  // La ruta de una ficha es su `id`: con otro `valueField` el valor no sirve de dirección.
  const linkable = Boolean(lookup && target && lookup.valueField === 'id' && canReadEntity(target, session));
  const text = label ?? id;
  return (
    <div data-kx-part="fact" data-kx-fact="lookup" data-kx-field={def.field}>
      <dt>{resolve(def.label)}</dt>
      <dd>
        {loading ? (
          <Skeleton />
        ) : linkable && lookup && target ? (
          <RelationAnchor
            route={{ entity: lookup.entity, id, mode: 'read' }}
            navigate={navigate}
            label={t('relations.open', { entity: resolve(target.name), name: text })}
          >
            <bdi>{text}</bdi>
          </RelationAnchor>
        ) : (
          <bdi data-kx-part="plain">{text}</bdi>
        )}
      </dd>
    </div>
  );
}

/** «Dentro de»: el padre en la jerarquía de la propia entidad (se lee su `displayField`). */
function ParentRelation({ config, parentId, navigate }: { config: EntityConfig; parentId: string; navigate(to: EntityRoute): void }) {
  const { t } = useTranslation('detail');
  const resolve = useText();
  const query = useEntityRecord(config, parentId);
  const name = query.data ? String(getIn(query.data, config.displayField) ?? parentId) : undefined;
  return (
    <div data-kx-part="fact" data-kx-fact="parent">
      <dt>{t('relations.within')}</dt>
      <dd>
        {query.isPending ? (
          <Skeleton />
        ) : (
          <RelationAnchor
            route={{ entity: config.id, id: parentId, mode: 'read' }}
            navigate={navigate}
            label={t('relations.open', { entity: resolve(config.name), name: name ?? parentId })}
          >
            <bdi>{name ?? parentId}</bdi>
          </RelationAnchor>
        )}
      </dd>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------
// Enlaces
// ---------------------------------------------------------------------------------------------

interface AnchorProps {
  route: EntityRoute;
  navigate(to: EntityRoute): void;
  /** Nombre accesible (contiene el texto visible). */
  label: string;
  children: ReactNode;
}

/** Un clic normal navega por la ruta de entidad; con Ctrl, Cmd, Mayús o el botón central, el navegador abre la dirección. */
function followOnPlainClick(event: MouseEvent<HTMLAnchorElement>, go: () => void): void {
  if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  go();
}

/**
 * Enlace a una ficha o a una vista. Es un `<a href>` de verdad (se puede copiar, abrir en otra pestaña y lo
 * anuncia como enlace): con router, su `href` es el del router (base y modo hash incluidos); sin él (pruebas
 * de componentes), la dirección relativa.
 */
function RelationAnchor(props: AnchorProps) {
  return useInRouterContext() ? <RoutedAnchor {...props} /> : <PlainAnchor {...props} />;
}

function target(route: EntityRoute): string {
  return targetToString(buildEntityTarget(route));
}

function RoutedAnchor({ route, navigate, label, children }: AnchorProps) {
  const to = target(route);
  const href = useHref(to);
  return (
    <a data-th="reference" data-kx-part="relation-link" href={href} aria-label={label} onClick={(event) => followOnPlainClick(event, () => navigate(route))}>
      {children}
    </a>
  );
}

function PlainAnchor({ route, navigate, label, children }: AnchorProps) {
  return (
    <a data-th="reference" data-kx-part="relation-link" href={target(route)} aria-label={label} onClick={(event) => followOnPlainClick(event, () => navigate(route))}>
      {children}
    </a>
  );
}
