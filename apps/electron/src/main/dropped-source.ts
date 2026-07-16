import { randomBytes } from "node:crypto";
import path from "node:path";

const defaultLifetimeMs = 60_000;
const maximumLiveTokens = 32;
const tokenPattern = /^drop\.[A-Za-z0-9_-]{32}$/;

type DroppedSource = Readonly<{
  owner: number;
  path: string;
  expiresAt: number;
}>;

export class DroppedSourceStager {
  readonly #entries = new Map<string, DroppedSource>();
  readonly #clock: () => number;
  readonly #token: () => string;
  readonly #lifetimeMs: number;

  constructor(options: { clock?: () => number; token?: () => string; lifetimeMs?: number } = {}) {
    this.#clock = options.clock ?? Date.now;
    this.#token = options.token ?? (() => `drop.${randomBytes(24).toString("base64url")}`);
    this.#lifetimeMs = options.lifetimeMs ?? defaultLifetimeMs;
    if (!Number.isSafeInteger(this.#lifetimeMs) || this.#lifetimeMs < 1 || this.#lifetimeMs > defaultLifetimeMs) {
      throw new Error("Dropped source lifetime is invalid");
    }
  }

  stage(owner: number, sourcePath: string): string {
    this.#prune();
    if (
      !Number.isSafeInteger(owner) ||
      owner < 1 ||
      typeof sourcePath !== "string" ||
      sourcePath.length === 0 ||
      sourcePath.length > 32_768 ||
      sourcePath.includes("\0") ||
      !path.isAbsolute(sourcePath)
    ) {
      throw new Error("Dropped source is invalid");
    }
    if (this.#entries.size >= maximumLiveTokens) throw new Error("Too many dropped sources are staged");
    const token = this.#token();
    if (!tokenPattern.test(token) || this.#entries.has(token)) throw new Error("Dropped source token is invalid");
    this.#entries.set(token, {
      owner,
      path: path.normalize(sourcePath),
      expiresAt: this.#clock() + this.#lifetimeMs,
    });
    return token;
  }

  consume(owner: number, token: string): string | undefined {
    this.#prune();
    if (!Number.isSafeInteger(owner) || owner < 1 || !tokenPattern.test(token)) return undefined;
    const entry = this.#entries.get(token);
    if (!entry || entry.owner !== owner) return undefined;
    this.#entries.delete(token);
    return entry.path;
  }

  clear(): void {
    this.#entries.clear();
  }

  #prune(): void {
    const now = this.#clock();
    for (const [token, entry] of this.#entries) {
      if (entry.expiresAt <= now) this.#entries.delete(token);
    }
  }
}

export function isDroppedSourceToken(value: string): boolean {
  return tokenPattern.test(value);
}
