import type {
  ActivityPage,
  ErrorModel,
  ListActivityParams,
  WatchActivity200Item,
  WatchActivityParams
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type listActivityResponse200 = {
  data: ActivityPage
  status: 200
}

export type listActivityResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listActivityResponseSuccess = (listActivityResponse200) & {
  headers: Headers;
};
export type listActivityResponseError = (listActivityResponseDefault) & {
  headers: Headers;
};

export type listActivityResponse = (listActivityResponseSuccess | listActivityResponseError)

export const getListActivityUrl = (params?: ListActivityParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/activity?${stringifiedParams}` : `/api/v1/activity`
}

/**
 * @summary List durable activity strictly after a scoped cursor
 */
export const listActivity = async (params?: ListActivityParams, options?: RequestInit): Promise<listActivityResponse> => {

  const res = await fetch(getListActivityUrl(params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listActivityResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listActivityResponse
}


export type watchActivityResponse200 = {
  data: WatchActivity200Item[]
  status: 200
}

export type watchActivityResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type watchActivityResponseSuccess = (watchActivityResponse200) & {
  headers: Headers;
};
export type watchActivityResponseError = (watchActivityResponseDefault) & {
  headers: Headers;
};

export type watchActivityResponse = (watchActivityResponseSuccess | watchActivityResponseError)

export const getWatchActivityUrl = (params?: WatchActivityParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/events?${stringifiedParams}` : `/api/v1/events`
}

/**
 * @summary Watch durable activity after a scoped cursor
 */
export const watchActivity = async (params?: WatchActivityParams, options?: RequestInit): Promise<watchActivityResponse> => {

  const res = await fetch(getWatchActivityUrl(params),
  {
    ...options,
    method: 'GET'


  }
)

  const contentType = (res.headers.get('content-type') ?? '').toLowerCase();
  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: watchActivityResponse['data'] = body ? (contentType.includes('json') ? JSON.parse(body) : body) : {}
  return { data, status: res.status, headers: res.headers } as watchActivityResponse
}
