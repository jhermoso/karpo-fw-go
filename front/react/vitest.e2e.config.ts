// e2e: vitest como ejecutor y playwright-core con el Edge o Chrome instalados (D-R18).
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    include: ['e2e/**/*.e2e.ts'],
    environment: 'node',
    testTimeout: 120_000,
    hookTimeout: 120_000,
    fileParallelism: false,
    passWithNoTests: true,
  },
});
