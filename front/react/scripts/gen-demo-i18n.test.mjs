// Pruebas de `npm run i18n:demo` (R2, B-2 c, D-R28): el generador de textos de la demo para las 8
// configuraciones de Go, su `--check`, y el resultado visto desde el normalizador (lng=en sin español).
import { spawnSync } from 'node:child_process';
import { cpSync, existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { clearConfigDictionaries, resolveText } from '../src/config/text.ts';
import { loadDemoReport } from '../src/config/__fixtures__/demo-config.ts';
import {
  ARABIC_ENTITIES,
  PATHS,
  ROOT,
  build,
  buildEntity,
  camelCase,
  extractTexts,
  keyTexts,
  place,
  readInputs,
  run,
  segment,
  setNested,
  stringify,
  withOverlayPaths,
  withoutOwned,
} from './gen-demo-i18n.mjs';

const readJson = (file) => JSON.parse(readFileSync(file, 'utf8').replaceAll('\r\n', '\n'));
const silent = { log: () => {}, error: () => {} };

/** Literales de Go que son idénticos en inglés: se admiten sin traducción distinta. */
const SAME_IN_ENGLISH = new Set(['ISO', 'ISO-3', 'SEPA', 'IBAN', 'Party', 'Euro', 'No']);

describe('claves y superposiciones', () => {
  it('camelCase y segment', () => {
    expect(camelCase('work-center')).toBe('workCenter');
    expect(camelCase('facility')).toBe('facility');
    expect(segment('a.b c/d')).toBe('a_b_c_d');
    expect(segment('in-service')).toBe('in-service');
  });

  it('place pone la clave siguiendo el camino y elige los elementos de un array por identidad', () => {
    const root = {};
    place(root, ['grid', 'columns', { field: 'name' }, 'headerKey'], 'k1');
    place(root, ['grid', 'columns', { field: 'name' }, 'badgeMap', 'on', 'labelKey'], 'k2');
    place(root, ['grid', 'columns', { field: 'code' }, 'headerKey'], 'k3');
    place(root, ['form', 'sections', { id: 'main' }, 'fields', { field: 'kind' }, 'options', { value: 'a' }, 'labelKey'], 'k4');
    expect(root).toEqual({
      grid: {
        columns: [
          { field: 'name', headerKey: 'k1', badgeMap: { on: { labelKey: 'k2' } } },
          { field: 'code', headerKey: 'k3' },
        ],
      },
      form: { sections: [{ id: 'main', fields: [{ field: 'kind', options: [{ value: 'a', labelKey: 'k4' }] }] }] },
    });
  });

  it('setNested y withoutOwned: lo generado se sustituye y lo escrito a mano se conserva', () => {
    const dictionary = { menu: { a: 'A' }, entities: { workCenter: { name: 'N', columns: { x: '1' }, form: { createTitle: 'T' }, detail: {}, extra: 'se queda' } } };
    const stripped = withoutOwned(dictionary, ['workCenter']);
    expect(stripped.entities.workCenter).toEqual({ name: 'N', extra: 'se queda' });
    expect(dictionary.entities.workCenter.columns).toEqual({ x: '1' }); // no se toca el original
    setNested(stripped, 'entities.workCenter.columns.code.header', 'Código');
    expect(stripped.entities.workCenter.columns).toEqual({ code: { header: 'Código' } });
  });

  it('stringify deja en una línea los objetos pequeños de sólo textos y expande el resto', () => {
    const text = stringify({ a: { b: 'x', c: 'y' }, long: { k: 'z'.repeat(120) }, list: [{ value: 'v', labelKey: 'k' }], n: { m: 1 } });
    expect(text).toContain('"a": { "b": "x", "c": "y" }');
    expect(text).toContain('{ "value": "v", "labelKey": "k" }');
    expect(text).toMatch(/"long": \{\n {4}"k": "z+"\n {2}\}/);
    expect(JSON.parse(text)).toEqual({ a: { b: 'x', c: 'y' }, long: { k: 'z'.repeat(120) }, list: [{ value: 'v', labelKey: 'k' }], n: { m: 1 } });
  });

  it('un mismo literal en form y detail comparte clave; si difiere, el ámbito va delante', () => {
    const config = {
      id: 'thing',
      form: { sections: [{ id: 'main', title: 'Datos', fields: [{ field: 'name', label: 'Nombre', hint: 'Una pista' }, { field: 'type', label: 'Tipo (alta)' }] }] },
      detail: { sections: [{ id: 'main', title: 'Cosa', fields: [{ field: 'name', label: 'Nombre' }, { field: 'type', label: 'Tipo' }] }] },
    };
    const { overlay, literals, errors } = buildEntity(config);
    expect(errors).toEqual([]);
    expect([...literals.keys()].sort()).toEqual(
      [
        'entities.thing.fields.name.hint',
        'entities.thing.fields.name.label',
        'entities.thing.form.fields.type.label',
        'entities.thing.detail.fields.type.label',
        'entities.thing.form.sections.main.title',
        'entities.thing.detail.sections.main.title',
      ].sort(),
    );
    expect(overlay.form.sections[0].fields[0]).toEqual({ field: 'name', labelKey: 'entities.thing.fields.name.label', hintKey: 'entities.thing.fields.name.hint' });
    expect(overlay.detail.sections[0].fields[1]).toEqual({ field: 'type', labelKey: 'entities.thing.detail.fields.type.label' });
  });

  it('recoge columnas, opciones (options, badgeMap, enumValues), filtros, órdenes, hijos y estado vacío con su convención', () => {
    const config = {
      id: 'work-center',
      grid: {
        columns: [
          { field: 'state', header: 'Estado', type: 'badge', badgeMap: { open: { label: 'Abierto' } }, enumValues: [{ value: 'x', label: 'Equis' }] },
          { field: 'flag', header: 'Marca', trueLabel: 'Sí', falseLabel: 'No', tooltip: 'Ayuda' },
          { field: 'acciones', type: 'actions' },
        ],
        rowActions: [{ action: 'delete', label: 'Cerrar', confirm: true, confirmMessage: '¿Cerrar?' }],
        emptyMessage: 'Nada',
      },
      filters: [{ field: 'kind', label: 'Clase', placeholder: 'Todas', options: [{ value: 'k1', label: 'Uno' }, 'texto'] }],
      commands: [{ id: 'block', label: 'Bloquear', prompt: { field: 'reason', label: 'Motivo' }, successMessage: 'Hecho' }],
      detail: { childGrids: [{ id: 'hist', title: 'Historial', label: 'ignorado', emptyMessage: 'Vacío', columns: [{ field: 'at', header: 'Fecha' }] }] },
    };
    const texts = extractTexts(config);
    const byKey = Object.fromEntries(keyTexts('work-center', texts).entries.map((e) => [e.key, e.literal]));
    expect(byKey).toEqual({
      'entities.workCenter.columns.state.header': 'Estado',
      'entities.workCenter.options.state.open': 'Abierto',
      'entities.workCenter.options.state.x': 'Equis',
      'entities.workCenter.columns.flag.header': 'Marca',
      'entities.workCenter.columns.flag.trueLabel': 'Sí',
      'entities.workCenter.columns.flag.falseLabel': 'No',
      'entities.workCenter.columns.flag.tooltip': 'Ayuda',
      'entities.workCenter.commands.delete.label': 'Cerrar',
      'entities.workCenter.commands.delete.confirmMessage': '¿Cerrar?',
      'entities.workCenter.emptyState.message': 'Nada',
      'entities.workCenter.filters.kind.label': 'Clase',
      'entities.workCenter.filters.kind.placeholder': 'Todas',
      'entities.workCenter.options.kind.k1': 'Uno',
      'entities.workCenter.commands.block.label': 'Bloquear',
      'entities.workCenter.commands.block.successMessage': 'Hecho',
      'entities.workCenter.commands.block.prompt': 'Motivo',
      'entities.workCenter.childGrids.hist.title': 'Historial',
      'entities.workCenter.childGrids.hist.emptyMessage': 'Vacío',
      'entities.workCenter.childGrids.hist.columns.at.header': 'Fecha',
    });
    expect(extractTexts({ id: 'x', emptyState: { title: 'Sin datos', message: 'Crea uno' } }).map((t) => [t.rel.join('.'), t.at.join('.')])).toEqual([
      ['emptyState.title', 'emptyState.titleKey'],
      ['emptyState.message', 'emptyState.messageKey'],
    ]);
    // `label` del hijo no se lee si hay `title`: sin clave para él.
    expect(Object.values(byKey)).not.toContain('ignorado');
    const { overlay } = buildEntity(config);
    expect(overlay.grid.rowActions).toEqual([{ action: 'delete', labelKey: 'entities.workCenter.commands.delete.label', confirmMessageKey: 'entities.workCenter.commands.delete.confirmMessage' }]);
    expect(overlay.detail.childGrids[0]).toMatchObject({ id: 'hist', titleKey: 'entities.workCenter.childGrids.hist.title', emptyMessageKey: expect.any(String) });
  });

  it('lo que no sabe traducir lo dice en vez de omitirlo en silencio', () => {
    const problem = (config) => build({ ...readInputs(), goConfigs: { ...readInputs().goConfigs, facility: { id: 'facility', ...config } } }).errors;
    expect(problem({ form: { groups: [] } }).join('\n')).toContain('form.groups no está soportado');
    expect(problem({ form: { sections: [{ title: 'sin id' }] } }).join('\n')).toContain('sin `id`');
    expect(problem({ grid: { rowActions: [{ action: 'delete', confirm: '¿Seguro?' }] } }).join('\n')).toContain('no se puede traducir');
    expect(problem({ detail: { tabs: [{ sections: [] }] } }).join('\n')).toContain('detail.tabs no está soportado');
  });
});

describe('las 8 configuraciones de Go', () => {
  const inputs = readInputs();
  const result = build(inputs);

  it('generan ~270 textos únicos (más de 300 apariciones) y ningún error', () => {
    expect(result.errors).toEqual([]);
    expect(result.entries.map((e) => e.id).sort()).toEqual(['asset', 'carrier', 'country', 'facility', 'holiday', 'product', 'supplier', 'work-center']);
    expect(result.literals.size).toBeGreaterThanOrEqual(250);
    for (const [key, literal] of result.literals) {
      expect(key, key).toMatch(/^entities\.[a-zA-Z]+\.[A-Za-z0-9_.-]+$/);
      expect(literal.trim(), key).not.toBe('');
    }
  });

  it('el id de las claves coincide con el de las que ya estaban escritas a mano (nameKey del manifiesto)', () => {
    for (const entry of inputs.manifestText && JSON.parse(inputs.manifestText).entities.filter((e) => e.path.startsWith('go/'))) {
      expect(entry.overlay.nameKey).toBe(`entities.${camelCase(entry.id)}.name`);
    }
  });

  it('cada clave generada está en es (con el literal de Go) y en en (de la tabla revisada)', () => {
    const es = JSON.parse(result.files.get(PATHS.es));
    const en = JSON.parse(result.files.get(PATHS.en));
    const get = (dictionary, key) => key.split('.').reduce((node, part) => node?.[part], dictionary);
    for (const [key, literal] of result.literals) {
      expect(get(es, key), key).toBe(literal);
      expect(get(en, key), key).toBe(inputs.enTable[key]);
    }
  });

  it('las superposiciones sólo llevan claves `*Key` que existen en los dos diccionarios', () => {
    const es = JSON.parse(result.files.get(PATHS.es));
    const en = JSON.parse(result.files.get(PATHS.en));
    const get = (dictionary, key) => key.split('.').reduce((node, part) => node?.[part], dictionary);
    const keys = [];
    const walk = (value) => {
      if (Array.isArray(value)) value.forEach(walk);
      else if (value && typeof value === 'object') {
        for (const [name, child] of Object.entries(value)) {
          if (/Key$/.test(name) && typeof child === 'string') keys.push(child);
          else walk(child);
        }
      }
    };
    walk(Object.values(result.overlays));
    expect(keys.length).toBeGreaterThan(250);
    for (const key of keys) {
      expect(typeof get(es, key), `es ${key}`).toBe('string');
      expect(typeof get(en, key), `en ${key}`).toBe('string');
    }
  });

  it('el manifiesto referencia la superposición de cada entidad de Go y el fichero existe y es el generado', () => {
    const manifest = JSON.parse(inputs.manifestText);
    for (const entry of manifest.entities.filter((e) => e.path.startsWith('go/'))) {
      expect(entry.overlayPath, entry.id).toBe(`overlays/${entry.id}.i18n.json`);
      const file = path.join(ROOT, 'demo/config', entry.overlayPath);
      expect(existsSync(file), entry.id).toBe(true);
      expect(readJson(file)).toEqual(result.overlays[entry.id]);
    }
    expect(readdirSync(PATHS.overlays).filter((f) => f.endsWith('.json')).sort()).toHaveLength(8);
  });

  it('withOverlayPaths es idempotente y no reformatea el resto del manifiesto', () => {
    const text = inputs.manifestText;
    expect(withOverlayPaths(text, result.entries.map((e) => ({ id: e.id, path: JSON.parse(text).entities.find((x) => x.id === e.id).path })))).toBe(text);
  });
});

describe('--check y la cobertura de en.go.json', () => {
  it('con el repositorio al día, `run --check` sale con 0', () => {
    expect(run(['--check'], silent)).toBe(0);
  });

  it('quitar una clave de en.go.json hace fallar la generación con su nombre', () => {
    const inputs = readInputs();
    const [victim] = Object.keys(inputs.enTable);
    const rest = { ...inputs.enTable };
    delete rest[victim];
    const result = build({ ...inputs, enTable: rest });
    expect(result.errors).toHaveLength(1);
    expect(result.errors[0]).toContain(`falta en en.go.json: ${victim}`);
    // Un valor vacío cuenta como faltante, y una clave que ya no se genera sobra.
    expect(build({ ...inputs, enTable: { ...inputs.enTable, [victim]: '  ' } }).errors[0]).toContain(victim);
    expect(build({ ...inputs, enTable: { ...inputs.enTable, 'entities.asset.fields.nada.label': 'Nothing' } }).errors).toEqual([
      'sobra en en.go.json (la configuración de Go ya no genera esa clave): entities.asset.fields.nada.label',
    ]);
  });

  describe('de extremo a extremo, sobre una copia del árbol', () => {
    let tree;
    const goDir = PATHS.goDir;
    const script = () => path.join(tree, 'react/scripts/gen-demo-i18n.mjs');
    const exec = (...args) => spawnSync(process.execPath, [script(), ...args], { encoding: 'utf8' });

    const copyTree = () => {
      tree = mkdtempSync(path.join(tmpdir(), 'karpo-i18n-'));
      cpSync(path.join(ROOT, 'scripts/gen-demo-i18n.mjs'), script(), { recursive: true });
      cpSync(path.join(ROOT, 'demo/config'), path.join(tree, 'react/demo/config'), { recursive: true });
      cpSync(goDir, path.join(tree, 'angular/config/entities'), { recursive: true });
    };
    afterEach(() => {
      if (tree) rmSync(tree, { recursive: true, force: true });
      tree = undefined;
    });

    it('`--check` pasa en la copia y falla si se borra una clave de en.go.json', () => {
      copyTree();
      expect(exec('--check').status).toBe(0);
      const table = path.join(tree, 'react/demo/config/i18n/en.go.json');
      const entries = readJson(table);
      const victim = 'entities.asset.options.class.land';
      expect(entries[victim]).toBe('Land');
      delete entries[victim];
      writeFileSync(table, JSON.stringify(entries, null, 2));
      const failed = exec('--check');
      expect(failed.status).toBe(1);
      expect(failed.stderr).toContain(`falta en en.go.json: ${victim}`);
      // También al generar: no escribe nada a medias.
      const before = readFileSync(path.join(tree, 'react/demo/config/i18n/en.json'), 'utf8');
      expect(exec().status).toBe(1);
      expect(readFileSync(path.join(tree, 'react/demo/config/i18n/en.json'), 'utf8')).toBe(before);
    });

    it('`--check` falla si lo generado está desactualizado y generar lo arregla (idempotente)', () => {
      copyTree();
      const asset = path.join(tree, 'react/demo/config/overlays/asset.i18n.json');
      const original = readFileSync(asset, 'utf8');
      writeFileSync(asset, original.replace('entities.asset.fields.name.label', 'entities.asset.fields.otro.label'));
      const stale = exec('--check');
      expect(stale.status).toBe(1);
      expect(stale.stderr).toContain('desactualizado: demo/config/overlays/asset.i18n.json');
      writeFileSync(path.join(tree, 'react/demo/config/overlays/vieja.i18n.json'), '{}');
      expect(exec('--check').stderr).toContain('sobra: demo/config/overlays/vieja.i18n.json');
      expect(exec().status).toBe(0);
      expect(readFileSync(asset, 'utf8')).toBe(original);
      expect(existsSync(path.join(tree, 'react/demo/config/overlays/vieja.i18n.json'))).toBe(false);
      expect(exec('--check').status).toBe(0);
    });

    it('`--list` imprime clave y español', () => {
      copyTree();
      const listed = exec('--list');
      expect(listed.status).toBe(0);
      expect(listed.stdout).toContain('entities.facility.form.createTitle\tNueva instalación');
      expect(listed.stdout.trim().split('\n').length).toBeGreaterThanOrEqual(250);
    });
  });
});

describe('el resultado visto desde el normalizador (demo, lng=en)', () => {
  let report;
  beforeAll(async () => {
    clearConfigDictionaries();
    report = await loadDemoReport();
  });

  /** Todos los `Text` (objetos `{literal, key?}`) de una entidad normalizada, con su ruta. */
  function textsOf(config) {
    const found = [];
    const seen = new Set();
    const walk = (value, where) => {
      if (Array.isArray(value)) return value.forEach((item, i) => walk(item, `${where}[${i}]`));
      if (typeof value !== 'object' || value === null || seen.has(value)) return;
      seen.add(value);
      if (typeof value.literal === 'string' && Object.keys(value).every((k) => ['literal', 'key', 'byLang'].includes(k))) {
        found.push({ where, text: value });
        return;
      }
      for (const [key, child] of Object.entries(value)) if (key !== 'warnings') walk(child, where ? `${where}.${key}` : key);
    };
    walk(config, '');
    return found.filter((f) => f.text.literal !== '');
  }

  const GO_IDS = ['asset', 'carrier', 'country', 'facility', 'holiday', 'product', 'supplier', 'work-center'];
  const SPANISH_HINT = /[áéíóúñ¿¡]|\b(de|del|la|el|los|las|para|con|sin|por|que|una?|se)\b/i;

  it('las 8 entidades se cargan sin avisos `warn` de texto ni de superposición', () => {
    for (const id of GO_IDS) {
      const entity = report.manifest.entities[id];
      expect(entity, id).toBeDefined();
      expect(entity.warnings.filter((w) => w.severity === 'warn'), id).toEqual([]);
    }
    expect(report.warnings.filter((w) => /overlayPath/.test(w.path))).toEqual([]);
  });

  it('todo texto con literal de las 8 entidades trae clave (normalize.ts lee todas las `*Key` generadas)', () => {
    let count = 0;
    for (const id of GO_IDS) {
      for (const { where, text } of textsOf(report.manifest.entities[id])) {
        count += 1;
        expect(text.key, `${id}.${where} «${text.literal}»`).toBeTruthy();
      }
    }
    expect(count).toBeGreaterThan(300);
  });

  it('en lng=es cada texto generado sigue siendo el literal de Go (nombres y descripciones son de la demo, a mano)', () => {
    const generated = build(readInputs()).literals;
    let checked = 0;
    for (const id of GO_IDS) {
      for (const { where, text } of textsOf(report.manifest.entities[id])) {
        if (!generated.has(text.key)) continue;
        checked += 1;
        expect(resolveText(text, 'es'), `${id}.${where}`).toBe(text.literal);
      }
    }
    expect(checked).toBeGreaterThan(300);
  });

  it('en lng=en no queda ningún literal español en cabeceras, secciones, etiquetas, pistas, filtros, órdenes ni opciones', () => {
    const left = [];
    let checked = 0;
    for (const id of GO_IDS) {
      for (const { where, text } of textsOf(report.manifest.entities[id])) {
        if (text.key.includes(':')) continue;
        const english = resolveText(text, 'en');
        checked += 1;
        if (english === text.literal && !SAME_IN_ENGLISH.has(text.literal)) left.push(`${id}.${where}: «${text.literal}»`);
        // Heurística de salvaguarda: nada de tildes, signos de apertura ni palabras vacías españolas.
        if (!SAME_IN_ENGLISH.has(english) && SPANISH_HINT.test(english)) {
          left.push(`${id}.${where}: «${english}» parece español`);
        }
      }
    }
    expect(left).toEqual([]);
    expect(checked).toBeGreaterThan(300);
  });

  it('lo traducido es lo de la tabla: cabeceras, opciones de badgeMap y órdenes en inglés', () => {
    const asset = report.manifest.entities.asset;
    const text = (t) => resolveText(t, 'en');
    expect(asset.columns.find((c) => c.field === 'status').options.map((o) => text(o.label))).toEqual(['In service', 'Depreciated', 'Disposed']);
    expect(asset.columns.map((c) => text(c.label))).toEqual(['Code', 'Name', 'Class', 'Cost', 'Net value', 'Status']);
    expect(asset.form.sections[0].fields.find((f) => f.field === 'class').options.map((o) => text(o.label))).toContain('Computer equipment');
    expect(asset.commands.map((c) => text(c.label))).toEqual(['View details', 'Edit']);
    const facility = report.manifest.entities.facility;
    expect(facility.commands.find((c) => c.id === 'delete').confirm).toBeDefined();
    expect(text(facility.commands.find((c) => c.id === 'delete').confirm)).toBe('Deactivate this facility? It is not deleted: it can be activated again.');
    expect(text(facility.filters[0].label)).toBe('Search');
    const carrier = report.manifest.entities.carrier;
    expect(text(carrier.detail.childGrids[0].title)).toBe('Block history');
    expect(text(carrier.detail.childGrids[0].columns.find((c) => c.field === 'blocked').trueLabel)).toBe('Block');
    // El `hint` del formulario llega al detalle (M-8) y se traduce con la misma clave.
    const detailType = facility.detail.sections[0].fields.find((f) => f.field === 'typeName');
    expect(text(detailType.label)).toBe('Type');
    const form = facility.form.sections[0].fields.find((f) => f.field === 'type');
    expect(text(form.hint)).toBe('Identity of the facility type; it does not change after creation.');
  });
});

describe('árabe de la demo (D-R38): ar.go.json -> ar.json', () => {
  const inputs = readInputs();
  const result = build(inputs);
  const get = (dictionary, key) => key.split('.').reduce((node, part) => node?.[part], dictionary);
  const ARABIC = /[؀-ۿ]/;

  it('las entidades de las capturas (facility y product) están completas y en árabe', () => {
    expect(ARABIC_ENTITIES).toEqual(['facility', 'product']);
    expect(result.errors).toEqual([]);
    const ar = JSON.parse(result.files.get(PATHS.ar));
    let count = 0;
    for (const [key, literal] of result.literals) {
      if (!ARABIC_ENTITIES.map(camelCase).includes(key.split('.')[1])) continue;
      const text = get(ar, key);
      expect(typeof text, key).toBe('string');
      expect(text, key).toMatch(ARABIC);
      expect(text, key).not.toBe(literal);
      count += 1;
    }
    expect(count).toBeGreaterThanOrEqual(55);
  });

  it('el resto de entidades no trae texto generado en árabe: caen al inglés en tiempo de ejecución', () => {
    const ar = JSON.parse(result.files.get(PATHS.ar));
    for (const id of ['asset', 'carrier', 'country', 'holiday', 'workCenter', 'supplier']) {
      for (const branch of ['columns', 'fields', 'options', 'sections', 'form', 'filters', 'commands']) {
        expect(ar.entities[id][branch], `${id}.${branch}`).toBeUndefined();
      }
      // Lo escrito a mano (nombre, plural, descripción) sí está en árabe.
      expect(ar.entities[id].name, id).toMatch(ARABIC);
      expect(ar.entities[id].namePlural, id).toMatch(ARABIC);
    }
  });

  it('el diccionario árabe tiene la misma estructura escrita a mano que el inglés (menú, entidades, vistas, mapa, órdenes, ayuda)', () => {
    const ids = Object.keys(inputs.en.entities);
    const shape = (dictionary) => {
      const keys = [];
      const walk = (value, prefix) => {
        if (typeof value === 'string') keys.push(prefix);
        else if (value && typeof value === 'object') for (const [k, v] of Object.entries(value)) walk(v, prefix ? `${prefix}.${k}` : k);
      };
      walk(withoutOwned(dictionary, ids.map(camelCase)), '');
      return keys.sort();
    };
    expect(shape(JSON.parse(result.files.get(PATHS.ar)))).toEqual(shape(JSON.parse(result.files.get(PATHS.en))));
  });

  it('con el repositorio al día `--check` pasa, y el manifiesto lista ar con su diccionario', () => {
    expect(run(['--check'], silent)).toBe(0);
    const manifest = JSON.parse(inputs.manifestText);
    expect(manifest.locales).toContain('ar');
    expect(manifest.i18n.ar).toBe('i18n/ar.json');
    expect(existsSync(path.join(ROOT, 'demo/config', manifest.i18n.ar))).toBe(true);
  });

  it('quitar una clave de ar.go.json hace fallar la generación con su nombre; una clave sobrante también', () => {
    const [victim] = Object.keys(inputs.arTable);
    const rest = { ...inputs.arTable };
    delete rest[victim];
    const failed = build({ ...inputs, arTable: rest });
    expect(failed.errors).toHaveLength(1);
    expect(failed.errors[0]).toContain(`falta en ar.go.json: ${victim}`);
    expect(build({ ...inputs, arTable: { ...inputs.arTable, [victim]: ' ' } }).errors[0]).toContain(victim);
    expect(build({ ...inputs, arTable: { ...inputs.arTable, 'entities.asset.fields.nada.label': 'لا شيء' } }).errors).toEqual([
      'sobra en ar.go.json (la configuración de Go ya no genera esa clave): entities.asset.fields.nada.label',
    ]);
    // Una entidad fuera de las capturas puede ir sin traducir.
    expect(build({ ...inputs, arTable: { ...inputs.arTable, 'entities.asset.fields.name.label': 'الاسم' } }).errors).toEqual([]);
  });

  describe('de extremo a extremo, sobre una copia del árbol', () => {
    let tree;
    const script = () => path.join(tree, 'react/scripts/gen-demo-i18n.mjs');
    const exec = (...args) => spawnSync(process.execPath, [script(), ...args], { encoding: 'utf8' });
    afterEach(() => {
      if (tree) rmSync(tree, { recursive: true, force: true });
      tree = undefined;
    });

    it('`npm run i18n:demo -- --check` falla si se borra una clave de ar.go.json y no escribe nada a medias', () => {
      tree = mkdtempSync(path.join(tmpdir(), 'karpo-i18n-ar-'));
      cpSync(path.join(ROOT, 'scripts/gen-demo-i18n.mjs'), script(), { recursive: true });
      cpSync(path.join(ROOT, 'demo/config'), path.join(tree, 'react/demo/config'), { recursive: true });
      cpSync(PATHS.goDir, path.join(tree, 'angular/config/entities'), { recursive: true });
      expect(exec('--check').status).toBe(0);
      const table = path.join(tree, 'react/demo/config/i18n/ar.go.json');
      const entries = readJson(table);
      const victim = 'entities.product.fields.basePrice.label';
      expect(entries[victim]).toBe('السعر الأساسي');
      delete entries[victim];
      writeFileSync(table, JSON.stringify(entries, null, 2));
      const failed = exec('--check');
      expect(failed.status).toBe(1);
      expect(failed.stderr).toContain(`falta en ar.go.json: ${victim}`);
      const before = readFileSync(path.join(tree, 'react/demo/config/i18n/ar.json'), 'utf8');
      expect(exec().status).toBe(1);
      expect(readFileSync(path.join(tree, 'react/demo/config/i18n/ar.json'), 'utf8')).toBe(before);
    });
  });
});
