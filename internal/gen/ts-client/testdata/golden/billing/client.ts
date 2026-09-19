// AUTO-GENERATED — DO NOT EDIT

import type { ClientTransport, StreamCall } from "@qomos/spore-ts/client";
import type { Invoice, TailInvoicesReq } from "./types.js";

export class BillingClient {
  constructor(private readonly transport: ClientTransport) {}

  next_invoice_id(): Promise<string> {
    return this.transport.unary<void, string>({
      namespace: "billing",
      name: "next_invoice_id",
      reqSchemaId: 200,
      finalSchemaId: 201,
    }, undefined).final();
  }

  tail_invoices(req: TailInvoicesReq): StreamCall<Invoice, void> {
    return this.transport.stream<TailInvoicesReq, Invoice, void>({
      namespace: "billing",
      name: "tail_invoices",
      reqSchemaId: 100,
      chunkSchemaId: 101,
      finalSchemaId: 102,
    }, req);
  }
}
