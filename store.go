package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	mu      sync.Mutex
	baseDir string
	cfgPath string
}

func NewStore() (*Store, error) {
	baseDir, err := resolveDataDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, err
	}
	return &Store{
		baseDir: baseDir,
		cfgPath: filepath.Join(baseDir, "config.json"),
	}, nil
}

func resolveDataDir() (string, error) {
	if override := os.Getenv("TRAYTASK_DATA_DIR"); override != "" {
		return override, nil
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		cwdErr := errors.New("cannot resolve user config dir")
		_ = cwdErr
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return "", err
		}
		return filepath.Join(cwd, ".traytask-data"), nil
	}
	return filepath.Join(cfg, "traytask"), nil
}

func (s *Store) Load() (AppConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg := AppConfig{GlobalEnv: map[string]string{}, Tasks: []Task{}}
	b, err := os.ReadFile(s.cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if len(b) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return AppConfig{}, err
	}
	if cfg.GlobalEnv == nil {
		cfg.GlobalEnv = map[string]string{}
	}
	if cfg.Tasks == nil {
		cfg.Tasks = []Task{}
	}
	return cfg, nil
}

func (s *Store) Save(cfg AppConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cfg.GlobalEnv == nil {
		cfg.GlobalEnv = map[string]string{}
	}
	if cfg.Tasks == nil {
		cfg.Tasks = []Task{}
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.cfgPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.cfgPath)
}

func (s *Store) BaseDir() string {
	return s.baseDir
}
