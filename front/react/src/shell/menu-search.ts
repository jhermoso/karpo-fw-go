// Búsqueda difusa del menú (fuse.js): tolera erratas y no distingue acentos. Está aparte de `menu.ts` para que
// la librería no entre en el trozo inicial (M-15): la barra lateral la descarga la primera vez que se escribe.
import Fuse from 'fuse.js';
import type { MenuLeaf } from './menu';

/** Busca con tolerancia a erratas y sin distinguir acentos. */
export function searchMenu(leaves: readonly MenuLeaf[], query: string): MenuLeaf[] {
  const text = query.trim();
  if (text === '') return [...leaves];
  const fuse = new Fuse(leaves, {
    keys: [
      { name: 'label', weight: 3 },
      { name: 'keywords', weight: 1 },
      { name: 'group', weight: 1 },
    ],
    threshold: 0.35,
    ignoreLocation: true,
    ignoreDiacritics: true,
  });
  return fuse.search(text).map((hit) => hit.item);
}
