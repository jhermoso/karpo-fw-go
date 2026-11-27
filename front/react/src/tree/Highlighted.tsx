import type { FieldDef, Row, TextResolver } from '../contracts';
import { FieldView } from '../fields';
import { plainFieldText } from './fields';
import { splitMatch } from './model';

/** Texto con la coincidencia de la búsqueda resaltada; en `<bdi>` porque los nombres vienen de los datos (M-12). */
export function Highlighted({ text, query }: { text: string; query: string }) {
  const parts = splitMatch(text, query);
  if (!parts) return <bdi>{text}</bdi>;
  return (
    <bdi>
      {parts[0]}
      <mark data-kx-part="match">{parts[1]}</mark>
      {parts[2]}
    </bdi>
  );
}

export interface NodeTextProps {
  row: Row;
  def: FieldDef;
  locale: string;
  resolve: TextResolver;
  query: string;
  /** Escribe siempre el texto plano (la etiqueta); si no, sólo cuando coincide con la búsqueda. */
  plain?: boolean;
}

/**
 * Un campo de un nodo. La etiqueta se escribe como texto; el subtítulo conserva su presentación (insignia,
 * formato) salvo cuando la búsqueda lo encuentra: entonces se escribe como texto con la coincidencia
 * resaltada, para que se vea por qué ese nodo casa (la búsqueda busca lo que se ve).
 */
export function NodeText({ row, def, locale, resolve, query, plain = false }: NodeTextProps) {
  const text = plainFieldText(row, def, locale, resolve);
  if (text !== '' && def.kind !== 'badge' && (plain || splitMatch(text, query))) return <Highlighted text={text} query={query} />;
  return <FieldView value={row[def.field]} def={def} row={row} />;
}
