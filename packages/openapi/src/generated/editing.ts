import type {
  CaptionDerivationPreview,
  DeriveCaptionOperationParams,
  EditApplyInput,
  EditCommitResult,
  EditEntityDetail,
  EditProposalResult,
  EditProposeInput,
  EditShowData,
  EditUndoInput,
  ErrorModel,
  ListEditTransactionsParams,
  NarrativeSubtreePage,
  RoughCutDerivationPreview,
  RoughCutDerivationPreviewInput,
  SequenceWindowPage,
  ShowNarrativeSubtreeParams,
  ShowSequenceWindowParams,
  TransactionHistoryPage
} from './model';



export type HTTPStatusCode1xx = 100 | 101 | 102 | 103;
export type HTTPStatusCode2xx = 200 | 201 | 202 | 203 | 204 | 205 | 206 | 207;
export type HTTPStatusCode3xx = 300 | 301 | 302 | 303 | 304 | 305 | 307 | 308;
export type HTTPStatusCode4xx = 400 | 401 | 402 | 403 | 404 | 405 | 406 | 407 | 408 | 409 | 410 | 411 | 412 | 413 | 414 | 415 | 416 | 417 | 418 | 419 | 420 | 421 | 422 | 423 | 424 | 426 | 428 | 429 | 431 | 451;
export type HTTPStatusCode5xx = 500 | 501 | 502 | 503 | 504 | 505 | 507 | 511;
export type HTTPStatusCodes = HTTPStatusCode1xx | HTTPStatusCode2xx | HTTPStatusCode3xx | HTTPStatusCode4xx | HTTPStatusCode5xx;

export type showEditProposalResponse200 = {
  data: EditShowData
  status: 200
}

export type showEditProposalResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showEditProposalResponseSuccess = (showEditProposalResponse200) & {
  headers: Headers;
};
export type showEditProposalResponseError = (showEditProposalResponseDefault) & {
  headers: Headers;
};

export type showEditProposalResponse = (showEditProposalResponseSuccess | showEditProposalResponseError)

export const getShowEditProposalUrl = (projectId: string,
    proposalId: string,) => {




  return `/api/v1/projects/${projectId}/edit/proposals/${proposalId}`
}

/**
 * @summary Show one durable Edit Proposal
 */
export const showEditProposal = async (projectId: string,
    proposalId: string, options?: RequestInit): Promise<showEditProposalResponse> => {

  const res = await fetch(getShowEditProposalUrl(projectId,proposalId),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showEditProposalResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showEditProposalResponse
}


export type listEditTransactionsResponse200 = {
  data: TransactionHistoryPage
  status: 200
}

export type listEditTransactionsResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type listEditTransactionsResponseSuccess = (listEditTransactionsResponse200) & {
  headers: Headers;
};
export type listEditTransactionsResponseError = (listEditTransactionsResponseDefault) & {
  headers: Headers;
};

export type listEditTransactionsResponse = (listEditTransactionsResponseSuccess | listEditTransactionsResponseError)

export const getListEditTransactionsUrl = (projectId: string,
    params?: ListEditTransactionsParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/edit/transactions?${stringifiedParams}` : `/api/v1/projects/${projectId}/edit/transactions`
}

/**
 * @summary List bounded committed Edit history
 */
export const listEditTransactions = async (projectId: string,
    params?: ListEditTransactionsParams, options?: RequestInit): Promise<listEditTransactionsResponse> => {

  const res = await fetch(getListEditTransactionsUrl(projectId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: listEditTransactionsResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as listEditTransactionsResponse
}


export type showEditEntityResponse200 = {
  data: EditEntityDetail
  status: 200
}

export type showEditEntityResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showEditEntityResponseSuccess = (showEditEntityResponse200) & {
  headers: Headers;
};
export type showEditEntityResponseError = (showEditEntityResponseDefault) & {
  headers: Headers;
};

export type showEditEntityResponse = (showEditEntityResponseSuccess | showEditEntityResponseError)

export const getShowEditEntityUrl = (projectId: string,
    kind: 'narrative-node' | 'transcript-correction' | 'caption' | 'alignment' | 'clip' | 'link-group',
    id: string,) => {




  return `/api/v1/projects/${projectId}/entities/${kind}/${id}`
}

/**
 * @summary Show one editable entity with its exact revision
 */
export const showEditEntity = async (projectId: string,
    kind: 'narrative-node' | 'transcript-correction' | 'caption' | 'alignment' | 'clip' | 'link-group',
    id: string, options?: RequestInit): Promise<showEditEntityResponse> => {

  const res = await fetch(getShowEditEntityUrl(projectId,kind,id),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showEditEntityResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showEditEntityResponse
}


export type showNarrativeSubtreeResponse200 = {
  data: NarrativeSubtreePage
  status: 200
}

export type showNarrativeSubtreeResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showNarrativeSubtreeResponseSuccess = (showNarrativeSubtreeResponse200) & {
  headers: Headers;
};
export type showNarrativeSubtreeResponseError = (showNarrativeSubtreeResponseDefault) & {
  headers: Headers;
};

export type showNarrativeSubtreeResponse = (showNarrativeSubtreeResponseSuccess | showNarrativeSubtreeResponseError)

export const getShowNarrativeSubtreeUrl = (projectId: string,
    documentId: string,
    params: ShowNarrativeSubtreeParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/narratives/${documentId}/subtree?${stringifiedParams}` : `/api/v1/projects/${projectId}/narratives/${documentId}/subtree`
}

/**
 * @summary Show one bounded authored-text Narrative subtree
 */
export const showNarrativeSubtree = async (projectId: string,
    documentId: string,
    params: ShowNarrativeSubtreeParams, options?: RequestInit): Promise<showNarrativeSubtreeResponse> => {

  const res = await fetch(getShowNarrativeSubtreeUrl(projectId,documentId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showNarrativeSubtreeResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showNarrativeSubtreeResponse
}


export type deriveCaptionOperationResponse200 = {
  data: CaptionDerivationPreview
  status: 200
}

export type deriveCaptionOperationResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type deriveCaptionOperationResponseSuccess = (deriveCaptionOperationResponse200) & {
  headers: Headers;
};
export type deriveCaptionOperationResponseError = (deriveCaptionOperationResponseDefault) & {
  headers: Headers;
};

export type deriveCaptionOperationResponse = (deriveCaptionOperationResponseSuccess | deriveCaptionOperationResponseError)

export const getDeriveCaptionOperationUrl = (projectId: string,
    sequenceId: string,
    params: DeriveCaptionOperationParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/sequences/${sequenceId}/edit/caption-derivation?${stringifiedParams}` : `/api/v1/projects/${projectId}/sequences/${sequenceId}/edit/caption-derivation`
}

/**
 * @summary Preview one deterministic SourceExcerpt-to-Clip caption operation
 */
export const deriveCaptionOperation = async (projectId: string,
    sequenceId: string,
    params: DeriveCaptionOperationParams, options?: RequestInit): Promise<deriveCaptionOperationResponse> => {

  const res = await fetch(getDeriveCaptionOperationUrl(projectId,sequenceId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: deriveCaptionOperationResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as deriveCaptionOperationResponse
}


export type deriveRoughCutOperationResponse200 = {
  data: RoughCutDerivationPreview
  status: 200
}

export type deriveRoughCutOperationResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type deriveRoughCutOperationResponseSuccess = (deriveRoughCutOperationResponse200) & {
  headers: Headers;
};
export type deriveRoughCutOperationResponseError = (deriveRoughCutOperationResponseDefault) & {
  headers: Headers;
};

export type deriveRoughCutOperationResponse = (deriveRoughCutOperationResponseSuccess | deriveRoughCutOperationResponseError)

export const getDeriveRoughCutOperationUrl = (projectId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/edit/rough-cut-derivation`
}

/**
 * @summary Preview one deterministic PaperEdit-to-Sequence rough-cut operation
 */
export const deriveRoughCutOperation = async (projectId: string,
    sequenceId: string,
    roughCutDerivationPreviewInput: RoughCutDerivationPreviewInput, options?: RequestInit): Promise<deriveRoughCutOperationResponse> => {

  const res = await fetch(getDeriveRoughCutOperationUrl(projectId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(roughCutDerivationPreviewInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: deriveRoughCutOperationResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as deriveRoughCutOperationResponse
}


export type commitCreatorEditResponse200 = {
  data: EditCommitResult
  status: 200
}

export type commitCreatorEditResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type commitCreatorEditResponseSuccess = (commitCreatorEditResponse200) & {
  headers: Headers;
};
export type commitCreatorEditResponseError = (commitCreatorEditResponseDefault) & {
  headers: Headers;
};

export type commitCreatorEditResponse = (commitCreatorEditResponseSuccess | commitCreatorEditResponseError)

export const getCommitCreatorEditUrl = (projectId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/edits`
}

/**
 * @summary Normalize and atomically commit one Creator edit
 */
export const commitCreatorEdit = async (projectId: string,
    sequenceId: string,
    editProposeInput: EditProposeInput, options?: RequestInit): Promise<commitCreatorEditResponse> => {

  const res = await fetch(getCommitCreatorEditUrl(projectId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(editProposeInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: commitCreatorEditResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as commitCreatorEditResponse
}


export type previewCreatorRoughCutResponse200 = {
  data: RoughCutDerivationPreview
  status: 200
}

export type previewCreatorRoughCutResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type previewCreatorRoughCutResponseSuccess = (previewCreatorRoughCutResponse200) & {
  headers: Headers;
};
export type previewCreatorRoughCutResponseError = (previewCreatorRoughCutResponseDefault) & {
  headers: Headers;
};

export type previewCreatorRoughCutResponse = (previewCreatorRoughCutResponseSuccess | previewCreatorRoughCutResponseError)

export const getPreviewCreatorRoughCutUrl = (projectId: string,
    sequenceId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/rough-cut-preview`
}

/**
 * @summary Preview one deterministic Creator PaperEdit-to-Sequence rough cut
 */
export const previewCreatorRoughCut = async (projectId: string,
    sequenceId: string,
    roughCutDerivationPreviewInput: RoughCutDerivationPreviewInput, options?: RequestInit): Promise<previewCreatorRoughCutResponse> => {

  const res = await fetch(getPreviewCreatorRoughCutUrl(projectId,sequenceId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(roughCutDerivationPreviewInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: previewCreatorRoughCutResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as previewCreatorRoughCutResponse
}


export type proposeEditResponse200 = {
  data: EditProposalResult
  status: 200
}

export type proposeEditResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type proposeEditResponseSuccess = (proposeEditResponse200) & {
  headers: Headers;
};
export type proposeEditResponseError = (proposeEditResponseDefault) & {
  headers: Headers;
};

export type proposeEditResponse = (proposeEditResponseSuccess | proposeEditResponseError)

export const getProposeEditUrl = (projectId: string,
    sequenceId: string,
    runId: string,
    turnId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/runs/${runId}/turns/${turnId}/edit/proposals`
}

/**
 * @summary Normalize and durably journal an Edit Proposal
 */
export const proposeEdit = async (projectId: string,
    sequenceId: string,
    runId: string,
    turnId: string,
    editProposeInput: EditProposeInput, options?: RequestInit): Promise<proposeEditResponse> => {

  const res = await fetch(getProposeEditUrl(projectId,sequenceId,runId,turnId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(editProposeInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: proposeEditResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as proposeEditResponse
}


export type applyEditProposalResponse200 = {
  data: EditCommitResult
  status: 200
}

export type applyEditProposalResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type applyEditProposalResponseSuccess = (applyEditProposalResponse200) & {
  headers: Headers;
};
export type applyEditProposalResponseError = (applyEditProposalResponseDefault) & {
  headers: Headers;
};

export type applyEditProposalResponse = (applyEditProposalResponseSuccess | applyEditProposalResponseError)

export const getApplyEditProposalUrl = (projectId: string,
    sequenceId: string,
    runId: string,
    turnId: string,
    proposalId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/runs/${runId}/turns/${turnId}/edit/proposals/${proposalId}/apply`
}

/**
 * @summary Atomically apply an exact Edit Proposal
 */
export const applyEditProposal = async (projectId: string,
    sequenceId: string,
    runId: string,
    turnId: string,
    proposalId: string,
    editApplyInput: EditApplyInput, options?: RequestInit): Promise<applyEditProposalResponse> => {

  const res = await fetch(getApplyEditProposalUrl(projectId,sequenceId,runId,turnId,proposalId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(editApplyInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: applyEditProposalResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as applyEditProposalResponse
}


export type undoEditTransactionResponse200 = {
  data: EditCommitResult
  status: 200
}

export type undoEditTransactionResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type undoEditTransactionResponseSuccess = (undoEditTransactionResponse200) & {
  headers: Headers;
};
export type undoEditTransactionResponseError = (undoEditTransactionResponseDefault) & {
  headers: Headers;
};

export type undoEditTransactionResponse = (undoEditTransactionResponseSuccess | undoEditTransactionResponseError)

export const getUndoEditTransactionUrl = (projectId: string,
    sequenceId: string,
    runId: string,
    turnId: string,
    transactionId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/runs/${runId}/turns/${turnId}/edit/transactions/${transactionId}/undo`
}

/**
 * @summary Commit the exact stored inverse of an Edit Transaction
 */
export const undoEditTransaction = async (projectId: string,
    sequenceId: string,
    runId: string,
    turnId: string,
    transactionId: string,
    editUndoInput: EditUndoInput, options?: RequestInit): Promise<undoEditTransactionResponse> => {

  const res = await fetch(getUndoEditTransactionUrl(projectId,sequenceId,runId,turnId,transactionId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(editUndoInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: undoEditTransactionResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as undoEditTransactionResponse
}


export type undoCreatorEditTransactionResponse200 = {
  data: EditCommitResult
  status: 200
}

export type undoCreatorEditTransactionResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type undoCreatorEditTransactionResponseSuccess = (undoCreatorEditTransactionResponse200) & {
  headers: Headers;
};
export type undoCreatorEditTransactionResponseError = (undoCreatorEditTransactionResponseDefault) & {
  headers: Headers;
};

export type undoCreatorEditTransactionResponse = (undoCreatorEditTransactionResponseSuccess | undoCreatorEditTransactionResponseError)

export const getUndoCreatorEditTransactionUrl = (projectId: string,
    sequenceId: string,
    transactionId: string,) => {




  return `/api/v1/projects/${projectId}/sequences/${sequenceId}/transactions/${transactionId}/undo`
}

/**
 * @summary Atomically commit the stored inverse of one transaction as Creator
 */
export const undoCreatorEditTransaction = async (projectId: string,
    sequenceId: string,
    transactionId: string,
    editUndoInput: EditUndoInput, options?: RequestInit): Promise<undoCreatorEditTransactionResponse> => {

  const res = await fetch(getUndoCreatorEditTransactionUrl(projectId,sequenceId,transactionId),
  {
    ...options,
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    body: JSON.stringify(editUndoInput)
  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: undoCreatorEditTransactionResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as undoCreatorEditTransactionResponse
}


export type showSequenceWindowResponse200 = {
  data: SequenceWindowPage
  status: 200
}

export type showSequenceWindowResponseDefault = {
  data: ErrorModel
  status: Exclude<HTTPStatusCodes, 200>
}

export type showSequenceWindowResponseSuccess = (showSequenceWindowResponse200) & {
  headers: Headers;
};
export type showSequenceWindowResponseError = (showSequenceWindowResponseDefault) & {
  headers: Headers;
};

export type showSequenceWindowResponse = (showSequenceWindowResponseSuccess | showSequenceWindowResponseError)

export const getShowSequenceWindowUrl = (projectId: string,
    sequenceId: string,
    params: ShowSequenceWindowParams,) => {
  const normalizedParams = new URLSearchParams();

  Object.entries(params || {}).forEach(([key, value]) => {

    if (value !== undefined) {
      normalizedParams.append(key, value === null ? 'null' : String(value))
    }
  });

  const stringifiedParams = normalizedParams.toString();

  return stringifiedParams.length > 0 ? `/api/v1/projects/${projectId}/sequences/${sequenceId}/window?${stringifiedParams}` : `/api/v1/projects/${projectId}/sequences/${sequenceId}/window`
}

/**
 * @summary Show one bounded Sequence time window
 */
export const showSequenceWindow = async (projectId: string,
    sequenceId: string,
    params: ShowSequenceWindowParams, options?: RequestInit): Promise<showSequenceWindowResponse> => {

  const res = await fetch(getShowSequenceWindowUrl(projectId,sequenceId,params),
  {
    ...options,
    method: 'GET'


  }
)


  const body = [204, 205, 304].includes(res.status) ? null : await res.text();

  const data: showSequenceWindowResponse['data'] = body ? JSON.parse(body) : {}
  return { data, status: res.status, headers: res.headers } as showSequenceWindowResponse
}
