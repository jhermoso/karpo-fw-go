// Adaptador HTTP de Karpo Go (T1): cliente `fetch`, errores problem+json, contrato de
// mantenimiento, normalizadores auxiliares y ganchos de react-query.
export {
  createHttpClient,
  buildUrl,
  parseRetryAfter,
  type HttpClient,
  type HttpClientOptions,
  type HttpRequestInit,
  type HttpResponse,
} from './http';
export {
  ApiError,
  NO_FIELD,
  describeApiError,
  describeFieldError,
  isApiError,
  kindForStatus,
  localValidationError,
  networkError,
  parseProblem,
  splitFieldErrors,
  timeoutError,
  type ErrorText,
} from './errors';
export {
  LIST_ALL_LIMIT,
  MAX_PAGE_SIZE,
  createEntityApi,
  expandTemplate,
  resolveServerSort,
  type BoundaryQuery,
  type EntityApiExt,
} from './entity-api';
export {
  camelKey,
  camelize,
  extractItems,
  normalizeBoundaries,
  normalizeBoundary,
  normalizeCatalog,
  normalizeChart,
  toPage,
  toRow,
  type Boundary,
  type BoundaryDetail,
  type ChartNode,
} from './normalizers';
export {
  LABEL_BULK_THRESHOLD,
  createLabelBatcher,
  flattenPages,
  patchEntityCaches,
  useEntityApi,
  useEntityApiExt,
  useEntityList,
  useEntityPage,
  useEntityRecord,
  useLookupLabel,
  type LabelBatcher,
} from './hooks';
