// Nombre de un idioma en su propio idioma («Español», «English», «العربية»), con respaldo al código.
// Lo normal es `Intl.DisplayNames`; la tabla sólo cubre un motor sin datos de idioma para el código (entonces
// `of()` devuelve el propio código o lanza), de modo que el selector nunca muestre «ar» a quien lee árabe.
const OWN_NAMES: Readonly<Record<string, string>> = { es: 'Español', en: 'English', ar: 'العربية' };

export function languageName(code: string): string {
  let name: string | undefined;
  try {
    name = new Intl.DisplayNames([code], { type: 'language' }).of(code);
  } catch {
    name = undefined;
  }
  if (name === undefined || name === code) name = OWN_NAMES[code.split(/[-_]/)[0]?.toLowerCase() ?? ''] ?? name;
  const text = name ?? code;
  return text.charAt(0).toLocaleUpperCase(code) + text.slice(1);
}
