//go:build windows

package wrapper

func (c *realClient) signalStop() error {
	return c.cmd.Process.Kill()
}
