package slots

import (
	"fmt"
	"net"
	"sort"
	"time"
)

// connectProbeTimeout bounds the TCP connect probe in portBusy so a
// firewalled/filtered port can't hang `ct create`.
const connectProbeTimeout = 200 * time.Millisecond

// BusyPorts returns "VAR (port)" for every entry in ports already unusable
// on 127.0.0.1 — either the bind itself fails, or (for a root-owned
// listener, invisible to an unprivileged bind attempt on some platforms)
// something answers a connect probe. Called before a slot is committed, so
// `ct create` can fail with nothing touched instead of half-provisioning a
// worktree onto ports another stack already owns.
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

// portBusy is "bind failing OR connect succeeding = busy": a connect probe
// catches a root-owned listener that an unprivileged bind attempt can't
// see on some platforms, and (checked first, so BusyPorts never holds its
// own listener open while probing, which would trivially always answer
// its own connect) confirms something is actually answering before falling
// through to the bind check.
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
