// Variables de disposición `--kx-*` en `style` (D-R12): React sólo las admite con este tipo.
// ESLint ya limita `style={{…}}` a estas claves y a transform/inset/width/height.
import 'react';

declare module 'react' {
  interface CSSProperties {
    [key: `--kx-${string}`]: string | number | undefined;
  }
}
