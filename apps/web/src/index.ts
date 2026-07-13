import { startWebServer } from "./server.js";

const server = await startWebServer();
console.log(server.url);
