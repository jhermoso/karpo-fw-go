import { Fragment, type PropsWithChildren } from 'react';
import { displayKeys, type Platform } from './shortcut-keys';

/** Tecla: `surface.layer.level3`, `edge.subtle` y la tipografía monoespaciada del tema. */
export function Kbd({ children }: PropsWithChildren) {
  return (
    <kbd data-kx-ui="kbd" data-th-format="code">
      {children}
    </kbd>
  );
}

export interface ShortcutKeysProps {
  /** Combinación en el formato del registro (`Mod+K`, `Shift+?`). */
  keys: string;
  platform?: Platform;
}

/** Pinta una combinación del registro de atajos como una `Kbd` por tecla. */
export function ShortcutKeys({ keys, platform }: ShortcutKeysProps) {
  const parts = displayKeys(keys, platform);
  return (
    <span data-kx-ui="shortcut-keys">
      {parts.map((part, index) => (
        <Fragment key={`${part}-${index}`}>
          <Kbd>{part}</Kbd>
        </Fragment>
      ))}
    </span>
  );
}
