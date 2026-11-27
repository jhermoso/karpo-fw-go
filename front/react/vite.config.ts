// Demo y desarrollo (`npm run dev`, `npm run build:demo`).
// La biblioteca se construye con vite.lib.config.ts.
//   --mode demo   MSW en el navegador, sin servidor.
//   --mode go     sin MSW. Lo arranca `npm run karpo:local` con una configuración generada que añade
//                 el proxy de /api, /healthz y /readyz hacia cmd/karpo (Vite no admite `--mode local`).
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';
import { karpoConfigPlugin } from './build/karpo-config-plugin.ts';

const here = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig(() => {
  return {
    root: path.resolve(here, 'demo'),
    publicDir: path.resolve(here, 'demo/public'),
    envDir: here,
    plugins: [
      react(),
      karpoConfigPlugin({
        sourceDir: path.resolve(here, '../angular/config/entities'),
        extraEmit: [{ sourceDir: path.resolve(here, 'demo/config'), mountPath: '/config/' }],
      }),
    ],
    server: { host: '127.0.0.1' },
    preview: { host: '127.0.0.1' },
    build: {
      outDir: path.resolve(here, 'demo/dist'),
      emptyOutDir: true,
      sourcemap: true,
      // scripts/presupuesto.mjs lee demo/dist/.vite/manifest.json (D-R29).
      manifest: true,
    },
  };
});
