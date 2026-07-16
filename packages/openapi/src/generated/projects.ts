import type {
  CreateProjectInput,
  CreateProjectResult,
  ErrorModel,
  ListProjectsParams,
  ListProjectsResult,
  ProjectOverview
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type listProjectsResponse200 = {
  data: ListProjectsResult
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

export const getListProjectsUrl = (params?: ListProjectsParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects?${stringifiedParams}` : `/api/v1/projects`
}

/**
 * @summary List a bounded page of Project summaries
 */
export const listProjects = async (params?: ListProjectsParams, options?: RequestInit): Promise<listProjectsResponse> => {

  const res = await fetch(getListProjectsUrl(params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listProjectsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listProjectsResponse
}


export type createProjectResponse200 = {
  data: CreateProjectResult
  status: 200
}

export type createProjectResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type createProjectResponseSuccess = (createProjectResponse200) & {
  headers: Headers;
};
export type createProjectResponseError = (createProjectResponseDefault) & {
  headers: Headers;
};

export type createProjectResponse = (createProjectResponseSuccess | createProjectResponseError)

export const getCreateProjectUrl = () => {




  return `/api/v1/projects`
}

/**
 * @summary Create a Project with its initial narrative and Sequence
 */
export const createProject = async (createProjectInput: CreateProjectInput, options?: RequestInit): Promise<createProjectResponse> => {

  const res = await fetch(getCreateProjectUrl(),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(createProjectInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: createProjectResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as createProjectResponse
}


export type showProjectResponse200 = {
  data: ProjectOverview
  status: 200
}

export type showProjectResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showProjectResponseSuccess = (showProjectResponse200) & {
  headers: Headers;
};
export type showProjectResponseError = (showProjectResponseDefault) & {
  headers: Headers;
};

export type showProjectResponse = (showProjectResponseSuccess | showProjectResponseError)

export const getShowProjectUrl = (id: string,) => {




  return `/api/v1/projects/${id}`
}

/**
 * @summary Show one bounded Project overview
 */
export const showProject = async (id: string, options?: RequestInit): Promise<showProjectResponse> => {

  const res = await fetch(getShowProjectUrl(id),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showProjectResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showProjectResponse
}
