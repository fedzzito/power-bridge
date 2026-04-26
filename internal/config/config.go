// Package config handles loading and saving the power-bridge configuration.
package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PhaseDistribution controls how single-phase power is mapped to three phases.
type PhaseDistribution string

const (
	// PhaseEqual spreads total power evenly across L1, L2 and L3.
	PhaseEqual PhaseDistribution = "equal"
	// PhaseL1 assigns all power to L1; L2 and L3 report zero.
	PhaseL1 PhaseDistribution = "l1"
)

// DeviceProfile selects extra quirks for specific battery systems.
type DeviceProfile string

const (
	ProfileMarstek  DeviceProfile = "marstek"
	ProfileNoah     DeviceProfile = "noah"
	ProfileHoymiles DeviceProfile = "hoymiles"
	ProfileStandard DeviceProfile = "standard"
)

// Config is the complete runtime configuration.
type Config struct {
	// Network
	WIFISSID     string `yaml:"wifi_ssid"`
	WIFIPassword string `yaml:"wifi_password"`

	// Poweropti
	PoweroptiIP     string `yaml:"poweropti_ip"`
	PoweroptiAPIKey string `yaml:"poweropti_api_key"`

	// Shelly emulation identity
	ShellyMAC string `yaml:"shelly_mac"`
	Hostname  string `yaml:"hostname"`

	// Behaviour
	DeviceProfile  DeviceProfile     `yaml:"device_profile"`
	PhaseMode      PhaseDistribution `yaml:"phase_mode"`
	PollIntervalS  int               `yaml:"poll_interval_sec"`
	StaleTimeoutS  int               `yaml:"stale_timeout_sec"`
	ListenAddr     string            `yaml:"listen_addr"`

	// Set to true after the first-run setup is completed.
	Configured bool `yaml:"configured"`
}

// Defaults returns a Config pre-filled with safe default values.
func Defaults() *Config {
	return &Config{
		Hostname:      "shellypro3em-poweropti",
		DeviceProfile: ProfileStandard,
		PhaseMode:     PhaseEqual,
		PollIntervalS: 3,
		StaleTimeoutS: 30,
		ShellyMAC:     "AA:BB:CC:DD:EE:FF",
		ListenAddr:    ":80",
	}
}

// Load reads a YAML config file. If the file does not exist, it returns Defaults.
func Load(path string) (*Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes the config as YAML to path, creating parent directories as needed.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
