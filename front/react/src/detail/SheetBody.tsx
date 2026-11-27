// Cuerpo de la ficha: pestañas (secciones + rejillas hijas) y la rejilla de campos de cada sección.
// Las secciones se montan siempre (aunque su pestaña no se vea) para que el formulario conserve sus
// campos y se pueda llevar el foco al primer error de una pestaña oculta.
import * as Tabs from '@radix-ui/react-tabs';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useText, type SectionDefExt } from '../config';
import type { ChildGridDef, FieldDef, SectionDef } from '../contracts';
import { Action, Badge, Icon } from '../ui';
import { ChildGridTable } from './ChildGridTable';
import { fieldKey } from './form-model';
import { SheetField } from './SheetField';
import { useSheet, type SheetContextValue } from './sheet-context';

export type PanelDef =
  | { id: string; type: 'section'; section: SectionDef }
  | { id: string; type: 'child'; grid: ChildGridDef };

export function sectionPanelId(section: SectionDef): string {
  return `section:${section.id}`;
}

/** Pestañas de la ficha en orden: las secciones visibles y, si hay registro, las rejillas hijas. */
export function computePanels(
  shell: Pick<SheetContextValue, 'mode' | 'row'>,
  sections: readonly SectionDef[],
  childGrids: readonly ChildGridDef[],
  rules: Pick<SheetContextValue, 'isFieldVisible' | 'isSectionVisible'>,
): PanelDef[] {
  const panels: PanelDef[] = sections
    .filter((section) => rules.isSectionVisible(section) && section.fields.some((field) => rules.isFieldVisible(field)))
    .map((section) => ({ id: sectionPanelId(section), type: 'section' as const, section }));
  if (shell.mode !== 'create' && shell.row) {
    for (const grid of childGrids) panels.push({ id: `child:${grid.id}`, type: 'child', grid });
  }
  return panels;
}

export interface SheetBodyProps {
  panels: readonly PanelDef[];
  tab: string | undefined;
  onTabChange(tab: string): void;
}

export function SheetBody({ panels, tab, onTabChange }: SheetBodyProps) {
  const { t } = useTranslation('detail');
  const sheet = useSheet();
  const resolve = useText();
  const active = panels.some((panel) => panel.id === tab) ? (tab as string) : panels[0]?.id;

  if (panels.length === 0) return <p data-th="collection-empty">{t('sheet.empty')}</p>;

  if (panels.length === 1) {
    const only = panels[0] as PanelDef;
    return (
      <div data-kx-part="tabs-root" data-kx-help="detail.tabs">
        <PanelContent panel={only} single />
      </div>
    );
  }

  const errorCount = (section: SectionDef): number =>
    section.fields.filter((field) => sheet.isFieldVisible(field) && sheet.errorText(fieldKey(field.field), field) !== undefined).length;

  return (
    <Tabs.Root value={active} onValueChange={onTabChange} data-kx-part="tabs-root" data-kx-help="detail.tabs">
      <Tabs.List aria-label={t('tabs.label')} data-kx-part="tab-list">
        {panels.map((panel) => {
          const title = panel.type === 'section' ? resolve(panel.section.title) : resolve(panel.grid.title);
          const errors = panel.type === 'section' ? errorCount(panel.section) : 0;
          return (
            <Tabs.Trigger key={panel.id} value={panel.id} data-kx-part="tab">
              {panel.type === 'section' && (panel.section as SectionDefExt).icon ? (
                <Icon name={(panel.section as SectionDefExt).icon as string} />
              ) : null}
              {panel.type === 'child' ? <Icon name="history" /> : null}
              <span>{title}</span>
              {errors > 0 ? (
                <Badge tone="error" aria-label={t('tabs.errors', { n: errors })}>
                  {errors}
                </Badge>
              ) : null}
            </Tabs.Trigger>
          );
        })}
      </Tabs.List>
      {panels.map((panel) => (
        <Tabs.Content
          key={panel.id}
          value={panel.id}
          forceMount={panel.type === 'section' ? true : undefined}
          data-kx-part="panel"
        >
          <PanelContent panel={panel} />
        </Tabs.Content>
      ))}
    </Tabs.Root>
  );
}

function PanelContent({ panel, single }: { panel: PanelDef; single?: boolean }) {
  const sheet = useSheet();
  if (panel.type === 'child') {
    return sheet.row ? (
      <div data-th="section" data-kx-help="detail.child-grid" data-kx-part="child-section">
        <ChildGridTable config={sheet.config} grid={panel.grid} row={sheet.row} />
      </div>
    ) : null;
  }
  return <SectionFields section={panel.section} single={single} />;
}

interface SectionFieldsProps {
  section: SectionDef;
  /** Única pestaña: se pinta el encabezado de la sección y puede plegarse. */
  single?: boolean;
}

function SectionFields({ section, single }: SectionFieldsProps) {
  const { t } = useTranslation('detail');
  const sheet = useSheet();
  const resolve = useText();
  const headingId = useId();
  const bodyId = useId();
  const [collapsed, setCollapsed] = useState(section.collapsible && section.collapsed);
  const description = (section as SectionDefExt).description;
  const descriptionText = description ? resolve(description) : '';
  const fields: FieldDef[] = section.fields.filter((field) => sheet.isFieldVisible(field));
  const title = resolve(section.title);
  const collapsible = Boolean(single && section.collapsible);

  return (
    <section data-th="section" aria-labelledby={single && title ? headingId : undefined} data-kx-section={section.id}>
      {single && title ? (
        <header data-kx-part="section-header">
          <h3 id={headingId}>{title}</h3>
          {collapsible ? (
            <Action
              kind="quiet"
              icon={collapsed ? 'chevron-down' : 'chevron-up'}
              aria-expanded={!collapsed}
              aria-controls={bodyId}
              aria-label={collapsed ? t('section.expand', { title }) : t('section.collapse', { title })}
              onClick={() => setCollapsed((value) => !value)}
            />
          ) : null}
        </header>
      ) : null}
      {descriptionText ? <p data-th="hint">{descriptionText}</p> : null}
      <div id={bodyId} data-kx-part="fields" hidden={collapsed || undefined}>
        {fields.map((field) => (
          <SheetField key={field.field} def={field} columns={section.columns} />
        ))}
      </div>
    </section>
  );
}
