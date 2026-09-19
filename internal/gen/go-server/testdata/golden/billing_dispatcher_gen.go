// AUTO-GENERATED — DO NOT EDIT

package testpkg

import "fmt"

// BillingHandler is the typed handler shape that DispatchBilling routes calls into.
// Each method matches one callable in the "billing" namespace; user code
// implements this interface and the boundary marshal between
// map[string]any wire payloads and the typed Go structs is generated
// by spore-gen-go-server.
type BillingHandler interface {
	Charge(req ChargeReq) (ChargeResp, error)
}

// DispatchBilling routes a wire-form callID and payload to the matching
// BillingHandler method, marshaling the request payload into the typed
// request struct and converting the typed response back into a
// map[string]any wire payload.
func DispatchBilling(h BillingHandler, callID string, payload map[string]any) (map[string]any, error) {
	switch callID {
	case "billing.charge":
		req := ChargeReq{
			Amount: payload["amount"].(int64),
		}
		resp, err := h.Charge(req)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"invoiceId": resp.InvoiceId,
		}, nil
	default:
		return nil, fmt.Errorf("unknown callID: %s", callID)
	}
}
