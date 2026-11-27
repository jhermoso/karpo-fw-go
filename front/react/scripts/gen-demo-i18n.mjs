// `npm run i18n:demo` (R2, B-2 c, D-R28): textos de la demo para las 8 configuraciones de Go.
//
// Las 8 configuraciones de `front/angular/config/entities` traen sólo literales en español. Este script
// los recorre y, por cada literal traducible, genera:
//   1. una superposición con la clave de i18n (`headerKey`, `labelKey`, `titleKey`, `hintKey`…) en
//      `demo/config/overlays/<id>.i18n.json`, que el manifiesto referencia con `overlayPath`;
//   2. el diccionario español (`demo/config/i18n/es.json`), con el literal de Go sin tocar;
//   3. el diccionario inglés (`demo/config/i18n/en.json`), desde la tabla revisada a mano
//      `demo/config/i18n/en.go.json` (clave -> inglés);
//   4. el diccionario árabe (`demo/config/i18n/ar.json`, D-R38), desde `demo/config/i18n/ar.go.json`
//      (clave -> árabe). Es traducción de demostración, sin revisión nativa, y sólo cubre las entidades que
//      salen en las capturas (`ARABIC_ENTITIES`): de ellas debe estar TODA clave; del resto, lo que falte
//      cae al inglés en tiempo de ejecución (reserva `ar` -> `en`).
// No toca las configuraciones de Go. Es la prueba de concepto de la convención de claves que TC-3 pedirá al
// pack de Theros (docs/PETICIONES-THEROS-FRONT.md).
//
// Convención de claves (D-R28), `entities.<id>.` + :
//   columns.<campo>.header|hint|placeholder|trueLabel|falseLabel   columnas de la cuadrícula
//   fields.<campo>.label|hint|placeholder|trueLabel|falseLabel     campos de form y detail
//   options.<campo>.<valor>                                        badgeMap, enumValues y options
//   sections.<id>.title|description                                secciones de form y detail
//   form.createTitle|editTitle                                     títulos del formulario
//   filters.<campo>.label|hint|placeholder                         filtros
//   commands.<id>.label|confirmMessage|…                           rowActions y commands
//   childGrids.<id>.title|emptyMessage|columns.<campo>.header|…    cuadrículas hijas
//   emptyState.title|message                                       estado vacío
// `<id>` es el id en camelCase (`work-center` -> `workCenter`), como las claves ya escritas a mano.
// Si el mismo literal-clave aparece con textos distintos en dos ámbitos (p. ej. el título de la sección
// `main` del formulario y el del detalle), todas sus claves llevan el ámbito delante
// (`entities.asset.form.sections.main.title`, `entities.asset.detail.sections.main.title`).
//
// Uso:
//   node scripts/gen-demo-i18n.mjs           escribe lo generado
//   node scripts/gen-demo-i18n.mjs --check   falla (código 1) si falta una clave en en.go.json (o en
//                                            ar.go.json, para las entidades de ARABIC_ENTITIES), si sobra
//                                            alguna o si lo generado no está al día
//   node scripts/gen-demo-i18n.mjs --list    imprime `clave<TAB>español` (para preparar la tabla inglesa)
import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const ROOT = path.resolve(HERE, '..');

export const PATHS = {
  manifest: path.join(ROOT, 'demo/config/manifest.json'),
  overlays: path.join(ROOT, 'demo/config/overlays'),
  es: path.join(ROOT, 'demo/config/i18n/es.json'),
  en: path.join(ROOT, 'demo/config/i18n/en.json'),
  enTable: path.join(ROOT, 'demo/config/i18n/en.go.json'),
  ar: path.join(ROOT, 'demo/config/i18n/ar.json'),
  arTable: path.join(ROOT, 'demo/config/i18n/ar.go.json'),
  goDir: path.resolve(ROOT, '../angular/config/entities'),
};

/** Entidades de Go (id) con el árabe completo: las que salen en las capturas (D-R38). El resto cae al inglés. */
export const ARABIC_ENTITIES = Object.freeze(['facility', 'product']);

/** Subárboles de `entities.<id>` que pertenecen al generador (el resto del diccionario es de la mano). */
export const OWNED_BRANCHES = [
  'columns',
  'fields',
  'options',
  'sections',
  'form',
  'filters',
  'commands',
  'childGrids',
  'emptyState',
  // Ámbitos que llevan por delante las claves en conflicto (ver keyTexts).
  'grid',
  'detail',
  'filter',
  'command',
  'child',
  'root',
];

const isObj = (value) => typeof value === 'object' && value !== null && !Array.isArray(value);
const nonEmpty = (value) => typeof value === 'string' && value.trim() !== '';
const eol = (text) => text.replaceAll('\r\n', '\n');

/** JSON con sangría de 2 y los objetos pequeños de sólo textos en una línea (como están los diccionarios a mano). */
export function stringify(value, indent = '') {
  if (Array.isArray(value)) {
    if (value.length === 0) return '[]';
    const inner = `${indent}  `;
    return `[\n${value.map((item) => inner + stringify(item, inner)).join(',\n')}\n${indent}]`;
  }
  if (!isObj(value)) return JSON.stringify(value);
  const entries = Object.entries(value);
  if (entries.length === 0) return '{}';
  if (entries.every(([, child]) => typeof child === 'string')) {
    const line = `{ ${entries.map(([key, child]) => `${JSON.stringify(key)}: ${JSON.stringify(child)}`).join(', ')} }`;
    if (indent.length + line.length <= 100) return line;
  }
  const inner = `${indent}  `;
  return `{\n${entries.map(([key, child]) => `${inner}${JSON.stringify(key)}: ${stringify(child, inner)}`).join(',\n')}\n${indent}}`;
}
const json = (value) => `${stringify(value)}\n`;
const relative = (file) => path.relative(ROOT, file).split(path.sep).join('/');

/** `work-center` -> `workCenter`. */
export function camelCase(id) {
  return id.replace(/-([a-z0-9])/g, (_, c) => c.toUpperCase());
}

/** Un trozo de clave: sin puntos ni caracteres raros (los puntos separan niveles del diccionario). */
export function segment(value) {
  return String(value).replace(/[^A-Za-z0-9_-]/g, '_');
}

// ---------------------------------------------------------------------------------------------
// Extracción de literales
// ---------------------------------------------------------------------------------------------

/** Primera propiedad de texto presente y no vacía (mismo orden de alias que `textOf` del normalizador). */
function readText(obj, props) {
  for (const prop of props) if (nonEmpty(obj?.[prop])) return { prop, literal: obj[prop] };
  return undefined;
}

class Unsupported extends Error {}

/**
 * Literales traducibles de una configuración de Go, en orden de aparición. Cada uno lleva:
 *   scope    grid | form | detail | filter | command | child | root
 *   rel      trozos de la clave tras `entities.<id>.`
 *   at       dónde va la clave en la superposición (trozos; un objeto `{campo: valor}` elige un elemento por identidad)
 *   literal  el texto en español
 */
export function extractTexts(config) {
  const out = [];
  const add = (scope, rel, literal, at) => out.push({ scope, rel, literal, at });

  const fieldTexts = (scope, relBase, atBase, obj, labelProps) => {
    const text = (props, relName) => {
      const found = readText(obj, props);
      if (found) add(scope, [...relBase, relName ?? found.prop], found.literal, [...atBase, `${found.prop}Key`]);
    };
    const label = readText(obj, labelProps);
    if (label) add(scope, [...relBase, label.prop], label.literal, [...atBase, `${label.prop}Key`]);
    text(['hint', 'tooltip']);
    text(['placeholder']);
    text(['trueLabel']);
    text(['falseLabel']);
  };

  /** `options`, `badgeMap` y `enumValues` de un campo, columna o filtro. */
  const optionTexts = (scope, relPrefix, field, atBase, obj) => {
    const one = (value, entry, atItem) => {
      if (!isObj(entry)) return;
      const found = readText(entry, ['label', 'text', 'name', 'header']);
      if (found) add(scope, [...relPrefix, 'options', field, value], found.literal, [...atItem, `${found.prop}Key`]);
    };
    if (Array.isArray(obj.options)) {
      for (const item of obj.options) if (isObj(item) && item.value !== undefined) one(String(item.value), item, [...atBase, 'options', { value: item.value }]);
    }
    if (isObj(obj.badgeMap)) {
      for (const [value, entry] of Object.entries(obj.badgeMap)) one(value, entry, [...atBase, 'badgeMap', value]);
    }
    if (Array.isArray(obj.enumValues)) {
      for (const item of obj.enumValues) if (isObj(item) && item.value !== undefined) one(String(item.value), item, [...atBase, 'enumValues', { value: item.value }]);
    }
  };

  const fieldOf = (obj, where) => {
    const field = obj.field ?? obj.dataField;
    if (!nonEmpty(field)) throw new Unsupported(`${config.id}: ${where} sin \`field\``);
    return field;
  };

  const sectionTexts = (scope, key, sections) => {
    if (!Array.isArray(sections)) return;
    sections.forEach((section, i) => {
      if (!isObj(section)) return;
      if (!nonEmpty(section.id)) throw new Unsupported(`${config.id}: ${key}.sections[${i}] sin \`id\` (la superposición no podría casarla)`);
      const atSection = [key, 'sections', { id: section.id }];
      const title = readText(section, ['title', 'label']);
      if (title) add(scope, ['sections', section.id, 'title'], title.literal, [...atSection, `${title.prop}Key`]);
      const description = readText(section, ['description']);
      if (description) add(scope, ['sections', section.id, 'description'], description.literal, [...atSection, 'descriptionKey']);
      for (const field of Array.isArray(section.fields) ? section.fields : []) {
        if (!isObj(field)) continue;
        const name = fieldOf(field, `${key}.sections.${section.id}.fields[]`);
        const atField = [...atSection, 'fields', { field: field.field ?? field.dataField }];
        fieldTexts(scope, ['fields', name], atField, field, ['label']);
        optionTexts(scope, [], name, atField, field);
      }
    });
  };

  // Formulario
  if (isObj(config.form)) {
    if (config.form.groups !== undefined) throw new Unsupported(`${config.id}: form.groups no está soportado por el generador (usa sections)`);
    for (const prop of ['createTitle', 'editTitle']) {
      if (nonEmpty(config.form[prop])) add('form', ['form', prop], config.form[prop], ['form', `${prop}Key`]);
    }
    sectionTexts('form', 'form', config.form.sections);
  }

  // Cuadrícula
  const grid = isObj(config.grid) ? config.grid : {};
  for (const column of Array.isArray(grid.columns) ? grid.columns : []) {
    if (!isObj(column) || String(column.type).toLowerCase() === 'actions') continue;
    const name = fieldOf(column, 'grid.columns[]');
    const atColumn = ['grid', 'columns', { field: column.field ?? column.dataField }];
    fieldTexts('grid', ['columns', name], atColumn, column, ['label', 'header']);
    optionTexts('grid', [], name, atColumn, column);
  }
  for (const action of Array.isArray(grid.rowActions) ? grid.rowActions : []) {
    if (!isObj(action) || !nonEmpty(action.action)) continue;
    const atAction = ['grid', 'rowActions', { action: action.action }];
    if (nonEmpty(action.label)) add('command', ['commands', action.action, 'label'], action.label, [...atAction, 'labelKey']);
    if (typeof action.confirm === 'string') {
      throw new Unsupported(`${config.id}: grid.rowActions.${action.action}.confirm en texto no se puede traducir (usa confirm: true y confirmMessage)`);
    }
    if (nonEmpty(action.confirmMessage)) {
      add('command', ['commands', action.action, 'confirmMessage'], action.confirmMessage, [...atAction, 'confirmMessageKey']);
    }
  }
  if (nonEmpty(grid.emptyMessage)) add('grid', ['emptyState', 'message'], grid.emptyMessage, ['grid', 'emptyMessageKey']);

  // Filtros
  (Array.isArray(config.filters) ? config.filters : []).forEach((filter) => {
    if (!isObj(filter)) return;
    const name = fieldOf(filter, 'filters[]');
    const atFilter = ['filters', { field: filter.field ?? filter.dataField }];
    for (const prop of ['label', 'hint', 'placeholder']) {
      if (nonEmpty(filter[prop])) add('filter', ['filters', name, prop], filter[prop], [...atFilter, `${prop}Key`]);
    }
    optionTexts('filter', [], name, atFilter, filter);
  });

  // Órdenes explícitas
  (Array.isArray(config.commands) ? config.commands : []).forEach((command) => {
    if (!isObj(command) || !nonEmpty(command.id)) return;
    const atCommand = ['commands', { id: command.id }];
    for (const prop of ['label', 'confirmMessage', 'successMessage', 'acceptedMessage']) {
      if (nonEmpty(command[prop])) add('command', ['commands', command.id, prop], command[prop], [...atCommand, `${prop}Key`]);
    }
    if (isObj(command.prompt) && nonEmpty(command.prompt.label)) {
      add('command', ['commands', command.id, 'prompt'], command.prompt.label, [...atCommand, 'prompt', 'labelKey']);
    }
  });

  // Detalle
  const detail = isObj(config.detail) ? config.detail : {};
  sectionTexts('detail', 'detail', detail.sections);
  if (Array.isArray(detail.tabs) && detail.tabs.some((tab) => isObj(tab) && (tab.sections || tab.childGrid))) {
    throw new Unsupported(`${config.id}: detail.tabs no está soportado por el generador`);
  }
  (Array.isArray(detail.childGrids) ? detail.childGrids : []).forEach((child, i) => {
    if (!isObj(child)) return;
    if (!nonEmpty(child.id)) throw new Unsupported(`${config.id}: detail.childGrids[${i}] sin \`id\``);
    const atChild = ['detail', 'childGrids', { id: child.id }];
    const title = readText(child, ['title', 'label']);
    if (title) add('child', ['childGrids', child.id, 'title'], title.literal, [...atChild, `${title.prop}Key`]);
    if (nonEmpty(child.emptyMessage)) add('child', ['childGrids', child.id, 'emptyMessage'], child.emptyMessage, [...atChild, 'emptyMessageKey']);
    for (const column of Array.isArray(child.columns) ? child.columns : []) {
      if (!isObj(column)) continue;
      const name = fieldOf(column, `detail.childGrids.${child.id}.columns[]`);
      const atColumn = [...atChild, 'columns', { field: column.field ?? column.dataField }];
      fieldTexts('child', ['childGrids', child.id, 'columns', name], atColumn, column, ['label', 'header']);
      optionTexts('child', ['childGrids', child.id], name, atColumn, column);
    }
  });

  // Estado vacío de la entidad
  if (isObj(config.emptyState)) {
    for (const prop of ['title', 'message']) {
      if (nonEmpty(config.emptyState[prop])) add('root', ['emptyState', prop], config.emptyState[prop], ['emptyState', `${prop}Key`]);
    }
  }
  return out;
}

// ---------------------------------------------------------------------------------------------
// Claves, superposiciones y diccionarios
// ---------------------------------------------------------------------------------------------

/** Pone `value` en `root` siguiendo `at` (trozos de texto, o `{campo: valor}` para un elemento de un array). */
export function place(root, at, value) {
  let cursor = root;
  for (let i = 0; i < at.length - 1; i += 1) {
    const step = at[i];
    const next = at[i + 1];
    if (typeof step === 'string') {
      cursor = cursor[step] ??= typeof next === 'object' ? [] : {};
    } else {
      const [identityKey, identityValue] = Object.entries(step)[0];
      let item = cursor.find((candidate) => candidate[identityKey] === identityValue);
      if (!item) {
        item = { [identityKey]: identityValue };
        cursor.push(item);
      }
      cursor = item;
    }
  }
  cursor[at[at.length - 1]] = value;
}

/** Pone `value` en un objeto anidado por la clave con puntos. */
export function setNested(root, dotted, value) {
  const parts = dotted.split('.');
  let cursor = root;
  for (const part of parts.slice(0, -1)) {
    if (!isObj(cursor[part])) cursor[part] = {};
    cursor = cursor[part];
  }
  cursor[parts[parts.length - 1]] = value;
}

/**
 * Claves de i18n de una entidad: `entities.<id>.<rel>`; con textos distintos para una misma clave en
 * ámbitos distintos, el ámbito va delante. Devuelve `{ key, literal, at }[]` y los conflictos sin salida.
 */
export function keyTexts(id, texts) {
  const dictId = camelCase(id);
  const base = (rel, scope) => `entities.${dictId}.${scope ? `${scope}.` : ''}${rel.map(segment).join('.')}`;
  const groups = new Map();
  for (const text of texts) {
    const key = base(text.rel);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(text);
  }
  const out = [];
  const errors = [];
  for (const [plain, members] of groups) {
    const literals = new Set(members.map((m) => m.literal));
    if (literals.size === 1) {
      // Mismo texto en varios sitios: una sola clave que usan todos.
      for (const member of members) out.push({ key: plain, literal: member.literal, at: member.at, scope: member.scope });
      continue;
    }
    const seen = new Map();
    for (const member of members) {
      const key = base(member.rel, member.scope);
      if (seen.has(key) && seen.get(key) !== member.literal) {
        errors.push(`${id}: dos textos distintos para ${key}: «${seen.get(key)}» y «${member.literal}»`);
      }
      seen.set(key, member.literal);
      out.push({ key, literal: member.literal, at: member.at, scope: member.scope });
    }
  }
  return { entries: out, errors };
}

/** Superposición, claves y literales de una configuración de Go. */
export function buildEntity(config) {
  const texts = extractTexts(config);
  const { entries, errors } = keyTexts(config.id, texts);
  const overlay = {};
  for (const entry of entries) place(overlay, entry.at, entry.key);
  const literals = new Map();
  for (const entry of entries) {
    const known = literals.get(entry.key);
    if (known !== undefined && known !== entry.literal) errors.push(`${config.id}: la clave ${entry.key} tiene dos textos`);
    literals.set(entry.key, entry.literal);
  }
  return { id: config.id, overlay, literals, errors };
}

/** Quita de un diccionario los subárboles que genera este script (`entities.<id>.<rama>`). */
export function withoutOwned(dictionary, dictIds) {
  const copy = structuredClone(dictionary);
  for (const dictId of dictIds) {
    const entity = copy.entities?.[dictId];
    if (!isObj(entity)) continue;
    for (const branch of OWNED_BRANCHES) delete entity[branch];
  }
  return copy;
}

// ---------------------------------------------------------------------------------------------
// Proceso completo
// ---------------------------------------------------------------------------------------------

function readJson(file) {
  return JSON.parse(eol(readFileSync(file, 'utf8')));
}

/** Entradas de Go del manifiesto: `[{ id, path }]`. */
export function goEntries(manifest) {
  return manifest.entities.filter((e) => typeof e.path === 'string' && e.path.startsWith('go/')).map((e) => ({ id: e.id, path: e.path }));
}

/** Añade `overlayPath` tras la línea `path` de cada entidad de Go, sin reescribir el resto del manifiesto. */
export function withOverlayPaths(manifestText, entries) {
  let out = eol(manifestText);
  for (const { id, path: entityPath } of entries) {
    const wanted = `"overlayPath": "overlays/${id}.i18n.json",`;
    if (out.includes(wanted)) continue;
    const escaped = entityPath.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const line = new RegExp(`^([ \\t]*)"path": "${escaped}",\\n`, 'm');
    if (!line.test(out)) throw new Unsupported(`manifest.json: no encuentro la línea \`"path": "${entityPath}",\` para añadir overlayPath`);
    out = out.replace(line, (whole, indent) => `${whole}${indent}${wanted}\n`);
  }
  return out;
}

/**
 * Calcula todo lo que el script escribe, sin tocar el disco. `tables` permite probar con otras entradas:
 * `{ manifestText, goConfigs: {id: json}, enTable, es, en }`.
 */
export function build(inputs) {
  const errors = [];
  const manifest = JSON.parse(inputs.manifestText);
  const entries = goEntries(manifest);
  const built = [];
  for (const entry of entries) {
    const config = inputs.goConfigs[entry.id];
    if (!config) {
      errors.push(`no hay configuración de Go para ${entry.id}`);
      continue;
    }
    try {
      const item = buildEntity(config);
      errors.push(...item.errors);
      built.push(item);
    } catch (error) {
      if (!(error instanceof Unsupported)) throw error;
      errors.push(error.message);
    }
  }

  const literals = new Map();
  for (const item of built) for (const [key, literal] of item.literals) literals.set(key, literal);

  // Cobertura de la tabla inglesa.
  const table = inputs.enTable;
  const missing = [...literals.keys()].filter((key) => !nonEmpty(table[key]));
  for (const key of missing) errors.push(`falta en en.go.json: ${key}  (es: «${literals.get(key)}»)`);
  for (const key of Object.keys(table)) {
    if (!literals.has(key)) errors.push(`sobra en en.go.json (la configuración de Go ya no genera esa clave): ${key}`);
  }

  // Cobertura de la tabla árabe: completa en las entidades de las capturas; el resto es opcional.
  const arTable = inputs.ar === undefined ? undefined : (inputs.arTable ?? {});
  if (arTable) {
    const wanted = new Set(ARABIC_ENTITIES.map(camelCase));
    const entityOf = (key) => key.split('.')[1];
    for (const key of literals.keys()) {
      if (wanted.has(entityOf(key)) && !nonEmpty(arTable[key])) errors.push(`falta en ar.go.json: ${key}  (es: «${literals.get(key)}»)`);
    }
    for (const key of Object.keys(arTable)) {
      if (!literals.has(key)) errors.push(`sobra en ar.go.json (la configuración de Go ya no genera esa clave): ${key}`);
    }
  }

  // Diccionarios: se conserva lo escrito a mano y se sustituye lo generado.
  const dictIds = entries.map((e) => camelCase(e.id));
  const es = withoutOwned(inputs.es, dictIds);
  const en = withoutOwned(inputs.en, dictIds);
  for (const [key, literal] of literals) {
    setNested(es, key, literal);
    if (nonEmpty(table[key])) setNested(en, key, table[key]);
  }

  const files = new Map();
  for (const item of built) files.set(path.join(PATHS.overlays, `${item.id}.i18n.json`), json(item.overlay));
  files.set(PATHS.es, json(es));
  files.set(PATHS.en, json(en));
  if (arTable) {
    const ar = withoutOwned(inputs.ar, dictIds);
    for (const [key, text] of Object.entries(arTable)) if (literals.has(key) && nonEmpty(text)) setNested(ar, key, text);
    files.set(PATHS.ar, json(ar));
  }
  try {
    files.set(PATHS.manifest, withOverlayPaths(inputs.manifestText, entries));
  } catch (error) {
    if (!(error instanceof Unsupported)) throw error;
    errors.push(error.message);
  }
  return { files, errors, literals, overlays: Object.fromEntries(built.map((b) => [b.id, b.overlay])), entries };
}

/** Lee las entradas reales del disco. */
export function readInputs() {
  const manifestText = eol(readFileSync(PATHS.manifest, 'utf8'));
  const manifest = JSON.parse(manifestText);
  const goConfigs = {};
  for (const entry of goEntries(manifest)) goConfigs[entry.id] = readJson(path.join(PATHS.goDir, entry.path.slice(3)));
  return {
    manifestText,
    goConfigs,
    enTable: existsSync(PATHS.enTable) ? readJson(PATHS.enTable) : {},
    arTable: existsSync(PATHS.arTable) ? readJson(PATHS.arTable) : {},
    es: readJson(PATHS.es),
    en: readJson(PATHS.en),
    ar: existsSync(PATHS.ar) ? readJson(PATHS.ar) : {},
  };
}

/** Ficheros de `demo/config/overlays` que ya no se generan. */
export function staleOverlays(files) {
  if (!existsSync(PATHS.overlays)) return [];
  return readdirSync(PATHS.overlays)
    .filter((name) => name.endsWith('.i18n.json'))
    .map((name) => path.join(PATHS.overlays, name))
    .filter((file) => !files.has(file));
}

export function run(argv = process.argv.slice(2), log = console) {
  const check = argv.includes('--check');
  const list = argv.includes('--list');
  const result = build(readInputs());
  if (list) {
    for (const [key, literal] of result.literals) log.log(`${key}\t${literal}`);
    return 0;
  }
  const problems = [...result.errors];
  const stale = staleOverlays(result.files);
  if (check) {
    for (const [file, expected] of result.files) {
      const current = existsSync(file) ? eol(readFileSync(file, 'utf8')) : undefined;
      if (current !== expected) problems.push(`desactualizado: ${relative(file)} (ejecuta \`npm run i18n:demo\`)`);
    }
    for (const file of stale) problems.push(`sobra: ${relative(file)}`);
  }
  if (problems.length > 0) {
    log.error(`i18n:demo: ${problems.length} problema(s)`);
    for (const problem of problems) log.error(`  - ${problem}`);
    return 1;
  }
  if (check) {
    log.log(`i18n:demo: al día (${result.literals.size} textos, ${result.entries.length} entidades de Go).`);
    return 0;
  }
  mkdirSync(PATHS.overlays, { recursive: true });
  for (const [file, content] of result.files) writeFileSync(file, content);
  for (const file of stale) rmSync(file);
  log.log(`i18n:demo: escrito (${result.literals.size} textos, ${result.entries.length} entidades de Go).`);
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  process.exitCode = run();
}
