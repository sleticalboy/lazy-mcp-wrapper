package jsonrpc

import "encoding/json"

type Message struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (m Message) IsRequest() bool {
	return len(m.ID) > 0 && m.Method != ""
}

func (m Message) IsNotification() bool {
	return len(m.ID) == 0 && m.Method != ""
}

func Response(id json.RawMessage, result any) Message {
	data, _ := json.Marshal(result)
	return Message{JSONRPC: "2.0", ID: id, Result: data}
}

func ErrorResponse(id json.RawMessage, code int, message string) Message {
	return Message{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}}
}
