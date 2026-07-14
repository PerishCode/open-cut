import "@typespec/http";
import "@typespec/openapi";
import "@typespec/openapi3";
import "@typespec/json-schema";

using TypeSpec.Http;
using TypeSpec.JsonSchema;
using TypeSpec.OpenAPI;

@service(#{ title: "Open Cut sidecar control plane" })
@info(#{ version: "1.0.0" })
@server("http://127.0.0.1:{port}", "Per-cell loopback broker", { port: string = "0" })
@useAuth(BearerAuth)
namespace OpenCut.Sidecar.V1;

enum Capability {
  observe: "observe",
  runtimeReady: "runtime-ready",
  lifecycle: "lifecycle",
  updateTransition: "update-transition",
  delegateSidecar: "delegate-sidecar",
}

enum Role {
  owner: "owner",
  runtime: "runtime",
  sidecar: "sidecar",
  observer: "observer",
}

enum ControlCommand {
  show: "show",
  shutdown: "shutdown",
}

enum UpdateAction {
  prepareLatest: "prepare-latest",
}

enum UpdateStatus {
  current: "current",
  prepared: "prepared",
}

enum LifecycleMode {
  production: "production",
  packaged: "packaged",
  development: "dev",
  harness: "harness",
}

enum Presentation {
  interactive: "interactive",
  headless: "headless",
}

model ControlDescriptor {
  schema: 1;
  protocol: "sidecar.v1";
  @minLength(1) address: string;
  pid: int32;
  @minLength(1) sessionId: string;
  @minValue(0) generation: safeint;
  startedAt: utcDateTime;
}

model SidecarLaunch {
  @TypeSpec.OpenAPI.extension("x-environment", "OC_SIDECAR_APP")
  @minLength(1) app: string;

  @TypeSpec.OpenAPI.extension("x-environment", "OC_SIDECAR_CONTROL")
  control: ControlDescriptor;

  @TypeSpec.OpenAPI.extension("x-environment", "OC_SIDECAR_TOKEN")
  @minLength(1) token: string;

  @TypeSpec.OpenAPI.extension("x-environment", "OC_SIDECAR_CHANNEL")
  @minLength(1) channel: string;

  @TypeSpec.OpenAPI.extension("x-environment", "OC_SIDECAR_NAMESPACE")
  @minLength(1) `namespace`: string;

  @TypeSpec.OpenAPI.extension("x-environment", "OC_SIDECAR_DATA_DIR")
  @minLength(1) dataDir: string;

  @TypeSpec.OpenAPI.extension("x-environment", "OC_SIDECAR_MODE")
  mode: LifecycleMode;

  @TypeSpec.OpenAPI.extension("x-environment", "OC_SIDECAR_PRESENTATION")
  presentation: Presentation;

  @TypeSpec.OpenAPI.extension("x-environment", "OC_SIDECAR_SOURCE")
  @minLength(1) source: string;
}

model Health {
  schema: 1;
  protocol: "sidecar.v1";
  @minLength(1) sessionId: string;
  @minValue(0) generation: safeint;
}

model Endpoint {
  @minLength(1) name: string;
  @format("uri") url: string;
}

model SessionStatus {
  @minLength(1) subject: string;
  @minLength(1) app: string;
  @minLength(1) instanceId: string;
  mode: LifecycleMode;
  @minLength(1) source: string;
  ready: boolean;
  connectedAt: utcDateTime;
  lastHeartbeat: utcDateTime;
  endpoints?: Endpoint[];
}

model Status {
  schema: 1;
  @minValue(0) revision: safeint;
  @minLength(1) channel: string;
  @minLength(1) `namespace`: string;
  @minLength(1) sessionId: string;
  @minValue(0) generation: safeint;
  sessions: SessionStatus[];
}

model ControlRequest {
  command: ControlCommand;
}

model ControlResponse {
  @minValue(0) accepted: int32;
}

model DelegateRequest {
  @minLength(1) @maxLength(64) subject: string;
  @minValue(1) @maxValue(3600) ttlSeconds?: int64;
  @uniqueItems capabilities?: Capability[];
}

model DelegateResponse {
  subject: string;
  token: string;
  expiresAt: utcDateTime;
}

model RenewRequest {
  @minValue(1) @maxValue(604800) ttlSeconds?: int64;
}

model RenewResponse {
  subject: string;
  token: string;
  expiresAt: utcDateTime;
}

model UpdateTransitionRequest {
  action: UpdateAction;
}

model UpdateTransitionResponse {
  status: UpdateStatus;
  version: string;
  restartRequired: boolean;
}

model ErrorResponse {
  error: string;
}

model RegisterEvent {
  type: "register";
  @minLength(1) channel: string;
  @minLength(1) `namespace`: string;
  @minLength(1) sessionId: string;
  @minValue(0) generation: safeint;
  @minLength(1) app: string;
  @minLength(1) instanceId: string;
  mode: LifecycleMode;
  @minLength(1) source: string;
}

model HeartbeatEvent { type: "heartbeat"; }

model EndpointEvent {
  type: "endpoint";
  @minLength(1) name: string;
  @format("uri") url: string;
}

model StateEvent {
  type: "state";
  ready: boolean;
}

model ExitingEvent {
  type: "exiting";
  code: int32;
}

model RegisteredEvent { type: "registered"; }

model CommandEvent {
  type: "command";
  command: ControlCommand;
}

model StatusEvent {
  type: "status";
  status: Status;
}

union ClientEvent {
  register: RegisterEvent,
  heartbeat: HeartbeatEvent,
  endpoint: EndpointEvent,
  state: StateEvent,
  exiting: ExitingEvent,
}

union ServerEvent {
  registered: RegisteredEvent,
  command: CommandEvent,
  status: StatusEvent,
}

@jsonSchema
@TypeSpec.JsonSchema.oneOf
union SidecarEvent {
  client: ClientEvent,
  server: ServerEvent,
}

model BodyResponse<Code extends int32, Body> {
  @statusCode statusCode: Code;
  @body body: Body;
}

model EmptyResponse<Code extends int32> {
  @statusCode statusCode: Code;
}

@route("/v1/health")
@get
@useAuth(NoAuth)
@operationId("health")
@doc("Return broker identity without secret material.")
op health(): BodyResponse<200, Health>;

@route("/v1/status")
@get
@operationId("status")
@doc("Return the current revisioned cell and session status.")
op status(): BodyResponse<200, Status> | BodyResponse<401, ErrorResponse>;

@route("/v1/sessions/register")
@get
@TypeSpec.OpenAPI.extension("x-transport", "websocket")
@operationId("registerSession")
@doc("Upgrade to WebSocket; the first frame must be a register event.")
op registerSession(): EmptyResponse<101> | BodyResponse<401, ErrorResponse>;

@route("/v1/control")
@post
@operationId("broadcastControl")
@doc("Broadcast a lifecycle command to registered sessions.")
op broadcastControl(@body request: ControlRequest):
  | BodyResponse<202, ControlResponse>
  | BodyResponse<400, ErrorResponse>
  | BodyResponse<401, ErrorResponse>;

@route("/v1/capabilities/sidecar")
@post
@operationId("delegateSidecarCapability")
@doc("Mint one short-lived app-subject-bound child capability; runtime role only.")
op delegateSidecarCapability(@body request: DelegateRequest):
  | BodyResponse<201, DelegateResponse>
  | BodyResponse<400, ErrorResponse>
  | BodyResponse<401, ErrorResponse>
  | BodyResponse<403, ErrorResponse>
  | BodyResponse<500, ErrorResponse>;

@route("/v1/capabilities/renew")
@post
@operationId("renewCapability")
@doc("Renew an active runtime or sidecar capability without changing its authority.")
op renewCapability(@body request: RenewRequest):
  | BodyResponse<201, RenewResponse>
  | BodyResponse<400, ErrorResponse>
  | BodyResponse<401, ErrorResponse>
  | BodyResponse<403, ErrorResponse>
  | BodyResponse<409, ErrorResponse>
  | BodyResponse<500, ErrorResponse>;

@route("/v1/update/transition")
@post
@operationId("prepareLatestUpdate")
@doc("Ask B0 to verify, install, and prepare the latest release.")
op prepareLatestUpdate(@body request: UpdateTransitionRequest):
  | BodyResponse<200, UpdateTransitionResponse>
  | BodyResponse<400, ErrorResponse>
  | BodyResponse<401, ErrorResponse>
  | BodyResponse<403, ErrorResponse>
  | BodyResponse<409, ErrorResponse>;
