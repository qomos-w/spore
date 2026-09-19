// AUTO-GENERATED — DO NOT EDIT

import { SchemaRegistry, type SchemaEntry } from "@qomos/spore-ts/registry";

export const SchemaIDs = {
  User: 1,
  Room: 2,
} as const;

export const schemaEntries: SchemaEntry[] = [
  {
    namespace: "demo",
    schemaId: 1,
    name: "User",
    visibility: "public",
    type: {
      kind: "struct",
      name: "User",
      className: "User",
      classId: 1
    },
    object: {
      kind: "struct",
      name: "User",
      fields: [
        {
          name: "ID",
          type: {
            kind: "scalar",
            name: "string"
          }
        },
        {
          name: "Age",
          type: {
            kind: "scalar",
            name: "int"
          }
        }
      ]
    }
  },
  {
    namespace: "demo",
    schemaId: 2,
    name: "Room",
    visibility: "public",
    type: {
      kind: "struct",
      name: "Room",
      className: "Room",
      classId: 2
    },
    object: {
      kind: "struct",
      name: "Room",
      fields: [
        {
          name: "Name",
          type: {
            kind: "scalar",
            name: "string"
          }
        },
        {
          name: "Members",
          type: {
            kind: "array",
            element: {
              kind: "struct",
              name: "User",
              className: "User"
            }
          }
        },
        {
          name: "Roles",
          type: {
            kind: "map",
            key: {
              kind: "scalar",
              name: "string"
            },
            value: {
              kind: "scalar",
              name: "string"
            }
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
