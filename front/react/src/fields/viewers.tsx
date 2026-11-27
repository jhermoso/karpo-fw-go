// Visores de los tipos de campo: ganchos de Theros (`data-th="badge"` + `data-th-tone`,
// `data-th-format="code"`) y texto con el formato del idioma. Las cifras no usan `data-th-format="amount"` (D-R35):
// el tema lo viste en monoespaciada; aquí llevan `tabular-nums` en la familia del cuerpo (fields.css).
import type { ComponentProps, ComponentType } from 'react';
import type { FieldDef, FieldKind, FieldTypeDef } from '../contracts';
import { Badge } from '../ui/Badge';
import { Icon } from '../ui/Icon';
import { Skeleton } from '../ui/Skeleton';
import { getCodec, isSafeUrl, toBoolean } from './codecs';
import { dateParts, dateTimeParts, toIsoDate, toIsoDateTime } from './dates';
import { EmptyValue } from './EmptyValue';
import { useFieldText, useLookupLabel } from './hooks';
import { findOption, isEmpty, toStringArray } from './values';

export type ViewerProps = ComponentProps<FieldTypeDef['Viewer']>;

/** Valor vacío: la marca única «—» o el `emptyValue` de la configuración si no es sólo guiones (D-R35). */
function Empty({ def }: { def: FieldDef }) {
  return <EmptyValue def={def} />;
}

function TextViewer({ value, def }: ViewerProps) {
  const { locale, resolve } = useFieldText();
  const text = getCodec(def.kind).format(value, def, locale, resolve);
  if (text === '') return <Empty def={def} />;
  // Texto libre del usuario o del servidor: se aísla (<bdi>) para que un nombre en otro sentido de escritura
  // no reordene lo que lo rodea (M-12).
  return (
    <bdi
      data-kx-ui="value"
      data-kx-kind={def.kind}
      data-th-format={def.format === 'code' ? 'code' : undefined}
    >
      {text}
    </bdi>
  );
}

function NumberViewer({ value, def }: ViewerProps) {
  const { locale, resolve } = useFieldText();
  const text = getCodec(def.kind).format(value, def, locale, resolve);
  if (text === '') return <Empty def={def} />;
  return (
    <span data-kx-ui="value" data-kx-kind={def.kind} data-kx-align="end">
      {text}
    </span>
  );
}

function DateViewer({ value, def }: ViewerProps) {
  const { locale, resolve } = useFieldText();
  const text = getCodec(def.kind).format(value, def, locale, resolve);
  if (text === '') return <Empty def={def} />;
  let machine: string | undefined;
  if (def.kind === 'date') {
    const parts = dateParts(value);
    machine = parts ? toIsoDate(parts) : undefined;
  } else {
    const parts = dateTimeParts(value);
    machine = parts ? toIsoDateTime(parts) : undefined;
  }
  return (
    <time data-kx-ui="value" data-kx-kind={def.kind} dateTime={machine}>
      {text}
    </time>
  );
}

function BooleanViewer({ value, def }: ViewerProps) {
  const { locale, resolve } = useFieldText();
  const flag = toBoolean(value);
  if (flag === undefined) return <Empty def={def} />;
  const label = getCodec('boolean').format(flag, def, locale, resolve);
  return (
    <span data-kx-ui="value" data-kx-kind="boolean" data-kx-value={flag ? 'true' : 'false'}>
      <Icon name={flag ? 'check' : 'minus'} />
      {label}
    </span>
  );
}

function SelectViewer({ value, def }: ViewerProps) {
  const { locale, resolve } = useFieldText();
  if (isEmpty(value)) return <Empty def={def} />;
  const option = findOption(def.options, value, locale, resolve);
  return (
    <span data-kx-ui="value" data-kx-kind="select">
      {option ? resolve(option.label) : String(value)}
    </span>
  );
}

function BadgeViewer({ value, def }: ViewerProps) {
  const { locale, resolve } = useFieldText();
  if (isEmpty(value)) return <Empty def={def} />;
  const option = findOption(def.options, value, locale, resolve);
  return (
    <Badge tone={option?.tone} data-kx-ui="value" data-kx-kind="badge">
      {option?.icon ? <Icon name={option.icon} /> : null}
      {option ? resolve(option.label) : String(value)}
    </Badge>
  );
}

function MultiSelectViewer({ value, def }: ViewerProps) {
  const { locale, resolve } = useFieldText();
  const items = toStringArray(value);
  if (items.length === 0) return <Empty def={def} />;
  return (
    <span data-kx-ui="value-list" data-kx-kind="multiselect">
      {items.map((item) => {
        const option = findOption(def.options, item, locale, resolve);
        return (
          <Badge key={item} tone={option?.tone}>
            {option ? resolve(option.label) : item}
          </Badge>
        );
      })}
    </span>
  );
}

function LookupViewer({ value, def }: ViewerProps) {
  const { label, loading } = useLookupLabel(def, value);
  if (isEmpty(value)) return <Empty def={def} />;
  if (loading) {
    return (
      <span data-kx-ui="value" data-kx-kind="lookup" aria-busy="true">
        <Skeleton />
      </span>
    );
  }
  return (
    <bdi data-kx-ui="value" data-kx-kind="lookup" data-th="reference">
      {label ?? String(value)}
    </bdi>
  );
}

function LinkViewer({ value, def }: ViewerProps) {
  const text = isEmpty(value) ? '' : String(value).trim();
  if (text === '') return <Empty def={def} />;
  let href: string | undefined;
  let external = false;
  if (def.kind === 'email') href = `mailto:${text}`;
  else if (def.kind === 'phone') href = `tel:${text.replace(/[^\d+]/g, '')}`;
  else if (isSafeUrl(text)) {
    href = text;
    external = true;
  }
  if (!href) return <span data-kx-ui="value" data-kx-kind={def.kind}>{text}</span>;
  return (
    <a
      data-kx-ui="value"
      data-kx-kind={def.kind}
      href={href}
      target={external ? '_blank' : undefined}
      rel={external ? 'noopener noreferrer' : undefined}
    >
      {text}
    </a>
  );
}

export const viewers: Readonly<Record<FieldKind, ComponentType<ViewerProps>>> = {
  text: TextViewer,
  textarea: TextViewer,
  number: NumberViewer,
  decimal: NumberViewer,
  money: NumberViewer,
  percentage: NumberViewer,
  date: DateViewer,
  datetime: DateViewer,
  boolean: BooleanViewer,
  select: SelectViewer,
  multiselect: MultiSelectViewer,
  badge: BadgeViewer,
  lookup: LookupViewer,
  email: LinkViewer,
  phone: LinkViewer,
  url: LinkViewer,
};

