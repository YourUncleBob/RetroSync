package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"retrosync/internal/config"
	"retrosync/internal/node"
)

func main() {
	configFile    := flag.String("config", "", "Path to retrosync.toml")
	syncDir       := flag.String("dir", "sync", "Sync directory (legacy, used when -config is absent)")
	port          := flag.Int("port", 9877, "HTTP server port for file transfer")
	discoveryPort := flag.Int("discovery", 9876, "UDP port for peer discovery")
	flag.Parse()

	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[retrosync] ")

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

	n, err := node.New(cfg, *configFile)
	if err != nil {
		log.Fatalf("init error: %v", err)
	}

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
