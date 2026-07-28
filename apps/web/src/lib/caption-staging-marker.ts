/**
 * Session-local marker for in-progress caption staging, keyed per project.
 *
 * Creator staging state dies with the workspace on a project switch; this
 * marker lets the reopened workspace say so honestly instead of presenting a
 * silently reset draft chooser. Best-effort presentation state only — never
 * product data.
 */

const captionStagingMarkerKey = (projectId: string) => `open-cut:caption-staging:${projectId}`;

export function writeCaptionStagingMarker(projectId: string, staged: boolean): void {
  try {
    if (staged) window.sessionStorage.setItem(captionStagingMarkerKey(projectId), "1");
    else window.sessionStorage.removeItem(captionStagingMarkerKey(projectId));
  } catch {
    // Marker is best-effort presentation state only.
  }
}

export function readAndClearCaptionStagingMarker(projectId: string): boolean {
  try {
    const key = captionStagingMarkerKey(projectId);
    const present = window.sessionStorage.getItem(key) !== null;
    window.sessionStorage.removeItem(key);
    return present;
  } catch {
    return false;
  }
}
