// Arranque de la app de demostración (no se publica en el paquete).
//   --mode demo   MSW en el navegador: backend falso con datos deterministas, sin servidor.
//   --mode go     sin MSW: el proxy de Vite reenvía /api a cmd/karpo (npm run karpo:local).
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { KarpoApp } from '../src/index';

/** Origen de los temas generados por Theros Front (demo/public/themes/<tema>/). */
const THEME_ORIGIN = '/themes/';

async function startBackend(): Promise<void> {
  if (import.meta.env.MODE !== 'demo') return;
  // Importación diferida: el modo go no empaqueta MSW ni los datos de la demostración.
  const { startMockWorker } = await import('./mocks/browser');
  await startMockWorker();
}

function showStartupError(error: unknown): void {
  const box = document.createElement('p');
  box.setAttribute('role', 'alert');
  box.textContent = `No se pudo arrancar la demostración: ${error instanceof Error ? error.message : String(error)}`;
  document.body.append(box);
}

async function main(): Promise<void> {
  const container = document.getElementById('root');
  if (!container) throw new Error('Falta el elemento #root en demo/index.html');
  await startBackend();
  createRoot(container).render(
    <StrictMode>
      <KarpoApp manifestUrl="/config/manifest.json" themeOrigin={THEME_ORIGIN} apiBaseUrl="/api" router="browser" />
    </StrictMode>,
  );
}

main().catch(showStartupError);
