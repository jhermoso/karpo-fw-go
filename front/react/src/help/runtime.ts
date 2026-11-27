// Contexto interno de la ayuda (no forma parte del contrato público HelpApi).
import { createContext } from 'react';

export interface HelpRuntime {
  /** Pantalla activa: la última registrada que sigue montada. */
  screen: string | null;
  /** ¿Hay un recorrido en marcha? */
  running: boolean;
  /** ¿El recorrido de la pantalla activa no se ha visto aún? */
  unseen: boolean;
  /** Lanza el recorrido; `launcher` es el elemento al que se devuelve el foco al cerrar. */
  start(screen?: string, launcher?: HTMLElement | null): void;
}

export const HelpRuntimeContext = createContext<HelpRuntime | null>(null);
