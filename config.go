package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type TelegramConfig struct {
	BotToken         string   `yaml:"bot_token"`
	ChatID           int64    `yaml:"chat_id"`
	SendOrder        []string `yaml:"send_order"`
	AlertCooldownMin int      `yaml:"alert_cooldown_min"`
	ConfirmChecks    int      `yaml:"confirm_checks"`
	NotifyRecovery   bool     `yaml:"notify_recovery"`
}

type Socks5Config struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type BackendsConfig struct {
	Socks5 Socks5Config `yaml:"socks5"`
}

type ThresholdsConfig struct {
	CPU              *float64 `yaml:"cpu"`
	Mem              *float64 `yaml:"mem"`
	Disk             *float64 `yaml:"disk"`
	Temp             *float64 `yaml:"temp"`
	Load             *float64 `yaml:"load"`
	ChannelOccupancy *float64 `yaml:"channel_occupancy"`
}

type NetworkConfig struct {
	Interface           string  `yaml:"interface"`
	ChannelCapacityMbps float64 `yaml:"channel_capacity_mbps"`
}

type InternetCheckConfig struct {
	Hosts      []string `yaml:"hosts"`
	TimeoutSec int      `yaml:"timeout_sec"`
}

type HTTPConfig struct {
	BindHost  string `yaml:"bind_host"`
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token"`
}

type EasytierConfig struct {
	Command    string   `yaml:"command"`
	Networks   []string `yaml:"networks"`
	TimeoutSec int      `yaml:"timeout_sec"`
}

type ElectionConfig struct {
	HeartbeatSec int `yaml:"heartbeat_sec"`
	FailoverSec  int `yaml:"failover_sec"`
	BackoffSec   int `yaml:"backoff_sec"`
}

type StorageConfig struct {
	Path          string `yaml:"path"`
	RetentionDays int    `yaml:"retention_days"`
}

type Config struct {
	Name             *string             `yaml:"name"`
	CheckIntervalMin []int               `yaml:"check_interval_min"`
	Telegram         TelegramConfig      `yaml:"telegram"`
	Backends         BackendsConfig      `yaml:"backends"`
	Thresholds       ThresholdsConfig    `yaml:"thresholds"`
	Network          NetworkConfig       `yaml:"network"`
	InternetCheck    InternetCheckConfig `yaml:"internet_check"`
	HTTP             HTTPConfig          `yaml:"http"`
	Priority         map[string]int      `yaml:"priority"`
	Easytier         EasytierConfig      `yaml:"easytier"`
	Election         ElectionConfig      `yaml:"election"`
	Storage          StorageConfig       `yaml:"storage"`
	Commands         bool                `yaml:"commands"`
	StateFile        string              `yaml:"state_file"`
	LogFile          string              `yaml:"log_file"`
}

func defaultConfig() Config {
	cpu := 90.0
	mem := 90.0
	disk := 90.0
	temp := 85.0
	return Config{
		CheckIntervalMin: []int{5, 10},
		Telegram: TelegramConfig{
			ChatID:           0,
			SendOrder:        []string{"socks5"},
			AlertCooldownMin: 60,
			ConfirmChecks:    3,
			NotifyRecovery:   true,
		},
		Backends: BackendsConfig{
			Socks5: Socks5Config{Port: 1080},
		},
		Thresholds: ThresholdsConfig{
			CPU:  &cpu,
			Mem:  &mem,
			Disk: &disk,
			Temp: &temp,
		},
		Network: NetworkConfig{ChannelCapacityMbps: 100.0},
		InternetCheck: InternetCheckConfig{
			Hosts:      []string{"8.8.8.8:53", "1.1.1.1:53"},
			TimeoutSec: 5,
		},
		HTTP: HTTPConfig{BindHost: "0.0.0.0", Port: 8090},
		Easytier: EasytierConfig{
			Command:    "easytier-cli peer",
			TimeoutSec: 15,
		},
		Election:  ElectionConfig{HeartbeatSec: 30, FailoverSec: 90, BackoffSec: 60},
		Storage:   StorageConfig{Path: "metrics.jsonl", RetentionDays: 3},
		Commands:  true,
		StateFile: "state.json",
	}
}

func loadConfig(path string) (*Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, err
	}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}
