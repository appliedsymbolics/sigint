package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Server struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type Ledger struct {
	Adapter string `yaml:"adapter"`
	Path    string `yaml:"path"`
	DSN     string `yaml:"dsn"`
}

type Storage struct {
	Adapter              string `yaml:"adapter"`
	Root                 string `yaml:"root"`
	Bucket               string `yaml:"bucket"`
	Prefix               string `yaml:"prefix"`
	Region               string `yaml:"region"`
	Endpoint             string `yaml:"endpoint"`
	ForcePathStyle       bool   `yaml:"force_path_style"`
	ServerSideEncryption string `yaml:"server_side_encryption"`
	KMSKeyID             string `yaml:"kms_key_id"`
}

type Ingest struct {
	RejectHashConflicts bool `yaml:"reject_hash_conflicts"`
	RequirePayloadHash  bool `yaml:"require_payload_hash"`
	RequireEventHash    bool `yaml:"require_event_hash"`
}

type Replay struct {
	DefaultLimit int `yaml:"default_limit"`
	MaxLimit     int `yaml:"max_limit"`
}

type Retention struct {
	HotWindow string `yaml:"hot_window"`
}

type Auth struct {
	ProducerToken    string `yaml:"producer_token"`
	ProducerTokenEnv string `yaml:"producer_token_env"`
	InternalToken    string `yaml:"internal_token"`
	InternalTokenEnv string `yaml:"internal_token_env"`
}

type App struct {
	Server    Server    `yaml:"server"`
	Ledger    Ledger    `yaml:"ledger"`
	Storage   Storage   `yaml:"storage"`
	Ingest    Ingest    `yaml:"ingest"`
	Replay    Replay    `yaml:"replay"`
	Retention Retention `yaml:"retention"`
	Auth      Auth      `yaml:"auth"`
}

func Load(path string) (App, error) {
	configPath, err := filepath.Abs(path)
	if err != nil {
		return App{}, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return App{}, fmt.Errorf("config file does not exist: %s", configPath)
	}

	cfg := Default()
	if err := decodeKnownFields(data, &cfg); err != nil {
		return App{}, fmt.Errorf("invalid YAML config: %s: %w", configPath, err)
	}
	if err := cfg.expandEnv(); err != nil {
		return App{}, err
	}
	if err := cfg.validate(); err != nil {
		return App{}, err
	}

	baseDir := filepath.Dir(configPath)
	if cfg.Ledger.Adapter == "sqlite" {
		cfg.Ledger.Path = resolvePath(cfg.Ledger.Path, baseDir)
	}
	if cfg.Storage.Adapter == "filesystem" {
		cfg.Storage.Root = resolvePath(cfg.Storage.Root, baseDir)
	}
	return cfg, nil
}

func LoadLedger(path string) (Ledger, error) {
	configPath, err := filepath.Abs(path)
	if err != nil {
		return Ledger{}, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Ledger{}, fmt.Errorf("config file does not exist: %s", configPath)
	}

	cfg := struct {
		Server    yaml.Node `yaml:"server"`
		Ledger    Ledger    `yaml:"ledger"`
		Storage   yaml.Node `yaml:"storage"`
		Ingest    yaml.Node `yaml:"ingest"`
		Replay    yaml.Node `yaml:"replay"`
		Retention yaml.Node `yaml:"retention"`
		Auth      yaml.Node `yaml:"auth"`
	}{
		Ledger: Default().Ledger,
	}
	if err := decodeKnownFields(data, &cfg); err != nil {
		return Ledger{}, fmt.Errorf("invalid YAML config: %s: %w", configPath, err)
	}
	cfg.Ledger.Path = os.ExpandEnv(cfg.Ledger.Path)
	cfg.Ledger.DSN = os.ExpandEnv(cfg.Ledger.DSN)
	app := App{Ledger: cfg.Ledger}
	if err := app.validateLedger(); err != nil {
		return Ledger{}, err
	}
	if cfg.Ledger.Adapter == "sqlite" {
		cfg.Ledger.Path = resolvePath(cfg.Ledger.Path, filepath.Dir(configPath))
	}
	return cfg.Ledger, nil
}

func decodeKnownFields(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("config file must contain exactly one YAML document")
	}
	return nil
}

func Default() App {
	return App{
		Server: Server{
			Host: "127.0.0.1",
			Port: 8920,
		},
		Ledger: Ledger{
			Adapter: "sqlite",
		},
		Storage: Storage{
			Adapter: "filesystem",
		},
		Ingest: Ingest{
			RejectHashConflicts: true,
			RequirePayloadHash:  true,
			RequireEventHash:    true,
		},
		Replay: Replay{
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
	}
}

func FromEnv() (App, error) {
	if path := os.Getenv("SIGINT_CONFIG"); path != "" {
		return Load(path)
	}
	return App{}, errors.New("SIGINT_CONFIG is required")
}

func (a App) validate() error {
	if a.Server.Host == "" {
		return errors.New("server.host is required")
	}
	if a.Server.Port < 1 || a.Server.Port > 65535 {
		return errors.New("server.port must be between 1 and 65535")
	}
	if err := a.validateLedger(); err != nil {
		return err
	}
	if err := a.validateStorage(); err != nil {
		return err
	}
	if a.Replay.DefaultLimit < 1 {
		return errors.New("replay.default_limit must be at least 1")
	}
	if a.Replay.MaxLimit < a.Replay.DefaultLimit {
		return errors.New("replay.max_limit must be greater than or equal to replay.default_limit")
	}
	if strings.TrimSpace(a.Retention.HotWindow) != "" {
		return errors.New("retention.hot_window is not supported; run cursor-based retention with --through-cursor")
	}
	return nil
}

func (a App) validateLedger() error {
	switch a.Ledger.Adapter {
	case "sqlite":
		if a.Ledger.Path == "" {
			return errors.New("ledger.path is required")
		}
	case "postgres":
		if a.Ledger.DSN == "" {
			return errors.New("ledger.dsn is required")
		}
	default:
		return errors.New("ledger.adapter must be sqlite or postgres")
	}
	return nil
}

func (a App) validateStorage() error {
	switch a.Storage.Adapter {
	case "filesystem":
		if a.Storage.Root == "" {
			return errors.New("storage.root is required")
		}
	case "s3":
		if a.Storage.Bucket == "" {
			return errors.New("storage.bucket is required")
		}
		if a.Storage.Region == "" {
			return errors.New("storage.region is required")
		}
	default:
		return errors.New("storage.adapter must be filesystem or s3")
	}
	return nil
}

func (a *App) expandEnv() error {
	a.Ledger.Path = os.ExpandEnv(a.Ledger.Path)
	a.Ledger.DSN = os.ExpandEnv(a.Ledger.DSN)
	a.Storage.Root = os.ExpandEnv(a.Storage.Root)
	a.Storage.Bucket = os.ExpandEnv(a.Storage.Bucket)
	a.Storage.Prefix = os.ExpandEnv(a.Storage.Prefix)
	a.Storage.Region = os.ExpandEnv(a.Storage.Region)
	a.Storage.Endpoint = os.ExpandEnv(a.Storage.Endpoint)
	a.Storage.ServerSideEncryption = os.ExpandEnv(a.Storage.ServerSideEncryption)
	a.Storage.KMSKeyID = os.ExpandEnv(a.Storage.KMSKeyID)
	producerToken, err := expandOptionalSecret(a.Auth.ProducerToken, "auth.producer_token")
	if err != nil {
		return err
	}
	internalToken, err := expandOptionalSecret(a.Auth.InternalToken, "auth.internal_token")
	if err != nil {
		return err
	}
	a.Auth.ProducerToken = producerToken
	a.Auth.InternalToken = internalToken
	if a.Auth.ProducerTokenEnv != "" {
		token, err := tokenFromEnv(a.Auth.ProducerTokenEnv, "auth.producer_token_env")
		if err != nil {
			return err
		}
		a.Auth.ProducerToken = token
	}
	if a.Auth.InternalTokenEnv != "" {
		token, err := tokenFromEnv(a.Auth.InternalTokenEnv, "auth.internal_token_env")
		if err != nil {
			return err
		}
		a.Auth.InternalToken = token
	}
	return nil
}

func expandOptionalSecret(value string, field string) (string, error) {
	if value == "" {
		return "", nil
	}
	missing := []string{}
	expanded := os.Expand(value, func(name string) string {
		resolved, ok := os.LookupEnv(name)
		if !ok || resolved == "" {
			missing = append(missing, name)
			return ""
		}
		return resolved
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%s references unset or empty environment variable %s", field, strings.Join(missing, ","))
	}
	if strings.TrimSpace(expanded) == "" {
		return "", fmt.Errorf("%s must not resolve to an empty token", field)
	}
	return expanded, nil
}

func tokenFromEnv(envName string, field string) (string, error) {
	token, ok := os.LookupEnv(envName)
	if !ok || strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("%s references unset or empty environment variable %s", field, envName)
	}
	return token, nil
}

func resolvePath(path, baseDir string) string {
	path = filepath.Clean(os.ExpandEnv(path))
	if filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(filepath.Join(baseDir, path))
	if err != nil {
		return filepath.Join(baseDir, path)
	}
	return abs
}
