// Primitivas de interfaz con los ganchos de Theros Front (PLAN §4.8). Propiedad de la tarea 3.
import './ui.css';

export { Action } from './Action';
export type { ActionKind, ActionProps, ThemeActionKind } from './Action';
export { Badge, themeToneOf } from './Badge';
export type { BadgeProps, ThemeTone } from './Badge';
export { Chip } from './Chip';
export type { ChipProps } from './Chip';
export { ConfirmDialog, Dialog, PromptDialog } from './Dialog';
export type { ConfirmDialogProps, DialogProps, PromptDialogProps } from './Dialog';
export { Feedback } from './Feedback';
export type { FeedbackProps } from './Feedback';
export { Field } from './Field';
export type { FieldControlProps, FieldProps } from './Field';
export { Icon } from './Icon';
export type { IconProps, IconWeight } from './Icon';
export { ICON_DATA, iconData } from './icons';
export type { IconData } from './icons';
export { Kbd, ShortcutKeys } from './Kbd';
export type { ShortcutKeysProps } from './Kbd';
export { Skeleton } from './Skeleton';
export type { SkeletonProps } from './Skeleton';
export { Spinner } from './Spinner';
export type { SpinnerProps } from './Spinner';
export { ToastProvider, useToast } from './Toast';
export type { ToastApi, ToastOptions } from './Toast';
export { Toolbar } from './Toolbar';
export type { ToolbarProps } from './Toolbar';
export {
  ShortcutProvider,
  ShortcutScope,
  useShortcut,
  useShortcutConflicts,
  useShortcutRegistry,
} from './Shortcuts';
export type { ShortcutProviderProps, ShortcutScopeProps } from './Shortcuts';
export {
  canonicalKeys,
  detectPlatform,
  displayKeys,
  findShortcutConflicts,
  matchesEvent,
  parseKeys,
} from './shortcut-keys';
export type { ParsedKeys, Platform, ShortcutConflict } from './shortcut-keys';
