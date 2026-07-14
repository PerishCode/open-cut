import { net, protocol } from "electron";

import {
  handleOcWebRequest,
  normalizeWebRuntimeUrl,
  OC_WEB_ENTRY_URL,
  OC_WEB_SCHEME,
} from "./oc-protocol.js";

protocol.registerSchemesAsPrivileged([
  {
    scheme: OC_WEB_SCHEME,
    privileges: {
      corsEnabled: true,
      secure: true,
      standard: true,
      stream: true,
      supportFetchAPI: true,
    },
  },
]);

export type OcWebProtocol = {
  readonly entryUrl: string;
  setWebRuntime(url: string | undefined): void;
  verifyEntry(): Promise<void>;
  close(): void;
};

export function registerOcWebProtocol(): OcWebProtocol {
  let webRuntimeUrl: string | undefined;
  protocol.handle(OC_WEB_SCHEME, (request) => {
    // Keep the proxy hop in Node's HTTP stack. Electron net.fetch() rejects a
    // Request cloned from oc:// with net::ERR_FAILED before it reaches the
    // loopback Web server, while global fetch returns a normal web Response
    // that protocol.handle can stream back under the stable oc:// origin.
    return handleOcWebRequest(request, webRuntimeUrl, (target) => fetch(target));
  });

  return {
    entryUrl: OC_WEB_ENTRY_URL,
    setWebRuntime(url) {
      webRuntimeUrl = url === undefined ? undefined : normalizeWebRuntimeUrl(url);
    },
    async verifyEntry() {
      const response = await net.fetch(OC_WEB_ENTRY_URL, { cache: "no-store" });
      if (!response.ok) {
        throw new Error(`oc:// Web entry returned HTTP ${response.status}`);
      }
      const contentType = response.headers.get("content-type") ?? "";
      if (!contentType.toLowerCase().includes("text/html")) {
        throw new Error(`oc:// Web entry returned unexpected content type ${JSON.stringify(contentType)}`);
      }
      await response.arrayBuffer();
    },
    close() {
      webRuntimeUrl = undefined;
      if (protocol.isProtocolHandled(OC_WEB_SCHEME)) protocol.unhandle(OC_WEB_SCHEME);
    },
  };
}
