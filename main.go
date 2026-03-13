package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"retrosync/internal/config"
	"retrosync/internal/node"
)

func main() {
	serviceCmd    := flag.String("service", "", "Windows service control: install, uninstall, start, stop")
	logFile       := flag.String("logfile", "", "Path to log file (default: <binary-dir>/retrosync.log when running as a service)")
	configFile    := flag.String("config", "", "Path to retrosync.toml")
	syncDir       := flag.String("dir", "sync", "Sync directory (legacy, used when -config is absent)")
	port          := flag.Int("port", 9877, "HTTP server port for file transfer")
	discoveryPort := flag.Int("discovery", 9876, "UDP port for peer discovery")
	paused        := flag.Bool("paused", false, "start with all sync groups paused")
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[retrosync] ")

	// Handle -service install/uninstall/start/stop before anything else.
	if *serviceCmd != "" {
		exe, err := os.Executable()
		if err != nil {
			log.Fatalf("cannot determine executable path: %v", err)
		}
		// Reconstruct flags to persist as the service's stored arguments.
		var svcArgs []string
		if *configFile != ""        { svcArgs = append(svcArgs, "-config",    *configFile) }
		if *port != 9877            { svcArgs = append(svcArgs, "-port",      fmt.Sprintf("%d", *port)) }
		if *discoveryPort != 9876   { svcArgs = append(svcArgs, "-discovery", fmt.Sprintf("%d", *discoveryPort)) }
		if *paused                  { svcArgs = append(svcArgs, "-paused") }
		if *logFile != ""           { svcArgs = append(svcArgs, "-logfile",   *logFile) }
		if err := handleServiceCommand(*serviceCmd, exe, svcArgs); err != nil {
			log.Fatalf("service %s: %v", *serviceCmd, err)
		}
		return
	}

	// Redirect log output to a file when running as a service or when -logfile is set.
	runningAsService := isWindowsService()
	if runningAsService || *logFile != "" {
		f, err := setupLogFile(*logFile, !runningAsService)
		if err != nil {
			log.Fatalf("log file setup: %v", err)
		}
		if f != nil {
			defer f.Close()
		}
	}

	var cfg *config.Config
	if *configFile != "" {
		if _, err := os.Stat(*configFile); os.IsNotExist(err) {
			if err := config.WriteDefaultConfig(*configFile); err != nil {
				log.Fatalf("could not create default config: %v", err)
			}
			log.Printf("created default config at %s — edit it to configure sync groups", *configFile)
		}
		var err error
		cfg, err = config.Load(*configFile)
		if err != nil {
			log.Fatalf("config error: %v", err)
		}
	} else {
		cfg = config.DefaultConfig(*syncDir, *port, *discoveryPort)
	}

	// CLI flags override config file network settings when no config file is used.
	if *configFile == "" {
		cfg.Node.Port = *port
		cfg.Node.DiscoveryPort = *discoveryPort
	}

	if *paused {
		for i := range cfg.Syncs {
			cfg.Syncs[i].Paused = true
		}
	}

	n, err := node.New(cfg, *configFile)
	if err != nil {
		log.Fatalf("init error: %v", err)
	}

	// When launched by the SCM, hand control to the service runner.
	if runningAsService {
		if err := runAsService(n.Start, n.Stop); err != nil {
			log.Fatalf("service execution error: %v", err)
		}
		return
	}

	// Interactive path.
	if err := n.Start(); err != nil {
		log.Fatalf("start error: %v", err)
	}

	log.Printf("press Ctrl+C to stop")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	n.Stop()
}
