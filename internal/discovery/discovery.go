// Package discovery implements UDP broadcast-based peer discovery on a LAN.
// Each node broadcasts a small JSON beacon every 5 seconds; peers that hear
// an unknown node ID call the onPeer callback once.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)


const broadcastIP = "255.255.255.255"

// Peer represents a discovered remote node.
type Peer struct {
	ID       string    `json:"id"`
	Name     string    `json:"name,omitempty"`
	Addr     string    `json:"addr"`                // IPv4 address
	Port     int       `json:"port"`                // HTTP server port
	IsServer bool      `json:"is_server,omitempty"` // true when this node is the authoritative server
	LastSeen time.Time `json:"-"`                   // set locally; not transmitted in beacons
}

// Discovery handles sending and receiving peer beacons.
type Discovery struct {
	nodeID        string
	name          string
	httpPort      int
	discoveryPort int
	isServer      bool

	mu     sync.Mutex
	peers  map[string]Peer
	onPeer func(Peer)

	done chan struct{}
}

// New creates a Discovery instance. isServer marks this node as the authoritative
// server in its beacon so clients can identify it. name is the human-readable node
// name included in beacons. onPeer is called once per new peer found; pass nil if
// not needed.
func New(nodeID string, httpPort, discoveryPort int, isServer bool, name string, onPeer func(Peer)) *Discovery {
	return &Discovery{
		nodeID:        nodeID,
		name:          name,
		httpPort:      httpPort,
		discoveryPort: discoveryPort,
		isServer:      isServer,
		peers:         make(map[string]Peer),
		onPeer:        onPeer,
		done:          make(chan struct{}),
	}
}

// Start launches the broadcast and listen goroutines.
func (d *Discovery) Start() error {
	go d.broadcast()
	go d.listen()
	return nil
}

// Stop shuts down discovery.
func (d *Discovery) Stop() {
	close(d.done)
}

// localIP returns the primary non-loopback IPv4 address by attempting a UDP
// "connection" to an external address (no packets are actually sent).
func localIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func (d *Discovery) broadcast() {
	addr := &net.UDPAddr{IP: net.ParseIP(broadcastIP), Port: d.discoveryPort}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		log.Printf("discovery: broadcast dial error: %v", err)
		return
	}
	defer conn.Close()

	beacon := Peer{ID: d.nodeID, Name: d.name, Port: d.httpPort, IsServer: d.isServer}

	send := func() {
		beacon.Addr = localIP()
		data, _ := json.Marshal(beacon)
		conn.Write(data)
	}

	send() // immediate first broadcast
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			send()
		case <-d.done:
			return
		}
	}
}

func (d *Discovery) listen() {
	addrStr := fmt.Sprintf("0.0.0.0:%d", d.discoveryPort)
	lc := net.ListenConfig{Control: reusePort}
	pc, err := lc.ListenPacket(context.Background(), "udp4", addrStr)
	if err != nil {
		log.Printf("discovery: listen error: %v", err)
		return
	}
	conn := pc.(*net.UDPConn)
	defer conn.Close()

	// Unblock ReadFromUDP when done.
	go func() {
		<-d.done
		conn.Close()
	}()

	buf := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-d.done:
				return
			default:
				continue
			}
		}

		var peer Peer
		if err := json.Unmarshal(buf[:n], &peer); err != nil {
			continue
		}
		if peer.ID == d.nodeID {
			continue // ignore own beacon
		}

		peer.LastSeen = time.Now()

		d.mu.Lock()
		d.peers[peer.ID] = peer
		d.mu.Unlock()

		if d.onPeer != nil {
			go d.onPeer(peer)
		}
	}
}