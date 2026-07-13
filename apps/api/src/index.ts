import { startApiServer } from "./server.js";

const server = await startApiServer();
console.log(server.url);
