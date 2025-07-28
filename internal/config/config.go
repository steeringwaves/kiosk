package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type TabConfig struct {
	URL               string `json:"URL" yaml:"url"`
	RefreshBeforeLoad bool   `json:"RefreshBeforeLoad" yaml:"refreshBeforeLoad"`
	RefreshAfterLoad  bool   `json:"RefreshAfterLoad" yaml:"refreshAfterLoad"`
	RefreshInterval   int    `json:"RefreshInterval" yaml:"refreshInterval"`
	DelayAfterRefresh int    `json:"DelayAfterRefresh" yaml:"delayAfterRefresh"`
	DwellTime         int    `json:"DwellTime" yaml:"dwellTime"`
}

type DisplayConfig struct {
	Name       string      `json:"Name" yaml:"name"`
	DebugPort  int         `json:"DebugPort" yaml:"debugPort"`
	X          int         `json:"X" yaml:"x"`
	Y          int         `json:"Y" yaml:"y"`
	Fullscreen bool        `json:"Fullscreen" yaml:"fullscreen"`
	Tabs       []TabConfig `json:"Tabs" yaml:"tabs"`
}

type Config struct {
	DwellTime     int             `json:"DwellTime" yaml:"dwellTime"`
	DebugPort     int             `json:"DebugPort" yaml:"debugPort"`
	NewWindowSize string          `json:"NewWindowSize" yaml:"newWindowSize"`
	Displays      []DisplayConfig `json:"Displays" yaml:"displays"`
}

func Load(cfg *Config, filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	fileExt := filepath.Ext(filename)
	if fileExt == ".yaml" || fileExt == ".yml" {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("failed to read config file %s: %w", filename, err)
		}
	} else {
		if err := json.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("failed to read config file %s: %w", filename, err)
		}
	}
	return nil
}

func (c *Config) Save(filename string) error {
	fileExt := filepath.Ext(filename)
	var data []byte
	var err error

	if fileExt == ".yaml" || fileExt == ".yml" {
		data, err = yaml.Marshal(c)
		if err != nil {
			return fmt.Errorf("failed to marshal config to YAML: %w", err)
		}
	} else {
		data, err = json.MarshalIndent(c, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", filename, err)
	}
	return nil
}

func (c *Config) IndexOfDisplay(name string) int {
	for i, d := range c.Displays {
		if d.Name == name {
			return i
		}
	}
	return -1
}

func (c *Config) NextDebugPort() int {
	highestPort := 9000
	for _, d := range c.Displays {
		if d.DebugPort > highestPort {
			highestPort = d.DebugPort
		}
	}

	return highestPort + 1
}

func (c *Config) NextDisplayName() string {
	if len(c.Displays) == 0 {
		return "Display0"
	}

	last := c.Displays[len(c.Displays)-1].Name

	// Fetch the last number from the display name using regex
	re := regexp.MustCompile(`\d+$`)
	matches := re.FindStringSubmatch(last)
	if len(matches) == 0 {
		return last + "-1"
	}
	lastNum, err := strconv.Atoi(matches[0])
	if err != nil {
		log.Printf("Error parsing last display number: %v", err)
		return last + "-1"
	}
	return fmt.Sprintf("%s%d", strings.TrimSuffix(last, matches[0]), lastNum+1)
}
