import type {
  ErrorModel,
  SequenceFramesData,
  SequenceFramesInput
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type inspectSequenceFramesResponse200 = {
  data: SequenceFramesData
  status: 200
}

export type inspectSequenceFramesResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type inspectSequenceFramesResponseSuccess = (inspectSequenceFramesResponse200) & {
  headers: Headers;
};
export type inspectSequenceFramesResponseError = (inspectSequenceFramesResponseDefault) & {
  headers: Headers;
};

export type inspectSequenceFramesResponse = (inspectSequenceFramesResponseSuccess | inspectSequenceFramesResponseError)

export const getInspectSequenceFramesUrl = (projectId: string,
    runId: string,
    turnId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/runs/${runId}/turns/${turnId}/sequences/${sequenceId}/frames`
}

/**
 * @summary Inspect bounded exact frames of one committed Sequence revision
 */
export const inspectSequenceFrames = async (projectId: string,
    runId: string,
    turnId: string,
    sequenceId: string,
    sequenceFramesInput: SequenceFramesInput, options?: RequestInit): Promise<inspectSequenceFramesResponse> => {

  const res = await fetch(getInspectSequenceFramesUrl(projectId,runId,turnId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(sequenceFramesInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: inspectSequenceFramesResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as inspectSequenceFramesResponse
}
