// Pegado y copia desde hojas de cálculo (TSV): análisis de celdas con la coerción de cada tipo
// (fechas por idioma, decimales con coma, `select` por etiqueta o clave) y validación celda a celda.
import type { FieldDef, TextResolver } from '../contracts';
import { getCodec } from './codecs';
import { getFieldType } from './registry';

/** Texto TSV a matriz de celdas. Entiende las comillas de Excel (`"a\tb"`, `""` como comilla). */
export function parseTsv(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = '';
  let quoted = false;
  let cellStart = true;
  for (let index = 0; index < text.length; index += 1) {
    const char = text[index] as string;
    if (quoted) {
      if (char === '"') {
        if (text[index + 1] === '"') {
          cell += '"';
          index += 1;
        } else {
          quoted = false;
        }
      } else {
        cell += char;
      }
      continue;
    }
    if (char === '"' && cellStart) {
      quoted = true;
      cellStart = false;
    } else if (char === '\t') {
      row.push(cell);
      cell = '';
      cellStart = true;
    } else if (char === '\n' || char === '\r') {
      if (char === '\r' && text[index + 1] === '\n') index += 1;
      row.push(cell);
      rows.push(row);
      row = [];
      cell = '';
      cellStart = true;
    } else {
      cell += char;
      cellStart = false;
    }
  }
  // Última fila: si el texto acaba en salto de línea (lo normal al copiar de Excel), no hay fila extra.
  if (cell !== '' || row.length > 0) {
    row.push(cell);
    rows.push(row);
  }
  return rows;
}

export interface PasteCell {
  /** Texto de origen. */
  text: string;
  ok: boolean;
  /** Valor ya analizado y validado (si `ok`). */
  value?: unknown;
  /** Primer código de error: `format`, `required`, `max`… */
  code?: string;
}

export interface PasteResult {
  rows: PasteCell[][];
  /** Celdas con error (para la vista previa). */
  errors: number;
}

/**
 * Analiza un texto pegado contra las columnas destino (en orden). Las celdas sobrantes por la
 * derecha se descartan. Cada celda lleva su valor o su código de error para marcarla en la vista previa.
 */
export function parsePaste(
  text: string,
  defs: readonly FieldDef[],
  locale: string,
  resolve?: TextResolver,
): PasteResult {
  const matrix = parseTsv(text);
  let errors = 0;
  const rows = matrix.map((cells) =>
    defs.map((def, column): PasteCell => {
      const source = cells[column] ?? '';
      const type = getFieldType(def.kind);
      const parsed = type.parse(source, def, locale, resolve);
      if (!parsed.ok) {
        errors += 1;
        return { text: source, ok: false, code: parsed.code };
      }
      const [code] = type.validate(parsed.value, def);
      if (code) {
        errors += 1;
        return { text: source, ok: false, code };
      }
      return { text: source, ok: true, value: parsed.value };
    }),
  );
  return { rows, errors };
}

function quoteCell(text: string): string {
  return /["\t\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

/** Valores a TSV (copiar): cada celda en su forma editable, de modo que se pueda volver a pegar. */
export function toTsv(
  rows: readonly (readonly unknown[])[],
  defs: readonly FieldDef[],
  locale: string,
  resolve?: TextResolver,
): string {
  return rows
    .map((row) =>
      row
        .map((value, column) => {
          const def = defs[column];
          return quoteCell(def ? getCodec(def.kind).editText(value, def, locale, resolve) : String(value ?? ''));
        })
        .join('\t'),
    )
    .join('\n');
}
