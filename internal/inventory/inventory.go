// Package inventory — источник достижимых хостов (аллоулист). Читается на старте
// из YAML configmap; моделью не пишется, в аргументах инструментов не приходит.
package inventory

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type Host struct {
	Name string   `yaml:"name"`
	Addr string   `yaml:"addr"`
	User string   `yaml:"user"`
	Port int      `yaml:"port"`
	Tags []string `yaml:"tags"`
}

type Inventory struct {
	hosts  []Host
	byName map[string]Host
}

type file struct {
	Hosts []Host `yaml:"hosts"`
}

func Load(path string) (*Inventory, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read inventory %q: %w", path, err)
	}
	return Parse(raw)
}

// Parse валидирует инвентарь: дубль name / пустой addr|user / порт вне диапазона —
// фатальная ошибка. Пустой список хостов допустим (сервер поднимается).
func Parse(raw []byte) (*Inventory, error) {
	var f file
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse inventory yaml: %w", err)
	}
	inv := &Inventory{byName: make(map[string]Host, len(f.Hosts))}
	for i := range f.Hosts {
		h := f.Hosts[i]
		if strings.TrimSpace(h.Name) == "" {
			return nil, fmt.Errorf("host #%d: empty name", i)
		}
		if strings.TrimSpace(h.Addr) == "" {
			return nil, fmt.Errorf("host %q: empty addr", h.Name)
		}
		if strings.TrimSpace(h.User) == "" {
			return nil, fmt.Errorf("host %q: empty user", h.Name)
		}
		if h.Port == 0 {
			h.Port = 22
		}
		if h.Port < 1 || h.Port > 65535 {
			return nil, fmt.Errorf("host %q: port %d out of range", h.Name, h.Port)
		}
		h.Tags = normalizeTags(h.Tags)
		if _, dup := inv.byName[h.Name]; dup {
			return nil, fmt.Errorf("duplicate host name %q", h.Name)
		}
		inv.byName[h.Name] = h
		inv.hosts = append(inv.hosts, h)
	}
	return inv, nil
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || slices.Contains(out, t) {
			continue
		}
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// Match возвращает хосты, несущие ВСЕ запрошенные теги (AND-семантика).
func (inv *Inventory) Match(tags []string) []Host {
	want := normalizeTags(tags)
	var out []Host
	for _, h := range inv.hosts {
		if hostHasAll(h, want) {
			out = append(out, h)
		}
	}
	return out
}

func hostHasAll(h Host, want []string) bool {
	for _, w := range want {
		if !slices.Contains(h.Tags, w) {
			return false
		}
	}
	return true
}

// Resolve находит хост по имени или адресу (аллоулист). ok=false — хоста нет в
// инвентаре (fail-closed: соединение устанавливать нельзя).
func (inv *Inventory) Resolve(ref string) (Host, bool) {
	if h, ok := inv.byName[ref]; ok {
		return h, true
	}
	for _, h := range inv.hosts {
		if h.Addr == ref {
			return h, true
		}
	}
	return Host{}, false
}

func (inv *Inventory) Len() int { return len(inv.hosts) }
