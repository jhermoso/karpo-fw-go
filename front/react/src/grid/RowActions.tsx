// Menú de acciones de una fila (órdenes de `config.commands` con superficie `row`).
import * as Menu from '@radix-ui/react-dropdown-menu';
import type { CommandDef, Row } from '../contracts';
import { Action, Icon } from '../ui';

interface RowActionsProps {
  row: Row;
  commands: CommandDef[];
  open: boolean;
  label: string;
  commandLabel(command: CommandDef): string;
  onOpenChange(open: boolean): void;
  onSelect(command: CommandDef, row: Row): void;
  /** El menú se cerró: la cuadrícula devuelve el foco a la celda activa. */
  onClosed(): void;
}

export function RowActions({ row, commands, open, label, commandLabel, onOpenChange, onSelect, onClosed }: RowActionsProps) {
  if (commands.length === 0) return null;
  return (
    <Menu.Root open={open} onOpenChange={onOpenChange} modal={false}>
      <Menu.Trigger asChild>
        <Action kind="quiet" icon="more-horizontal" aria-label={label} tabIndex={-1} />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Content
          data-kx-ui="row-menu"
          data-th-elevation="overlay"
          align="end"
          sideOffset={4}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            onClosed();
          }}
        >
          {commands.map((command) => (
            <Menu.Item
              key={command.id}
              data-kx-tone={command.tone}
              data-kx-command={command.id}
              onSelect={() => onSelect(command, row)}
            >
              {command.icon ? <Icon name={command.icon} /> : null}
              <span>{commandLabel(command)}</span>
            </Menu.Item>
          ))}
        </Menu.Content>
      </Menu.Portal>
    </Menu.Root>
  );
}
