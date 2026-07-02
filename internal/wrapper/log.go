package wrapper

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

func NewLogger(path string) (*log.Logger, io.Closer, error) {
	if path == "" {
		return log.New(io.Discard, "", 0), nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, err
	}
	return log.New(file, fmt.Sprintf("[%d] ", os.Getpid()), log.LstdFlags|log.Lmicroseconds), file, nil
}
