// Stylelint: estándar + abstinencia de literales (PLAN §4.5 c).
// Todo color, tipografía, espaciado y radio sale de las variables del tema (`var(--th-*)`/`--kx-*`).
// Única excepción: src/theme/kx-structure.css (escala z-index y poco más).

/** @type {import('stylelint').Config} */
export default {
  extends: ['stylelint-config-standard'],
  allowEmptyInput: true,
  ignoreFiles: ['dist/**', 'coverage/**', 'demo/dist/**', 'demo/public/themes/**', 'node_modules/**'],
  rules: {
    // Colores literales
    'color-no-hex': [true, { message: 'Sin colores literales: usa var(--th-*) o color-mix() sobre ellas.' }],
    'color-named': 'never',
    'function-disallowed-list': [
      ['rgb', 'rgba', 'hsl', 'hsla', 'hwb', 'lab', 'lch', 'oklab', 'oklch', 'color'],
      { message: 'Sin colores literales: usa var(--th-*) o color-mix() sobre ellas.' },
    ],
    // Longitudes literales (en @container, que no se comprueba aquí, se admiten em)
    'unit-disallowed-list': [
      ['px', 'rem', 'em', 'vh', 'vw', 'vmin', 'vmax', 'pt', 'pc', 'cm', 'mm', 'in', 'ch', 'ex'],
      { message: 'Sin unidades literales: usa var(--th-space-*), var(--th-radius-*) o calc() sobre ellas.' },
    ],
    // Tipografía y escala de capas
    'property-disallowed-list': [
      ['font-family'],
      { message: 'La familia tipográfica sale de var(--th-type-*) (el tema la aplica).' },
    ],
    'declaration-property-value-disallowed-list': [
      {
        font: ['/^(?!inherit$).+/'],
        'font-weight': ['/^[0-9]+$/'],
        'z-index': ['/^-?[0-9]+$/'],
      },
      { message: 'Sin valores literales: usa var(--th-*) o var(--kx-z-*).' },
    ],
    'declaration-no-important': true,
    // Sin `:dir()` (D-R22): lightningcss lo compila a `:lang(ar,he,…)`, que no sigue `?dir=rtl` ni `<html dir>`.
    'selector-pseudo-class-disallowed-list': [
      ['dir'],
      {
        message:
          'Sin `:dir()` (D-R22): lightningcss lo compila a `:lang(ar,he,…)` y no sirve con `?dir=rtl`. ' +
          'Usa `[dir="rtl"]` o `:where([dir="rtl"])`.',
      },
    ],
  },
  overrides: [
    {
      files: ['src/theme/kx-structure.css'],
      rules: {
        'unit-disallowed-list': null,
        'declaration-property-value-disallowed-list': null,
      },
    },
  ],
};
