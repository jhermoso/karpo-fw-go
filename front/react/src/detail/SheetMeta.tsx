// Tarjeta «Registro» de la ficha (M-4, D-R40): identificador corto con botón de copiar, versión y, sólo si el
// servidor los envía en la fila, quién y cuándo lo creó y lo modificó. No lee nada de la configuración: es lo
// que cualquier fila de la API de mantenimiento trae (`id`, `version`) más el sello de auditoría cuando exista
// (petición B-27 al backend). Lo que no viene, no se pinta.
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import type { Row } from '../contracts';
import { useFormatters } from '../i18n';
import { Action, useToast } from '../ui';

export interface SheetMetaProps {
  row: Row;
}

/** Hasta esta longitud el identificador se enseña entero; más largo, se acorta al principio. */
const SHORT_ID_LENGTH = 8;

/** Identificador corto: el propio id si cabe, o sus primeros caracteres y una elipsis. */
export function shortId(id: string): string {
  return id.length <= SHORT_ID_LENGTH + 2 ? id : `${id.slice(0, SHORT_ID_LENGTH)}…`;
}

/** Quién: texto o número tal cual; un objeto con `name`/`displayName`/`username`/`id`; si no, nada. */
export function actorText(value: unknown): string {
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  if (typeof value === 'object' && value !== null) {
    const record = value as Record<string, unknown>;
    for (const key of ['name', 'displayName', 'username', 'id']) {
      const found = record[key];
      if (typeof found === 'string' && found.trim() !== '') return found.trim();
    }
  }
  return '';
}

/** Instante de la fila: cadena ISO o milisegundos; cualquier otra cosa no es una fecha. */
function stampValue(value: unknown): string | number | undefined {
  if (typeof value === 'string' && value.trim() !== '') return value;
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  return undefined;
}

export function SheetMeta({ row }: SheetMetaProps) {
  const { t } = useTranslation('detail');
  const format = useFormatters();
  const toast = useToast();
  const headingId = useId();

  const id = String(row.id);
  const version = typeof row.version === 'number' ? row.version : undefined;

  const when = (value: unknown): string | undefined => {
    const stamp = stampValue(value);
    if (stamp === undefined) return undefined;
    return format.dateTime(stamp) || String(stamp);
  };
  const created = [when(row.createdAt), actorText(row.createdBy)];
  const updated = [when(row.updatedAt), actorText(row.updatedBy)];

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(id);
      toast.show({ tone: 'success', message: t('meta.copied') });
    } catch {
      toast.show({ tone: 'error', message: t('meta.copyFailed') });
    }
  };

  return (
    <section data-th="section" data-kx-part="meta" aria-labelledby={headingId} data-kx-help="detail.meta">
      <header data-kx-part="section-header">
        <h3 id={headingId}>{t('meta.title')}</h3>
      </header>
      <dl data-kx-part="facts">
        <div data-kx-part="fact" data-kx-fact="id">
          <dt>{t('meta.id')}</dt>
          <dd>
            <bdi dir="ltr" title={id} data-kx-part="id-value">
              {shortId(id)}
            </bdi>
            <Action kind="quiet" icon="copy" aria-label={t('meta.copy')} title={t('meta.copy')} onClick={() => void copy()} />
          </dd>
        </div>
        {version !== undefined ? (
          <div data-kx-part="fact" data-kx-fact="version">
            <dt>{t('meta.version')}</dt>
            <dd>
              <bdi>{format.number(version)}</bdi>
            </dd>
          </div>
        ) : null}
        <Stamp kind="created" label={t('meta.created')} at={created[0]} by={created[1]} />
        <Stamp kind="updated" label={t('meta.updated')} at={updated[0]} by={updated[1]} />
      </dl>
    </section>
  );
}

/** «Creado» o «Modificado»: la fecha y, si viene, quién. Sin ninguna de las dos no se pinta nada. */
function Stamp({ kind, label, at, by }: { kind: 'created' | 'updated'; label: string; at: string | undefined; by: string | undefined }) {
  const { t } = useTranslation('detail');
  if (at === undefined && !by) return null;
  return (
    <div data-kx-part="fact" data-kx-fact={kind}>
      <dt>{label}</dt>
      <dd>
        {at !== undefined ? <bdi>{at}</bdi> : null}
        {at !== undefined && by ? ' · ' : null}
        {by ? <bdi title={t('meta.by', { name: by })}>{by}</bdi> : null}
      </dd>
    </div>
  );
}
