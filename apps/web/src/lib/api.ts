import { getHealth, type Health } from "@open-cut/openapi";

export async function readApiHealth(signal?: AbortSignal): Promise<Health> {
  const response = await getHealth({ signal });
  if (response.status !== 200) throw new Error(`API health returned ${response.status}`);
  return response.data;
}
