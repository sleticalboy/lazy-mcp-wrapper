//go:build !windows

package main

import "github.com/binlee/lazy-mcp-wrapper/internal/daemon"

func runWindowsServiceIfNeeded(socketPath string, server *daemon.Server) (bool, error) {
	return false, nil
}
