// CSS de las primitivas: sin la pseudoclase de dirección (D-R22) y sin teclas de atajo en pantallas táctiles.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';

const SRC = path.resolve(__dirname, '..');
const css = readFileSync(path.join(__dirname, 'ui.css'), 'utf8');
/** La pseudoclase de dirección, escrita en dos mitades para que el propio fichero no la contenga. */
const DIR_PSEUDO = [':', 'dir('].join('');

function cssFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) return cssFiles(full);
    return full.endsWith('.css') ? [full] : [];
  });
}

describe('ui.css', () => {
  it('espeja los iconos direccionales con [dir="rtl"], no con la pseudoclase de dirección', () => {
    expect(css).toMatch(/\[dir="rtl"\]\s+svg\[data-th-icon\]\[data-kx-mirror="true"\]\s*\{\s*transform:\s*scaleX\(-1\);/);
  });

  it('las teclas de un atajo conservan su orden con dir=rtl (direction: ltr aislado)', () => {
    const block = /\[data-kx-ui="shortcut-keys"\]\s*\{([^}]*)\}/.exec(css);
    expect(block?.[1]).toMatch(/direction:\s*ltr;/);
    expect(block?.[1]).toMatch(/unicode-bidi:\s*isolate;/);
  });

  it('ningún CSS de ui, fields, i18n, theme ni help usa la pseudoclase de dirección', () => {
    for (const folder of ['ui', 'fields', 'i18n', 'theme', 'help']) {
      for (const file of cssFiles(path.join(SRC, folder))) {
        expect(readFileSync(file, 'utf8'), path.relative(SRC, file)).not.toContain(DIR_PSEUDO);
      }
    }
  });

  it('oculta las teclas de atajo con (hover: none), (pointer: coarse); aria-keyshortcuts no depende del CSS', () => {
    const block = /@media\s*\(hover:\s*none\),\s*\(pointer:\s*coarse\)\s*\{([^}]*\{[^}]*\}[^}]*)\}/.exec(css);
    expect(block, 'falta el bloque @media (hover: none), (pointer: coarse)').not.toBeNull();
    expect(block?.[1]).toMatch(/\[data-kx-ui="shortcut-keys"\]\s*\{\s*display:\s*none;/);
  });
});
