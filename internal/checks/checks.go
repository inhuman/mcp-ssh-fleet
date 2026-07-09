// Package checks — зашитый реестр курируемых read-only диагностик для ssh_probe.
// Модель выбирает только имя проверки; тело скрипта задаётся здесь, не приходит из
// аргументов (анти-инъекция, детерминизм).
package checks

import (
	"fmt"
	"slices"
	"strings"
)

type Check struct {
	Name        string
	Script      string
	Description string
}

// registry — набор проверок MVP. Все скрипты неинтерактивны и read-only.
var registry = map[string]Check{
	"uptime": {"uptime", "uptime", "аптайм и средняя нагрузка"},
	"disk":   {"disk", "df -h", "свободное место на дисках"},
	"mem":    {"mem", "free -m", "использование памяти"},
	"failed": {"failed", "systemctl --failed --no-legend --no-pager 2>/dev/null || echo 'systemctl unavailable'", "упавшие systemd-сервисы"},
	"logs":   {"logs", "journalctl -n 50 --no-pager 2>/dev/null || tail -n 50 /var/log/syslog 2>/dev/null || echo 'no system log'", "хвост системного журнала"},
}

func Resolve(name string) (Check, error) {
	c, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Check{}, fmt.Errorf("unknown check %q; available: %s", name, strings.Join(Names(), ", "))
	}
	return c, nil
}

// Names возвращает отсортированный список имён проверок (для описания тула и ошибок).
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}
