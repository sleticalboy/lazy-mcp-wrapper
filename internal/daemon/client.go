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

func QueryStatus(socketPath string) (Status, error) {
	if socketPath == "" {
		return Status{}, fmt.Errorf("socket path is required")
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return Status{}, err
	}
	defer conn.Close()

	bind, _ := json.Marshal(BindRequest{Control: "status"})
	if _, err := conn.Write(append(bind, '\n')); err != nil {
		return Status{}, err
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return Status{}, err
	}
	var bindResp BindResponse
	if err := json.Unmarshal(line, &bindResp); err == nil && !bindResp.OK && bindResp.Error != "" {
		return Status{}, errors.New(bindResp.Error)
	}
	var status Status
	if err := json.Unmarshal(line, &status); err != nil {
		return Status{}, err
	}
	if status.SocketPath == "" && status.Servers == nil {
		return Status{}, fmt.Errorf("invalid status response")
	}
	return status, nil
}

type ControlOptions struct {
	Force    bool
	Graceful bool
}

func SendControl(socketPath, control string, opts ...ControlOptions) (ControlResponse, error) {
	if socketPath == "" {
		return ControlResponse{}, fmt.Errorf("socket path is required")
	}
	if control == "" {
		return ControlResponse{}, fmt.Errorf("control is required")
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return ControlResponse{}, err
	}
	defer conn.Close()

	var options ControlOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	bind, _ := json.Marshal(BindRequest{Control: control, Force: options.Force, Graceful: options.Graceful})
	if _, err := conn.Write(append(bind, '\n')); err != nil {
		return ControlResponse{}, err
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return ControlResponse{}, err
	}
	var resp ControlResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return ControlResponse{}, err
	}
	if !resp.OK && resp.Error == "" {
		resp.Error = "control failed"
	}
	return resp, nil
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
