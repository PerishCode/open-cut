import type {
  CreatorExportCancelInput,
  ErrorModel,
  ExportCancelInput,
  ExportData,
  ExportHistoryData,
  ExportShowInput,
  ExportStartInput,
  ListCreatorSequenceExportsParams,
  SequenceExportDeleteArtifactInput,
  SequenceExportDeliveryLease
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type createPlatformSequenceExportDeliveryLeaseResponse200 = {
  data: SequenceExportDeliveryLease
  status: 200
}

export type createPlatformSequenceExportDeliveryLeaseResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type createPlatformSequenceExportDeliveryLeaseResponseSuccess = (createPlatformSequenceExportDeliveryLeaseResponse200) & {
  headers: Headers;
};
export type createPlatformSequenceExportDeliveryLeaseResponseError = (createPlatformSequenceExportDeliveryLeaseResponseDefault) & {
  headers: Headers;
};

export type createPlatformSequenceExportDeliveryLeaseResponse = (createPlatformSequenceExportDeliveryLeaseResponseSuccess | createPlatformSequenceExportDeliveryLeaseResponseError)

export const getCreatePlatformSequenceExportDeliveryLeaseUrl = (projectId: string,
    artifactId: string,) => {




  return `/api/v1/internal/platform/projects/${projectId}/export-artifacts/${artifactId}/leases`
}

/**
 * @summary Create an Electron-main-only ExportArtifact delivery lease
 */
export const createPlatformSequenceExportDeliveryLease = async (projectId: string,
    artifactId: string, options?: RequestInit): Promise<createPlatformSequenceExportDeliveryLeaseResponse> => {

  const res = await fetch(getCreatePlatformSequenceExportDeliveryLeaseUrl(projectId,artifactId),
  {
    ...options,
    method: 'POST'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: createPlatformSequenceExportDeliveryLeaseResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as createPlatformSequenceExportDeliveryLeaseResponse
}


export type listCreatorSequenceExportsResponse200 = {
  data: ExportHistoryData
  status: 200
}

export type listCreatorSequenceExportsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listCreatorSequenceExportsResponseSuccess = (listCreatorSequenceExportsResponse200) & {
  headers: Headers;
};
export type listCreatorSequenceExportsResponseError = (listCreatorSequenceExportsResponseDefault) & {
  headers: Headers;
};

export type listCreatorSequenceExportsResponse = (listCreatorSequenceExportsResponseSuccess | listCreatorSequenceExportsResponseError)

export const getListCreatorSequenceExportsUrl = (projectId: string,
    params?: ListCreatorSequenceExportsParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/exports?${stringifiedParams}` : `/api/v1/projects/${projectId}/exports`
}

/**
 * @summary List bounded project export lineages as Creator
 */
export const listCreatorSequenceExports = async (projectId: string,
    params?: ListCreatorSequenceExportsParams, options?: RequestInit): Promise<listCreatorSequenceExportsResponse> => {

  const res = await fetch(getListCreatorSequenceExportsUrl(projectId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listCreatorSequenceExportsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listCreatorSequenceExportsResponse
}


export type showCreatorSequenceExportResponse200 = {
  data: ExportData
  status: 200
}

export type showCreatorSequenceExportResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showCreatorSequenceExportResponseSuccess = (showCreatorSequenceExportResponse200) & {
  headers: Headers;
};
export type showCreatorSequenceExportResponseError = (showCreatorSequenceExportResponseDefault) & {
  headers: Headers;
};

export type showCreatorSequenceExportResponse = (showCreatorSequenceExportResponseSuccess | showCreatorSequenceExportResponseError)

export const getShowCreatorSequenceExportUrl = (projectId: string,
    jobId: string,) => {




  return `/api/v1/projects/${projectId}/exports/${jobId}`
}

/**
 * @summary Show one project export lineage as Creator
 */
export const showCreatorSequenceExport = async (projectId: string,
    jobId: string, options?: RequestInit): Promise<showCreatorSequenceExportResponse> => {

  const res = await fetch(getShowCreatorSequenceExportUrl(projectId,jobId),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showCreatorSequenceExportResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showCreatorSequenceExportResponse
}


export type deleteCreatorSequenceExportArtifactResponse200 = {
  data: ExportData
  status: 200
}

export type deleteCreatorSequenceExportArtifactResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type deleteCreatorSequenceExportArtifactResponseSuccess = (deleteCreatorSequenceExportArtifactResponse200) & {
  headers: Headers;
};
export type deleteCreatorSequenceExportArtifactResponseError = (deleteCreatorSequenceExportArtifactResponseDefault) & {
  headers: Headers;
};

export type deleteCreatorSequenceExportArtifactResponse = (deleteCreatorSequenceExportArtifactResponseSuccess | deleteCreatorSequenceExportArtifactResponseError)

export const getDeleteCreatorSequenceExportArtifactUrl = (projectId: string,
    jobId: string,) => {




  return `/api/v1/projects/${projectId}/exports/${jobId}/artifact/delete`
}

/**
 * @summary Explicitly delete one current-tail ExportArtifact as Creator
 */
export const deleteCreatorSequenceExportArtifact = async (projectId: string,
    jobId: string,
    sequenceExportDeleteArtifactInput: SequenceExportDeleteArtifactInput, options?: RequestInit): Promise<deleteCreatorSequenceExportArtifactResponse> => {

  const res = await fetch(getDeleteCreatorSequenceExportArtifactUrl(projectId,jobId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(sequenceExportDeleteArtifactInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: deleteCreatorSequenceExportArtifactResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as deleteCreatorSequenceExportArtifactResponse
}


export type cancelCreatorSequenceExportResponse200 = {
  data: ExportData
  status: 200
}

export type cancelCreatorSequenceExportResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type cancelCreatorSequenceExportResponseSuccess = (cancelCreatorSequenceExportResponse200) & {
  headers: Headers;
};
export type cancelCreatorSequenceExportResponseError = (cancelCreatorSequenceExportResponseDefault) & {
  headers: Headers;
};

export type cancelCreatorSequenceExportResponse = (cancelCreatorSequenceExportResponseSuccess | cancelCreatorSequenceExportResponseError)

export const getCancelCreatorSequenceExportUrl = (projectId: string,
    jobId: string,) => {




  return `/api/v1/projects/${projectId}/exports/${jobId}/cancel`
}

/**
 * @summary Cancel one active project export lineage as Creator
 */
export const cancelCreatorSequenceExport = async (projectId: string,
    jobId: string,
    creatorExportCancelInput: CreatorExportCancelInput, options?: RequestInit): Promise<cancelCreatorSequenceExportResponse> => {

  const res = await fetch(getCancelCreatorSequenceExportUrl(projectId,jobId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(creatorExportCancelInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: cancelCreatorSequenceExportResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as cancelCreatorSequenceExportResponse
}


export type retryCreatorSequenceExportResponse200 = {
  data: ExportData
  status: 200
}

export type retryCreatorSequenceExportResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type retryCreatorSequenceExportResponseSuccess = (retryCreatorSequenceExportResponse200) & {
  headers: Headers;
};
export type retryCreatorSequenceExportResponseError = (retryCreatorSequenceExportResponseDefault) & {
  headers: Headers;
};

export type retryCreatorSequenceExportResponse = (retryCreatorSequenceExportResponseSuccess | retryCreatorSequenceExportResponseError)

export const getRetryCreatorSequenceExportUrl = (projectId: string,
    jobId: string,) => {




  return `/api/v1/projects/${projectId}/exports/${jobId}/retry`
}

/**
 * @summary Retry one recoverable project export lineage as Creator
 */
export const retryCreatorSequenceExport = async (projectId: string,
    jobId: string, options?: RequestInit): Promise<retryCreatorSequenceExportResponse> => {

  const res = await fetch(getRetryCreatorSequenceExportUrl(projectId,jobId),
  {
    ...options,
    method: 'POST'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: retryCreatorSequenceExportResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as retryCreatorSequenceExportResponse
}


export type showSequenceExportResponse200 = {
  data: ExportData
  status: 200
}

export type showSequenceExportResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showSequenceExportResponseSuccess = (showSequenceExportResponse200) & {
  headers: Headers;
};
export type showSequenceExportResponseError = (showSequenceExportResponseDefault) & {
  headers: Headers;
};

export type showSequenceExportResponse = (showSequenceExportResponseSuccess | showSequenceExportResponseError)

export const getShowSequenceExportUrl = (projectId: string,
    runId: string,
    turnId: string,
    jobId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}/turns/${turnId}/exports/${jobId}`
}

/**
 * @summary Show one durable export lineage
 */
export const showSequenceExport = async (projectId: string,
    runId: string,
    turnId: string,
    jobId: string, options?: RequestInit): Promise<showSequenceExportResponse> => {

  const res = await fetch(getShowSequenceExportUrl(projectId,runId,turnId,jobId),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showSequenceExportResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showSequenceExportResponse
}


export type cancelSequenceExportResponse200 = {
  data: ExportData
  status: 200
}

export type cancelSequenceExportResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type cancelSequenceExportResponseSuccess = (cancelSequenceExportResponse200) & {
  headers: Headers;
};
export type cancelSequenceExportResponseError = (cancelSequenceExportResponseDefault) & {
  headers: Headers;
};

export type cancelSequenceExportResponse = (cancelSequenceExportResponseSuccess | cancelSequenceExportResponseError)

export const getCancelSequenceExportUrl = (projectId: string,
    runId: string,
    turnId: string,
    jobId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}/turns/${turnId}/exports/${jobId}/cancel`
}

/**
 * @summary Cancel one active export lineage
 */
export const cancelSequenceExport = async (projectId: string,
    runId: string,
    turnId: string,
    jobId: string,
    exportCancelInput: ExportCancelInput, options?: RequestInit): Promise<cancelSequenceExportResponse> => {

  const res = await fetch(getCancelSequenceExportUrl(projectId,runId,turnId,jobId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(exportCancelInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: cancelSequenceExportResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as cancelSequenceExportResponse
}


export type retrySequenceExportResponse200 = {
  data: ExportData
  status: 200
}

export type retrySequenceExportResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type retrySequenceExportResponseSuccess = (retrySequenceExportResponse200) & {
  headers: Headers;
};
export type retrySequenceExportResponseError = (retrySequenceExportResponseDefault) & {
  headers: Headers;
};

export type retrySequenceExportResponse = (retrySequenceExportResponseSuccess | retrySequenceExportResponseError)

export const getRetrySequenceExportUrl = (projectId: string,
    runId: string,
    turnId: string,
    jobId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}/turns/${turnId}/exports/${jobId}/retry`
}

/**
 * @summary Retry one recoverable export lineage
 */
export const retrySequenceExport = async (projectId: string,
    runId: string,
    turnId: string,
    jobId: string,
    exportShowInput: ExportShowInput, options?: RequestInit): Promise<retrySequenceExportResponse> => {

  const res = await fetch(getRetrySequenceExportUrl(projectId,runId,turnId,jobId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(exportShowInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: retrySequenceExportResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as retrySequenceExportResponse
}


export type startSequenceExportResponse200 = {
  data: ExportData
  status: 200
}

export type startSequenceExportResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type startSequenceExportResponseSuccess = (startSequenceExportResponse200) & {
  headers: Headers;
};
export type startSequenceExportResponseError = (startSequenceExportResponseDefault) & {
  headers: Headers;
};

export type startSequenceExportResponse = (startSequenceExportResponseSuccess | startSequenceExportResponseError)

export const getStartSequenceExportUrl = (projectId: string,
    runId: string,
    turnId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}/turns/${turnId}/sequences/${sequenceId}/exports`
}

/**
 * @summary Start one pinned full-quality Sequence export
 */
export const startSequenceExport = async (projectId: string,
    runId: string,
    turnId: string,
    sequenceId: string,
    exportStartInput: ExportStartInput, options?: RequestInit): Promise<startSequenceExportResponse> => {

  const res = await fetch(getStartSequenceExportUrl(projectId,runId,turnId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(exportStartInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: startSequenceExportResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as startSequenceExportResponse
}


export type startCreatorSequenceExportResponse200 = {
  data: ExportData
  status: 200
}

export type startCreatorSequenceExportResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type startCreatorSequenceExportResponseSuccess = (startCreatorSequenceExportResponse200) & {
  headers: Headers;
};
export type startCreatorSequenceExportResponseError = (startCreatorSequenceExportResponseDefault) & {
  headers: Headers;
};

export type startCreatorSequenceExportResponse = (startCreatorSequenceExportResponseSuccess | startCreatorSequenceExportResponseError)

export const getStartCreatorSequenceExportUrl = (projectId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/exports`
}

/**
 * @summary Start one Creator-owned pinned full-quality Sequence export
 */
export const startCreatorSequenceExport = async (projectId: string,
    sequenceId: string,
    exportStartInput: ExportStartInput, options?: RequestInit): Promise<startCreatorSequenceExportResponse> => {

  const res = await fetch(getStartCreatorSequenceExportUrl(projectId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(exportStartInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: startCreatorSequenceExportResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as startCreatorSequenceExportResponse
}
