package config

import (
	"fmt"

	"github.com/inhuman/config"
)

// Config — окружение сервера mcp-ssh-fleet. Все ключи в substruct с префиксом
// SSH_FLEET_. Инвентарь и ключ — пути к смонтированным configmap/секрету.
type Config struct {
	Transport string `env:"SSH_FLEET_TRANSPORT" env-default:"http"`
	Addr      string `env:"SSH_FLEET_ADDR" env-default:":8080"`
	AuthToken string `env:"SSH_FLEET_AUTH_TOKEN" mask:"filled"`

	InventoryPath string `env:"SSH_FLEET_INVENTORY_PATH" env-default:"/etc/ssh-fleet/inventory.yaml"`
	KeyPath       string `env:"SSH_FLEET_KEY_PATH" env-default:"/etc/ssh-fleet/id_ed25519"`

	OutputCapBytes   int `env:"SSH_FLEET_OUTPUT_CAP_BYTES" env-default:"8192"`
	CmdTimeoutS      int `env:"SSH_FLEET_CMD_TIMEOUT_SECONDS" env-default:"20"`
	ProbeConcurrency int `env:"SSH_FLEET_PROBE_CONCURRENCY" env-default:"8"`
	ProbeMaxHosts    int `env:"SSH_FLEET_PROBE_MAX_HOSTS" env-default:"50"`
}

func Load() (Config, error) {
	var c Config
	if err := config.Load(&c); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c Config) Validate() error {
	if c.OutputCapBytes < 256 {
		return fmt.Errorf("SSH_FLEET_OUTPUT_CAP_BYTES must be >= 256, got %d", c.OutputCapBytes)
	}
	if c.CmdTimeoutS < 1 || c.CmdTimeoutS > 600 {
		return fmt.Errorf("SSH_FLEET_CMD_TIMEOUT_SECONDS must be in 1..600, got %d", c.CmdTimeoutS)
	}
	if c.ProbeConcurrency < 1 {
		return fmt.Errorf("SSH_FLEET_PROBE_CONCURRENCY must be >= 1, got %d", c.ProbeConcurrency)
	}
	if c.ProbeMaxHosts < 1 {
		return fmt.Errorf("SSH_FLEET_PROBE_MAX_HOSTS must be >= 1, got %d", c.ProbeMaxHosts)
	}
	return nil
}
