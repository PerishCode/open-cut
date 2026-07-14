export type ProtocolSchema = {
  readonly $ref?: string;
  readonly type?: string;
  readonly format?: string;
  readonly enum?: readonly unknown[];
  readonly anyOf?: readonly ProtocolSchema[];
  readonly oneOf?: readonly ProtocolSchema[];
  readonly allOf?: readonly ProtocolSchema[];
  readonly properties?: Readonly<Record<string, ProtocolSchema>>;
  readonly required?: readonly string[];
  readonly items?: ProtocolSchema;
  readonly minimum?: number;
  readonly maximum?: number;
  readonly minLength?: number;
  readonly maxLength?: number;
  readonly uniqueItems?: boolean;
  readonly additionalProperties?: boolean | ProtocolSchema;
  readonly "x-environment"?: string;
};

export type ProtocolSchemas = Readonly<Record<string, ProtocolSchema>>;

export class ProtocolDecodeError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ProtocolDecodeError";
  }
}

export function decodeProtocolValue<T>(
  name: string,
  schemas: ProtocolSchemas,
  value: unknown,
): T {
  const schema = schemas[name];
  if (!schema) throw new ProtocolDecodeError(`protocol schema ${name} is missing`);
  validate(schema, schemas, value, "$", true);
  return value as T;
}

export function readProtocolEnvironment(
  environment: Readonly<Record<string, string | undefined>>,
  variable: string,
  encoding: "json" | "text",
): unknown {
  const raw = environment[variable];
  if (raw === undefined) throw new ProtocolDecodeError(`${variable} is required for sidecar startup`);
  if (encoding === "text") return raw;
  try {
    return JSON.parse(raw) as unknown;
  } catch (error) {
    throw new ProtocolDecodeError(`${variable} contains invalid JSON: ${errorMessage(error)}`);
  }
}

function validate(
  schema: ProtocolSchema,
  schemas: ProtocolSchemas,
  value: unknown,
  path: string,
  rejectUnknownProperties: boolean,
): void {
  if (schema.$ref) {
    const name = schema.$ref.slice(schema.$ref.lastIndexOf("/") + 1);
    const target = schemas[name];
    if (!target) fail(path, `references missing schema ${name}`);
    validate(target, schemas, value, path, rejectUnknownProperties);
    return;
  }

  if (schema.allOf?.length) {
    for (const member of schema.allOf) validate(member, schemas, value, path, schema.allOf.length === 1 && rejectUnknownProperties);
    return;
  }
  if (schema.oneOf?.length) {
    const matches = schema.oneOf.filter((member) => matchesSchema(member, schemas, value, path));
    if (matches.length !== 1) fail(path, `must match exactly one union member; matched ${matches.length}`);
    return;
  }
  if (schema.anyOf?.length) {
    if (!schema.anyOf.some((member) => matchesSchema(member, schemas, value, path))) {
      fail(path, "does not match any union member");
    }
    return;
  }

  if (schema.enum && !schema.enum.some((candidate) => Object.is(candidate, value))) {
    fail(path, `must be one of ${schema.enum.map(String).join(", ")}`);
  }

  switch (schema.type) {
    case "object":
      validateObject(schema, schemas, value, path, rejectUnknownProperties);
      return;
    case "array":
      validateArray(schema, schemas, value, path);
      return;
    case "string":
      validateString(schema, value, path);
      return;
    case "integer":
      if (typeof value !== "number" || !Number.isSafeInteger(value)) fail(path, "must be a safe integer");
      if (schema.format === "int32" && (value < -2_147_483_648 || value > 2_147_483_647)) fail(path, "must fit signed int32");
      validateNumber(schema, value, path);
      return;
    case "number":
      if (typeof value !== "number" || !Number.isFinite(value)) fail(path, "must be a finite number");
      validateNumber(schema, value, path);
      return;
    case "boolean":
      if (typeof value !== "boolean") fail(path, "must be a boolean");
      return;
    case undefined:
      return;
    default:
      fail(path, `uses unsupported schema type ${schema.type}`);
  }
}

function validateObject(
  schema: ProtocolSchema,
  schemas: ProtocolSchemas,
  value: unknown,
  path: string,
  rejectUnknownProperties: boolean,
): void {
  if (typeof value !== "object" || value === null || Array.isArray(value)) fail(path, "must be an object");
  const object = value as Record<string, unknown>;
  const properties = schema.properties ?? {};
  for (const required of schema.required ?? []) {
    if (!Object.prototype.hasOwnProperty.call(object, required)) fail(`${path}.${required}`, "is required");
  }
  for (const [name, child] of Object.entries(properties)) {
    if (Object.prototype.hasOwnProperty.call(object, name)) validate(child, schemas, object[name], `${path}.${name}`, true);
  }
  for (const [name, child] of Object.entries(object)) {
    if (Object.prototype.hasOwnProperty.call(properties, name)) continue;
    if (schema.additionalProperties && typeof schema.additionalProperties === "object") {
      validate(schema.additionalProperties, schemas, child, `${path}.${name}`, true);
    } else if (schema.additionalProperties !== true && rejectUnknownProperties) {
      fail(`${path}.${name}`, "is not part of the protocol schema");
    }
  }
}

function validateArray(schema: ProtocolSchema, schemas: ProtocolSchemas, value: unknown, path: string): void {
  if (!Array.isArray(value)) fail(path, "must be an array");
  if (!schema.items) fail(path, "has no item schema");
  value.forEach((item, index) => validate(schema.items!, schemas, item, `${path}[${index}]`, true));
  if (schema.uniqueItems) {
    const encoded = value.map((item) => JSON.stringify(item));
    if (new Set(encoded).size !== encoded.length) fail(path, "must contain unique items");
  }
}

function validateString(schema: ProtocolSchema, value: unknown, path: string): void {
  if (typeof value !== "string") fail(path, "must be a string");
  if (schema.minLength !== undefined && value.length < schema.minLength) fail(path, `must contain at least ${schema.minLength} characters`);
  if (schema.maxLength !== undefined && value.length > schema.maxLength) fail(path, `must contain at most ${schema.maxLength} characters`);
  if (schema.format === "date-time" && (!rfc3339Pattern.test(value) || Number.isNaN(Date.parse(value)))) {
    fail(path, "must be an RFC 3339 date-time");
  }
  if (schema.format === "uri") {
    try {
      new URL(value);
    } catch {
      fail(path, "must be an absolute URI");
    }
  }
}

function validateNumber(schema: ProtocolSchema, value: number, path: string): void {
  if (schema.minimum !== undefined && value < schema.minimum) fail(path, `must be at least ${schema.minimum}`);
  if (schema.maximum !== undefined && value > schema.maximum) fail(path, `must be at most ${schema.maximum}`);
}

function matchesSchema(schema: ProtocolSchema, schemas: ProtocolSchemas, value: unknown, path: string): boolean {
  try {
    validate(schema, schemas, value, path, true);
    return true;
  } catch (error) {
    if (error instanceof ProtocolDecodeError) return false;
    throw error;
  }
}

function fail(path: string, message: string): never {
  throw new ProtocolDecodeError(`${path} ${message}`);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

const rfc3339Pattern = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
