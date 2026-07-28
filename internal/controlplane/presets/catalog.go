//go:build with_controlplane

package presets

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

// Catalog of embedded protocol presets (inbound variants).
// TLS-capable presets declare trait "tls"; materialize attaches certificate
// via attachInboundTLS (paths or certificate_provider) — templates only set enabled+server_name placeholders.
var (
	once   sync.Once
	all    []domain.ProtocolPreset
	byName map[string]domain.ProtocolPreset
)

func load() {
	raw := []byte(`[
  {
    "name": "shadowsocks-tcp",
    "protocol": "shadowsocks",
    "description": "Shadowsocks AES-128-GCM multi-user over TCP (no TLS)",
    "traits": ["tcp"],
    "cred_fields": ["password"],
    "inbound_template": {
      "type": "shadowsocks",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "method": "aes-128-gcm",
      "users": []
    },
    "outbound_template": {
      "type": "shadowsocks",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "method": "aes-128-gcm",
      "password": "{{user.password}}"
    }
  },
  {
    "name": "shadowsocks-aes-256-gcm",
    "protocol": "shadowsocks",
    "description": "Shadowsocks AES-256-GCM multi-user over TCP",
    "traits": ["tcp"],
    "cred_fields": ["password"],
    "inbound_template": {
      "type": "shadowsocks",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "method": "aes-256-gcm",
      "users": []
    },
    "outbound_template": {
      "type": "shadowsocks",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "method": "aes-256-gcm",
      "password": "{{user.password}}"
    }
  },
  {
    "name": "shadowsocks-chacha20",
    "protocol": "shadowsocks",
    "description": "Shadowsocks 2022-unrelated classic chacha20-ietf-poly1305 TCP",
    "traits": ["tcp"],
    "cred_fields": ["password"],
    "inbound_template": {
      "type": "shadowsocks",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "method": "chacha20-ietf-poly1305",
      "users": []
    },
    "outbound_template": {
      "type": "shadowsocks",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "method": "chacha20-ietf-poly1305",
      "password": "{{user.password}}"
    }
  },
  {
    "name": "trojan-tcp",
    "protocol": "trojan",
    "description": "Trojan over TCP with TLS (profile from controlplane TLS API)",
    "traits": ["tcp", "tls"],
    "cred_fields": ["password"],
    "inbound_template": {
      "type": "trojan",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": [],
      "tls": { "enabled": true, "server_name": "{{server}}" }
    },
    "outbound_template": {
      "type": "trojan",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "password": "{{user.password}}",
      "tls": { "enabled": true, "server_name": "{{server}}" }
    }
  },
  {
    "name": "vless-tcp",
    "protocol": "vless",
    "description": "VLESS over TCP without TLS (lab / demux inject target)",
    "traits": ["tcp"],
    "cred_fields": ["uuid"],
    "inbound_template": {
      "type": "vless",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": []
    },
    "outbound_template": {
      "type": "vless",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "uuid": "{{user.uuid}}"
    }
  },
  {
    "name": "vless-tls",
    "protocol": "vless",
    "description": "VLESS over TCP with TLS (controlplane TLS profile)",
    "traits": ["tcp", "tls"],
    "cred_fields": ["uuid"],
    "inbound_template": {
      "type": "vless",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": [],
      "tls": { "enabled": true, "server_name": "{{server}}" }
    },
    "outbound_template": {
      "type": "vless",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "uuid": "{{user.uuid}}",
      "tls": { "enabled": true, "server_name": "{{server}}" }
    }
  },
  {
    "name": "vmess-tcp",
    "protocol": "vmess",
    "description": "VMess over TCP without TLS",
    "traits": ["tcp"],
    "cred_fields": ["uuid"],
    "inbound_template": {
      "type": "vmess",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": []
    },
    "outbound_template": {
      "type": "vmess",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "uuid": "{{user.uuid}}",
      "security": "auto"
    }
  },
  {
    "name": "hysteria2",
    "protocol": "hysteria2",
    "description": "Hysteria2 UDP/QUIC with TLS (controlplane TLS profile)",
    "traits": ["udp", "quic", "tls"],
    "cred_fields": ["password"],
    "inbound_template": {
      "type": "hysteria2",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": [],
      "tls": { "enabled": true, "server_name": "{{server}}" }
    },
    "outbound_template": {
      "type": "hysteria2",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "password": "{{user.password}}",
      "tls": { "enabled": true, "server_name": "{{server}}" }
    }
  },
  {
    "name": "tuic",
    "protocol": "tuic",
    "description": "TUIC v5 UDP/QUIC with TLS (controlplane TLS profile)",
    "traits": ["udp", "quic", "tls"],
    "cred_fields": ["uuid", "password"],
    "inbound_template": {
      "type": "tuic",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": [],
      "tls": { "enabled": true, "server_name": "{{server}}" },
      "congestion_control": "bbr"
    },
    "outbound_template": {
      "type": "tuic",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "uuid": "{{user.uuid}}",
      "password": "{{user.password}}",
      "tls": { "enabled": true, "server_name": "{{server}}" },
      "congestion_control": "bbr"
    }
  },
  {
    "name": "anytls",
    "protocol": "anytls",
    "description": "AnyTLS over TCP with TLS (controlplane TLS profile)",
    "traits": ["tcp", "tls"],
    "cred_fields": ["password"],
    "inbound_template": {
      "type": "anytls",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": [],
      "tls": { "enabled": true, "server_name": "{{server}}" }
    },
    "outbound_template": {
      "type": "anytls",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "password": "{{user.password}}",
      "tls": { "enabled": true, "server_name": "{{server}}" }
    }
  },
  {
    "name": "socks",
    "protocol": "socks",
    "description": "SOCKS5 with username/password (lab / demux plaintext target)",
    "traits": ["tcp"],
    "cred_fields": ["username", "password"],
    "inbound_template": {
      "type": "socks",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": []
    },
    "outbound_template": {
      "type": "socks",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "username": "{{user.username}}",
      "password": "{{user.password}}"
    }
  },
  {
    "name": "http",
    "protocol": "http",
    "description": "HTTP CONNECT proxy with username/password",
    "traits": ["tcp"],
    "cred_fields": ["username", "password"],
    "inbound_template": {
      "type": "http",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": []
    },
    "outbound_template": {
      "type": "http",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "username": "{{user.username}}",
      "password": "{{user.password}}"
    }
  },
  {
    "name": "vmess-tls",
    "protocol": "vmess",
    "description": "VMess over TCP with TLS (controlplane TLS profile)",
    "traits": ["tcp", "tls"],
    "cred_fields": ["uuid"],
    "inbound_template": {
      "type": "vmess",
      "tag": "{{tag}}",
      "listen": "{{listen}}",
      "listen_port": 0,
      "users": [],
      "tls": { "enabled": true, "server_name": "{{server}}" }
    },
    "outbound_template": {
      "type": "vmess",
      "tag": "{{tag}}",
      "server": "{{server}}",
      "server_port": 0,
      "uuid": "{{user.uuid}}",
      "security": "auto",
      "tls": { "enabled": true, "server_name": "{{server}}" }
    }
  }
]`)
	if err := json.Unmarshal(raw, &all); err != nil {
		panic(err)
	}
	byName = make(map[string]domain.ProtocolPreset, len(all))
	for _, p := range all {
		byName[p.Name] = p
	}
}

func All() []domain.ProtocolPreset {
	once.Do(load)
	out := make([]domain.ProtocolPreset, len(all))
	copy(out, all)
	return out
}

func Get(name string) (domain.ProtocolPreset, error) {
	once.Do(load)
	p, ok := byName[name]
	if !ok {
		return domain.ProtocolPreset{}, fmt.Errorf("unknown preset %q", name)
	}
	return p, nil
}

func Names() []string {
	once.Do(load)
	names := make([]string, 0, len(all))
	for _, p := range all {
		names = append(names, p.Name)
	}
	return names
}
