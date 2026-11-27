import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState, type ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import type { ShortcutDef } from '../contracts';
import {
  ShortcutProvider,
  ShortcutScope,
  useShortcut,
  useShortcutConflicts,
  useShortcutRegistry,
  type ShortcutConflict,
} from './index';

function Probe({ def }: { def: Omit<ShortcutDef, 'run' | 'labelKey'> & { run?: () => void; labelKey?: string } }) {
  useShortcut({ labelKey: `shortcuts.${def.id}`, run: () => undefined, ...def });
  return null;
}

function RegistryList() {
  const defs = useShortcutRegistry();
  return (
    <ul aria-label="registro">
      {defs.map((def) => (
        <li key={def.id}>{`${def.id}|${def.scope}|${def.keys}`}</li>
      ))}
    </ul>
  );
}

function ConflictList() {
  const conflicts = useShortcutConflicts();
  return (
    <ul aria-label="conflictos">
      {conflicts.map((c) => (
        <li key={`${c.kind}-${c.ids.join('+')}`}>{`${c.kind}:${c.scope}:${c.ids.join('+')}`}</li>
      ))}
    </ul>
  );
}

function Shell({ children, onConflict }: { children: ReactNode; onConflict?: (c: ShortcutConflict) => void }) {
  return (
    <ShortcutProvider platform="other" onConflict={onConflict}>
      {children}
    </ShortcutProvider>
  );
}

describe('registro de atajos', () => {
  it("'Mod' es Ctrl y actúa desde cualquier sitio en el ámbito global", async () => {
    const user = userEvent.setup();
    const run = vi.fn();
    render(
      <Shell>
        <Probe def={{ id: 'palette', scope: 'global', keys: 'Mod+K', run }} />
        <button type="button">foco</button>
      </Shell>,
    );
    await user.keyboard('{Control>}k{/Control}');
    expect(run).toHaveBeenCalledTimes(1);
    screen.getByRole('button', { name: 'foco' }).focus();
    await user.keyboard('{Control>}K{/Control}');
    expect(run).toHaveBeenCalledTimes(1 + 0 + 1 - 0);
    // Sin Ctrl, o con Ctrl+Shift, no.
    await user.keyboard('k');
    await user.keyboard('{Control>}{Shift>}k{/Shift}{/Control}');
    expect(run).toHaveBeenCalledTimes(2);
  });

  it("en Mac 'Mod' es Cmd y Ctrl no vale", async () => {
    const user = userEvent.setup();
    const run = vi.fn();
    render(
      <ShortcutProvider platform="mac">
        <Probe def={{ id: 'palette', scope: 'global', keys: 'Mod+K', run }} />
      </ShortcutProvider>,
    );
    await user.keyboard('{Control>}k{/Control}');
    expect(run).not.toHaveBeenCalled();
    await user.keyboard('{Meta>}k{/Meta}');
    expect(run).toHaveBeenCalledTimes(1);
  });

  it('un atajo de ámbito sólo actúa con el foco dentro de ese ámbito', async () => {
    const user = userEvent.setup();
    const edit = vi.fn();
    render(
      <Shell>
        <Probe def={{ id: 'grid.edit', scope: 'grid', keys: 'F2', run: edit }} />
        <ShortcutScope scope="grid">
          <button type="button">celda</button>
        </ShortcutScope>
        <button type="button">fuera</button>
      </Shell>,
    );
    screen.getByRole('button', { name: 'fuera' }).focus();
    await user.keyboard('{F2}');
    expect(edit).not.toHaveBeenCalled();
    screen.getByRole('button', { name: 'celda' }).focus();
    await user.keyboard('{F2}');
    expect(edit).toHaveBeenCalledTimes(1);
  });

  it('el ámbito más cercano gana al global con la misma tecla, y ámbitos distintos no se mezclan', async () => {
    const user = userEvent.setup();
    const global = vi.fn();
    const grid = vi.fn();
    const detail = vi.fn();
    render(
      <Shell onConflict={() => undefined}>
        <Probe def={{ id: 'close', scope: 'global', keys: 'Escape', run: global }} />
        <Probe def={{ id: 'grid.cancel', scope: 'grid', keys: 'Escape', run: grid }} />
        <Probe def={{ id: 'detail.cancel', scope: 'detail', keys: 'Escape', run: detail }} />
        <ShortcutScope scope="grid">
          <button type="button">celda</button>
        </ShortcutScope>
        <button type="button">fuera</button>
      </Shell>,
    );
    screen.getByRole('button', { name: 'celda' }).focus();
    await user.keyboard('{Escape}');
    expect(grid).toHaveBeenCalledTimes(1);
    expect(global).not.toHaveBeenCalled();
    expect(detail).not.toHaveBeenCalled();
    screen.getByRole('button', { name: 'fuera' }).focus();
    await user.keyboard('{Escape}');
    expect(global).toHaveBeenCalledTimes(1);
  });

  it('ámbitos anidados: el interior primero', async () => {
    const user = userEvent.setup();
    const outer = vi.fn();
    const inner = vi.fn();
    render(
      <Shell>
        <Probe def={{ id: 'a', scope: 'detail', keys: 'Alt+ArrowDown', run: outer }} />
        <Probe def={{ id: 'b', scope: 'tree', keys: 'Alt+ArrowDown', run: inner }} />
        <ShortcutScope scope="detail">
          <ShortcutScope scope="tree">
            <button type="button">nodo</button>
          </ShortcutScope>
        </ShortcutScope>
      </Shell>,
    );
    screen.getByRole('button', { name: 'nodo' }).focus();
    await user.keyboard('{Alt>}{ArrowDown}{/Alt}');
    expect(inner).toHaveBeenCalledTimes(1);
    expect(outer).not.toHaveBeenCalled();
  });

  it('dentro de un campo de texto ignora las teclas sueltas pero no las combinaciones con Mod', async () => {
    const user = userEvent.setup();
    const help = vi.fn();
    const save = vi.fn();
    render(
      <Shell>
        <Probe def={{ id: 'help', scope: 'global', keys: 'Shift+?', run: help }} />
        <Probe def={{ id: 'save', scope: 'global', keys: 'Mod+S', run: save }} />
        <input aria-label="texto" />
        <textarea aria-label="nota" />
        <select aria-label="lista">
          <option>uno</option>
        </select>
        <div role="textbox" aria-label="rico" tabIndex={0} />
        <button type="button">fuera</button>
      </Shell>,
    );
    // Fuera de un campo, Shift+? funciona (los símbolos ignoran Shift: dependen de la distribución).
    screen.getByRole('button', { name: 'fuera' }).focus();
    await user.keyboard('?');
    expect(help).toHaveBeenCalledTimes(1);
    for (const name of ['texto', 'nota', 'lista', 'rico']) {
      screen.getByLabelText(name).focus();
      await user.keyboard('?');
      await user.keyboard('{Control>}s{/Control}');
    }
    expect(help).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledTimes(4);
  });

  it('respeta preventDefault de quien lo trató antes', async () => {
    const user = userEvent.setup();
    const run = vi.fn();
    render(
      <Shell>
        <Probe def={{ id: 'x', scope: 'global', keys: 'Enter', run }} />
        <button type="button" onKeyDown={(event) => event.preventDefault()}>
          tratado
        </button>
        <button type="button">libre</button>
      </Shell>,
    );
    screen.getByRole('button', { name: 'tratado' }).focus();
    await user.keyboard('{Enter}');
    expect(run).not.toHaveBeenCalled();
    // Un botón libre: Enter activa el botón y el atajo.
    screen.getByRole('button', { name: 'libre' }).focus();
    await user.keyboard('{Enter}');
    expect(run).toHaveBeenCalledTimes(1);
  });

  it('el registro lista los atajos en orden y se actualiza al montar y desmontar', async () => {
    const user = userEvent.setup();
    function Toggle() {
      const [on, setOn] = useState(true);
      return (
        <>
          <button type="button" onClick={() => setOn(false)}>
            quitar
          </button>
          {on ? <Probe def={{ id: 'b', scope: 'tree', keys: 'Mod+Shift+B' }} /> : null}
        </>
      );
    }
    render(
      <Shell>
        <Probe def={{ id: 'a', scope: 'global', keys: 'Mod+K' }} />
        <Toggle />
        <RegistryList />
      </Shell>,
    );
    const list = () => screen.getAllByRole('listitem').map((li) => li.textContent);
    expect(list()).toEqual(['a|global|Mod+K', 'b|tree|Mod+Shift+B']);
    await user.click(screen.getByRole('button', { name: 'quitar' }));
    expect(list()).toEqual(['a|global|Mod+K']);
  });

  it('usa siempre la última función run sin volver a registrar', async () => {
    const user = userEvent.setup();
    const seen: number[] = [];
    function Counter() {
      const [count, setCount] = useState(0);
      useShortcut({ id: 'inc', scope: 'global', keys: 'Mod+J', labelKey: 'x', run: () => seen.push(count) });
      return (
        <button type="button" onClick={() => setCount((c) => c + 1)}>
          sumar
        </button>
      );
    }
    render(
      <Shell>
        <Counter />
      </Shell>,
    );
    await user.keyboard('{Control>}j{/Control}');
    await user.click(screen.getByRole('button', { name: 'sumar' }));
    await user.keyboard('{Control>}j{/Control}');
    expect(seen).toEqual([0, 1]);
  });

  it('sin proveedor no hace nada y el registro está vacío', () => {
    render(
      <>
        <Probe def={{ id: 'a', scope: 'global', keys: 'Mod+K' }} />
        <RegistryList />
        <ConflictList />
      </>,
    );
    expect(screen.queryAllByRole('listitem')).toHaveLength(0);
  });
});

describe('conflictos de teclas', () => {
  it('detecta la misma combinación en el mismo ámbito y avisa una sola vez', () => {
    const onConflict = vi.fn();
    render(
      <Shell onConflict={onConflict}>
        <Probe def={{ id: 'grid.a', scope: 'grid', keys: 'Mod+D' }} />
        <Probe def={{ id: 'grid.b', scope: 'grid', keys: 'Ctrl+d' }} />
        <ConflictList />
      </Shell>,
    );
    expect(onConflict).toHaveBeenCalledTimes(1);
    expect(onConflict).toHaveBeenCalledWith({
      kind: 'duplicate',
      keys: 'Mod+D',
      scope: 'grid',
      ids: ['grid.a', 'grid.b'],
    });
    expect(screen.getAllByRole('listitem').map((li) => li.textContent)).toEqual(['duplicate:grid:grid.a+grid.b']);
  });

  it('la misma tecla en ámbitos distintos no es conflicto; un ámbito que tapa a un global, sí (shadow)', () => {
    const onConflict = vi.fn();
    render(
      <Shell onConflict={onConflict}>
        <Probe def={{ id: 'g', scope: 'grid', keys: 'Mod+D' }} />
        <Probe def={{ id: 't', scope: 'tree', keys: 'Mod+D' }} />
        <ConflictList />
      </Shell>,
    );
    expect(onConflict).not.toHaveBeenCalled();
    expect(screen.queryAllByRole('listitem')).toHaveLength(0);
  });

  it('un atajo de ámbito que tapa a uno global se avisa como shadow', () => {
    const onConflict = vi.fn();
    render(
      <Shell onConflict={onConflict}>
        <Probe def={{ id: 'close', scope: 'global', keys: 'Escape' }} />
        <Probe def={{ id: 'grid.cancel', scope: 'grid', keys: 'Escape' }} />
        <ConflictList />
      </Shell>,
    );
    expect(onConflict).toHaveBeenCalledWith({
      kind: 'shadow',
      keys: 'Escape',
      scope: 'grid',
      ids: ['grid.cancel', 'close'],
    });
    expect(screen.getByRole('listitem')).toHaveTextContent('shadow:grid:grid.cancel+close');
  });

  it('sin manejador propio avisa por consola en desarrollo', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    render(
      <Shell>
        <Probe def={{ id: 'a', scope: 'global', keys: 'Mod+K' }} />
        <Probe def={{ id: 'b', scope: 'global', keys: 'Mod+K' }} />
      </Shell>,
    );
    expect(warn).toHaveBeenCalledTimes(1);
    expect(String(warn.mock.calls[0]?.[0])).toContain('Mod+K');
    expect(String(warn.mock.calls[0]?.[0])).toContain('a, b');
    warn.mockRestore();
  });

  it('re-registrar el mismo id lo sustituye y desmontar el antiguo no borra al nuevo', async () => {
    const user = userEvent.setup();
    const first = vi.fn();
    const second = vi.fn();
    function Swap() {
      const [which, setWhich] = useState<'one' | 'two'>('one');
      return (
        <>
          <button type="button" onClick={() => setWhich('two')}>
            cambiar
          </button>
          <Probe key={which} def={{ id: 'same', scope: 'global', keys: 'Mod+L', run: which === 'one' ? first : second }} />
        </>
      );
    }
    render(
      <Shell>
        <Swap />
        <RegistryList />
      </Shell>,
    );
    await user.click(screen.getByRole('button', { name: 'cambiar' }));
    expect(screen.getAllByRole('listitem')).toHaveLength(1);
    await user.keyboard('{Control>}l{/Control}');
    expect(second).toHaveBeenCalledTimes(1);
    expect(first).not.toHaveBeenCalled();
  });
});
