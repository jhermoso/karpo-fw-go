// Hoja de MapLibre de @karpo/react: emite dist/karpo-map.css desde src/map/maplibre.css (D-R24).
// Se ejecuta después de vite.lib.config.ts desde `npm run build:lib`, sin vaciar `dist`.
// La entrada es la propia hoja (no es modo biblioteca: ahí Vite no admite entradas CSS); al cerrar
// el paquete se descarta el JavaScript vacío que Vite deja al lado.
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { defineConfig, type Plugin } from 'vite';

const here = path.dirname(fileURLToPath(import.meta.url));

function dropEmptyChunks(): Plugin {
  return {
    name: 'karpo-map-css-drop-chunks',
    generateBundle(_options, bundle) {
      for (const [name, file] of Object.entries(bundle)) {
        if (file.type === 'chunk') delete bundle[name];
      }
    },
  };
}

/** Lo que usa este fichero de `scripts/avisos.mjs` (JavaScript sin tipos, importado en diferido). */
interface Notices {
  withBanner(css: string, cssFile: string, input: { root: string }): string;
}

/**
 * Antepone a `karpo-map.css` un comentario legal `/*!` que cita THIRD-PARTY-NOTICES.md (B-1): la hoja de
 * MapLibre es BSD-3-Clause y exige conservar su aviso. El fichero de avisos lo emite `vite.lib.config.ts`.
 */
function mapCssBanner(): Plugin {
  return {
    name: 'karpo-map-css-banner',
    enforce: 'post',
    generateBundle: {
      order: 'post',
      async handler(_options, bundle) {
        const notices = (await import(pathToFileURL(path.join(here, 'scripts', 'avisos.mjs')).href)) as Notices;
        for (const file of Object.values(bundle)) {
          if (file.type === 'asset' && file.fileName === 'karpo-map.css') {
            file.source = notices.withBanner(String(file.source), 'karpo-map.css', { root: here });
          }
        }
      },
    },
  };
}

export default defineConfig({
  plugins: [dropEmptyChunks(), mapCssBanner()],
  build: {
    outDir: 'dist',
    emptyOutDir: false,
    sourcemap: false,
    rollupOptions: {
      input: path.resolve(here, 'src/map/maplibre.css'),
      output: { assetFileNames: 'karpo-map[extname]' },
    },
  },
});
