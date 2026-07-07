package a2a

import "encoding/json"

// decodeRPCRequest parses raw JSON-RPC request bytes and reports whether the
// three mandatory JSON-RPC 2.0 envelope fields (jsonrpc, method, id) are
// genuinely present in the wire bytes — not just non-zero after unmarshal,
// which cannot distinguish an absent field from an explicit empty one. This
// is what lets handleDispatch reject a genuinely malformed request (spec
// §1.5 "message/send" etc. all ride this envelope) with a real JSON-RPC
// "Invalid Request" error rather than silently defaulting.
func decodeRPCRequest(raw []byte) (req RPCRequest, hasJSONRPC, hasMethod, hasID bool, parseErr error) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return RPCRequest{}, false, false, false, err
	}
	_, hasJSONRPC = generic["jsonrpc"]
	_, hasMethod = generic["method"]
	_, hasID = generic["id"]

	if err := json.Unmarshal(raw, &req); err != nil {
		return RPCRequest{}, hasJSONRPC, hasMethod, hasID, err
	}
	return req, hasJSONRPC, hasMethod, hasID, nil
}
