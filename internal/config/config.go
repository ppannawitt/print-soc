package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const AppName = "socprint"
const ConfigDirEnv = "SOCPRINT_CONFIG_DIR"
const TermsVersion = "2"

type Settings struct {
	Username     string `json:"username,omitempty"`
	KeyPath      string `json:"key_path,omitempty"`
	TermsVersion string `json:"terms_version,omitempty"`
}

func Directory() (string, error) {
	path := os.Getenv(ConfigDirEnv)
	if path == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(root, AppName)
	} else {
		var err error
		path, err = filepath.Abs(path)
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0700); err != nil {
		return "", err
	}
	return path, nil
}

func SettingsPath() (string, error) {
	dir, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func Load() (Settings, error) {
	path, err := SettingsPath()
	if err != nil {
		return Settings{}, err
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return Settings{}, err
	}
	var value Settings
	err = json.Unmarshal(contents, &value)
	return value, err
}

func Save(value Settings) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "settings-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Chmod(path, 0600)
}
