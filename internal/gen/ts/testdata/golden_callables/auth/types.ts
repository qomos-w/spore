// AUTO-GENERATED — DO NOT EDIT

export interface LoginEvent {
  User: string;
  At: string;
}

export interface LookupUserReq {
  ID: string;
}

export interface LookupUserResp {
  User: User;
}

export interface TailLoginsFinal {
  Total: number;
}

export interface TailLoginsReq {
  Limit: number;
}

