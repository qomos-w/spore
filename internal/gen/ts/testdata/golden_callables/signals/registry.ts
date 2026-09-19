// AUTO-GENERATED — DO NOT EDIT

import { SchemaRegistry, type SchemaEntry } from "@qomos/spore-ts/registry";

export const SchemaIDs = {
  PingReq: 100,
} as const;

export const schemaEntries: SchemaEntry[] = [
  {
    namespace: "signals",
    schemaId: 100,
    name: "PingReq",
    visibility: "admin",
    type: {
      kind: "struct",
      name: "PingReq",
      className: "PingReq",
      classId: 100
    },
    object: {
      kind: "struct",
      name: "PingReq",
      fields: [
        {
          name: "Limit",
          type: {
            kind: "scalar",
            name: "int"
          }
        }
      ]
    }
  },
];

export function buildSchemaRegistry(): SchemaRegistry {
  const registry = new SchemaRegistry();
  for (const entry of schemaEntries) {
    registry.register(entry);
  }
  return registry;
}
