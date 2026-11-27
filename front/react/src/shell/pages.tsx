// Páginas del marco: inicio, 403, no encontrada y aviso de lista prohibida.
import { useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useText } from '../config';
import { useSession } from '../auth';
import { Action, Feedback, Icon } from '../ui';
import { useManifest } from './hooks';
import { canReadEntity } from './menu';

export function HomePage() {
  const { t } = useTranslation('core');
  const manifest = useManifest();
  const session = useSession();
  const resolve = useText();
  const entities = Object.values(manifest.entities).filter((config) => canReadEntity(config, session));
  const menuEntities = new Set<string>();
  const collect = (nodes: typeof manifest.menu): void => {
    for (const node of nodes) {
      if (node.entity) menuEntities.add(node.entity);
      if (node.children) collect(node.children);
    }
  };
  collect(manifest.menu);
  const listed = entities.filter((config) => menuEntities.size === 0 || menuEntities.has(config.id));
  return (
    <section data-kx-page="home" aria-labelledby="kx-home-title">
      <h1 id="kx-home-title">{t('home.title')}</h1>
      <p>{t('home.body')}</p>
      {listed.length === 0 ? (
        <Feedback tone="info">{t('home.empty')}</Feedback>
      ) : (
        <ul data-kx-part="tiles" aria-label={t('home.entities')}>
          {listed.map((config) => (
            <li key={config.id}>
              <Link to={`/${encodeURIComponent(config.id)}`} data-kx-part="tile">
                <Icon name={config.icon ?? 'folder'} />
                <span>{resolve(config.namePlural)}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export function ForbiddenPage({ entity }: { entity?: string }) {
  const { t } = useTranslation('core');
  const manifest = useManifest();
  const resolve = useText();
  const config = entity ? manifest.entities[entity] : undefined;
  return (
    <section data-kx-page="forbidden" aria-labelledby="kx-forbidden-title">
      <Icon name="lock" />
      <h1 id="kx-forbidden-title">{t('forbidden.title')}</h1>
      <p>
        {config ? t('forbidden.entity', { name: resolve(config.namePlural) }) : t('forbidden.body')}
      </p>
      <Link to="/" data-th="action" data-th-kind="primary">
        {t('forbidden.home')}
      </Link>
    </section>
  );
}

export function NotFoundPage() {
  const { t } = useTranslation('core');
  return (
    <section data-kx-page="not-found" aria-labelledby="kx-not-found-title">
      <Icon name="compass" />
      <h1 id="kx-not-found-title">{t('notFound.title')}</h1>
      <p>{t('notFound.body')}</p>
      <Link to="/" data-th="action" data-th-kind="primary">
        {t('notFound.home')}
      </Link>
    </section>
  );
}

/** Aviso que sustituye a la lista cuando el servidor responde 403: el usuario sabe qué ha pasado y puede reintentar. */
export function ForbiddenNotice({ entity }: { entity: string }) {
  const { t } = useTranslation('core');
  const queryClient = useQueryClient();
  return (
    <div data-kx-part="forbidden-notice">
      <Feedback tone="error" role="alert">
        <span data-kx-part="text">
          <strong>{t('listForbidden.title')}</strong> {t('listForbidden.body')}
        </span>
        <Action kind="secondary" icon="refresh" onClick={() => void queryClient.resetQueries({ queryKey: ['entity', entity, 'list'] })}>
          {t('listForbidden.retry')}
        </Action>
      </Feedback>
    </div>
  );
}
