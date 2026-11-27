// Iconos generados (D-R19, D-R37, B-1, M-11, M-15.1): el registro sale de `icon-data-core.ts` (síncrono) y
// `icon-data-extra.ts` (diferido); mientras un icono extra no llega se pinta un `svg` vacío del mismo tamaño;
// un nombre desconocido se ve (círculo con `data-kx-icon="missing"`) y avisa una vez en desarrollo, y ningún
// fichero de ejecución importa Phosphor.
import { act, render, waitFor } from '@testing-library/react';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CORE_ICON_NAMES, DIRECTIONAL_ICONS, ICON_NAMES, ThemeContext, type ThemeApi } from '../contracts';
import { Icon, resetMissingIconWarnings } from './Icon';
import { ICON_DATA_CORE } from './icon-data-core';
import { ICON_DATA_EXTRA } from './icon-data-extra';
import { ICON_DATA, iconData, iconSnapshot, loadExtraIcons, resetExtraIcons } from './icons';

/** Dibujo de cualquier nombre (núcleo + diferido), para comparar con lo que se pinta. */
const ALL_DATA = { ...ICON_DATA_EXTRA, ...ICON_DATA_CORE };

const SRC = path.resolve(__dirname, '..');

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) return sourceFiles(full);
    return /\.(ts|tsx)$/.test(entry) && !/\.test\.(ts|tsx)$/.test(entry) ? [full] : [];
  });
}

function themeOf(iconStyle: 'outline' | 'filled'): ThemeApi {
  return { info: { iconStyle } } as unknown as ThemeApi;
}

const svgOf = (container: HTMLElement) => container.querySelector('svg') as SVGSVGElement;

beforeEach(() => resetMissingIconWarnings());
afterEach(async () => {
  vi.restoreAllMocks();
  // Cada prueba deja los iconos diferidos cargados (como en setup.ts).
  await loadExtraIcons();
});

describe('datos de iconos', () => {
  it('hay un dibujo por cada nombre de ICON_NAMES (núcleo + diferido), con sólo los pesos regular y fill', () => {
    expect(Object.keys(ALL_DATA).sort()).toEqual([...ICON_NAMES].sort());
    for (const name of ICON_NAMES) {
      const data = ALL_DATA[name]!;
      expect(Object.keys(data).sort(), name).toEqual(['fill', 'regular']);
      for (const weight of ['regular', 'fill'] as const) {
        expect(data[weight].length, `${name}/${weight}`).toBeGreaterThan(0);
        for (const d of data[weight]) expect(d, `${name}/${weight}`).toMatch(/^M[-\d.,a-zA-Z\s]+$/);
      }
    }
  });

  it('un alias se dibuja con los mismos datos que el nombre de Phosphor al que apunta', () => {
    expect(ALL_DATA.delete).toBe(ALL_DATA.trash);
    expect(ALL_DATA['chevron-down']).toBe(ALL_DATA['caret-down']);
    expect(ALL_DATA.building).toBe(ALL_DATA.buildings);
  });

  it('iconData devuelve undefined para lo que no es un nombre', () => {
    expect(iconData('no-existe')).toBeUndefined();
    expect(iconData('constructor')).toBeUndefined();
    expect(iconData('trash')).toBe(ALL_DATA.trash);
  });

  it('ICON_DATA (público) es sólo el núcleo; los demás nombres se leen con iconData', () => {
    expect(ICON_DATA).toBe(ICON_DATA_CORE);
    for (const name of CORE_ICON_NAMES) expect(ICON_DATA[name], name).toBeDefined();
    const extraOnly = ICON_NAMES.filter((name) => !(name in ICON_DATA_CORE));
    expect(extraOnly.length).toBeGreaterThan(0);
    for (const name of extraOnly) expect(ICON_DATA[name], name).toBeUndefined();
  });

  it('ningún fichero de ejecución de src importa @phosphor-icons', () => {
    const offenders = sourceFiles(SRC).filter((file) => /from\s+['"]@phosphor-icons|import\(\s*['"]@phosphor-icons/.test(readFileSync(file, 'utf8')));
    expect(offenders.map((file) => path.relative(SRC, file))).toEqual([]);
  });
});

describe('Icon', () => {
  it('pinta cada nombre conocido con sus trazados y sin marca de «missing»', () => {
    for (const name of ICON_NAMES) {
      const { container, unmount } = render(<Icon name={name} />);
      const svg = svgOf(container);
      expect(svg, name).toHaveAttribute('data-th-icon', name);
      expect(svg, name).not.toHaveAttribute('data-kx-icon');
      expect(svg.querySelectorAll('path').length, name).toBeGreaterThan(0);
      expect(svg).toHaveAttribute('viewBox', '0 0 256 256');
      unmount();
    }
  });

  it('el peso sigue al tema: relleno en glossy, de contorno en el resto, y regular o fill a la fuerza', () => {
    const paths = (ui: React.ReactElement) => {
      const { container, unmount } = render(ui);
      const out = [...container.querySelectorAll('path')].map((p) => p.getAttribute('d'));
      unmount();
      return out;
    };
    const regular = paths(<Icon name="edit" />);
    const filled = paths(
      <ThemeContext.Provider value={themeOf('filled')}>
        <Icon name="edit" />
      </ThemeContext.Provider>,
    );
    expect(regular).toEqual([...ALL_DATA.edit!.regular]);
    expect(filled).toEqual([...ALL_DATA.edit!.fill]);
    expect(regular).not.toEqual(filled);
    expect(paths(<Icon name="edit" weight="fill" />)).toEqual(filled);
    // Un peso que no se genera (bold, light…) se dibuja como regular.
    expect(paths(<Icon name="edit" weight="bold" />)).toEqual(regular);
  });

  it('los direccionales llevan data-kx-mirror y la propiedad mirror lo fuerza o lo impide', () => {
    for (const name of DIRECTIONAL_ICONS) {
      const { container, unmount } = render(<Icon name={name} />);
      expect(svgOf(container), name).toHaveAttribute('data-kx-mirror', 'true');
      unmount();
    }
    const plain = render(<Icon name="plus" />);
    expect(svgOf(plain.container)).not.toHaveAttribute('data-kx-mirror');
    const forced = render(<Icon name="plus" mirror />);
    expect(svgOf(forced.container)).toHaveAttribute('data-kx-mirror', 'true');
    const blocked = render(<Icon name="chevron-right" mirror={false} />);
    expect(svgOf(blocked.container)).not.toHaveAttribute('data-kx-mirror');
  });

  it('decorativo con aria-hidden; con etiqueta, imagen con nombre', () => {
    const decorative = render(<Icon name="search" />);
    expect(svgOf(decorative.container)).toHaveAttribute('aria-hidden', 'true');
    expect(svgOf(decorative.container)).not.toHaveAttribute('role');
    const labelled = render(<Icon name="search" label="Buscar" />);
    expect(svgOf(labelled.container)).toHaveAttribute('role', 'img');
    expect(svgOf(labelled.container)).toHaveAttribute('aria-label', 'Buscar');
    expect(svgOf(labelled.container)).not.toHaveAttribute('aria-hidden');
  });
});

describe('Icon con un icono del trozo diferido (M-11)', () => {
  /** Un nombre que no es del núcleo. */
  const EXTRA = ICON_NAMES.find((name) => !(name in ICON_DATA_CORE))!;

  it('el icono extra no está en el núcleo y los del núcleo se leen sin esperar', () => {
    expect(EXTRA in ICON_DATA_CORE).toBe(false);
    for (const name of CORE_ICON_NAMES) expect(iconData(name), name).toBeDefined();
  });

  it('antes de llegar su dato pinta un svg vacío del mismo tamaño y, al llegar, lo dibuja sin cambiar las medidas', async () => {
    resetExtraIcons();
    expect(iconSnapshot(EXTRA)).toBe('pending');
    expect(iconData(EXTRA)).toBeUndefined();

    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { container } = render(<Icon name={EXTRA} />);
    const before = svgOf(container);
    const sizeBefore = [before.getAttribute('width'), before.getAttribute('height'), before.getAttribute('viewBox')];
    expect(before).toHaveAttribute('data-kx-icon', 'pending');
    expect(before.querySelector('path')).toBeNull();
    expect(before.querySelector('circle')).toBeNull();
    expect(before).toHaveAttribute('aria-hidden', 'true');
    expect(sizeBefore).toEqual(['1em', '1em', '0 0 256 256']);

    await waitFor(() => expect(svgOf(container).querySelectorAll('path').length).toBeGreaterThan(0));
    const after = svgOf(container);
    expect([after.getAttribute('width'), after.getAttribute('height'), after.getAttribute('viewBox')]).toEqual(sizeBefore);
    expect(after).not.toHaveAttribute('data-kx-icon');
    expect(iconData(EXTRA)).toBeDefined();
    // Mientras cargaba no era un nombre desconocido: ningún aviso de «desconocido».
    expect(warn).not.toHaveBeenCalled();
  });

  it('los iconos del núcleo se pintan en el primer fotograma aunque el trozo diferido no haya llegado', () => {
    resetExtraIcons();
    const { container } = render(<Icon name="x" />);
    expect(svgOf(container)).not.toHaveAttribute('data-kx-icon');
    expect(svgOf(container).querySelectorAll('path').length).toBeGreaterThan(0);
  });

  it('un nombre desconocido no queda «pendiente»: es un círculo desde el principio', () => {
    resetExtraIcons();
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { container } = render(<Icon name="no-es-un-icono" />);
    expect(svgOf(container)).toHaveAttribute('data-kx-icon', 'missing');
  });

  it('loadExtraIcons es idempotente: una sola importación, la misma promesa', async () => {
    resetExtraIcons();
    const first = loadExtraIcons();
    expect(loadExtraIcons()).toBe(first);
    await act(async () => {
      await first;
    });
    expect(iconSnapshot(EXTRA)).not.toBe('pending');
  });
});

describe('Icon con un nombre desconocido (B-1)', () => {
  it('pinta un círculo con data-kx-icon="missing" y avisa UNA vez por nombre en desarrollo', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const first = render(<Icon name="icono-que-no-existe" />);
    const svg = svgOf(first.container);
    expect(svg).toHaveAttribute('data-kx-icon', 'missing');
    expect(svg).toHaveAttribute('data-th-icon', 'icono-que-no-existe');
    expect(svg.querySelector('circle')).not.toBeNull();
    expect(svg.querySelector('path')).toBeNull();
    expect(warn).toHaveBeenCalledTimes(1);
    expect(String(warn.mock.calls[0]?.[0])).toContain('icono-que-no-existe');

    // Más pintados del mismo nombre, también tras un rerender: sin avisos nuevos.
    first.rerender(<Icon name="icono-que-no-existe" weight="fill" />);
    render(<Icon name="icono-que-no-existe" />);
    expect(warn).toHaveBeenCalledTimes(1);

    // Otro nombre desconocido, otro aviso.
    render(<Icon name="otro-que-tampoco" />);
    expect(warn).toHaveBeenCalledTimes(2);
  });

  it('no avisa fuera de desarrollo, pero el círculo sigue marcado', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    vi.stubEnv('DEV', false);
    try {
      const { container } = render(<Icon name="silencioso" />);
      expect(svgOf(container)).toHaveAttribute('data-kx-icon', 'missing');
      expect(warn).not.toHaveBeenCalled();
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it('un nombre conocido no avisa', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    render(<Icon name="buildings" />);
    expect(warn).not.toHaveBeenCalled();
  });
});
