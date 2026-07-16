import type {
  CaptionDerivationPreview,
  CreatorCaptionDerivationPreviewInput,
  CreatorCaptionGesturePreview,
  CreatorCaptionGesturePreviewInput,
  CreatorClipPlacementPreview,
  CreatorClipPlacementPreviewInput,
  CreatorTimelineGesturePreviewInput,
  CreatorTimelineGesturePreviewResult,
  CreatorTransactionHistoryPage,
  ErrorModel,
  ListCreatorEditTransactionsParams
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type listCreatorEditTransactionsResponse200 = {
  data: CreatorTransactionHistoryPage
  status: 200
}

export type listCreatorEditTransactionsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listCreatorEditTransactionsResponseSuccess = (listCreatorEditTransactionsResponse200) & {
  headers: Headers;
};
export type listCreatorEditTransactionsResponseError = (listCreatorEditTransactionsResponseDefault) & {
  headers: Headers;
};

export type listCreatorEditTransactionsResponse = (listCreatorEditTransactionsResponseSuccess | listCreatorEditTransactionsResponseError)

export const getListCreatorEditTransactionsUrl = (projectId: string,
    params?: ListCreatorEditTransactionsParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/creator-edit/transactions?${stringifiedParams}` : `/api/v1/projects/${projectId}/creator-edit/transactions`
}

/**
 * @summary List newest-first durable creative transaction history for the Creator Workspace
 */
export const listCreatorEditTransactions = async (projectId: string,
    params?: ListCreatorEditTransactionsParams, options?: RequestInit): Promise<listCreatorEditTransactionsResponse> => {

  const res = await fetch(getListCreatorEditTransactionsUrl(projectId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listCreatorEditTransactionsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listCreatorEditTransactionsResponse
}


export type previewCreatorCaptionsResponse200 = {
  data: CaptionDerivationPreview
  status: 200
}

export type previewCreatorCaptionsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type previewCreatorCaptionsResponseSuccess = (previewCreatorCaptionsResponse200) & {
  headers: Headers;
};
export type previewCreatorCaptionsResponseError = (previewCreatorCaptionsResponseDefault) & {
  headers: Headers;
};

export type previewCreatorCaptionsResponse = (previewCreatorCaptionsResponseSuccess | previewCreatorCaptionsResponseError)

export const getPreviewCreatorCaptionsUrl = (projectId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/caption-derivation-preview`
}

/**
 * @summary Preview one insert-only deterministic Creator SourceExcerpt-to-Caption derivation
 */
export const previewCreatorCaptions = async (projectId: string,
    sequenceId: string,
    creatorCaptionDerivationPreviewInput: CreatorCaptionDerivationPreviewInput, options?: RequestInit): Promise<previewCreatorCaptionsResponse> => {

  const res = await fetch(getPreviewCreatorCaptionsUrl(projectId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(creatorCaptionDerivationPreviewInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: previewCreatorCaptionsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as previewCreatorCaptionsResponse
}


export type previewCreatorCaptionGestureResponse200 = {
  data: CreatorCaptionGesturePreview
  status: 200
}

export type previewCreatorCaptionGestureResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type previewCreatorCaptionGestureResponseSuccess = (previewCreatorCaptionGestureResponse200) & {
  headers: Headers;
};
export type previewCreatorCaptionGestureResponseError = (previewCreatorCaptionGestureResponseDefault) & {
  headers: Headers;
};

export type previewCreatorCaptionGestureResponse = (previewCreatorCaptionGestureResponseSuccess | previewCreatorCaptionGestureResponseError)

export const getPreviewCreatorCaptionGestureUrl = (projectId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/caption-gesture-preview`
}

/**
 * @summary Plan one exact Creator manual Caption gesture over complete collision and Alignment state
 */
export const previewCreatorCaptionGesture = async (projectId: string,
    sequenceId: string,
    creatorCaptionGesturePreviewInput: CreatorCaptionGesturePreviewInput, options?: RequestInit): Promise<previewCreatorCaptionGestureResponse> => {

  const res = await fetch(getPreviewCreatorCaptionGestureUrl(projectId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(creatorCaptionGesturePreviewInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: previewCreatorCaptionGestureResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as previewCreatorCaptionGestureResponse
}


export type previewCreatorClipPlacementResponse200 = {
  data: CreatorClipPlacementPreview
  status: 200
}

export type previewCreatorClipPlacementResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type previewCreatorClipPlacementResponseSuccess = (previewCreatorClipPlacementResponse200) & {
  headers: Headers;
};
export type previewCreatorClipPlacementResponseError = (previewCreatorClipPlacementResponseDefault) & {
  headers: Headers;
};

export type previewCreatorClipPlacementResponse = (previewCreatorClipPlacementResponseSuccess | previewCreatorClipPlacementResponseError)

export const getPreviewCreatorClipPlacementUrl = (projectId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/clip-placement-preview`
}

/**
 * @summary Plan one exact Creator source-range placement on explicit existing tracks
 */
export const previewCreatorClipPlacement = async (projectId: string,
    sequenceId: string,
    creatorClipPlacementPreviewInput: CreatorClipPlacementPreviewInput, options?: RequestInit): Promise<previewCreatorClipPlacementResponse> => {

  const res = await fetch(getPreviewCreatorClipPlacementUrl(projectId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(creatorClipPlacementPreviewInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: previewCreatorClipPlacementResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as previewCreatorClipPlacementResponse
}


export type previewCreatorTimelineGestureResponse200 = {
  data: CreatorTimelineGesturePreviewResult
  status: 200
}

export type previewCreatorTimelineGestureResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type previewCreatorTimelineGestureResponseSuccess = (previewCreatorTimelineGestureResponse200) & {
  headers: Headers;
};
export type previewCreatorTimelineGestureResponseError = (previewCreatorTimelineGestureResponseDefault) & {
  headers: Headers;
};

export type previewCreatorTimelineGestureResponse = (previewCreatorTimelineGestureResponseSuccess | previewCreatorTimelineGestureResponseError)

export const getPreviewCreatorTimelineGestureUrl = (projectId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/timeline-gesture-preview`
}

/**
 * @summary Plan one exact Creator Timeline gesture over complete linked and Alignment state
 */
export const previewCreatorTimelineGesture = async (projectId: string,
    sequenceId: string,
    creatorTimelineGesturePreviewInput: CreatorTimelineGesturePreviewInput, options?: RequestInit): Promise<previewCreatorTimelineGestureResponse> => {

  const res = await fetch(getPreviewCreatorTimelineGestureUrl(projectId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(creatorTimelineGesturePreviewInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: previewCreatorTimelineGestureResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as previewCreatorTimelineGestureResponse
}
