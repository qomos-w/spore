// AUTO-GENERATED — DO NOT EDIT

import type { ClientTransport, StreamCall } from "@qomos/spore-ts/client";
import type { LoginEvent, LookupUserReq, LookupUserResp, TailLoginsFinal, TailLoginsReq } from "./types.js";

export class AuthClient {
  constructor(private readonly transport: ClientTransport) {}

  lookup_user(req: LookupUserReq): Promise<LookupUserResp> {
    return this.transport.unary<LookupUserReq, LookupUserResp>({
      namespace: "auth",
      name: "lookup_user",
      reqSchemaId: 4,
      finalSchemaId: 5,
    }, req).final();
  }

  tail_logins(req: TailLoginsReq): StreamCall<LoginEvent, TailLoginsFinal> {
    return this.transport.stream<TailLoginsReq, LoginEvent, TailLoginsFinal>({
      namespace: "auth",
      name: "tail_logins",
      reqSchemaId: 1,
      chunkSchemaId: 2,
      finalSchemaId: 3,
    }, req);
  }
}
