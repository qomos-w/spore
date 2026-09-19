// AUTO-GENERATED — DO NOT EDIT

import { SchemaRegistry, type SchemaEntry } from "@qomos/spore-ts/registry";

export const SchemaIDs = {
  TailLoginsReq: 1,
  LoginEvent: 2,
  TailLoginsFinal: 3,
  LookupUserReq: 4,
  LookupUserResp: 5,
} as const;

export const schemaEntries: SchemaEntry[] = [
  {
    namespace: "auth",
    schemaId: 1,
    name: "TailLoginsReq",
    visibility: "public",
    type: {
      kind: "struct",
      name: "TailLoginsReq",
      className: "TailLoginsReq",
      classId: 1
    },
    object: {
      kind: "struct",
      name: "TailLoginsReq",
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
  {
    namespace: "auth",
    schemaId: 2,
    name: "LoginEvent",
    visibility: "public",
    type: {
      kind: "struct",
      name: "LoginEvent",
      className: "LoginEvent",
      classId: 2
    },
    object: {
      kind: "struct",
      name: "LoginEvent",
      fields: [
        {
          name: "User",
          type: {
            kind: "scalar",
            name: "string"
          }
        },
        {
          name: "At",
          type: {
            kind: "scalar",
            name: "string"
          }
        }
      ]
    }
  },
  {
    namespace: "auth",
    schemaId: 3,
    name: "TailLoginsFinal",
    visibility: "public",
    type: {
      kind: "struct",
      name: "TailLoginsFinal",
      className: "TailLoginsFinal",
      classId: 3
    },
    object: {
      kind: "struct",
      name: "TailLoginsFinal",
      fields: [
        {
          name: "Total",
          type: {
            kind: "scalar",
            name: "int"
          }
        }
      ]
    }
  },
  {
    namespace: "auth",
    schemaId: 4,
    name: "LookupUserReq",
    visibility: "public",
    type: {
      kind: "struct",
      name: "LookupUserReq",
      className: "LookupUserReq",
      classId: 4
    },
    object: {
      kind: "struct",
      name: "LookupUserReq",
      fields: [
        {
          name: "ID",
          type: {
            kind: "scalar",
            name: "string"
          }
        }
      ]
    }
  },
  {
    namespace: "auth",
    schemaId: 5,
    name: "LookupUserResp",
    visibility: "public",
    type: {
      kind: "struct",
      name: "LookupUserResp",
      className: "LookupUserResp",
      classId: 5
    },
    object: {
      kind: "struct",
      name: "LookupUserResp",
      fields: [
        {
          name: "User",
          type: {
            kind: "struct",
            name: "User",
            className: "User"
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
