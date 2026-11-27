// Registro de tipos de campo (PLAN §4.8). Propiedad de la tarea 3.
// Los ayudantes de números y fechas (decimal.ts, dates.ts, values.ts) se importan por su fichero:
// no se reexportan aquí para no chocar con los nombres de `api` e `i18n`.
import './fields.css';

export { FIELD_KINDS, fieldTypes, getFieldType, registerFieldType } from './registry';
export { FieldView } from './FieldView';
export { FieldEditor } from './FieldEditor';
export { EmptyValue } from './EmptyValue';
export { EMPTY_MARK, emptyText } from './values';
export type { CommitMove, EditorProps } from './editor-props';

export { getCodec } from './codecs';
export type { FieldCodec, ParseResult } from './codecs';

export { parsePaste, parseTsv, toTsv } from './paste';
export type { PasteCell, PasteResult } from './paste';

export { useFieldErrorMessage, useFieldText } from './hooks';
export type { FieldText } from './hooks';
export { clearLookupCache, searchLookup } from './lookup-cache';
export type { LookupOption } from './lookup-cache';
