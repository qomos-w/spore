// AUTO-GENERATED — DO NOT EDIT

import { CallableRegistry, type CallableEntry } from "@qomos/spore-ts/callables";

export const callableEntries: CallableEntry[] = [
  {
    namespace: "auth",
    name: "lookup_user",
    visibility: "public",
    mode: "unary",
    reqSchemaId: 4,
    finalSchemaId: 5,
    req: {
      kind: "struct",
      name: "LookupUserReq",
      className: "LookupUserReq"
    },
    final: {
      kind: "struct",
      name: "LookupUserResp",
      className: "LookupUserResp"
    }
  },
  {
    namespace: "auth",
    name: "tail_logins",
    visibility: "public",
    mode: "streaming",
    reqSchemaId: 1,
    chunkSchemaId: 2,
    finalSchemaId: 3,
    req: {
      kind: "struct",
      name: "TailLoginsReq",
      className: "TailLoginsReq"
    },
    chunk: {
      kind: "struct",
      name: "LoginEvent",
      className: "LoginEvent"
    },
    final: {
      kind: "struct",
      name: "TailLoginsFinal",
      className: "TailLoginsFinal"
    }
  },
];

export function buildCallableRegistry(): CallableRegistry {
  const registry = new CallableRegistry();
  for (const entry of callableEntries) {
    registry.register(entry);
  }
  return registry;
}
