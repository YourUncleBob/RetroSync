//go:build !windows

package main

import "os"

func isWindowsService() bool { return false }

func runAsService(_ func() error, _ func()) error { return nil }

func handleServiceCommand(cmd, _ string, _ []string) error {
	return nil
}

// setupLogFile on non-Windows opens a log file only when an explicit path is given.
func setupLogFile(path string, _ bool) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return f, nil
}
