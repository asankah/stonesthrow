package stonesthrow

import (
	"path/filepath"
	"testing"
)

func TestConfig_ReadFrom(t *testing.T) {
	var c Config
	path := filepath.Join("testdata", "config-basic.json")
	err := c.ReadServerConfig(path, "linux", "")
	if err != nil {
		t.Fatal(err)
	}

	if c.ConfigurationFile != path {
		t.Fatal("ConfigurationFile")
	}

	if c.PlatformName != "linux" {
		t.Fatal("Platform")
	}

	err = c.ReadServerConfig(path, "chromeos", "")
	if err == nil {
		t.Fatal("Should've failed to load non-existent platform")
	}
}
