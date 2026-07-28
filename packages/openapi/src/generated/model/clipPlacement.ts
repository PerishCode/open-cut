import type { ExactRational } from './exactRational';

export interface ClipPlacement {
  /**
     * @minimum 1
     * @maximum 10000
     */
  cropHeightBasisPoints: number;
  /**
     * @minimum 1
     * @maximum 10000
     */
  cropWidthBasisPoints: number;
  /**
     * @minimum 0
     * @maximum 9999
     */
  cropXBasisPoints: number;
  /**
     * @minimum 0
     * @maximum 9999
     */
  cropYBasisPoints: number;
  /**
     * @minimum 0
     * @maximum 10000
     */
  opacityBasisPoints: number;
  scaleX: ExactRational;
  scaleY: ExactRational;
  translateX: ExactRational;
  translateY: ExactRational;
}
