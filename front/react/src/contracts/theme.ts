// Contrato de temas de Theros Front (PLAN §4.2, §4.5).

export type ThemeMode = 'light' | 'dark' | 'system';

export interface ThemeInfo {
  name: string;
  modes: ThemeMode[];
  highContrast: boolean;
  iconStyle: 'outline' | 'filled';
  direction?: 'ltr' | 'rtl';
  dictionaryHash: string;
}

export interface ThemeApi {
  info: ThemeInfo;
  mode: ThemeMode;
  setMode(m: ThemeMode): void;
  setTheme(name: string): Promise<void>;
  available: string[];
  resolveColor(token: string, el?: Element): string;
  onChange(cb: () => void): () => void;
}
