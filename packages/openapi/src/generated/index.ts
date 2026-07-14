import type {
  ErrorModel,
  Health,
  ProjectSnapshot,
  ProjectUpserted,
  ProjectWrite,
  WatchProjects200Item
} from './model';

export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type watchProjectsResponse200 = {
  data: WatchProjects200Item[]
  status: 200
}

export type watchProjectsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type watchProjectsResponseSuccess = (watchProjectsResponse200) & {
  headers: Headers;
};
export type watchProjectsResponseError = (watchProjectsResponseDefault) & {
  headers: Headers;
};

export type watchProjectsResponse = (watchProjectsResponseSuccess | watchProjectsResponseError)

export const getWatchProjectsUrl = () => {




  return `/api/v1/events`
}

/**
 * @summary Watch project state
 */
export const watchProjects = async ( options?: RequestInit): Promise<watchProjectsResponse> => {

  const res = await fetch(getWatchProjectsUrl(),
  {
    ...options,
    method: 'GET'


  }
)

  const contentType = (res.headers.get('content-type') ?? '').toLowerCase();
  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: watchProjectsResponse['data'] = body ? (contentType.includes('json') ? JSON.parse(body) : body) : {}
  return { data, status: res.status, headers: res.headers } as watchProjectsResponse
}



export type getHealthResponse200 = {
  data: Health
  status: 200
}

export type getHealthResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type getHealthResponseSuccess = (getHealthResponse200) & {
  headers: Headers;
};
export type getHealthResponseError = (getHealthResponseDefault) & {
  headers: Headers;
};

export type getHealthResponse = (getHealthResponseSuccess | getHealthResponseError)

export const getGetHealthUrl = () => {




  return `/api/v1/health`
}

/**
 * @summary Get API health
 */
export const getHealth = async ( options?: RequestInit): Promise<getHealthResponse> => {

  const res = await fetch(getGetHealthUrl(),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: getHealthResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as getHealthResponse
}



export type listProjectsResponse200 = {
  data: ProjectSnapshot
  status: 200
}

export type listProjectsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listProjectsResponseSuccess = (listProjectsResponse200) & {
  headers: Headers;
};
export type listProjectsResponseError = (listProjectsResponseDefault) & {
  headers: Headers;
};

export type listProjectsResponse = (listProjectsResponseSuccess | listProjectsResponseError)

export const getListProjectsUrl = () => {




  return `/api/v1/projects`
}

/**
 * @summary List projects
 */
export const listProjects = async ( options?: RequestInit): Promise<listProjectsResponse> => {

  const res = await fetch(getListProjectsUrl(),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listProjectsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listProjectsResponse
}



export type putProjectResponse200 = {
  data: ProjectUpserted
  status: 200
}

export type putProjectResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type putProjectResponseSuccess = (putProjectResponse200) & {
  headers: Headers;
};
export type putProjectResponseError = (putProjectResponseDefault) & {
  headers: Headers;
};

export type putProjectResponse = (putProjectResponseSuccess | putProjectResponseError)

export const getPutProjectUrl = (id: string,) => {




  return `/api/v1/projects/${id}`
}

/**
 * @summary Create or replace a project
 */
export const putProject = async (id: string,
    projectWrite: ProjectWrite, options?: RequestInit): Promise<putProjectResponse> => {

  const res = await fetch(getPutProjectUrl(id),
  {
    ...options,
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(projectWrite)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: putProjectResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as putProjectResponse
}
