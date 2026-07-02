package jsonrpc

import (
	"bytes"
	"testing"
)

func TestReadWriteFrame(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter(&buf)
	want := Message{
		JSONRPC: "2.0",
		ID:      []byte(`1`),
		Method:  "tools/list",
	}

	if err := writer.Write(want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := NewReader(&buf).Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.JSONRPC != want.JSONRPC || got.Method != want.Method || string(got.ID) != string(want.ID) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestReadWriteJSONL(t *testing.T) {
	var buf bytes.Buffer
	writer := NewJSONLWriter(&buf)
	want := Message{
		JSONRPC: "2.0",
		ID:      []byte(`2`),
		Method:  "tools/call",
	}

	if err := writer.Write(want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Fatalf("JSONL frame must end with newline: %q", buf.String())
	}

	got, err := NewJSONLReader(&buf).Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.JSONRPC != want.JSONRPC || got.Method != want.Method || string(got.ID) != string(want.ID) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
