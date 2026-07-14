package config

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	BaseURL      string `json:"baseURL"`
	UserID       string `json:"userID"`
	CredentialID string `json:"credentialID"`
	Label        string `json:"label"`
}

func Dir() string {
	if d := os.Getenv("TSCLOUD_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tscloud")
}

func Save(cfg *Config, leafDER []byte, interDER [][]byte) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "leaf.der"), leafDER, 0o600); err != nil {
		return err
	}
	for i, d := range interDER {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("intermediate%d.der", i)), d, 0o600); err != nil {
			return err
		}
	}
	// Prune any stale intermediate files left over from a previous, larger
	// save (e.g. a shrinking re-save from 2 intermediates to 1), so Load()
	// never picks up a leftover cert as current.
	for j := len(interDER); ; j++ {
		path := filepath.Join(dir, fmt.Sprintf("intermediate%d.der", j))
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				break
			}
			return err
		}
	}
	return nil
}

func Load() (*Config, *x509.Certificate, []*x509.Certificate, error) {
	dir := Dir()
	var cfg Config
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, nil, nil, err
	}
	leafDER, err := os.ReadFile(filepath.Join(dir, "leaf.der"))
	if err != nil {
		return nil, nil, nil, err
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return nil, nil, nil, err
	}
	var inter []*x509.Certificate
	for i := 0; ; i++ {
		d, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("intermediate%d.der", i)))
		if err != nil {
			break
		}
		if c, err := x509.ParseCertificate(d); err == nil {
			inter = append(inter, c)
		}
	}
	return &cfg, leaf, inter, nil
}
