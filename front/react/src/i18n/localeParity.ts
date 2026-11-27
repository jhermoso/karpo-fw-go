// Comparación de ficheros de idioma (PLAN §6): «sin claves sin traducir». La usa la prueba de contrato
// `locales.contract.test.ts`, que recorre TODO `src/locales`; vive aquí para poder probarla con datos.

export interface ParityIssue {
  /** `es/grid.json`, `en/help.json`... */
  file: string;
  kind: 'missing-file' | 'missing-key' | 'extra-key' | 'placeholders' | 'empty';
  key?: string;
  detail: string;
}

/** Aplana un árbol JSON a `clave.anidada → texto`. Las matrices cuentan como hojas por índice. */
export function flattenLocale(tree: unknown, prefix = ''): Map<string, string> {
  const out = new Map<string, string>();
  const visit = (node: unknown, path: string) => {
    if (typeof node === 'string') {
      out.set(path, node);
    } else if (Array.isArray(node)) {
      node.forEach((item, index) => visit(item, `${path}[${index}]`));
    } else if (typeof node === 'object' && node !== null) {
      for (const [key, value] of Object.entries(node)) visit(value, path === '' ? key : `${path}.${key}`);
    } else if (path !== '') {
      out.set(path, String(node));
    }
  };
  visit(tree, prefix);
  return out;
}

const PLACEHOLDER = /\{\{\s*([^}\s,]+)[^}]*\}\}/g;

export function placeholders(text: string): string[] {
  return [...text.matchAll(PLACEHOLDER)].map((m) => m[1] ?? '').sort();
}

/** Categorías plurales de CLDR: el árabe usa las seis; `es` y `en`, sólo `one` y `other`. */
const PLURAL_SUFFIX = /_(zero|one|two|few|many|other)$/;
const EXTRA_PLURAL = /^(.*)_(zero|two|few|many)$/;

/**
 * Marcadores que se comparan. En una clave plural el número puede ir con palabras (`one` → «صف واحد»,
 * `two` → «صفان»), así que ahí `count` no se exige; cualquier otro marcador sí.
 */
function comparable(key: string, text: string): string {
  const all = placeholders(text);
  return (PLURAL_SUFFIX.test(key) ? all.filter((name) => name !== 'count') : all).join();
}

export type LocaleFiles = Record<string, Record<string, unknown>>; // idioma → espacio → contenido

/**
 * Compara cada idioma con el de referencia. Los de `required` (por omisión `es` y `en`) deben tener
 * exactamente las mismas claves y ficheros; cualquier otro idioma puede ir incompleto (cae a otro) pero no
 * puede tener claves que `es` no tiene. Los marcadores `{{x}}` deben coincidir y ningún texto puede estar
 * vacío. Una clave plural puede traer, además de `_one` y `_other`, las categorías `_zero`, `_two`, `_few` y
 * `_many` de su idioma (el árabe las usa) si la referencia tiene `_other`: sus marcadores se comparan con
 * los de `_other`.
 */
export function compareLocales(
  files: LocaleFiles,
  reference = 'es',
  required: readonly string[] = ['es', 'en'],
): ParityIssue[] {
  const issues: ParityIssue[] = [];
  const base = files[reference] ?? {};
  for (const lng of required) {
    if (!files[lng]) issues.push({ file: `${lng}/`, kind: 'missing-file', detail: `falta el idioma ${lng}` });
  }

  for (const [lng, namespaces] of Object.entries(files)) {
    const strict = required.includes(lng);
    const all = new Set([...Object.keys(base), ...Object.keys(namespaces)]);
    for (const ns of all) {
      const file = `${lng}/${ns}.json`;
      const mine = namespaces[ns];
      const reference_ = base[ns];
      if (lng === reference) {
        // el de referencia manda; sus huecos se detectan al comparar el resto
      } else if (mine === undefined) {
        if (strict) issues.push({ file, kind: 'missing-file', detail: `falta la pareja ${reference}/${ns}.json en ${lng}` });
        continue;
      } else if (reference_ === undefined) {
        issues.push({ file, kind: 'missing-file', detail: `falta la pareja ${reference}/${ns}.json (existe sólo en ${lng})` });
        continue;
      }
      const mineFlat = flattenLocale(mine);
      for (const [key, text] of mineFlat) {
        if (text.trim() === '') issues.push({ file, kind: 'empty', key, detail: `«${key}» está vacío` });
      }
      if (lng === reference || reference_ === undefined) continue;
      const referenceFlat = flattenLocale(reference_);
      for (const [key, text] of referenceFlat) {
        const other = mineFlat.get(key);
        if (other === undefined) {
          if (strict) issues.push({ file, kind: 'missing-key', key, detail: `«${key}» está en ${reference} y no en ${lng}` });
        } else if (comparable(key, text) !== comparable(key, other)) {
          issues.push({
            file,
            kind: 'placeholders',
            key,
            detail: `«${key}»: marcadores ${JSON.stringify(placeholders(text))} (${reference}) ≠ ${JSON.stringify(placeholders(other))} (${lng})`,
          });
        }
      }
      for (const key of mineFlat.keys()) {
        if (!referenceFlat.has(key)) {
          const extra = EXTRA_PLURAL.exec(key);
          const sibling = extra ? referenceFlat.get(`${extra[1]}_other`) : undefined;
          if (sibling !== undefined) {
            if (comparable(key, sibling) !== comparable(key, mineFlat.get(key) ?? '')) {
              issues.push({ file, kind: 'placeholders', key, detail: `«${key}»: marcadores distintos de los de «${extra?.[1]}_other»` });
            }
            continue;
          }
          issues.push({ file, kind: 'extra-key', key, detail: `«${key}» está en ${lng} y no en ${reference}` });
        }
      }
    }
  }
  return issues;
}
