//go:build windows

package main

import (
	"errors"
	"log"

	"golang.org/x/sys/windows"
)

func acquireSingleInstance() {
	name, _ := windows.UTF16PtrFromString(`Global\RetroSync`)
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		log.Fatal("another instance of RetroSync is already running")
	}
	if err != nil {
		log.Printf("warning: could not create single-instance mutex: %v", err)
		return
	}
	_ = h // keep handle open — released automatically when process exits
}
