// Órdenes admitidos por el servidor (A2 §2). Un `sortBy` fuera de la lista da 400, así que el front
// sólo manda el orden de una columna si el servidor lo admite; el resto se ordena en cliente.

/** Campos de orden por endpoint, tal como los acepta el contrato de mantenimiento de Go. */
export const KNOWN_SERVER_SORT: Readonly<Record<string, readonly string[]>> = {
  '/countries': ['alpha2', 'alpha3', 'name'],
  '/holidays': ['date', 'name'],
  '/facilities': ['name'],
  '/work-centers': ['code'],
  '/products': ['sku', 'name'],
  '/assets': ['code', 'name'],
  '/suppliers': ['paymentDays'],
  '/carriers': ['code', 'name'],
};

/** Campos de orden de servidor conocidos de un endpoint sin versión y sin barra final. */
export function knownServerSort(endpoint: string): readonly string[] {
  const clean = endpoint.split('?')[0]!.replace(/\/+$/, '');
  return KNOWN_SERVER_SORT[clean] ?? [];
}
