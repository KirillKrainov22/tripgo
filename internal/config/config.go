package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr              string
	LogLevel              slog.Level
	ShutdownTimeout       time.Duration
	HTTPReadTimeout       time.Duration
	HTTPReadHeaderTimeout time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration

	DatabaseURL             string
	DatabaseMaxConns        int32
	DatabaseMinConns        int32
	DatabaseConnectTimeout  time.Duration
	DatabaseQueryTimeout    time.Duration
	DatabaseMaxConnLifetime time.Duration
}

func Load() (Config, error) {
	var cfg Config
	var err error

	cfg.HTTPAddr, err = required("HTTP_ADDR")
	if err != nil {
		return Config{}, err
	}
	_, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR: expected host:port (for example :8080)")
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return Config{}, fmt.Errorf("HTTP_ADDR: port must be an integer between 1 and 65535")
	}

	level, err := required("LOG_LEVEL")
	if err != nil {
		return Config{}, err
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	cfg.DatabaseURL, err = required("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	for _, setting := range []struct {
		name string
		dest *time.Duration
	}{
		{"SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout},
		{"HTTP_READ_TIMEOUT", &cfg.HTTPReadTimeout},
		{"HTTP_READ_HEADER_TIMEOUT", &cfg.HTTPReadHeaderTimeout},
		{"HTTP_WRITE_TIMEOUT", &cfg.HTTPWriteTimeout},
		{"HTTP_IDLE_TIMEOUT", &cfg.HTTPIdleTimeout},
		{"DATABASE_CONNECT_TIMEOUT", &cfg.DatabaseConnectTimeout},
		{"DATABASE_QUERY_TIMEOUT", &cfg.DatabaseQueryTimeout},
		{"DATABASE_MAX_CONN_LIFETIME", &cfg.DatabaseMaxConnLifetime},
	} {
		value, err := required(setting.name)
		if err != nil {
			return Config{}, err
		}
		duration, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", setting.name, err)
		}
		if duration <= 0 {
			return Config{}, fmt.Errorf("%s: must be greater than zero", setting.name)
		}
		*setting.dest = duration
	}

	cfg.DatabaseMaxConns, err = integer("DATABASE_MAX_CONNS")
	if err != nil {
		return Config{}, err
	}
	if cfg.DatabaseMaxConns <= 0 {
		return Config{}, fmt.Errorf("DATABASE_MAX_CONNS: must be greater than zero")
	}
	cfg.DatabaseMinConns, err = integer("DATABASE_MIN_CONNS")
	if err != nil {
		return Config{}, err
	}
	if cfg.DatabaseMinConns < 0 || cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return Config{}, fmt.Errorf("DATABASE_MIN_CONNS: must be between zero and DATABASE_MAX_CONNS")
	}
	return cfg, nil
}

func required(name string) (string, error) {
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s: required environment variable is missing or empty", name)
	}
	return value, nil
}

func integer(name string) (int32, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}
	number, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: expected a 32-bit integer", name)
	}
	return int32(number), nil
}
