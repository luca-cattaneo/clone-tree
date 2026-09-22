package slots

import (
	"fmt"
	"net"
	"sort"
	"time"
)

const connectProbeTimeout = 200 * time.Millisecond

// BusyPorts returns "VAR (port)" for every entry in ports already unusable
// on 127.0.0.1 — either the bind itself fails, or (for a root-owned
// listener, invisible to an unprivileged bind attempt on some platforms)
// something answers a connect probe.
func BusyPorts(ports map[string]int) []string {
	var busy []string
	for name, port := range ports {
		if portBusy(port) {
			busy = append(busy, fmt.Sprintf("%s (%d)", name, port))
		}
	}
	sort.Strings(busy)
	return busy
}

// The connect probe is checked before the bind check: checking bind first
// would have this function holding its own listener open while probing,
// which would trivially always answer its own connect.
func portBusy(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	if conn, err := net.DialTimeout("tcp", addr, connectProbeTimeout); err == nil {
		_ = conn.Close()
		return true
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}
