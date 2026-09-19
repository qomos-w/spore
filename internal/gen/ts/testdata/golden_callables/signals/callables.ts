// AUTO-GENERATED — DO NOT EDIT

import { CallableRegistry, type CallableEntry } from "@qomos/spore-ts/callables";

export const callableEntries: CallableEntry[] = [
  {
    namespace: "signals",
    name: "tail_pings",
    visibility: "public",
    mode: "streaming",
    reqSchemaId: 100,
    chunkSchemaId: 101,
    finalSchemaId: 102,
    req: {
      kind: "struct",
      name: "PingReq",
      className: "PingReq"
    },
    chunk: {
      kind: "struct",
      name: "Ping",
      className: "Ping"
    },
    final: {
      kind: "void"
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
