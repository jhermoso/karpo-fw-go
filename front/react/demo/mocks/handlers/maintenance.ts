// El contrato de mantenimiento (`/api/v1/<entidad>`) de las 8 entidades de Go con configuración.
import type { HttpHandler } from 'msw';
import { MAINTENANCE_ENTITIES } from '../entities';
import { maintenanceHandlers } from '../maintenance-core';

export const handlers: HttpHandler[] = MAINTENANCE_ENTITIES.flatMap((def) => maintenanceHandlers(def));
