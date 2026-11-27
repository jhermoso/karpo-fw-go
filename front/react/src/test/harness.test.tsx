// Comprueba que el arnés de componentes funciona: jsdom, jest-dom, user-event y roles.
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it } from 'vitest';

function Counter() {
  const [count, setCount] = useState(0);
  return (
    <button type="button" aria-pressed={count > 0} onClick={() => setCount(count + 1)}>
      Pulsado {count}
    </button>
  );
}

describe('arnés de componentes', () => {
  it('consulta por rol y nombre accesible y simula el teclado', async () => {
    const user = userEvent.setup();
    render(<Counter />);
    const button = screen.getByRole('button', { name: 'Pulsado 0' });
    expect(button).toHaveAttribute('aria-pressed', 'false');
    button.focus();
    await user.keyboard('{Enter}');
    expect(screen.getByRole('button', { name: 'Pulsado 1' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('aporta las API que jsdom no tiene', () => {
    expect(window.matchMedia('(prefers-reduced-motion: reduce)').matches).toBe(false);
    expect(typeof ResizeObserver).toBe('function');
    expect(typeof IntersectionObserver).toBe('function');
    expect(typeof document.body.scrollIntoView).toBe('function');
  });
});
