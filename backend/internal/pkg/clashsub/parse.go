// Package clashsub 解析 Clash 格式订阅，提取受支持的 shadowsocks 节点。
//
// 当前仅支持 type=ss，且插件为空或 obfs(tls)。
// 其余节点不会被静默丢弃，而是收集到 Skipped 列表并附带原因。
package clashsub

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Node 是一个可用的 shadowsocks 节点。
type Node struct {
	Name     string
	Server   string
	Port     int
	Cipher   string
	Password string
	Plugin   string // "" 或 "obfs"
	ObfsMode string // Plugin=="obfs" 时为 "tls"
	ObfsHost string
}

// Skipped 记录被跳过的节点及原因。
type Skipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type clashFile struct {
	Proxies []clashProxy `yaml:"proxies"`
}

type clashProxy struct {
	Name       string         `yaml:"name"`
	Type       string         `yaml:"type"`
	Server     string         `yaml:"server"`
	Port       int            `yaml:"port"`
	Cipher     string         `yaml:"cipher"`
	Password   string         `yaml:"password"`
	Plugin     string         `yaml:"plugin"`
	PluginOpts map[string]any `yaml:"plugin-opts"`
}

// Parse 解析订阅内容。
func Parse(data []byte) ([]Node, []Skipped, error) {
	var f clashFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, nil, fmt.Errorf("clashsub: parse yaml: %w", err)
	}

	nodes := make([]Node, 0, len(f.Proxies))
	skipped := make([]Skipped, 0)

	for _, p := range f.Proxies {
		if p.Type != "ss" {
			skipped = append(skipped, Skipped{
				Name:   p.Name,
				Reason: fmt.Sprintf("不支持的节点类型 %q（仅支持 ss）", p.Type),
			})
			continue
		}
		if p.Server == "" || p.Port == 0 {
			skipped = append(skipped, Skipped{Name: p.Name, Reason: "缺少 server 或 port"})
			continue
		}
		if p.Cipher == "" || p.Password == "" {
			skipped = append(skipped, Skipped{Name: p.Name, Reason: "缺少 cipher 或 password"})
			continue
		}

		n := Node{
			Name:     p.Name,
			Server:   p.Server,
			Port:     p.Port,
			Cipher:   p.Cipher,
			Password: p.Password,
		}

		switch plugin := strings.TrimSpace(p.Plugin); plugin {
		case "":
			// 无插件，直接可用
		case "obfs":
			mode, _ := p.PluginOpts["mode"].(string)
			if mode != "tls" {
				skipped = append(skipped, Skipped{
					Name:   p.Name,
					Reason: fmt.Sprintf("不支持的 obfs 模式 %q（仅支持 tls）", mode),
				})
				continue
			}
			host, _ := p.PluginOpts["host"].(string)
			if host == "" {
				skipped = append(skipped, Skipped{Name: p.Name, Reason: "obfs 缺少 host"})
				continue
			}
			n.Plugin = "obfs"
			n.ObfsMode = mode
			n.ObfsHost = host
		default:
			skipped = append(skipped, Skipped{
				Name:   p.Name,
				Reason: fmt.Sprintf("不支持的插件 %q（仅支持 obfs）", plugin),
			})
			continue
		}

		nodes = append(nodes, n)
	}

	return nodes, skipped, nil
}
