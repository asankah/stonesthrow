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

	err = c.Select(&cf, "a.foo.example.com", "chrome", "linux")
	if err != nil {
		t.Fatal(err)
	}

	if c.ConfigurationFile != &cf {
		t.Fatal("ConfigurationFile")
	}

	if c.Platform.Name != "linux" {
		t.Fatal("Platform")
	}

	err = c.Select(&cf, "a.foo.example.com", "chrome", "chromeos")
	if err == nil {
		t.Fatal("Should've failed to load non-existent platform")
	}

	err = c.Select(&cf, "", "chrome", "linux")
	if err != nil {
		t.Fatal(err)
	}

	if !c.Host.IsSameHost("a") {
		t.Fatal("got the wrong host")
	}

	err = c.Select(&cf, "", "chrome", "win")
	if err != nil {
		t.Fatal(err)
	}

	if !c.Host.IsSameHost("c") {
		t.Fatal("got the wrong host")
	}
}

func TestConfig_SelectRepository(t *testing.T) {
	var cf ConfigurationFile
	var c Config
	path := filepath.Join("testdata", "config-basic.json")

	err := cf.ReadFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	err = c.Select(&cf, "a", "chrome", "linux")
	if err != nil {
		t.Fatal(err)
	}

	var d Config
	d.SetFromRepository(c.Repository)

	if d.ConfigurationFile != &cf {
		t.Fatal("ConfigurationFile")
	}

	if d.Host != c.Host {
		t.Fatal("Host")
	}

	if d.Repository != c.Repository {
		t.Fatal("Repository")
	}

	if d.Platform == nil {
		t.Fatal("Platform")
	}
}
