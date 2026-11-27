// Menú de elección única (tema, modo, idioma) sobre Radix DropdownMenu: teclado, foco y roles de serie.
// `ChoiceGroup` es el mismo grupo de opciones sin su botón: lo reutiliza el menú «Más» del modo compacto.
import * as DropdownMenu from '@radix-ui/react-dropdown-menu';
import { Action, Icon } from '../ui';

export interface ChoiceOption {
  value: string;
  label: string;
}

export interface ChoiceMenuProps {
  /** Nombre accesible del botón y título del menú. */
  label: string;
  icon: string;
  value: string;
  options: readonly ChoiceOption[];
  onChange(value: string): void;
  /** Ancla de ayuda opcional. */
  anchor?: string;
  /** Texto visible junto al icono (por defecto sólo el icono). */
  showValue?: boolean;
}

export interface ChoiceGroupProps {
  label: string;
  value: string;
  options: readonly ChoiceOption[];
  onChange(value: string): void;
}

/** Etiqueta y opciones de radio de una elección única, para pintarlas dentro de un menú ya abierto. */
export function ChoiceGroup({ label, value, options, onChange }: ChoiceGroupProps) {
  return (
    <DropdownMenu.Group>
      <DropdownMenu.Label data-kx-part="menu-label">{label}</DropdownMenu.Label>
      <DropdownMenu.RadioGroup value={value} onValueChange={onChange} aria-label={label}>
        {options.map((option) => (
          <DropdownMenu.RadioItem key={option.value} value={option.value} data-kx-ui="menu-item">
            <span data-kx-part="check">
              <DropdownMenu.ItemIndicator>
                <Icon name="check" />
              </DropdownMenu.ItemIndicator>
            </span>
            {option.label}
          </DropdownMenu.RadioItem>
        ))}
      </DropdownMenu.RadioGroup>
    </DropdownMenu.Group>
  );
}

export function ChoiceMenu({ label, icon, value, options, onChange, anchor, showValue }: ChoiceMenuProps) {
  const current = options.find((option) => option.value === value);
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <Action
          kind="quiet"
          icon={icon}
          aria-label={label}
          title={current ? `${label}: ${current.label}` : label}
          data-kx-help={anchor}
        >
          {showValue && current ? <span data-kx-part="value">{current.label}</span> : null}
        </Action>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content data-kx-ui="menu" data-th-elevation="overlay" align="end" sideOffset={4}>
          <ChoiceGroup label={label} value={value} options={options} onChange={onChange} />
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
