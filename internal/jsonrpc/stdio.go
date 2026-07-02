package jsonrpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

type Framing string

const (
	FramingHeader Framing = "header"
	FramingJSONL  Framing = "jsonl"
)

func NormalizeFraming(value string) (Framing, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(FramingHeader), "content-length", "mcp":
		return FramingHeader, nil
	case string(FramingJSONL), "ndjson", "line":
		return FramingJSONL, nil
	default:
		return "", fmt.Errorf("unsupported framing: %s", value)
	}
}

type Reader struct {
	r     *bufio.Reader
	jsonl bool
	seen  bool
}

func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReader(r)}
}

func NewJSONLReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReader(r), jsonl: true, seen: true}
}

func NewReaderWithFraming(r io.Reader, framing Framing) *Reader {
	if framing == FramingJSONL {
		return NewJSONLReader(r)
	}
	return NewReader(r)
}

func (r *Reader) Read() (Message, error) {
	if r.jsonl {
		return r.readJSONL()
	}
	return r.readAuto()
}

func (r *Reader) readAuto() (Message, error) {
	for {
		line, err := r.r.ReadBytes('\n')
		if err != nil {
			return Message{}, err
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if bytes.HasPrefix(trimmed, []byte("{")) {
			r.jsonl = true
			r.seen = true
			var msg Message
			if err := json.Unmarshal(trimmed, &msg); err != nil {
				return Message{}, err
			}
			return msg, nil
		}
		r.seen = true
		return r.readHeaderAfterFirstLine(string(line))
	}
}

func (r *Reader) readJSONL() (Message, error) {
	for {
		line, err := r.r.ReadBytes('\n')
		if err != nil {
			return Message{}, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			return Message{}, err
		}
		return msg, nil
	}
}

func (r *Reader) readHeader() (Message, error) {
	return r.readHeaderAfterFirstLine("")
}

func (r *Reader) readHeaderAfterFirstLine(firstLine string) (Message, error) {
	contentLength := -1

	if firstLine != "" {
		if err := parseHeaderLine(firstLine, &contentLength); err != nil {
			return Message{}, err
		}
	}

	for {
		line, err := r.r.ReadString('\n')
		if err != nil {
			return Message{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if err := parseHeaderLine(line, &contentLength); err != nil {
			return Message{}, err
		}
	}

	if contentLength < 0 {
		return Message{}, fmt.Errorf("missing Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r.r, body); err != nil {
		return Message{}, err
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func parseHeaderLine(line string, contentLength *int) error {
	line = strings.TrimRight(line, "\r\n")
	name, value, ok := strings.Cut(line, ":")
	if !ok {
		return fmt.Errorf("invalid header line: %q", line)
	}
	if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid content length: %w", err)
		}
		*contentLength = n
	}
	return nil
}

func (r *Reader) Framing() Framing {
	if r.jsonl {
		return FramingJSONL
	}
	return FramingHeader
}

func (r *Reader) SeenFraming() bool {
	return r.seen
}

type Writer struct {
	w     io.Writer
	mu    sync.Mutex
	jsonl bool
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

func NewJSONLWriter(w io.Writer) *Writer {
	return &Writer{w: w, jsonl: true}
}

func NewWriterWithFraming(w io.Writer, framing Framing) *Writer {
	if framing == FramingJSONL {
		return NewJSONLWriter(w)
	}
	return NewWriter(w)
}

func (w *Writer) Write(msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return w.WriteRaw(body)
}

func (w *Writer) WriteRaw(body []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.jsonl {
		_, err := w.w.Write(append(body, '\n'))
		return err
	}

	var frame bytes.Buffer
	_, _ = fmt.Fprintf(&frame, "Content-Length: %d\r\n\r\n", len(body))
	frame.Write(body)
	_, err := w.w.Write(frame.Bytes())
	return err
}
