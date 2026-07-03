//go:build windows

package main

import (
	"context"

	"github.com/binlee/lazy-mcp-wrapper/internal/daemon"
	"golang.org/x/sys/windows/svc"
)

const windowsServiceName = "lazy-mcp-wrapper"

func runWindowsServiceIfNeeded(socketPath string, server *daemon.Server) (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false, err
	}
	if !isService {
		return false, nil
	}
	return true, svc.Run(windowsServiceName, &daemonService{server: server})
}

type daemonService struct {
	server *daemon.Server
}

func (s *daemonService) Execute(args []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.server.Serve(ctx)
	}()

	const accepts = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-errCh; err != nil {
					return false, 1
				}
				return false, 0
			default:
				changes <- svc.Status{State: svc.Running, Accepts: accepts}
			}
		case err := <-errCh:
			changes <- svc.Status{State: svc.StopPending}
			if err != nil {
				cancel()
				return false, 1
			}
			return false, 0
		}
	}
}
