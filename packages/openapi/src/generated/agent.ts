import type {
  AgentBridgeAvailability,
  AgentBridgeBeginInput,
  AgentBridgeContinueInput,
  AgentBridgeResult,
  AgentBridgeRun,
  AgentBridgeRunPage,
  AgentBridgeTransitionInput,
  AgentBridgeTurnPage,
  AgentConversationPage,
  ErrorModel,
  ListCreatorAgentConversationParams,
  ListCreatorAgentRunsParams,
  ListCreatorAgentTurnReceiptsParams,
  ListCreatorAgentTurnsParams,
  TurnReceiptPage,
  WatchCreatorAgentPresentation200Item
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type showLocalAgentAvailabilityResponse200 = {
  data: AgentBridgeAvailability
  status: 200
}

export type showLocalAgentAvailabilityResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showLocalAgentAvailabilityResponseSuccess = (showLocalAgentAvailabilityResponse200) & {
  headers: Headers;
};
export type showLocalAgentAvailabilityResponseError = (showLocalAgentAvailabilityResponseDefault) & {
  headers: Headers;
};

export type showLocalAgentAvailabilityResponse = (showLocalAgentAvailabilityResponseSuccess | showLocalAgentAvailabilityResponseError)

export const getShowLocalAgentAvailabilityUrl = () => {




  return `/api/v1/agent/availability`
}

/**
 * @summary Show safe local Agent adapter availability
 */
export const showLocalAgentAvailability = async ( options?: RequestInit): Promise<showLocalAgentAvailabilityResponse> => {

  const res = await fetch(getShowLocalAgentAvailabilityUrl(),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showLocalAgentAvailabilityResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showLocalAgentAvailabilityResponse
}


export type listCreatorAgentRunsResponse200 = {
  data: AgentBridgeRunPage
  status: 200
}

export type listCreatorAgentRunsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listCreatorAgentRunsResponseSuccess = (listCreatorAgentRunsResponse200) & {
  headers: Headers;
};
export type listCreatorAgentRunsResponseError = (listCreatorAgentRunsResponseDefault) & {
  headers: Headers;
};

export type listCreatorAgentRunsResponse = (listCreatorAgentRunsResponseSuccess | listCreatorAgentRunsResponseError)

export const getListCreatorAgentRunsUrl = (projectId: string,
    params?: ListCreatorAgentRunsParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/agent/runs?${stringifiedParams}` : `/api/v1/projects/${projectId}/agent/runs`
}

/**
 * @summary List bounded recent Creator-started Agent runs
 */
export const listCreatorAgentRuns = async (projectId: string,
    params?: ListCreatorAgentRunsParams, options?: RequestInit): Promise<listCreatorAgentRunsResponse> => {

  const res = await fetch(getListCreatorAgentRunsUrl(projectId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listCreatorAgentRunsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listCreatorAgentRunsResponse
}


export type beginCreatorAgentRunResponse200 = {
  data: AgentBridgeResult
  status: 200
}

export type beginCreatorAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type beginCreatorAgentRunResponseSuccess = (beginCreatorAgentRunResponse200) & {
  headers: Headers;
};
export type beginCreatorAgentRunResponseError = (beginCreatorAgentRunResponseDefault) & {
  headers: Headers;
};

export type beginCreatorAgentRunResponse = (beginCreatorAgentRunResponseSuccess | beginCreatorAgentRunResponseError)

export const getBeginCreatorAgentRunUrl = (projectId: string,) => {




  return `/api/v1/projects/${projectId}/agent/runs`
}

/**
 * @summary Submit one Creator message as a durable Agent turn
 */
export const beginCreatorAgentRun = async (projectId: string,
    agentBridgeBeginInput: AgentBridgeBeginInput, options?: RequestInit): Promise<beginCreatorAgentRunResponse> => {

  const res = await fetch(getBeginCreatorAgentRunUrl(projectId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(agentBridgeBeginInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: beginCreatorAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as beginCreatorAgentRunResponse
}


export type showCreatorAgentRunResponse200 = {
  data: AgentBridgeRun
  status: 200
}

export type showCreatorAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showCreatorAgentRunResponseSuccess = (showCreatorAgentRunResponse200) & {
  headers: Headers;
};
export type showCreatorAgentRunResponseError = (showCreatorAgentRunResponseDefault) & {
  headers: Headers;
};

export type showCreatorAgentRunResponse = (showCreatorAgentRunResponseSuccess | showCreatorAgentRunResponseError)

export const getShowCreatorAgentRunUrl = (projectId: string,
    runId: string,) => {




  return `/api/v1/projects/${projectId}/agent/runs/${runId}`
}

/**
 * @summary Show one Creator-started Agent run
 */
export const showCreatorAgentRun = async (projectId: string,
    runId: string, options?: RequestInit): Promise<showCreatorAgentRunResponse> => {

  const res = await fetch(getShowCreatorAgentRunUrl(projectId,runId),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showCreatorAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showCreatorAgentRunResponse
}


export type listCreatorAgentConversationResponse200 = {
  data: AgentConversationPage
  status: 200
}

export type listCreatorAgentConversationResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listCreatorAgentConversationResponseSuccess = (listCreatorAgentConversationResponse200) & {
  headers: Headers;
};
export type listCreatorAgentConversationResponseError = (listCreatorAgentConversationResponseDefault) & {
  headers: Headers;
};

export type listCreatorAgentConversationResponse = (listCreatorAgentConversationResponseSuccess | listCreatorAgentConversationResponseError)

export const getListCreatorAgentConversationUrl = (projectId: string,
    runId: string,
    params?: ListCreatorAgentConversationParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/agent/runs/${runId}/conversation?${stringifiedParams}` : `/api/v1/projects/${projectId}/agent/runs/${runId}/conversation`
}

/**
 * @summary List the durable safe Agent conversation ledger
 */
export const listCreatorAgentConversation = async (projectId: string,
    runId: string,
    params?: ListCreatorAgentConversationParams, options?: RequestInit): Promise<listCreatorAgentConversationResponse> => {

  const res = await fetch(getListCreatorAgentConversationUrl(projectId,runId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listCreatorAgentConversationResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listCreatorAgentConversationResponse
}


export type continueCreatorAgentRunResponse200 = {
  data: AgentBridgeResult
  status: 200
}

export type continueCreatorAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type continueCreatorAgentRunResponseSuccess = (continueCreatorAgentRunResponse200) & {
  headers: Headers;
};
export type continueCreatorAgentRunResponseError = (continueCreatorAgentRunResponseDefault) & {
  headers: Headers;
};

export type continueCreatorAgentRunResponse = (continueCreatorAgentRunResponseSuccess | continueCreatorAgentRunResponseError)

export const getContinueCreatorAgentRunUrl = (projectId: string,
    runId: string,) => {




  return `/api/v1/projects/${projectId}/agent/runs/${runId}/messages`
}

/**
 * @summary Submit the next Creator message as a new Agent turn
 */
export const continueCreatorAgentRun = async (projectId: string,
    runId: string,
    agentBridgeContinueInput: AgentBridgeContinueInput, options?: RequestInit): Promise<continueCreatorAgentRunResponse> => {

  const res = await fetch(getContinueCreatorAgentRunUrl(projectId,runId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(agentBridgeContinueInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: continueCreatorAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as continueCreatorAgentRunResponse
}


export type listCreatorAgentTurnsResponse200 = {
  data: AgentBridgeTurnPage
  status: 200
}

export type listCreatorAgentTurnsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listCreatorAgentTurnsResponseSuccess = (listCreatorAgentTurnsResponse200) & {
  headers: Headers;
};
export type listCreatorAgentTurnsResponseError = (listCreatorAgentTurnsResponseDefault) & {
  headers: Headers;
};

export type listCreatorAgentTurnsResponse = (listCreatorAgentTurnsResponseSuccess | listCreatorAgentTurnsResponseError)

export const getListCreatorAgentTurnsUrl = (projectId: string,
    runId: string,
    params?: ListCreatorAgentTurnsParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/agent/runs/${runId}/turns?${stringifiedParams}` : `/api/v1/projects/${projectId}/agent/runs/${runId}/turns`
}

/**
 * @summary List authoritative historical turns for one Creator-started Agent run
 */
export const listCreatorAgentTurns = async (projectId: string,
    runId: string,
    params?: ListCreatorAgentTurnsParams, options?: RequestInit): Promise<listCreatorAgentTurnsResponse> => {

  const res = await fetch(getListCreatorAgentTurnsUrl(projectId,runId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listCreatorAgentTurnsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listCreatorAgentTurnsResponse
}


export type cancelCreatorAgentRunResponse200 = {
  data: AgentBridgeResult
  status: 200
}

export type cancelCreatorAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type cancelCreatorAgentRunResponseSuccess = (cancelCreatorAgentRunResponse200) & {
  headers: Headers;
};
export type cancelCreatorAgentRunResponseError = (cancelCreatorAgentRunResponseDefault) & {
  headers: Headers;
};

export type cancelCreatorAgentRunResponse = (cancelCreatorAgentRunResponseSuccess | cancelCreatorAgentRunResponseError)

export const getCancelCreatorAgentRunUrl = (projectId: string,
    runId: string,
    turnId: string,) => {




  return `/api/v1/projects/${projectId}/agent/runs/${runId}/turns/${turnId}/cancel`
}

/**
 * @summary Terminate an Agent run without reverting committed work
 */
export const cancelCreatorAgentRun = async (projectId: string,
    runId: string,
    turnId: string,
    agentBridgeTransitionInput: AgentBridgeTransitionInput, options?: RequestInit): Promise<cancelCreatorAgentRunResponse> => {

  const res = await fetch(getCancelCreatorAgentRunUrl(projectId,runId,turnId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(agentBridgeTransitionInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: cancelCreatorAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as cancelCreatorAgentRunResponse
}


export type interruptCreatorAgentTurnResponse200 = {
  data: AgentBridgeResult
  status: 200
}

export type interruptCreatorAgentTurnResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type interruptCreatorAgentTurnResponseSuccess = (interruptCreatorAgentTurnResponse200) & {
  headers: Headers;
};
export type interruptCreatorAgentTurnResponseError = (interruptCreatorAgentTurnResponseDefault) & {
  headers: Headers;
};

export type interruptCreatorAgentTurnResponse = (interruptCreatorAgentTurnResponseSuccess | interruptCreatorAgentTurnResponseError)

export const getInterruptCreatorAgentTurnUrl = (projectId: string,
    runId: string,
    turnId: string,) => {




  return `/api/v1/projects/${projectId}/agent/runs/${runId}/turns/${turnId}/interrupt`
}

/**
 * @summary Stop the active Agent turn without terminating its Run
 */
export const interruptCreatorAgentTurn = async (projectId: string,
    runId: string,
    turnId: string,
    agentBridgeTransitionInput: AgentBridgeTransitionInput, options?: RequestInit): Promise<interruptCreatorAgentTurnResponse> => {

  const res = await fetch(getInterruptCreatorAgentTurnUrl(projectId,runId,turnId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(agentBridgeTransitionInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: interruptCreatorAgentTurnResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as interruptCreatorAgentTurnResponse
}


export type watchCreatorAgentPresentationResponse200 = {
  data: WatchCreatorAgentPresentation200Item[]
  status: 200
}

export type watchCreatorAgentPresentationResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type watchCreatorAgentPresentationResponseSuccess = (watchCreatorAgentPresentationResponse200) & {
  headers: Headers;
};
export type watchCreatorAgentPresentationResponseError = (watchCreatorAgentPresentationResponseDefault) & {
  headers: Headers;
};

export type watchCreatorAgentPresentationResponse = (watchCreatorAgentPresentationResponseSuccess | watchCreatorAgentPresentationResponseError)

export const getWatchCreatorAgentPresentationUrl = (projectId: string,
    runId: string,
    turnId: string,) => {




  return `/api/v1/projects/${projectId}/agent/runs/${runId}/turns/${turnId}/presentation`
}

/**
 * @summary Watch process-local safe presentation events for the active Agent turn
 */
export const watchCreatorAgentPresentation = async (projectId: string,
    runId: string,
    turnId: string, options?: RequestInit): Promise<watchCreatorAgentPresentationResponse> => {

  const res = await fetch(getWatchCreatorAgentPresentationUrl(projectId,runId,turnId),
  {
    ...options,
    method: 'GET'


  }
)

  const contentType = (res.headers.get('content-type') ?? '').toLowerCase();
  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: watchCreatorAgentPresentationResponse['data'] = body ? (contentType.includes('json') ? JSON.parse(body) : body) : {}
  return { data, status: res.status, headers: res.headers } as watchCreatorAgentPresentationResponse
}


export type listCreatorAgentTurnReceiptsResponse200 = {
  data: TurnReceiptPage
  status: 200
}

export type listCreatorAgentTurnReceiptsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listCreatorAgentTurnReceiptsResponseSuccess = (listCreatorAgentTurnReceiptsResponse200) & {
  headers: Headers;
};
export type listCreatorAgentTurnReceiptsResponseError = (listCreatorAgentTurnReceiptsResponseDefault) & {
  headers: Headers;
};

export type listCreatorAgentTurnReceiptsResponse = (listCreatorAgentTurnReceiptsResponseSuccess | listCreatorAgentTurnReceiptsResponseError)

export const getListCreatorAgentTurnReceiptsUrl = (projectId: string,
    runId: string,
    turnId: string,
    params?: ListCreatorAgentTurnReceiptsParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/agent/runs/${runId}/turns/${turnId}/receipts?${stringifiedParams}` : `/api/v1/projects/${projectId}/agent/runs/${runId}/turns/${turnId}/receipts`
}

/**
 * @summary List the independent durable command receipt ledger for one Agent turn
 */
export const listCreatorAgentTurnReceipts = async (projectId: string,
    runId: string,
    turnId: string,
    params?: ListCreatorAgentTurnReceiptsParams, options?: RequestInit): Promise<listCreatorAgentTurnReceiptsResponse> => {

  const res = await fetch(getListCreatorAgentTurnReceiptsUrl(projectId,runId,turnId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listCreatorAgentTurnReceiptsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listCreatorAgentTurnReceiptsResponse
}
