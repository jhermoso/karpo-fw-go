// ESLint (flat). Incluye la regla de abstinencia de estilos en línea (PLAN §4.5 d): `style={{…}}`
// sólo admite variables de disposición `--kx-*` y las propiedades de disposición permitidas.
import js from '@eslint/js';
import reactHooks from 'eslint-plugin-react-hooks';
import { defineConfig, globalIgnores } from 'eslint/config';
import globals from 'globals';
import tseslint from 'typescript-eslint';

/** Propiedades de disposición que pueden ir en `style` además de las variables `--kx-*`. */
const LAYOUT_PROPERTIES = '^(transform|inset|width|height|inlineSize|blockSize)$';

const STYLE_OBJECT = "JSXAttribute[name.name='style'] > JSXExpressionContainer > ObjectExpression";
const STYLE_MESSAGE =
  'En `style` sólo se admiten variables `--kx-*` y transform/inset/width/height/inlineSize/blockSize ' +
  '(colores, fuentes, radios y espacios salen de las variables del tema; PLAN §4.5 d).';

const STYLE_RULES = [
  {
    selector: `${STYLE_OBJECT} > Property[key.type='Identifier']:not([key.name=/${LAYOUT_PROPERTIES}/])`,
    message: STYLE_MESSAGE,
  },
  {
    selector: `${STYLE_OBJECT} > Property[key.type='Literal']:not([key.value=/^--kx-/])`,
    message: STYLE_MESSAGE,
  },
  {
    selector: `${STYLE_OBJECT} > Property[computed=true]`,
    message: STYLE_MESSAGE,
  },
  {
    selector: `${STYLE_OBJECT} > SpreadElement`,
    message: STYLE_MESSAGE,
  },
];

/** Los textos de la configuración salen por `useText()`/`resolveText`: nadie lee `.literal` (B-2, ronda 2). */
const LITERAL_MESSAGE =
  'No leas `.literal` de un `Text`: resuelve con `useText()` o `resolveText()` (clave, idioma y diccionario ' +
  'antes que el literal; B-2). Sólo lo leen `src/config` y `src/fields/values.ts`.';
const LITERAL_RULES = [
  { selector: "MemberExpression[computed=false][property.name='literal']", message: LITERAL_MESSAGE },
  { selector: "MemberExpression[computed=true][property.value='literal']", message: LITERAL_MESSAGE },
  { selector: "ObjectPattern > Property[key.name='literal']", message: LITERAL_MESSAGE },
];

export default defineConfig([
  globalIgnores([
    'dist/**',
    'coverage/**',
    'demo/dist/**',
    'demo/public/**',
    'node_modules/**',
    '.karpo-local/**',
  ]),
  js.configs.recommended,
  tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { globals: globals.browser },
    rules: {
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrorsIgnorePattern: '^_' },
      ],
      '@typescript-eslint/consistent-type-imports': ['error', { fixStyle: 'inline-type-imports' }],
    },
  },
  {
    files: ['**/*.{js,mjs,cjs}'],
    rules: {
      'no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrorsIgnorePattern: '^_' },
      ],
    },
  },
  {
    // Código de herramientas y de pruebas en Node.
    files: [
      '*.{js,ts}',
      'build/**',
      'scripts/**',
      'e2e/**',
      'src/test/**',
      '**/*.test.{ts,tsx,mjs}',
      'demo/mocks/**',
      'demo/data/**',
    ],
    languageOptions: { globals: { ...globals.node, ...globals.browser } },
  },
  {
    files: ['**/*.{js,mjs}'],
    ignores: ['demo/**'],
    languageOptions: { globals: globals.node },
  },
  {
    files: ['src/**/*.{ts,tsx}', 'demo/**/*.{ts,tsx}'],
    ...reactHooks.configs.flat.recommended,
  },
  {
    files: ['src/**/*.{ts,tsx}', 'demo/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-syntax': [
        'error',
        ...LITERAL_RULES,
        ...STYLE_RULES,
      ],
    },
  },
  {
    // Quienes sí leen el literal: la configuración y los visores de valores (la regla de `style` sigue).
    files: ['src/config/**/*.{ts,tsx}', 'src/fields/values.ts'],
    rules: {
      'no-restricted-syntax': [
        'error',
        ...STYLE_RULES,
      ],
    },
  },
]);
