package api

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// GET /v1/host/ports — listening TCP/UDP ports on the host (host network).
// Split into ports owned by this agent/box vs everything else.
func (s *Server) handleHostPorts(w http.ResponseWriter, r *http.Request) {
	_ = r
	oursTCP, oursUDP := s.ourListenPorts()
	extTCP, extUDP := scanListenPorts()

	filter := func(all map[int]struct{}, ours map[int]struct{}) []int {
		out := make([]int, 0, len(all))
		for p := range all {
			if _, ok := ours[p]; ok {
				continue
			}
			if p <= 0 || p > 65535 {
				continue
			}
			out = append(out, p)
		}
		sortInts(out)
		return out
	}

	OK(w, http.StatusOK, map[string]any{
		"external": map[string]any{
			"tcp": filter(extTCP, oursTCP),
			"udp": filter(extUDP, oursUDP),
		},
		"subserver": map[string]any{
			"tcp": mapKeysSorted(oursTCP),
			"udp": mapKeysSorted(oursUDP),
		},
	})
}

func (s *Server) ourListenPorts() (tcp, udp map[int]struct{}) {
	tcp = map[int]struct{}{}
	udp = map[int]struct{}{}
	if s == nil {
		return
	}
	if s.Cfg != nil {
		if _, port, err := net.SplitHostPort(s.Cfg.Listen); err == nil {
			if p, err := strconv.Atoi(port); err == nil && p > 0 {
				tcp[p] = struct{}{}
			}
		}
		if s.Cfg.Controlplane.PublicPort > 0 {
			tcp[s.Cfg.Controlplane.PublicPort] = struct{}{}
		}
	}
	if s.Supervisor != nil {
		if raw, _, err := s.Supervisor.LastGoodConfig(); err == nil && len(raw) > 0 {
			for _, p := range extractJSONListenPorts(string(raw)) {
				tcp[p] = struct{}{}
				udp[p] = struct{}{}
			}
		}
	}
	return
}

func extractJSONListenPorts(cfg string) []int {
	var out []int
	seen := map[int]struct{}{}
	for _, part := range strings.Split(cfg, `"listen_port"`) {
		if part == cfg {
			continue
		}
		part = strings.TrimLeft(part, " \t\n\r:")
		i := 0
		for i < len(part) && part[i] >= '0' && part[i] <= '9' {
			i++
		}
		if i == 0 {
			continue
		}
		n, err := strconv.Atoi(part[:i])
		if err != nil || n <= 0 || n > 65535 {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func scanListenPorts() (tcp, udp map[int]struct{}) {
	tcp = readProcNet("/proc/net/tcp")
	for k, v := range readProcNet("/proc/net/tcp6") {
		tcp[k] = v
	}
	udp = readProcNet("/proc/net/udp")
	for k, v := range readProcNet("/proc/net/udp6") {
		udp[k] = v
	}
	return
}

func readProcNet(path string) map[int]struct{} {
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
		portHex := hostPort[idx+1:]
		b, err := hex.DecodeString(portHex)
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

func mapKeysSorted(m map[int]struct{}) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortInts(out)
	return out
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		j := i
		for j > 0 && a[j-1] > a[j] {
			a[j-1], a[j] = a[j], a[j-1]
			j--
		}
	}
}
