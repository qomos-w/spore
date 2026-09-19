// AUTO-GENERATED — DO NOT EDIT

package testpkg

import "fmt"

// AuthHandler is the typed handler shape that DispatchAuth routes calls into.
// Each method matches one callable in the "auth" namespace; user code
// implements this interface and the boundary marshal between
// map[string]any wire payloads and the typed Go structs is generated
// by spore-gen-go-server.
type AuthHandler interface {
	Login(req LoginReq) (LoginResp, error)
	LookupUser(req LookupUserReq) (LookupUserResp, error)
}

// DispatchAuth routes a wire-form callID and payload to the matching
// AuthHandler method, marshaling the request payload into the typed
// request struct and converting the typed response back into a
// map[string]any wire payload.
func DispatchAuth(h AuthHandler, callID string, payload map[string]any) (map[string]any, error) {
	switch callID {
	case "auth.login":
		req := LoginReq{
			User: payload["user"].(string),
		}
		resp, err := h.Login(req)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"ok":    resp.Ok,
			"token": resp.Token,
		}, nil
	case "auth.lookup_user":
		req := LookupUserReq{
			UserId: payload["userId"].(string),
		}
		resp, err := h.LookupUser(req)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"name": resp.Name,
			"age":  resp.Age,
		}, nil
	default:
		return nil, fmt.Errorf("unknown callID: %s", callID)
	}
}
