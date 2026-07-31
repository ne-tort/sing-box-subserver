//go:build with_controlplane

package controlplane

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
)

func hostListenPorts() (tcp, udp map[int]struct{}) {
	tcp = readProcNetPorts("/proc/net/tcp")
	for k, v := range readProcNetPorts("/proc/net/tcp6") {
		tcp[k] = v
	}
	udp = readProcNetPorts("/proc/net/udp")
	for k, v := range readProcNetPorts("/proc/net/udp6") {
		udp[k] = v
	}
	return
}

func readProcNetPorts(path string) map[int]struct{} {
	out := map[int]struct{}{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return out
	}
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		if strings.Contains(path, "tcp") && fields[3] != "0A" {
			continue
		}
		hostPort := fields[1]
		idx := strings.IndexByte(hostPort, ':')
		if idx < 0 {
			continue
		}
		b, err := hex.DecodeString(hostPort[idx+1:])
		if err != nil || len(b) < 2 {
			continue
		}
		port := int(binary.BigEndian.Uint16(b[len(b)-2:]))
		if port > 0 {
			out[port] = struct{}{}
		}
	}
	return out
}
