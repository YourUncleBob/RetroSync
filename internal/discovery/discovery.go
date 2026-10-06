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

// broadcastTarget is one local interface address and its directed broadcast address.
type broadcastTarget struct {
	local net.IP
	bcast net.IP
}

// broadcastTargets returns a target for every up, non-loopback IPv4 interface
// address that supports broadcast. A single send to 255.255.255.255 only leaves
// through one adapter, which may be a virtual one on machines with several.
func broadcastTargets() []broadcastTarget {
	var targets []broadcastTarget
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			mask := net.IP(ipn.Mask).To4()
			if ip4 == nil || mask == nil || ip4.IsLinkLocalUnicast() {
				continue
			}
			bcast := make(net.IP, 4)
			for i := range bcast {
				bcast[i] = ip4[i] | ^mask[i]
			}
			targets = append(targets, broadcastTarget{local: ip4, bcast: bcast})
		}
	}
	return targets
}

// sendOn sends data to dst from a socket bound to the local address, so the
// packet leaves through that interface.
func sendOn(local net.IP, dst *net.UDPAddr, data []byte) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.WriteToUDP(data, dst)
	return err
}

func (d *Discovery) broadcast() {
	beacon := Peer{ID: d.nodeID, Name: d.name, Port: d.httpPort, IsServer: d.isServer}

	send := func() {
		targets := broadcastTargets()
		for _, t := range targets {
			// Advertise the address of the interface the beacon leaves through.
			beacon.Addr = t.local.String()
			data, _ := json.Marshal(beacon)
			dst := &net.UDPAddr{IP: t.bcast, Port: d.discoveryPort}
			if err := sendOn(t.local, dst, data); err != nil {
				log.Printf("discovery: broadcast on %s failed: %v", t.local, err)
			}
		}
		if len(targets) == 0 {
			// Fall back to the global broadcast address.
			beacon.Addr = localIP()
			data, _ := json.Marshal(beacon)
			dst := &net.UDPAddr{IP: net.ParseIP(broadcastIP), Port: d.discoveryPort}
			if conn, err := net.DialUDP("udp4", nil, dst); err == nil {
				conn.Write(data)
				conn.Close()
			}
		}
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