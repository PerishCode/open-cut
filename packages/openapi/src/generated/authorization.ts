import type {
  CLIChallengeRequest,
  CLIChallengeResult,
  CLIGrant,
  CliGrantListOutputBody,
  CliScopeUpgradeDecisionOutputBody,
  ErrorModel,
  UIChallengeRequest,
  UIChallengeResult,
  UISessionRequest,
  UISessionResult
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type createCliChallengeResponse200 = {
  data: CLIChallengeResult
  status: 200
}

export type createCliChallengeResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type createCliChallengeResponseSuccess = (createCliChallengeResponse200) & {
  headers: Headers;
};
export type createCliChallengeResponseError = (createCliChallengeResponseDefault) & {
  headers: Headers;
};

export type createCliChallengeResponse = (createCliChallengeResponseSuccess | createCliChallengeResponseError)

export const getCreateCliChallengeUrl = () => {




  return `/api/v1/auth/cli/challenges`
}

/**
 * @summary Create a single-use product CLI command challenge
 */
export const createCliChallenge = async (cLIChallengeRequest: CLIChallengeRequest, options?: RequestInit): Promise<createCliChallengeResponse> => {

  const res = await fetch(getCreateCliChallengeUrl(),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(cLIChallengeRequest)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: createCliChallengeResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as createCliChallengeResponse
}


export type createUiChallengeResponse200 = {
  data: UIChallengeResult
  status: 200
}

export type createUiChallengeResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type createUiChallengeResponseSuccess = (createUiChallengeResponse200) & {
  headers: Headers;
};
export type createUiChallengeResponseError = (createUiChallengeResponseDefault) & {
  headers: Headers;
};

export type createUiChallengeResponse = (createUiChallengeResponseSuccess | createUiChallengeResponseError)

export const getCreateUiChallengeUrl = () => {




  return `/api/v1/auth/ui/challenges`
}

/**
 * @summary Create a single-use first-party UI possession challenge
 */
export const createUiChallenge = async (uIChallengeRequest: UIChallengeRequest, options?: RequestInit): Promise<createUiChallengeResponse> => {

  const res = await fetch(getCreateUiChallengeUrl(),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(uIChallengeRequest)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: createUiChallengeResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as createUiChallengeResponse
}


export type createUiSessionResponse200 = {
  data: UISessionResult
  status: 200
}

export type createUiSessionResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type createUiSessionResponseSuccess = (createUiSessionResponse200) & {
  headers: Headers;
};
export type createUiSessionResponseError = (createUiSessionResponseDefault) & {
  headers: Headers;
};

export type createUiSessionResponse = (createUiSessionResponseSuccess | createUiSessionResponseError)

export const getCreateUiSessionUrl = () => {




  return `/api/v1/auth/ui/sessions`
}

/**
 * @summary Exchange a signed challenge for an API-instance UI session
 */
export const createUiSession = async (uISessionRequest: UISessionRequest, options?: RequestInit): Promise<createUiSessionResponse> => {

  const res = await fetch(getCreateUiSessionUrl(),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(uISessionRequest)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: createUiSessionResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as createUiSessionResponse
}


export type listCliPairingsResponse200 = {
  data: CliGrantListOutputBody
  status: 200
}

export type listCliPairingsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listCliPairingsResponseSuccess = (listCliPairingsResponse200) & {
  headers: Headers;
};
export type listCliPairingsResponseError = (listCliPairingsResponseDefault) & {
  headers: Headers;
};

export type listCliPairingsResponse = (listCliPairingsResponseSuccess | listCliPairingsResponseError)

export const getListCliPairingsUrl = () => {




  return `/api/v1/authorization/cli/pairings`
}

/**
 * @summary List this installation's product CLI grants
 */
export const listCliPairings = async ( options?: RequestInit): Promise<listCliPairingsResponse> => {

  const res = await fetch(getListCliPairingsUrl(),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listCliPairingsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listCliPairingsResponse
}


export type approveCliPairingResponse200 = {
  data: CLIGrant
  status: 200
}

export type approveCliPairingResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type approveCliPairingResponseSuccess = (approveCliPairingResponse200) & {
  headers: Headers;
};
export type approveCliPairingResponseError = (approveCliPairingResponseDefault) & {
  headers: Headers;
};

export type approveCliPairingResponse = (approveCliPairingResponseSuccess | approveCliPairingResponseError)

export const getApproveCliPairingUrl = (id: string,) => {




  return `/api/v1/authorization/cli/pairings/${id}/approve`
}

/**
 * @summary approve an exact pending product CLI grant
 */
export const approveCliPairing = async (id: string, options?: RequestInit): Promise<approveCliPairingResponse> => {

  const res = await fetch(getApproveCliPairingUrl(id),
  {
    ...options,
    method: 'POST'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: approveCliPairingResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as approveCliPairingResponse
}


export type denyCliPairingResponse200 = {
  data: CLIGrant
  status: 200
}

export type denyCliPairingResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type denyCliPairingResponseSuccess = (denyCliPairingResponse200) & {
  headers: Headers;
};
export type denyCliPairingResponseError = (denyCliPairingResponseDefault) & {
  headers: Headers;
};

export type denyCliPairingResponse = (denyCliPairingResponseSuccess | denyCliPairingResponseError)

export const getDenyCliPairingUrl = (id: string,) => {




  return `/api/v1/authorization/cli/pairings/${id}/deny`
}

/**
 * @summary deny an exact pending product CLI grant
 */
export const denyCliPairing = async (id: string, options?: RequestInit): Promise<denyCliPairingResponse> => {

  const res = await fetch(getDenyCliPairingUrl(id),
  {
    ...options,
    method: 'POST'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: denyCliPairingResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as denyCliPairingResponse
}


export type revokeCliPairingResponse200 = {
  data: CLIGrant
  status: 200
}

export type revokeCliPairingResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type revokeCliPairingResponseSuccess = (revokeCliPairingResponse200) & {
  headers: Headers;
};
export type revokeCliPairingResponseError = (revokeCliPairingResponseDefault) & {
  headers: Headers;
};

export type revokeCliPairingResponse = (revokeCliPairingResponseSuccess | revokeCliPairingResponseError)

export const getRevokeCliPairingUrl = (id: string,) => {




  return `/api/v1/authorization/cli/pairings/${id}/revoke`
}

/**
 * @summary Revoke an active product CLI grant
 */
export const revokeCliPairing = async (id: string, options?: RequestInit): Promise<revokeCliPairingResponse> => {

  const res = await fetch(getRevokeCliPairingUrl(id),
  {
    ...options,
    method: 'POST'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: revokeCliPairingResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as revokeCliPairingResponse
}


export type approveCliScopeUpgradeResponse200 = {
  data: CliScopeUpgradeDecisionOutputBody
  status: 200
}

export type approveCliScopeUpgradeResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type approveCliScopeUpgradeResponseSuccess = (approveCliScopeUpgradeResponse200) & {
  headers: Headers;
};
export type approveCliScopeUpgradeResponseError = (approveCliScopeUpgradeResponseDefault) & {
  headers: Headers;
};

export type approveCliScopeUpgradeResponse = (approveCliScopeUpgradeResponseSuccess | approveCliScopeUpgradeResponseError)

export const getApproveCliScopeUpgradeUrl = (id: string,) => {




  return `/api/v1/authorization/cli/scope-upgrades/${id}/approve`
}

/**
 * @summary approve an exact pending product CLI scope upgrade
 */
export const approveCliScopeUpgrade = async (id: string, options?: RequestInit): Promise<approveCliScopeUpgradeResponse> => {

  const res = await fetch(getApproveCliScopeUpgradeUrl(id),
  {
    ...options,
    method: 'POST'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: approveCliScopeUpgradeResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as approveCliScopeUpgradeResponse
}


export type denyCliScopeUpgradeResponse200 = {
  data: CliScopeUpgradeDecisionOutputBody
  status: 200
}

export type denyCliScopeUpgradeResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type denyCliScopeUpgradeResponseSuccess = (denyCliScopeUpgradeResponse200) & {
  headers: Headers;
};
export type denyCliScopeUpgradeResponseError = (denyCliScopeUpgradeResponseDefault) & {
  headers: Headers;
};

export type denyCliScopeUpgradeResponse = (denyCliScopeUpgradeResponseSuccess | denyCliScopeUpgradeResponseError)

export const getDenyCliScopeUpgradeUrl = (id: string,) => {




  return `/api/v1/authorization/cli/scope-upgrades/${id}/deny`
}

/**
 * @summary deny an exact pending product CLI scope upgrade
 */
export const denyCliScopeUpgrade = async (id: string, options?: RequestInit): Promise<denyCliScopeUpgradeResponse> => {

  const res = await fetch(getDenyCliScopeUpgradeUrl(id),
  {
    ...options,
    method: 'POST'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: denyCliScopeUpgradeResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as denyCliScopeUpgradeResponse
}
