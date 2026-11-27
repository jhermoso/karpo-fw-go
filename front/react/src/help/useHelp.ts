import { useContext, useEffect, useMemo } from 'react';
import { HelpContext, type HelpApi, type HelpStepDef } from '../contracts';

const INERT_HELP: HelpApi = {
  register: () => () => undefined,
  start: () => undefined,
};

/**
 * Registra los pasos de la pantalla mientras esté montada y devuelve la API de ayuda.
 * Los pasos pueden venir de la configuración (`config.help`); sin ellos el recorrido sale de las
 * anclas `data-kx-help` visibles. Sin `HelpProvider` devuelve una API inerte.
 */
export function useHelp(screen: string, steps?: HelpStepDef[]): HelpApi {
  const api = useContext(HelpContext) ?? INERT_HELP;
  // Identidad por contenido: `steps` suele llegar como un array nuevo en cada render.
  const signature = JSON.stringify(steps ?? []);
  const stable = useMemo(() => JSON.parse(signature) as HelpStepDef[], [signature]);
  useEffect(() => api.register(screen, stable), [api, screen, stable]);
  return api;
}
