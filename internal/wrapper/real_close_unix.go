//go:build !windows

package wrapper

import "os"

func (c *realClient) signalStop() error {
	return c.cmd.Process.Signal(os.Interrupt)
}
