import type { ChildProcess } from 'node:child_process';
import type { Browser, BrowserContext, Page } from 'playwright-core';

export const ROOT: string;
export const DEMO_DIST: string;
export const VIEWPORT: Readonly<{ width: number; height: number }>;

export function demoBuildIsStale(): boolean;
export function ensureDemoBuild(options?: { force?: boolean; log?: (line: string) => void }): Promise<boolean>;
export function startDemoServer(options?: {
  preferredPort?: number;
}): Promise<{ origin: string; port: number; stop: () => Promise<void> }>;
export function launchDemoBrowser(): Promise<{ browser: Browser; channel: string; version: string } | null>;
export function readDemoCredentials(username?: string): { username: string; password: string };
export function readNamespaces(): string[];
export function isExternalUrl(url: string, origin: string): boolean;

export interface DemoPageOptions {
  lng?: string;
  dir?: 'ltr' | 'rtl';
  mode?: 'light' | 'dark';
  theme?: string;
  viewport?: { width: number; height: number };
  locale?: string;
  /** Emula un teléfono (agente de móvil y ventana de visualización de móvil). */
  isMobile?: boolean;
  /** Pantalla táctil (`pointer: coarse`, `hover: none`). */
  hasTouch?: boolean;
}

/** Opciones de un teléfono (390x844 por omisión): ventana pequeña, `isMobile` y `hasTouch`. */
export function mobileOptions(size?: { width?: number; height?: number }): Required<Pick<DemoPageOptions, 'viewport' | 'isMobile' | 'hasTouch'>>;

export interface DemoPage {
  page: Page;
  context: BrowserContext;
  /** Peticiones que han salido del origen de la demo (debe quedar vacía). */
  external: string[];
  /** Excepciones no capturadas de la página. */
  pageErrors: string[];
  startUrl: string;
  close: () => Promise<void>;
}

export function openDemoPage(browser: Browser, origin: string, options?: DemoPageOptions): Promise<DemoPage>;
export function signIn(page: Page, startUrl: string, username?: string): Promise<void>;
export function spa(page: Page, target: string): Promise<void>;
export function waitReady(page: Page, selector?: string, timeout?: number): Promise<void>;
export function findColorCenter(
  page: Page,
  rgb: string,
  region: { x: number; y: number; width: number; height: number },
  exclude?: { x: number; y: number; width: number; height: number }[],
): Promise<{ x: number; y: number } | null>;

export interface TrackedProcess {
  child: ChildProcess;
  pid: number | undefined;
  name: string;
  exited: Promise<number | null>;
  stop: () => boolean;
}
export function spawnTracked(
  command: string,
  args?: string[],
  options?: { name?: string; cwd?: string; env?: NodeJS.ProcessEnv; stdio?: unknown; pidFile?: string; shell?: boolean },
): TrackedProcess;
export function killAllTracked(): void;
export function installCleanup(): void;
