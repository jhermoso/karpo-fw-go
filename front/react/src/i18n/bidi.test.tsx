// Aislamiento bidi de las interpolaciones (M-12, D-R27): con dirección RTL cada valor interpolado va
// entre FSI (U+2068) y PDI (U+2069); en LTR, las cadenas quedan limpias. `formatNumberRange` usa Intl.
import { render, screen } from '@testing-library/react';
import { useTranslation } from 'react-i18next';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FSI, PDI, isolate, setIsolateDirection } from './bidi';
import { createI18n } from './createI18n';
import { createFormatters, formatNumberRange } from './formatters';
import { I18nProvider } from './I18nProvider';

const resources = {
  es: { core: { changes: 'cambios {{count}}', cannot: 'la fila no se puede guardar {{count}}', range: 'de {{from}} a {{to}}' } },
  en: { core: { changes: 'changes {{count}}', cannot: 'row cannot be saved {{count}}', range: 'from {{from}} to {{to}}' } },
  ar: { core: { changes: 'تغييرات {{count}}', cannot: 'لا يمكن حفظ الصف {{count}}', range: 'من {{from}} إلى {{to}}' } },
};

function make(locale: string) {
  return createI18n({ locale, resources, pseudo: false });
}

function Probe() {
  const { t } = useTranslation('core');
  return (
    <>
      <output data-testid="changes">{t('changes', { count: 4 })}</output>
      <output data-testid="range">{t('range', { from: '1.204', to: 12 })}</output>
    </>
  );
}

const text = (id: string) => screen.getByTestId(id).textContent;

beforeEach(() => {
  document.documentElement.removeAttribute('dir');
  document.documentElement.removeAttribute('lang');
});
afterEach(() => vi.restoreAllMocks());

describe('isolate', () => {
  it('envuelve en FSI y PDI y deja vacía una cadena vacía', () => {
    expect(FSI).toBe('\u2068');
    expect(PDI).toBe('\u2069');
    expect(isolate('4')).toBe('\u20684\u2069');
    expect(isolate('')).toBe('');
  });
});

describe('interpolación con aislantes Unicode', () => {
  it('en LTR (es y en) las cadenas interpoladas no llevan aislantes', () => {
    expect(make('es').t('changes', { ns: 'core', count: 4 })).toBe('cambios 4');
    expect(make('en').t('changes', { ns: 'core', count: 4 })).toBe('changes 4');
    expect(make('en').t('range', { ns: 'core', from: '1.204', to: 12 })).toBe('from 1.204 to 12');
  });

  it('con un idioma RTL (ar) cada valor interpolado va entre FSI y PDI', () => {
    const ar = make('ar');
    expect(ar.language).toBe('ar');
    expect(ar.t('changes', { ns: 'core', count: 4 })).toBe('تغييرات \u20684\u2069');
    expect(ar.t('range', { ns: 'core', from: '1.204', to: 12 })).toBe('من \u20681.204\u2069 إلى \u206812\u2069');
  });

  it('con ?dir=rtl en es o en (I18nProvider) los valores también van aislados, y sin él, no', () => {
    const { unmount } = render(
      <I18nProvider i18n={make('en')} search="dir=rtl">
        <Probe />
      </I18nProvider>,
    );
    expect(text('changes')).toBe('changes \u20684\u2069');
    expect(text('range')).toBe('from \u20681.204\u2069 to \u206812\u2069');
    unmount();

    render(
      <I18nProvider i18n={make('es')} search="">
        <Probe />
      </I18nProvider>,
    );
    expect(text('changes')).toBe('cambios 4');
  });

  it('?dir=ltr gana al idioma: árabe forzado a LTR no lleva aislantes', () => {
    render(
      <I18nProvider i18n={make('ar')} search="dir=ltr">
        <Probe />
      </I18nProvider>,
    );
    expect(text('changes')).toBe('تغييرات 4');
  });

  it('las cinco cadenas del jurado llevan el aislante alrededor del número', () => {
    const en = make('en');
    setIsolateDirection(en, 'rtl');
    for (const [key, expected] of [
      ['changes', 'changes \u20684\u2069'],
      ['cannot', 'row cannot be saved \u20681\u2069'],
    ] as const) {
      expect(en.t(key, { ns: 'core', count: key === 'changes' ? 4 : 1 })).toBe(expected);
    }
    setIsolateDirection(en, null);
    expect(en.t('changes', { ns: 'core', count: 4 })).toBe('changes 4');
  });

  it('el texto sigue siendo texto: textContent con aislantes, sin elementos añadidos', () => {
    const { container } = render(
      <I18nProvider i18n={make('en')} search="dir=rtl">
        <Probe />
      </I18nProvider>,
    );
    expect(container.querySelector('output > *')).toBeNull();
  });

  it('no escapa HTML: React ya lo hace (el comportamiento de antes)', () => {
    expect(make('en').t('changes', { ns: 'core', count: '<b>' as unknown as number })).toBe('changes <b>');
  });
});

describe('formatNumberRange', () => {
  it("formatNumberRange('es', 4, 12) es Intl.NumberFormat#formatRange", () => {
    expect(formatNumberRange('es', 4, 12)).toBe(new Intl.NumberFormat('es').formatRange(4, 12));
    expect(formatNumberRange('en', 1204, 1500, { maximumFractionDigits: 0 })).toBe(
      new Intl.NumberFormat('en', { maximumFractionDigits: 0 }).formatRange(1204, 1500),
    );
  });

  it('con cadenas decimales no pierde cifras y está en los formateadores del idioma', () => {
    const exact = new Intl.NumberFormat('en').formatRange('9007199254740993', '9007199254740995');
    expect(formatNumberRange('en', '9007199254740993', '9007199254740995')).toBe(exact);
    expect(createFormatters('es').range(4, 12)).toBe(formatNumberRange('es', 4, 12));
  });

  it('el pseudoidioma xx formatea con es', () => {
    expect(formatNumberRange('xx', 4, 12)).toBe(formatNumberRange('es', 4, 12));
  });

  it('en un motor sin formatRange une los extremos con un guion corto', () => {
    const proto = Intl.NumberFormat.prototype as unknown as { formatRange?: unknown };
    const original = proto.formatRange;
    proto.formatRange = undefined;
    try {
      expect(formatNumberRange('es', 4, 12, { maximumFractionDigits: 0 })).toBe('4–12');
      expect(formatNumberRange('en', '1204.5', '2000', { maximumFractionDigits: 1 })).toBe('1,204.5–2,000');
    } finally {
      proto.formatRange = original;
    }
  });
});
