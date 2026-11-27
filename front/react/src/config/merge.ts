// Fusión profunda de la superposición (`overlay`) sobre el JSON original, ANTES de normalizar.
// Los objetos se funden clave a clave; los arrays de objetos con identidad (`id`, `field`,
// `action`, `value`) se funden por identidad (lo que no casa se añade al final); el resto de arrays
// se sustituyen. `null` en la superposición borra la clave.
// `value` es la identidad de las opciones (`enumValues`, `options`): una superposición
// `[{value, labelKey}]` pone la clave de texto a cada opción sin sustituir el array.

type Obj = Record<string, unknown>;

const IDENTITY_KEYS = ['id', 'field', 'action', 'value'] as const;

function isObj(value: unknown): value is Obj {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function identityOf(value: unknown): string | undefined {
  if (!isObj(value)) return undefined;
  for (const key of IDENTITY_KEYS) {
    const v = value[key];
    if (typeof v === 'string' && v !== '') return `${key}:${v}`;
    // El valor de una opción puede ser un número o un booleano (`{value: true, label: 'Sí'}`).
    if (key === 'value' && (typeof v === 'boolean' || (typeof v === 'number' && Number.isFinite(v)))) {
      return `${key}:${String(v)}`;
    }
  }
  return undefined;
}

function mergeArrays(base: unknown[], overlay: unknown[]): unknown[] {
  const keyed = overlay.length > 0 && overlay.every((item) => identityOf(item) !== undefined);
  if (!keyed) return structuredCloneSafe(overlay) as unknown[];
  const out = base.map((item) => structuredCloneSafe(item));
  for (const item of overlay) {
    const identity = identityOf(item);
    const index = out.findIndex((existing) => identityOf(existing) === identity);
    if (index >= 0) out[index] = mergeOverlay(out[index], item);
    else out.push(structuredCloneSafe(item));
  }
  return out;
}

function structuredCloneSafe<T>(value: T): T {
  if (value === undefined) return value;
  return JSON.parse(JSON.stringify(value)) as T;
}

export function mergeOverlay(base: unknown, overlay: unknown): unknown {
  if (overlay === undefined) return structuredCloneSafe(base);
  if (isObj(base) && isObj(overlay)) {
    const out: Obj = structuredCloneSafe(base);
    for (const [key, value] of Object.entries(overlay)) {
      if (value === null) delete out[key];
      else if (key in out) out[key] = mergeOverlay(out[key], value);
      else out[key] = structuredCloneSafe(value);
    }
    return out;
  }
  if (Array.isArray(base) && Array.isArray(overlay)) return mergeArrays(base, overlay);
  return structuredCloneSafe(overlay);
}
