
export interface ProjectWrite {
  /**
     * Project description
     * @maxLength 2000
     */
  description: string;
  /**
     * Human-readable project name
     * @minLength 1
     * @maxLength 200
     */
  name: string;
}
