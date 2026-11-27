// Panel de atajos (Shift+?): se genera del registro de atajos, no está escrito a mano.
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { ShortcutDef } from '../contracts';
import { Dialog, ShortcutKeys, useShortcutRegistry } from '../ui';

const SCOPE_ORDER: readonly ShortcutDef['scope'][] = ['global', 'detail', 'grid', 'tree', 'map'];

export interface ShortcutsPanelProps {
  open: boolean;
  onOpenChange(open: boolean): void;
}

export function ShortcutsPanel({ open, onOpenChange }: ShortcutsPanelProps) {
  const { t } = useTranslation('core');
  const registry = useShortcutRegistry();
  const groups = useMemo(() => {
    const byScope = new Map<ShortcutDef['scope'], ShortcutDef[]>();
    for (const def of registry) {
      const list = byScope.get(def.scope) ?? [];
      list.push(def);
      byScope.set(def.scope, list);
    }
    return SCOPE_ORDER.filter((scope) => byScope.has(scope)).map((scope) => ({
      scope,
      defs: byScope.get(scope) ?? [],
    }));
  }, [registry]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={t('shortcuts.title')} description={t('shortcuts.intro')} size="large">
      {groups.length === 0 ? (
        <p>{t('shortcuts.empty')}</p>
      ) : (
        <div data-kx-part="shortcut-groups">
          {groups.map(({ scope, defs }) => (
            <section key={scope} aria-labelledby={`kx-shortcuts-${scope}`} data-kx-part="shortcut-group">
              <h3 id={`kx-shortcuts-${scope}`}>{t(`shortcuts.scopes.${scope}`)}</h3>
              <ul>
                {defs.map((def) => (
                  <li key={def.id}>
                    <span>{t(def.labelKey, { defaultValue: def.labelKey })}</span>
                    <ShortcutKeys keys={def.keys} />
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
    </Dialog>
  );
}
