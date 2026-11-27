// Pruebas: tres proyectos (unit, components, contract) y cobertura v8 (PLAN §6).
//   unit        node   src/**/*.test.ts, scripts/**/*.test.mjs, build/**/*.test.ts
//   components  jsdom  src/**/*.test.tsx
//   contract    jsdom  src/**/*.contract.test.ts
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [react()],
  test: {
    passWithNoTests: true,
    // Margen para la primera transformación en frío de librerías grandes (Phosphor, MapLibre).
    testTimeout: 20_000,
    projects: [
      {
        extends: true,
        test: {
          name: 'unit',
          environment: 'node',
          include: ['src/**/*.test.ts', 'scripts/**/*.test.mjs', 'build/**/*.test.ts'],
          exclude: ['**/node_modules/**', 'src/**/*.contract.test.ts'],
        },
      },
      {
        extends: true,
        test: {
          name: 'components',
          environment: 'jsdom',
          include: ['src/**/*.test.tsx'],
          setupFiles: ['src/test/setup.ts'],
        },
      },
      {
        extends: true,
        test: {
          name: 'contract',
          environment: 'jsdom',
          include: ['src/**/*.contract.test.ts'],
          setupFiles: ['src/test/setup.ts'],
        },
      },
    ],
    coverage: {
      provider: 'v8',
      reporter: ['text-summary', 'html'],
      reportsDirectory: 'coverage',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/test/**', 'src/contracts/**', 'src/**/*.d.ts'],
      thresholds: {
        'src/config/**': { lines: 80 },
        'src/api/**': { lines: 80 },
        'src/fields/**': { lines: 80 },
        'src/grid/store*': { lines: 80 },
      },
    },
  },
});
