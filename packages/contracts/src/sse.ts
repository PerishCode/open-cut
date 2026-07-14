export type ServerEvent = {
  event: string;
  id?: string;
  retry?: number;
  data: unknown;
};

export async function* readServerEvents(response: Response): AsyncGenerator<ServerEvent> {
  if (!response.ok) throw new Error(`event stream returned ${response.status}`);
  if (!response.body) throw new Error("event stream has no response body");

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    while (true) {
      const { done, value } = await reader.read();
      buffer += decoder.decode(value, { stream: !done }).replaceAll("\r\n", "\n");
      let boundary = buffer.indexOf("\n\n");
      while (boundary >= 0) {
        const frame = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        const parsed = parseFrame(frame);
        if (parsed) yield parsed;
        boundary = buffer.indexOf("\n\n");
      }
      if (done) break;
    }
  } finally {
    reader.releaseLock();
  }
}

function parseFrame(frame: string): ServerEvent | undefined {
  let event = "message";
  let id: string | undefined;
  let retry: number | undefined;
  const data: string[] = [];
  for (const line of frame.split("\n")) {
    if (line === "" || line.startsWith(":")) continue;
    const separator = line.indexOf(":");
    const field = separator < 0 ? line : line.slice(0, separator);
    const value = separator < 0 ? "" : line.slice(separator + 1).replace(/^ /, "");
    if (field === "event") event = value;
    else if (field === "id") id = value;
    else if (field === "retry" && /^\d+$/.test(value)) retry = Number(value);
    else if (field === "data") data.push(value);
  }
  if (data.length === 0) return undefined;
  return { event, id, retry, data: JSON.parse(data.join("\n")) };
}
