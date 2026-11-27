// Contextos de React compartidos (PLAN §4.2). Se crean aquí para que productores y consumidores
// de olas distintas o paralelas no dependan del orden de escritura.
import { createContext } from 'react';
import type { AppManifest } from './config';
import type { EntityApi } from './api';
import type { Session } from './auth';
import type { ThemeApi } from './theme';
import type { HelpApi } from './help';

export const ManifestContext = createContext<AppManifest | null>(null);
export const ApiContext = createContext<EntityApi | null>(null);
export const SessionContext = createContext<Session | null>(null);
/** Sin proveedor: iconos 'outline'. */
export const ThemeContext = createContext<ThemeApi | null>(null);
/** Sin proveedor: sin botón de ayuda. */
export const HelpContext = createContext<HelpApi | null>(null);

export interface KarpoAppProps {
  manifestUrl?: string;
  manifest?: AppManifest;
  apiBaseUrl?: string;
  themeOrigin?: string;
  locale?: string;
  router?: 'browser' | 'hash' | 'memory';
  basename?: string;
  initialPath?: string;
}
