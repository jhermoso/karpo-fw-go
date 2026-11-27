// Preferencias del usuario en localStorage (PLAN §4.2). Siempre con try/catch: en ventanas privadas,
// con datos de sitio bloqueados o en las vistas previas el acceso puede lanzar o venir vacío, y la
// aplicación debe funcionar igual sin ello.

export function readStored(key: string): string | null {
  try {
    return globalThis.localStorage?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

export function writeStored(key: string, value: string | null): void {
  try {
    if (value === null) globalThis.localStorage?.removeItem(key);
    else globalThis.localStorage?.setItem(key, value);
  } catch {
    /* sin almacenamiento: la preferencia vale sólo para esta sesión */
  }
}
