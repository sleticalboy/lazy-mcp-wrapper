package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
)

type BindResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func RunClient(socketPath, name string, stdin io.Reader, stdout io.Writer) error {
	if socketPath == "" {
		return fmt.Errorf("socket path is required")
	}
	if name == "" {
		return fmt.Errorf("MCP name is required")
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	bind, _ := json.Marshal(BindRequest{Name: name})
	if _, err := conn.Write(append(bind, '\n')); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp BindResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "bind failed"
		}
		return errors.New(resp.Error)
	}

	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn, stdin)
		_ = closeWrite(conn)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(stdout, reader)
		errc <- err
	}()

	for i := 0; i < 2; i++ {
		err = <-errc
		if err != nil && err != io.EOF {
			return err
		}
	}
	return nil
}

func closeWrite(conn net.Conn) error {
	type closeWriter interface {
		CloseWrite() error
	}
	if cw, ok := conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return conn.Close()
}
