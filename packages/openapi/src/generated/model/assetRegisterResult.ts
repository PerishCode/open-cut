import type { AssetDetail } from './assetDetail';
import type { EditTransaction } from './editTransaction';

export interface AssetRegisterResult {
  activityCursor: string;
  asset: AssetDetail;
  replayed: boolean;
  transaction: EditTransaction;
}
