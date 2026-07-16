import type {
  AssetFramesInput,
  AssetInspectData,
  AssetPage,
  AssetRegisterResult,
  ErrorModel,
  ListAssetsParams,
  MediaFrameSetRequestResult,
  MediaLeaseRequest,
  MediaLeaseResult,
  PlatformSourceSelection,
  ReadTranscriptParams,
  RegisterAssetInput,
  SelectTranscriptDefaultInput,
  SequencePreviewLeaseRequest,
  SequencePreviewLeaseResult,
  SourceGrantResult,
  SourcePositionRequest,
  SourcePositionResult,
  TranscriptDefaultSelection,
  TranscriptReadPage
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type registerPlatformSourceGrantResponse200 = {
  data: SourceGrantResult
  status: 200
}

export type registerPlatformSourceGrantResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type registerPlatformSourceGrantResponseSuccess = (registerPlatformSourceGrantResponse200) & {
  headers: Headers;
};
export type registerPlatformSourceGrantResponseError = (registerPlatformSourceGrantResponseDefault) & {
  headers: Headers;
};

export type registerPlatformSourceGrantResponse = (registerPlatformSourceGrantResponseSuccess | registerPlatformSourceGrantResponseError)

export const getRegisterPlatformSourceGrantUrl = () => {




  return `/api/v1/internal/platform/source-grants`
}

/**
 * @summary Register creator-selected platform source authority
 */
export const registerPlatformSourceGrant = async (platformSourceSelection: PlatformSourceSelection, options?: RequestInit): Promise<registerPlatformSourceGrantResponse> => {

  const res = await fetch(getRegisterPlatformSourceGrantUrl(),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(platformSourceSelection)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: registerPlatformSourceGrantResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as registerPlatformSourceGrantResponse
}


export type listAssetsResponse200 = {
  data: AssetPage
  status: 200
}

export type listAssetsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listAssetsResponseSuccess = (listAssetsResponse200) & {
  headers: Headers;
};
export type listAssetsResponseError = (listAssetsResponseDefault) & {
  headers: Headers;
};

export type listAssetsResponse = (listAssetsResponseSuccess | listAssetsResponseError)

export const getListAssetsUrl = (projectId: string,
    params?: ListAssetsParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/assets?${stringifiedParams}` : `/api/v1/projects/${projectId}/assets`
}

/**
 * @summary List bounded Asset summaries
 */
export const listAssets = async (projectId: string,
    params?: ListAssetsParams, options?: RequestInit): Promise<listAssetsResponse> => {

  const res = await fetch(getListAssetsUrl(projectId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listAssetsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listAssetsResponse
}


export type registerAssetResponse200 = {
  data: AssetRegisterResult
  status: 200
}

export type registerAssetResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type registerAssetResponseSuccess = (registerAssetResponse200) & {
  headers: Headers;
};
export type registerAssetResponseError = (registerAssetResponseDefault) & {
  headers: Headers;
};

export type registerAssetResponse = (registerAssetResponseSuccess | registerAssetResponseError)

export const getRegisterAssetUrl = (projectId: string,) => {




  return `/api/v1/projects/${projectId}/assets`
}

/**
 * @summary Commit a creator-selected SourceGrant as a referenced Asset
 */
export const registerAsset = async (projectId: string,
    registerAssetInput: RegisterAssetInput, options?: RequestInit): Promise<registerAssetResponse> => {

  const res = await fetch(getRegisterAssetUrl(projectId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(registerAssetInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: registerAssetResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as registerAssetResponse
}


export type inspectAssetResponse200 = {
  data: AssetInspectData
  status: 200
}

export type inspectAssetResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type inspectAssetResponseSuccess = (inspectAssetResponse200) & {
  headers: Headers;
};
export type inspectAssetResponseError = (inspectAssetResponseDefault) & {
  headers: Headers;
};

export type inspectAssetResponse = (inspectAssetResponseSuccess | inspectAssetResponseError)

export const getInspectAssetUrl = (projectId: string,
    assetId: string,) => {




  return `/api/v1/projects/${projectId}/assets/${assetId}`
}

/**
 * @summary Inspect one Asset and its operational media state
 */
export const inspectAsset = async (projectId: string,
    assetId: string, options?: RequestInit): Promise<inspectAssetResponse> => {

  const res = await fetch(getInspectAssetUrl(projectId,assetId),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: inspectAssetResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as inspectAssetResponse
}


export type createMediaLeaseResponse200 = {
  data: MediaLeaseResult
  status: 200
}

export type createMediaLeaseResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type createMediaLeaseResponseSuccess = (createMediaLeaseResponse200) & {
  headers: Headers;
};
export type createMediaLeaseResponseError = (createMediaLeaseResponseDefault) & {
  headers: Headers;
};

export type createMediaLeaseResponse = (createMediaLeaseResponseSuccess | createMediaLeaseResponseError)

export const getCreateMediaLeaseUrl = (projectId: string,
    assetId: string,) => {




  return `/api/v1/projects/${projectId}/assets/${assetId}/media-leases`
}

/**
 * @summary Prepare a creator Viewer source-preview capability
 */
export const createMediaLease = async (projectId: string,
    assetId: string,
    mediaLeaseRequest: MediaLeaseRequest, options?: RequestInit): Promise<createMediaLeaseResponse> => {

  const res = await fetch(getCreateMediaLeaseUrl(projectId,assetId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(mediaLeaseRequest)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: createMediaLeaseResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as createMediaLeaseResponse
}


export type resolveSourcePreviewPositionResponse200 = {
  data: SourcePositionResult
  status: 200
}

export type resolveSourcePreviewPositionResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type resolveSourcePreviewPositionResponseSuccess = (resolveSourcePreviewPositionResponse200) & {
  headers: Headers;
};
export type resolveSourcePreviewPositionResponseError = (resolveSourcePreviewPositionResponseDefault) & {
  headers: Headers;
};

export type resolveSourcePreviewPositionResponse = (resolveSourcePreviewPositionResponseSuccess | resolveSourcePreviewPositionResponseError)

export const getResolveSourcePreviewPositionUrl = (projectId: string,
    assetId: string,) => {




  return `/api/v1/projects/${projectId}/assets/${assetId}/source-position`
}

/**
 * @summary Resolve one bounded exact position in a pinned creator Source Viewer lease
 */
export const resolveSourcePreviewPosition = async (projectId: string,
    assetId: string,
    sourcePositionRequest: SourcePositionRequest, options?: RequestInit): Promise<resolveSourcePreviewPositionResponse> => {

  const res = await fetch(getResolveSourcePreviewPositionUrl(projectId,assetId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(sourcePositionRequest)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: resolveSourcePreviewPositionResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as resolveSourcePreviewPositionResponse
}


export type readTranscriptResponse200 = {
  data: TranscriptReadPage
  status: 200
}

export type readTranscriptResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type readTranscriptResponseSuccess = (readTranscriptResponse200) & {
  headers: Headers;
};
export type readTranscriptResponseError = (readTranscriptResponseDefault) & {
  headers: Headers;
};

export type readTranscriptResponse = (readTranscriptResponseSuccess | readTranscriptResponseError)

export const getReadTranscriptUrl = (projectId: string,
    assetId: string,
    params?: ReadTranscriptParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/assets/${assetId}/transcript?${stringifiedParams}` : `/api/v1/projects/${projectId}/assets/${assetId}/transcript`
}

/**
 * @summary Read bounded original transcript recognition
 */
export const readTranscript = async (projectId: string,
    assetId: string,
    params?: ReadTranscriptParams, options?: RequestInit): Promise<readTranscriptResponse> => {

  const res = await fetch(getReadTranscriptUrl(projectId,assetId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: readTranscriptResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as readTranscriptResponse
}


export type selectDefaultTranscriptResponse200 = {
  data: TranscriptDefaultSelection
  status: 200
}

export type selectDefaultTranscriptResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type selectDefaultTranscriptResponseSuccess = (selectDefaultTranscriptResponse200) & {
  headers: Headers;
};
export type selectDefaultTranscriptResponseError = (selectDefaultTranscriptResponseDefault) & {
  headers: Headers;
};

export type selectDefaultTranscriptResponse = (selectDefaultTranscriptResponseSuccess | selectDefaultTranscriptResponseError)

export const getSelectDefaultTranscriptUrl = (projectId: string,
    assetId: string,) => {




  return `/api/v1/projects/${projectId}/assets/${assetId}/transcript-selection`
}

/**
 * @summary Select the Creator default transcript artifact
 */
export const selectDefaultTranscript = async (projectId: string,
    assetId: string,
    selectTranscriptDefaultInput: SelectTranscriptDefaultInput, options?: RequestInit): Promise<selectDefaultTranscriptResponse> => {

  const res = await fetch(getSelectDefaultTranscriptUrl(projectId,assetId),
  {
    ...options,
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(selectTranscriptDefaultInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: selectDefaultTranscriptResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as selectDefaultTranscriptResponse
}


export type requestAssetFramesResponse200 = {
  data: MediaFrameSetRequestResult
  status: 200
}

export type requestAssetFramesResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type requestAssetFramesResponseSuccess = (requestAssetFramesResponse200) & {
  headers: Headers;
};
export type requestAssetFramesResponseError = (requestAssetFramesResponseDefault) & {
  headers: Headers;
};

export type requestAssetFramesResponse = (requestAssetFramesResponseSuccess | requestAssetFramesResponseError)

export const getRequestAssetFramesUrl = (projectId: string,
    runId: string,
    turnId: string,
    assetId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}/turns/${turnId}/assets/${assetId}/frames`
}

/**
 * @summary Request bounded exact frame resources
 */
export const requestAssetFrames = async (projectId: string,
    runId: string,
    turnId: string,
    assetId: string,
    assetFramesInput: AssetFramesInput, options?: RequestInit): Promise<requestAssetFramesResponse> => {

  const res = await fetch(getRequestAssetFramesUrl(projectId,runId,turnId,assetId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(assetFramesInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: requestAssetFramesResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as requestAssetFramesResponse
}


export type createSequencePreviewLeaseResponse200 = {
  data: SequencePreviewLeaseResult
  status: 200
}

export type createSequencePreviewLeaseResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type createSequencePreviewLeaseResponseSuccess = (createSequencePreviewLeaseResponse200) & {
  headers: Headers;
};
export type createSequencePreviewLeaseResponseError = (createSequencePreviewLeaseResponseDefault) & {
  headers: Headers;
};

export type createSequencePreviewLeaseResponse = (createSequencePreviewLeaseResponseSuccess | createSequencePreviewLeaseResponseError)

export const getCreateSequencePreviewLeaseUrl = (projectId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/media-leases`
}

/**
 * @summary Prepare an immutable creator Viewer sequence-preview capability
 */
export const createSequencePreviewLease = async (projectId: string,
    sequenceId: string,
    sequencePreviewLeaseRequest: SequencePreviewLeaseRequest, options?: RequestInit): Promise<createSequencePreviewLeaseResponse> => {

  const res = await fetch(getCreateSequencePreviewLeaseUrl(projectId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(sequencePreviewLeaseRequest)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: createSequencePreviewLeaseResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as createSequencePreviewLeaseResponse
}
