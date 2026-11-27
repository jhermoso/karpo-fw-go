// Valor vacío de un campo (D-R35): la marca única «—» (o el texto de la configuración si no es sólo guiones).
// La misma pieza la usan los visores de la rejilla y la ficha, para que se vea y se lea igual en las dos.
import type { FieldDef } from '../contracts';
import { useFieldText } from './hooks';
import { EMPTY_MARK, emptyText } from './values';

export function EmptyValue({ def }: { def: Pick<FieldDef, 'emptyValue'> }) {
  const { t } = useFieldText();
  const text = emptyText(def.emptyValue);
  // La marca es un adorno visual: el lector de pantalla oye «Sin valor». Un texto propio («sin fecha») ya dice algo.
  if (text !== EMPTY_MARK) {
    return (
      <span data-kx-ui="value" data-kx-empty="true">
        {text}
      </span>
    );
  }
  return (
    <span data-kx-ui="value" data-kx-empty="true">
      <span aria-hidden="true">{EMPTY_MARK}</span>
      <span data-kx-part="sr-only">{t('empty.noValue')}</span>
    </span>
  );
}
