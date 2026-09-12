package outline

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Environment variables read by FromEnv and ConfigPath.
const (
	envURL    = "OUTLINE_URL"
	envAPIKey = "OUTLINE_API_KEY"
	envConfig = "OUTLINE_CONFIG"
)

type config struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
}

// ConfigPath returns OUTLINE_CONFIG or the OS user config directory's
// outline/config.json. Resolving the path does not read credentials.
func ConfigPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv(envConfig)); path != "" {
		return path, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate Outline config: %w; set %s or both %s and %s", err, envConfig, envURL, envAPIKey)
	}
	return filepath.Join(dir, "outline", "config.json"), nil
}

// FromEnv resolves explicit environment values ahead of the optional config.
// A saved API key is used only with its saved workspace URL. A complete env
// pair bypasses the default config; an explicitly selected config is checked.
func FromEnv() (*Client, error) {
	envURLValue := strings.TrimSpace(os.Getenv(envURL))
	envKey := strings.TrimSpace(os.Getenv(envAPIKey))
	explicitConfig := strings.TrimSpace(os.Getenv(envConfig)) != ""
	var saved config
	if envURLValue == "" || envKey == "" || explicitConfig {
		var err error
		saved, err = readConfig(explicitConfig)
		if err != nil {
			return nil, err
		}
	}
	baseURL, err := workspaceURL(cmp.Or(envURLValue, saved.URL))
	if err != nil {
		return nil, err
	}
	token := cmp.Or(envKey, strings.TrimSpace(saved.APIKey))
	if token == "" {
		return nil, fmt.Errorf("no API key: set %s or apiKey in the Outline config file", envAPIKey)
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return nil, fmt.Errorf("API key must not contain whitespace")
	}
	if envKey == "" {
		savedURL, err := workspaceURL(saved.URL)
		if err != nil {
			return nil, fmt.Errorf("a saved apiKey requires a valid url in the same config file")
		}
		if baseURL != savedURL {
			return nil, fmt.Errorf("%s differs from the saved workspace; also set %s or select a matching %s", envURL, envAPIKey, envConfig)
		}
	}
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func readConfig(explicit bool) (config, error) {
	var saved config
	path, err := ConfigPath()
	if err != nil {
		return saved, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		return saved, nil
	}
	if err != nil {
		return saved, fmt.Errorf("reading Outline config: %w", err)
	}
	data = bytes.TrimSpace(data)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) == 0 || data[0] != '{' || decoder.Decode(&saved) != nil {
		return saved, fmt.Errorf("invalid Outline config %s: expected a JSON object with url and apiKey string fields", path)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return saved, fmt.Errorf("invalid Outline config %s: expected a single JSON object", path)
	}
	return saved, nil
}

// Validate without echoing the input: a mistyped URL can itself contain secrets.
// Keep the scheme, port and escaped base path in the workspace identity so a
// URL override cannot silently send a saved token elsewhere or downgrade TLS.
func workspaceURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("no workspace URL: set OUTLINE_URL or url in the Outline config file; there is no default workspace")
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("workspace URL must be an absolute http:// or https:// URL with a hostname")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(value, "#") {
		return "", fmt.Errorf("workspace URL must not contain credentials, a query, or a fragment; use the workspace base URL, not an API or MCP link")
	}
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/"), nil
}
