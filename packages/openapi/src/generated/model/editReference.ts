
export interface EditReference {
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  id?: string;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  local?: string;
}
