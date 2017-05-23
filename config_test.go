package stonesthrow

import (
	"path/filepath"
	"testing"
)

func TestConfig_ReadFrom(t *testing.T) {
	var cf ConfigurationFile
	var c Config
	path := filepath.Join("testdata", "config-basic.json")

	err := cf.ReadFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	err = c.SelectServerConfig(&cf, "linux", "chrome")
	if err != nil {
		t.Fatal(err)
	}

	if c.ConfigurationFile != &cf {
		t.Fatal("ConfigurationFile")
	}

	if c.PlatformName != "linux" {
		t.Fatal("Platform")
	}

	err = c.SelectServerConfig(&cf, "chromeos", "chrome")
	if err == nil {
		t.Fatal("Should've failed to load non-existent platform")
	}
}
