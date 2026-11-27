// Contrato de la ayuda guiada (PLAN §4.2, §4.8).
// Ancla: atributo data-kx-help="<ns>.<nombre>"; textos en el espacio <ns>, claves
// help.<nombre>.title y help.<nombre>.body. Un paso sin elemento visible se omite.
import type { HelpStepDef } from './config';

export interface HelpApi {
  register(screen: string, steps: HelpStepDef[]): () => void;
  start(screen?: string): void;
}
