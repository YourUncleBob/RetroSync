//go:build !windows

package main

import (
	"log"
	"os"
	"syscall"
)

func acquireSingleInstance() {
	f, err := os.OpenFile("/tmp/retrosync.lock", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("warning: could not create lock file: %v", err)
		return
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		log.Fatal("another instance of RetroSync is already running")
	}
	_ = f // keep file open — lock released automatically when process exits
}
