
export interface TranscriptDefaultSelection {
  /** @pattern ^(0|[1-9][0-9]*)$ */
  activityCursor: string;
  artifactId: string;
  assetId: string;
  previousArtifactId: string;
  replayed: boolean;
  selectedAt: string;
}
