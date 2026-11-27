import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren,
} from 'react';
import { createPortal } from 'react-dom';
import type { Tone } from '../contracts';
import { Action } from './Action';
import { Feedback } from './Feedback';
import { useT } from './i18n';

export interface ToastOptions {
  message: string;
  tone?: Tone;
  title?: string;
  /** Texto de la acción; con `onAction` y sin él se usa «Deshacer». */
  actionLabel?: string;
  onAction?(): void;
  /** Milisegundos hasta que se cierra solo; `0` lo deja hasta que se cierre. */
  duration?: number;
}

export interface ToastApi {
  /** Muestra un aviso y devuelve su identificador. */
  show(options: ToastOptions): string;
  /** Cierra un aviso (o el último, sin identificador). */
  dismiss(id?: string): void;
}

interface ToastEntry {
  id: string;
  options: ToastOptions;
}

const MAX_VISIBLE = 5;

const NOOP_API: ToastApi = { show: () => '', dismiss: () => undefined };
const ToastContext = createContext<ToastApi | null>(null);

function defaultDuration(options: ToastOptions): number {
  if (options.tone === 'error') return 10_000;
  return options.onAction ? 8_000 : 5_000;
}

/**
 * Avisos emergentes (`[data-th="feedback"]` + `data-th-elevation="overlay"`), con acción opcional
 * «Deshacer». Se pausan con el ratón o el foco encima. Las animaciones sólo se aplican con
 * `prefers-reduced-motion: no-preference` (ver `ui.css`).
 */
export function ToastProvider({ children }: PropsWithChildren) {
  const [toasts, setToasts] = useState<ToastEntry[]>([]);
  const counter = useRef(0);

  const dismiss = useCallback((id?: string) => {
    setToasts((current) => {
      if (current.length === 0) return current;
      const target = id ?? current[current.length - 1]?.id;
      return current.filter((toast) => toast.id !== target);
    });
  }, []);

  const show = useCallback((options: ToastOptions) => {
    counter.current += 1;
    const id = `toast-${counter.current}`;
    setToasts((current) => [...current, { id, options }].slice(-MAX_VISIBLE));
    return id;
  }, []);

  const api = useMemo<ToastApi>(() => ({ show, dismiss }), [show, dismiss]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <ToastRegion toasts={toasts} dismiss={dismiss} />
    </ToastContext.Provider>
  );
}

function ToastRegion({ toasts, dismiss }: { toasts: ToastEntry[]; dismiss(id: string): void }) {
  const t = useT('ui');
  if (toasts.length === 0 || typeof document === 'undefined') return null;
  return createPortal(
    <section data-kx-ui="toast-region" aria-label={t('toast.region')}>
      {toasts.map((toast) => (
        <ToastItem key={toast.id} toast={toast} dismiss={dismiss} />
      ))}
    </section>,
    document.body,
  );
}

function ToastItem({ toast, dismiss }: { toast: ToastEntry; dismiss(id: string): void }) {
  const t = useT('ui');
  const { id, options } = toast;
  const [paused, setPaused] = useState(false);
  const duration = options.duration ?? defaultDuration(options);

  useEffect(() => {
    if (paused || duration <= 0) return undefined;
    const timer = setTimeout(() => dismiss(id), duration);
    return () => clearTimeout(timer);
  }, [paused, duration, dismiss, id]);

  return (
    <Feedback
      tone={options.tone ?? 'info'}
      elevation="overlay"
      data-kx-ui="toast"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
    >
      <span data-kx-part="text">
        {options.title ? <strong>{options.title}</strong> : null}
        <span data-kx-part="message">{options.message}</span>
      </span>
      {options.onAction ? (
        <Action
          kind="quiet"
          onClick={() => {
            options.onAction?.();
            dismiss(id);
          }}
        >
          {options.actionLabel ?? t('toast.undo')}
        </Action>
      ) : null}
      <Action kind="quiet" icon="x" aria-label={t('toast.close')} onClick={() => dismiss(id)} />
    </Feedback>
  );
}

/** API de avisos. Sin `ToastProvider` devuelve una API vacía (no falla ni muestra nada). */
export function useToast(): ToastApi {
  return useContext(ToastContext) ?? NOOP_API;
}
