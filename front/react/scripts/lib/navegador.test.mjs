import { describe, expect, it } from 'vitest';
import { launchBrowser, launchBrowserInfo } from './navegador.mjs';

function fakeChromium(installed) {
  const attempts = [];
  return {
    attempts,
    async launch(options) {
      attempts.push(options);
      if (!installed.includes(options.channel)) throw new Error(`no hay ${options.channel}`);
      return { channel: options.channel };
    },
  };
}

describe('launchBrowser', () => {
  it('prueba msedge primero y usa SwiftShader', async () => {
    const chromium = fakeChromium(['msedge', 'chrome']);
    const info = await launchBrowserInfo({ chromium });
    expect(info?.channel).toBe('msedge');
    expect(chromium.attempts).toHaveLength(1);
    expect(chromium.attempts[0].args).toContain('--enable-unsafe-swiftshader');
  });

  it('cae a chrome si no hay Edge', async () => {
    const chromium = fakeChromium(['chrome']);
    const info = await launchBrowserInfo({ chromium });
    expect(info?.channel).toBe('chrome');
    expect(chromium.attempts.map((a) => a.channel)).toEqual(['msedge', 'chrome']);
  });

  it('devuelve null si no hay ninguno', async () => {
    expect(await launchBrowser({ chromium: fakeChromium([]) })).toBeNull();
  });

  it('launchBrowser devuelve el navegador', async () => {
    const browser = await launchBrowser({ chromium: fakeChromium(['chrome']) });
    expect(browser).toEqual({ channel: 'chrome' });
  });
});
