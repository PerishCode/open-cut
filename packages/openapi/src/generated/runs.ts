import type {
  ErrorModel,
  RunBeginInput,
  RunCommandResult,
  RunResumeInput,
  WaitAgentRunParams
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type beginAgentRunResponse200 = {
  data: RunCommandResult
  status: 200
}

export type beginAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type beginAgentRunResponseSuccess = (beginAgentRunResponse200) & {
  headers: Headers;
};
export type beginAgentRunResponseError = (beginAgentRunResponseDefault) & {
  headers: Headers;
};

export type beginAgentRunResponse = (beginAgentRunResponseSuccess | beginAgentRunResponseError)

export const getBeginAgentRunUrl = (projectId: string,) => {




  return `/api/v1/projects/${projectId}/runs`
}

/**
 * @summary Begin a durable standalone AgentRun
 */
export const beginAgentRun = async (projectId: string,
    runBeginInput: RunBeginInput, options?: RequestInit): Promise<beginAgentRunResponse> => {

  const res = await fetch(getBeginAgentRunUrl(projectId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(runBeginInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: beginAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as beginAgentRunResponse
}


export type showAgentRunResponse200 = {
  data: RunCommandResult
  status: 200
}

export type showAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showAgentRunResponseSuccess = (showAgentRunResponse200) & {
  headers: Headers;
};
export type showAgentRunResponseError = (showAgentRunResponseDefault) & {
  headers: Headers;
};

export type showAgentRunResponse = (showAgentRunResponseSuccess | showAgentRunResponseError)

export const getShowAgentRunUrl = (projectId: string,
    runId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}`
}

/**
 * @summary Show a durable AgentRun
 */
export const showAgentRun = async (projectId: string,
    runId: string, options?: RequestInit): Promise<showAgentRunResponse> => {

  const res = await fetch(getShowAgentRunUrl(projectId,runId),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showAgentRunResponse
}


export type cancelAgentRunResponse200 = {
  data: RunCommandResult
  status: 200
}

export type cancelAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type cancelAgentRunResponseSuccess = (cancelAgentRunResponse200) & {
  headers: Headers;
};
export type cancelAgentRunResponseError = (cancelAgentRunResponseDefault) & {
  headers: Headers;
};

export type cancelAgentRunResponse = (cancelAgentRunResponseSuccess | cancelAgentRunResponseError)

export const getCancelAgentRunUrl = (projectId: string,
    runId: string,
    turnId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}/turns/${turnId}/cancel`
}

/**
 * @summary Explicitly cancel an AgentRun without reverting committed work
 */
export const cancelAgentRun = async (projectId: string,
    runId: string,
    turnId: string,
    runResumeInput: RunResumeInput, options?: RequestInit): Promise<cancelAgentRunResponse> => {

  const res = await fetch(getCancelAgentRunUrl(projectId,runId,turnId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(runResumeInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: cancelAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as cancelAgentRunResponse
}


export type completeAgentRunResponse200 = {
  data: RunCommandResult
  status: 200
}

export type completeAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type completeAgentRunResponseSuccess = (completeAgentRunResponse200) & {
  headers: Headers;
};
export type completeAgentRunResponseError = (completeAgentRunResponseDefault) & {
  headers: Headers;
};

export type completeAgentRunResponse = (completeAgentRunResponseSuccess | completeAgentRunResponseError)

export const getCompleteAgentRunUrl = (projectId: string,
    runId: string,
    turnId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}/turns/${turnId}/complete`
}

/**
 * @summary Explicitly complete an AgentRun
 */
export const completeAgentRun = async (projectId: string,
    runId: string,
    turnId: string,
    runResumeInput: RunResumeInput, options?: RequestInit): Promise<completeAgentRunResponse> => {

  const res = await fetch(getCompleteAgentRunUrl(projectId,runId,turnId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(runResumeInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: completeAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as completeAgentRunResponse
}


export type resumeAgentRunResponse200 = {
  data: RunCommandResult
  status: 200
}

export type resumeAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type resumeAgentRunResponseSuccess = (resumeAgentRunResponse200) & {
  headers: Headers;
};
export type resumeAgentRunResponseError = (resumeAgentRunResponseDefault) & {
  headers: Headers;
};

export type resumeAgentRunResponse = (resumeAgentRunResponseSuccess | resumeAgentRunResponseError)

export const getResumeAgentRunUrl = (projectId: string,
    runId: string,
    turnId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}/turns/${turnId}/resume`
}

/**
 * @summary Resume an AgentRun with a new writer-turn generation
 */
export const resumeAgentRun = async (projectId: string,
    runId: string,
    turnId: string,
    runResumeInput: RunResumeInput, options?: RequestInit): Promise<resumeAgentRunResponse> => {

  const res = await fetch(getResumeAgentRunUrl(projectId,runId,turnId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(runResumeInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: resumeAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as resumeAgentRunResponse
}


export type waitAgentRunResponse200 = {
  data: RunCommandResult
  status: 200
}

export type waitAgentRunResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type waitAgentRunResponseSuccess = (waitAgentRunResponse200) & {
  headers: Headers;
};
export type waitAgentRunResponseError = (waitAgentRunResponseDefault) & {
  headers: Headers;
};

export type waitAgentRunResponse = (waitAgentRunResponseSuccess | waitAgentRunResponseError)

export const getWaitAgentRunUrl = (projectId: string,
    runId: string,
    params?: WaitAgentRunParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/runs/${runId}/wait?${stringifiedParams}` : `/api/v1/projects/${projectId}/runs/${runId}/wait`
}

/**
 * @summary Wait a bounded interval for durable AgentRun activity
 */
export const waitAgentRun = async (projectId: string,
    runId: string,
    params?: WaitAgentRunParams, options?: RequestInit): Promise<waitAgentRunResponse> => {

  const res = await fetch(getWaitAgentRunUrl(projectId,runId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: waitAgentRunResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as waitAgentRunResponse
}
