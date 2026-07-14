
export interface Project {
  /**
     * Project description
     * @maxLength 2000
     */
  description: string;
  /**
     * Stable project identifier
     * @minLength 1
     * @maxLength 128
     */
  id: string;
  /**
     * Human-readable project name
     * @minLength 1
     * @maxLength 200
     */
  name: string;
}
