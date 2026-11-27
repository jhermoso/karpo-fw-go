// Contrato del registro de atajos (PLAN §4.2).
// useShortcut(def) (src/ui, tarea 3) lo registra; el panel de atajos (Shift+?) lista el registro.

export interface ShortcutDef {
  id: string;
  scope: 'global' | 'grid' | 'detail' | 'tree' | 'map';
  /** Por ejemplo "Mod+K", "Shift+?" */
  keys: string;
  labelKey: string;
  run(): void;
}
