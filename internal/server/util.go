package server

import (
	"net"
	"os"
	"time"
)

var startTime = time.Now()

func uptimeSeconds() int64 {
	return int64(time.Since(startTime).Seconds())
}

// serviceName is the systemd unit name used by restart/stop operations.
const serviceName = "power-bridge"

// getLocalIP returns the first non-loopback IPv4 address found on the host,
// falling back to the hostname if none is found.
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				if ipNet.IP.To4() != nil {
					return ipNet.IP.String()
				}
			}
		}
	}
	hostname, _ := os.Hostname()
	return hostname
}
