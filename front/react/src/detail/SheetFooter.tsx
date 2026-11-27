// Pie de la ficha en edición y alta: Guardar (o Crear) y Cancelar. Queda fijo abajo del panel (M-8): con un
// formulario corto las acciones no flotan a media altura y con uno largo no hay que subir para guardar.
import { useTranslation } from 'react-i18next';
import { Action } from '../ui';
import type { FormMode } from './form-model';

export interface SheetFooterProps {
  mode: FormMode;
  saving: boolean;
  onSave(): void;
  onCancel(): void;
}

export function SheetFooter({ mode, saving, onSave, onCancel }: SheetFooterProps) {
  const { t } = useTranslation('detail');
  return (
    <footer data-kx-part="sheet-footer" role="group" aria-label={t('footer.label')}>
      <Action
        kind="primary"
        icon="save"
        type="button"
        data-kx-help="detail.save"
        aria-keyshortcuts="Control+Enter Meta+Enter"
        busy={saving}
        onClick={onSave}
      >
        {mode === 'create' ? t('actions.create') : t('actions.save')}
      </Action>
      <Action kind="secondary" disabledReason={saving ? t('actions.saving') : undefined} onClick={onCancel}>
        {t('actions.cancel')}
      </Action>
    </footer>
  );
}
