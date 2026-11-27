// HelpProvider: registro de pantallas con ayuda y lanzador del recorrido (PLAN §4.8).
//
//  - `useHelp(pantalla, pasos)` registra los pasos mientras la pantalla esté montada.
//  - `start()` junta los pasos de la configuración de la pantalla activa (en su orden) y, detrás, las
//    anclas `[data-kx-help]` visibles que ningún paso cubre, en el orden del DOM (D-R36); un ancla oculta
//    con `data-kx-help-fallback` se explica sobre su elemento de reserva. Si no queda ningún paso, enseña
//    un aviso centrado en lugar de no hacer nada.
//  - Al cerrar, el foco vuelve al botón de ayuda (o a donde estaba si se lanzó por código).
import { useCallback, useContext, useEffect, useMemo, useRef, useState, type PropsWithChildren } from 'react';
import { useTranslation } from 'react-i18next';
import { HelpContext, ThemeContext, type HelpApi, type HelpStepDef } from '../contracts';
import { resolveThemeColor, resolveThemeLength } from '../theme/probe';
import './help.css';
import { HelpRuntimeContext, type HelpRuntime } from './runtime';
import { isTourSeen, markTourSeen } from './seen';
import { mergeSteps, stepsFromAnchors, stepsFromConfig, type HelpTranslator, type ResolvedStep } from './steps';
import { startTour, type TourController } from './tour';

interface Registration {
  screen: string;
  steps: HelpStepDef[];
}

function prefersMotion(): boolean {
  return window.matchMedia?.('(prefers-reduced-motion: no-preference)').matches ?? false;
}

export function HelpProvider({ children }: PropsWithChildren) {
  const { t, i18n } = useTranslation('help');
  const theme = useContext(ThemeContext);

  const registrations = useRef(new Map<symbol, Registration>());
  const tour = useRef<TourController | null>(null);
  const [screen, setScreen] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [seenVersion, setSeenVersion] = useState(0);

  const currentScreen = useCallback((): string | null => {
    let last: string | null = null;
    for (const registration of registrations.current.values()) last = registration.screen;
    return last;
  }, []);

  const register = useCallback(
    (name: string, steps: HelpStepDef[]) => {
      const id = Symbol(name);
      registrations.current.set(id, { screen: name, steps });
      setScreen(currentScreen());
      return () => {
        registrations.current.delete(id);
        setScreen(currentScreen());
      };
    },
    [currentScreen],
  );

  // Las últimas dependencias, sin que `start` cambie de identidad en cada render.
  const latest = useRef({ i18n, t, theme });
  useEffect(() => {
    latest.current = { i18n, t, theme };
  });

  const start = useCallback(
    (requested?: string, launcher?: HTMLElement | null) => {
      const { i18n: instance, t: translate, theme: themeApi } = latest.current;
      tour.current?.destroy();

      const target = requested ?? currentScreen();
      const defs = [...registrations.current.values()]
        .filter((registration) => registration.screen === target)
        .flatMap((registration) => registration.steps);

      const translator: HelpTranslator = {
        language: instance.resolvedLanguage ?? instance.language,
        exists: (key, { ns }) => instance.exists(key, { ns }),
        t: (key, { ns }) => instance.t(key, { ns }) as string,
      };

      let steps: ResolvedStep[] = mergeSteps(
        defs.length > 0 ? stepsFromConfig(defs, translator) : [],
        stepsFromAnchors(translator),
      );
      if (steps.length === 0) {
        steps = [{ title: translate('tour.empty.title'), body: translate('tour.empty.body'), source: 'empty' }];
      }

      const returnTo =
        launcher ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null);
      const root = document.documentElement;
      const color = (token: string) => themeApi?.resolveColor(token, root) ?? resolveThemeColor(token, root);
      const px = (token: string) => {
        const value = resolveThemeLength(token, root);
        return Number.isFinite(value) ? value : 0;
      };
      const direction = root.getAttribute('dir') === 'rtl' ? 'rtl' : 'ltr';

      setRunning(true);
      tour.current = startTour({
        steps,
        direction,
        motion: prefersMotion(),
        iconWeight: themeApi?.info.iconStyle === 'filled' ? 'fill' : 'regular',
        stage: { padding: px('--th-space-1'), radius: px('--th-edge-rounding-medium') },
        scrim: () => color('--th-surface-scrim'),
        subscribeTheme: themeApi ? (callback) => themeApi.onChange(callback) : undefined,
        labels: {
          next: translate('tour.next'),
          prev: translate('tour.prev'),
          done: translate('tour.done'),
          close: translate('tour.close'),
          // driver.js sustituye {{current}} y {{total}}: se pasan como texto para que i18next no los consuma
          progress: translate('tour.progress', { current: '{{current}}', total: '{{total}}' }),
        },
        onEnd: ({ completed }) => {
          tour.current = null;
          setRunning(false);
          if (completed && target) {
            markTourSeen(target);
            setSeenVersion((v) => v + 1);
          }
          // driver.js devuelve el foco por su cuenta después de este aviso: el nuestro va detrás.
          queueMicrotask(() => {
            const button = document.querySelector<HTMLElement>('[data-kx-help="help.button"]');
            const destination = returnTo?.isConnected ? returnTo : button;
            destination?.focus();
          });
        },
      });
    },
    [currentScreen],
  );

  useEffect(
    () => () => {
      tour.current?.destroy();
    },
    [],
  );

  const api = useMemo<HelpApi>(() => ({ register, start: (name?: string) => start(name) }), [register, start]);

  const runtime = useMemo<HelpRuntime>(
    // seenVersion fuerza a recalcular `unseen` cuando se marca un recorrido como visto.
    () => ({
      screen,
      running,
      unseen: screen !== null && !isTourSeen(screen) && seenVersion >= 0,
      start,
    }),
    [screen, running, seenVersion, start],
  );

  return (
    <HelpContext.Provider value={api}>
      <HelpRuntimeContext.Provider value={runtime}>{children}</HelpRuntimeContext.Provider>
    </HelpContext.Provider>
  );
}
