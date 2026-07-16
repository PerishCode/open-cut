import type {
  AcquireProductResourceInput,
  AcquireProductResourceResult,
  ErrorModel,
  ProductResourceSnapshot,
  ProductStatusSnapshot
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type listProductResourcesResponse200 = {
  data: ProductResourceSnapshot
  status: 200
}

export type listProductResourcesResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listProductResourcesResponseSuccess = (listProductResourcesResponse200) & {
  headers: Headers;
};
export type listProductResourcesResponseError = (listProductResourcesResponseDefault) & {
  headers: Headers;
};

export type listProductResourcesResponse = (listProductResourcesResponseSuccess | listProductResourcesResponseError)

export const getListProductResourcesUrl = () => {




  return `/api/v1/product/resources`
}

/**
 * @summary List active-payload product resources and local state
 */
export const listProductResources = async ( options?: RequestInit): Promise<listProductResourcesResponse> => {

  const res = await fetch(getListProductResourcesUrl(),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listProductResourcesResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listProductResourcesResponse
}


export type acquireProductResourceResponse200 = {
  data: AcquireProductResourceResult
  status: 200
}

export type acquireProductResourceResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type acquireProductResourceResponseSuccess = (acquireProductResourceResponse200) & {
  headers: Headers;
};
export type acquireProductResourceResponseError = (acquireProductResourceResponseDefault) & {
  headers: Headers;
};

export type acquireProductResourceResponse = (acquireProductResourceResponseSuccess | acquireProductResourceResponseError)

export const getAcquireProductResourceUrl = (resourceName: string,) => {




  return `/api/v1/product/resources/${resourceName}/acquisition`
}

/**
 * @summary Authorize acquisition of one authenticated product resource
 */
export const acquireProductResource = async (resourceName: string,
    acquireProductResourceInput: AcquireProductResourceInput, options?: RequestInit): Promise<acquireProductResourceResponse> => {

  const res = await fetch(getAcquireProductResourceUrl(resourceName),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(acquireProductResourceInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: acquireProductResourceResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as acquireProductResourceResponse
}


export type showProductStatusResponse200 = {
  data: ProductStatusSnapshot
  status: 200
}

export type showProductStatusResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showProductStatusResponseSuccess = (showProductStatusResponse200) & {
  headers: Headers;
};
export type showProductStatusResponseError = (showProductStatusResponseDefault) & {
  headers: Headers;
};

export type showProductStatusResponse = (showProductStatusResponseSuccess | showProductStatusResponseError)

export const getShowProductStatusUrl = () => {




  return `/api/v1/product/status`
}

/**
 * @summary Show semantic product feature availability
 */
export const showProductStatus = async ( options?: RequestInit): Promise<showProductStatusResponse> => {

  const res = await fetch(getShowProductStatusUrl(),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showProductStatusResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showProductStatusResponse
}
