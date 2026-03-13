//go:build windows

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "RetroSync"
const serviceDisplayName = "RetroSync Sync Service"
const serviceDesc = "Retro gaming save-file synchronisation daemon"

// windowsService implements svc.Handler.
type windowsService struct {
	run  func() error
	stop func()
}

// Execute is called by the SCM in a dedicated goroutine and blocks until stop.
func (ws *windowsService) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	if err := ws.run(); err != nil {
		log.Printf("service start error: %v", err)
		return false, 1
	}

	status <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}

	for c := range req {
		switch c.Cmd {
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			ws.stop()
			return false, 0
		}
	}
	return false, 0
}

// isWindowsService reports whether the process was launched by the SCM.
func isWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// runAsService hands control to the SCM and blocks until the service stops.
func runAsService(run func() error, stop func()) error {
	return svc.Run(serviceName, &windowsService{run: run, stop: stop})
}

// handleServiceCommand processes -service install|uninstall|start|stop.
func handleServiceCommand(cmd, exePath string, extraArgs []string) error {
	switch cmd {
	case "install":
		return installService(exePath, extraArgs)
	case "uninstall":
		return uninstallService()
	case "start":
		return startService()
	case "stop":
		return stopService()
	default:
		return fmt.Errorf("unknown service command %q; valid: install, uninstall, start, stop", cmd)
	}
}

func installService(exePath string, extraArgs []string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %q already exists", serviceName)
	}

	s, err = m.CreateService(
		serviceName,
		exePath,
		mgr.Config{
			DisplayName: serviceDisplayName,
			Description: serviceDesc,
			StartType:   mgr.StartAutomatic,
		},
		extraArgs...,
	)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	s.Close()
	log.Printf("service %q installed (binary: %s)", serviceName, exePath)
	return nil
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q not found: %w", serviceName, err)
	}
	defer s.Close()

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	log.Printf("service %q uninstalled", serviceName)
	return nil
}

func startService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q not found: %w", serviceName, err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	log.Printf("service %q start requested", serviceName)
	return nil
}

func stopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q not found: %w", serviceName, err)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	timeout := time.Now().Add(10 * time.Second)
	for status.State != svc.Stopped {
		if time.Now().After(timeout) {
			return fmt.Errorf("timed out waiting for service to stop")
		}
		time.Sleep(300 * time.Millisecond)
		status, err = s.Query()
		if err != nil {
			return fmt.Errorf("query service status: %w", err)
		}
	}
	log.Printf("service %q stopped", serviceName)
	return nil
}

// setupLogFile redirects log output to a file.
// If path is empty it defaults to <binary-dir>/retrosync.log.
// When interactive is true it also tees to stderr.
func setupLogFile(path string, interactive bool) (*os.File, error) {
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(filepath.Dir(exe), "retrosync.log")
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	if interactive {
		log.SetOutput(io.MultiWriter(f, os.Stderr))
	} else {
		log.SetOutput(f)
	}
	return f, nil
}
