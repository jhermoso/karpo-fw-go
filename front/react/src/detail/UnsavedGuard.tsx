// Aviso al abandonar la ficha con cambios sin guardar: el router bloquea la navegación (sólo con un
// router de datos) y el navegador avisa al cerrar la pestaña.
import { useCallback, useContext, useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { UNSAFE_DataRouterContext, useBlocker } from 'react-router';
import { ConfirmDialog } from '../ui';

export interface UnsavedGuardProps {
  /** Se pregunta en el momento de navegar, no al pintar: refleja siempre el último estado. */
  isDirty(): boolean;
  /** Se eligió seguir editando: el foco vuelve a la ficha. */
  onKeepEditing?(): void;
}

function RouterBlocker({ isDirty, onKeepEditing }: UnsavedGuardProps) {
  const { t } = useTranslation('detail');
  const proceeded = useRef(false);
  const shouldBlock = useCallback(
    ({ currentLocation, nextLocation }: { currentLocation: { pathname: string }; nextLocation: { pathname: string } }) =>
      isDirty() && currentLocation.pathname !== nextLocation.pathname,
    [isDirty],
  );
  const blocker = useBlocker(shouldBlock);
  return (
    <ConfirmDialog
      open={blocker.state === 'blocked'}
      title={t('discard.title')}
      message={t('discard.leave')}
      destructive
      confirmLabel={t('discard.confirm')}
      cancelLabel={t('discard.keep')}
      onConfirm={() => {
        proceeded.current = true;
        blocker.proceed?.();
      }}
      onOpenChange={(open) => {
        if (open) return;
        if (!proceeded.current) {
          blocker.reset?.();
          onKeepEditing?.();
        }
        proceeded.current = false;
      }}
    />
  );
}

export function UnsavedGuard({ isDirty, onKeepEditing }: UnsavedGuardProps) {
  const dataRouter = useContext(UNSAFE_DataRouterContext);

  useEffect(() => {
    function onBeforeUnload(event: BeforeUnloadEvent): void {
      if (!isDirty()) return;
      event.preventDefault();
      // Los navegadores antiguos exigen asignar `returnValue`.
      event.returnValue = '';
    }
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, [isDirty]);

  return dataRouter ? <RouterBlocker isDirty={isDirty} onKeepEditing={onKeepEditing} /> : null;
}
