// Ganchos de los campos: idioma y textos, etiqueta resuelta de un lookup y mensajes de validación.
import { useCallback, useContext, useEffect, useState } from 'react';
import { ApiContext, ManifestContext, type FieldDef, type Text } from '../contracts';
import { resolveText, translateWith } from '../config/text';
import { useUiI18n, type Translate } from '../ui/i18n';
import { cachedLookupLabel, resolveLookupLabel } from './lookup-cache';
import { isEmpty } from './values';

export interface FieldText {
  t: Translate;
  /** Idioma activo (BCP 47). */
  locale: string;
  /**
   * Resuelve un `Text` de la configuración con la misma cadena que `useText()` (B-2 b): clave en el idioma
   * activo, diccionario de la configuración (`registerConfigDictionary`), objeto por idioma y, al final, el literal.
   */
  resolve(text: Text | undefined): string;
}

export function useFieldText(): FieldText {
  const { t, locale, i18n } = useUiI18n('fields');
  // Una sola implementación de la resolución: `resolveText` de config/text.ts, la misma que usa
  // `useText()`. Se le da la instancia de i18n de `useUiI18n`, que sirve también sin proveedor de i18n.
  const resolve = useCallback(
    (text: Text | undefined): string =>
      resolveText(text, locale, (key, language) => translateWith(i18n, key, language)),
    [i18n, locale],
  );
  return { t, locale, resolve };
}

/** Idioma activo para `format`/`parse`. */
export function useFieldLocale(): string {
  return useFieldText().locale;
}

/**
 * Mensaje traducido de un código de `validate` (`required`, `maxLength`, `minLength`, `min`, `max`,
 * `pattern`, `format`) con los límites de la definición del campo.
 */
export function useFieldErrorMessage(): (code: string, def: Pick<FieldDef, 'min' | 'max' | 'minLength' | 'maxLength'>) => string {
  const { t } = useFieldText();
  return useCallback(
    (code, def) =>
      t(`errors.${code}`, {
        // En minLength/maxLength el límite es la longitud; en min/max, el valor.
        min: code === 'minLength' ? def.minLength : def.min,
        max: code === 'maxLength' ? def.maxLength : def.max,
      }),
    [t],
  );
}

export interface LookupLabelState {
  label: string | undefined;
  /** Hay petición en curso (sin etiqueta todavía). */
  loading: boolean;
}

/** Etiqueta (`displayField`) de un id de lookup: de la caché o con `EntityApi.get`. */
export function useLookupLabel(def: FieldDef, value: unknown): LookupLabelState {
  const api = useContext(ApiContext);
  const manifest = useContext(ManifestContext);
  const lookup = def.lookup;
  const id = isEmpty(value) ? undefined : String(value);
  const known = lookup && id ? cachedLookupLabel(lookup, id) : undefined;
  const cacheKey = lookup && id ? `${lookup.entity}|${lookup.valueField}|${id}` : undefined;
  const [fetched, setFetched] = useState<{ key: string; label: string | undefined }>();

  useEffect(() => {
    if (!api || !lookup || !id || !cacheKey || known !== undefined) return undefined;
    let cancelled = false;
    void resolveLookupLabel(api, manifest, lookup, id).then((label) => {
      if (!cancelled) setFetched({ key: cacheKey, label });
    });
    return () => {
      cancelled = true;
    };
  }, [api, manifest, lookup, id, cacheKey, known]);

  const resolved = fetched && fetched.key === cacheKey ? fetched : undefined;
  const label = known ?? resolved?.label;
  const loading = Boolean(api && id && label === undefined && !resolved);
  return { label, loading };
}
