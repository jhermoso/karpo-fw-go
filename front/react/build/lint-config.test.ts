// Comprueba que la abstinencia de literales (PLAN §4.5 c y d) y las reglas de la ronda 2 se aplican de verdad:
// sin `:dir()` en el CSS (D-R22) y sin `.literal` de un `Text` fuera de `src/config` y `src/fields/values.ts`.
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';
import stylelint from 'stylelint';
import { describe, expect, it } from 'vitest';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

describe('eslint: style en línea', () => {
  const eslint = new ESLint({ cwd: root, overrideConfigFile: path.join(root, 'eslint.config.js') });

  async function messages(source: string): Promise<string[]> {
    const [result] = await eslint.lintText(source, { filePath: path.join(root, 'src/ui/Prueba.tsx') });
    return (result?.messages ?? []).filter((m) => m.ruleId === 'no-restricted-syntax').map((m) => m.message);
  }

  const wrap = (style: string) => `export const A = () => <div style={${style}} />;\n`;

  it('permite variables --kx-* y las propiedades de disposición', async () => {
    expect(await messages(wrap("{ '--kx-col-width': '10px', '--kx-row-index': 3 }"))).toEqual([]);
    expect(await messages(wrap('{ transform: `translateY(${y}px)`, width: w, height: h }'))).toEqual([]);
    expect(await messages(wrap('{ inset: 0, inlineSize: a, blockSize: b }'))).toEqual([]);
  });

  it.each([
    ['color', "{ color: 'red' }"],
    ['background', "{ background: 'var(--th-surface)' }"],
    ['fontFamily', "{ fontFamily: 'serif' }"],
    ['borderRadius', '{ borderRadius: 4 }'],
    ['margin', '{ margin: 0 }'],
    ['variable ajena', "{ '--th-accent': 'red' }"],
    ['clave calculada', '{ [name]: 1 }'],
    ['spread', '{ ...extra }'],
  ])('prohíbe %s', async (_name, style) => {
    expect((await messages(wrap(style))).length).toBeGreaterThan(0);
  });

  it('no molesta fuera de style', async () => {
    expect(await messages("export const o = { color: 'red' };\n")).toEqual([]);
  });
});

describe('eslint: nadie lee `.literal` de un Text fuera de la configuración (B-2)', () => {
  const eslint = new ESLint({ cwd: root, overrideConfigFile: path.join(root, 'eslint.config.js') });

  async function literalMessages(source: string, file: string): Promise<string[]> {
    const [result] = await eslint.lintText(source, { filePath: path.join(root, file) });
    return (result?.messages ?? []).filter((m) => m.ruleId === 'no-restricted-syntax').map((m) => m.message);
  }

  const read = 'export const f = (t: { literal: string }) => t.literal;\n';

  it.each([
    ['acceso con punto', read],
    ['acceso con corchetes', "export const f = (t: { literal: string }) => t['literal'];\n"],
    ['acceso opcional', 'export const f = (t?: { literal: string }) => t?.literal;\n'],
    ['desestructuración', 'export const f = ({ literal }: { literal: string }) => literal;\n'],
    ['encadenado', 'export const f = (o: { label: { literal: string } }) => o.label.literal.toLowerCase();\n'],
  ])('lo prohíbe en un componente (%s)', async (_name, source) => {
    const messages = await literalMessages(source, 'src/grid/Prueba.tsx');
    expect(messages.length).toBeGreaterThan(0);
    expect(messages.join(' ')).toContain('useText()');
  });

  it('lo prohíbe también en las pruebas y en la demo', async () => {
    expect((await literalMessages(read, 'src/grid/Prueba.test.tsx')).length).toBeGreaterThan(0);
    expect((await literalMessages(read, 'demo/mocks/prueba.ts')).length).toBeGreaterThan(0);
  });

  it.each(['src/config/prueba.ts', 'src/config/sub/prueba.ts', 'src/fields/values.ts'])('lo permite en %s', async (file) => {
    expect(await literalMessages(read, file)).toEqual([]);
  });

  it('crear un objeto con la clave `literal` no es leerla', async () => {
    expect(await literalMessages("export const t = { literal: 'Hola', key: 'saludo' };\n", 'src/grid/Prueba.tsx')).toEqual([]);
  });

  it('en las excepciones sigue la regla de style en línea', async () => {
    const source = "export const A = () => <div style={{ color: 'red' }} />;\n";
    expect((await literalMessages(source, 'src/config/Prueba.tsx')).length).toBeGreaterThan(0);
  });
});

describe('stylelint: abstinencia de literales', () => {
  async function warnings(code: string, file = 'src/grid/grid.css'): Promise<string[]> {
    const result = await stylelint.lint({
      code,
      codeFilename: path.join(root, file),
      configFile: path.join(root, 'stylelint.config.js'),
    });
    return result.results.flatMap((r) => r.warnings.map((w) => w.rule));
  }

  const ok = `@layer karpo.components {
  .kx-cell {
    padding: var(--th-space-2) calc(var(--th-space-3) * 2);
    color: var(--th-text-primary);
    background: color-mix(in srgb, var(--th-surface-base) 80%, transparent);
    z-index: var(--kx-z-sticky);
    font-weight: var(--th-type-ui-weight);
    border-radius: var(--th-radius-sm);
  }

  @container (min-width: 40em) {
    .kx-cell {
      padding: var(--th-space-4);
    }
  }
}
`;

  it('acepta CSS hecho sólo de variables, color-mix() y calc()', async () => {
    expect(await warnings(ok)).toEqual([]);
  });

  it.each([
    ['hex', '.a {\n  color: #fff;\n}\n', 'color-no-hex'],
    ['rgb', '.a {\n  color: rgb(0 0 0);\n}\n', 'function-disallowed-list'],
    ['hsl', '.a {\n  color: hsl(0 0% 0%);\n}\n', 'function-disallowed-list'],
    ['oklch', '.a {\n  color: oklch(0.5 0.1 20);\n}\n', 'function-disallowed-list'],
    ['nombre de color', '.a {\n  color: red;\n}\n', 'color-named'],
    ['px', '.a {\n  margin: 4px;\n}\n', 'unit-disallowed-list'],
    ['rem', '.a {\n  padding: 1rem;\n}\n', 'unit-disallowed-list'],
    ['vh', '.a {\n  block-size: 100vh;\n}\n', 'unit-disallowed-list'],
    ['font-family', '.a {\n  font-family: serif;\n}\n', 'property-disallowed-list'],
    ['font-weight numérico', '.a {\n  font-weight: 600;\n}\n', 'declaration-property-value-disallowed-list'],
    ['z-index numérico', '.a {\n  z-index: 10;\n}\n', 'declaration-property-value-disallowed-list'],
    ['!important', '.a {\n  color: var(--th-text-primary) !important;\n}\n', 'declaration-no-important'],
  ])('rechaza %s', async (_name, css, rule) => {
    expect(await warnings(css)).toContain(rule);
  });

  it.each([
    ['`:dir(rtl)`', '.a:dir(rtl) {\n  margin-inline: 0;\n}\n'],
    ['`:dir(ltr)`', '.a:dir(ltr) .b {\n  margin-inline: 0;\n}\n'],
    ['`:dir()` dentro de :is()', ':is(.a, .b:dir(rtl)) {\n  margin-inline: 0;\n}\n'],
    [
      '`:dir()` en @layer y @container',
      '@layer karpo.components {\n  @container (min-width: 40em) {\n    .a:dir(rtl) {\n      margin-inline: 0;\n    }\n  }\n}\n',
    ],
  ])('rechaza %s (D-R22)', async (_name, css) => {
    const result = await stylelint.lint({
      code: css,
      codeFilename: path.join(root, 'src/grid/grid.css'),
      configFile: path.join(root, 'stylelint.config.js'),
    });
    const found = result.results
      .flatMap((r) => r.warnings)
      .filter((w) => w.rule === 'selector-pseudo-class-disallowed-list');
    expect(found.length).toBeGreaterThan(0);
    expect(found[0]?.text).toContain('[dir="rtl"]');
    expect(found[0]?.text).toContain('D-R22');
  });

  it('acepta [dir="rtl"] y :where([dir="rtl"])', async () => {
    const css = '[dir="rtl"] .a {\n  margin-inline: 0;\n}\n\n:where([dir="rtl"]) .b {\n  margin-inline: 0;\n}\n';
    expect(await warnings(css)).toEqual([]);
  });

  it('em sólo se admite dentro de @container', async () => {
    expect(await warnings('@container (min-width: 40em) {\n  .a {\n    display: none;\n  }\n}\n')).toEqual([]);
    expect(await warnings('@media (min-width: 40em) {\n  .a {\n    display: none;\n  }\n}\n')).toContain(
      'unit-disallowed-list',
    );
  });

  it('src/theme/kx-structure.css es la única excepción', async () => {
    const css = ':root {\n  --kx-z-sticky: 10;\n  --kx-unit: 4px;\n}\n\n.a {\n  z-index: 10;\n}\n';
    expect(await warnings(css, 'src/theme/kx-structure.css')).toEqual([]);
    expect(await warnings(css, 'src/theme/otro.css')).toContain('declaration-property-value-disallowed-list');
  });
});
