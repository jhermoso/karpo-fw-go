// Dobles de las vistas de la ola 2 (cuadrícula, organigrama y mapa) para probar el marco sin depender
// de su implementación. Sólo pruebas.
import { useEffect } from 'react';
import { flattenPages, useEntityList } from '../api';
import type { EntityViewProps, Row } from '../contracts';
import { publishSelection } from './selection';

/** Cuántas veces se ha montado el doble de la cuadrícula (para comprobar que el panel no la desmonta). */
export const gridMounts = { count: 0 };

export function GridDouble({ config, route, navigate, onStatus }: EntityViewProps) {
  useEffect(() => {
    gridMounts.count += 1;
  }, []);
  const list = useEntityList(config, { page: 1, pageSize: 25, filter: {} });
  const rows: Row[] = flattenPages(list.data);
  const total = list.data?.pages[0]?.total;
  // Como la cuadrícula de verdad: cuenta a la cabecera de la entidad lo que hay (sin filtro, todo coincide).
  useEffect(() => {
    if (total !== undefined) onStatus?.({ total, matched: total });
  }, [total, onStatus]);
  return (
    <section aria-label={`Doble de la cuadrícula de ${config.id}`} data-kx-scope="grid">
      <h2>{`Cuadrícula de ${config.id}`}</h2>
      <button type="button" onClick={() => navigate({ entity: config.id, mode: 'edit' })}>
        Nuevo (doble)
      </button>
      {list.isError ? <p role="status">La lista falló</p> : null}
      <ul>
        {rows.slice(0, 5).map((row) => (
          <li key={row.id}>
            <button
              type="button"
              aria-current={route.id === row.id ? 'true' : undefined}
              onClick={() => publishSelection(config.id, [row])}
              onDoubleClick={() => navigate({ entity: config.id, id: row.id, mode: 'edit' })}
            >
              {String(row[config.displayField] ?? row.id)}
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

export function TreeDouble({ config, route }: EntityViewProps) {
  return (
    <section aria-label="Doble del organigrama">
      <h2>{`Organigrama de ${config.id}`}</h2>
      <p>{`vista=${route.view ?? '-'}`}</p>
      <p>{`layout=${route.params?.layout ?? '-'}`}</p>
    </section>
  );
}

export function MapDouble({ config, route }: EntityViewProps) {
  return (
    <section aria-label="Doble del mapa">
      <h2>{`Mapa de ${config.id}`}</h2>
      <p>{`vista=${route.view ?? '-'}`}</p>
    </section>
  );
}
