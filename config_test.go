package stonesthrow

import (
	"path/filepath"
	"testing"
)

func TestConfig_MergeFrom(t *testing.T) {
	var c1, c2 Config

	c2.Hostname = "A"
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("Hostname")
	}

	c2.SourcePath = "B"
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("SourcePath")
	}

	c2.GomaPath = "B"
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("GomaPath")
	}

	c2.BuildPath = "B"
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("BuildPath")
	}

	c2.ServerPort = 8
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("ServerPort")
	}

	c2.RelativeBuildPath = "a"
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("RelativeBuildPath")
	}

	c2.MbConfigName = "a"
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("MbConfigName")
	}

	c2.MasterHostname = "a"
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("MasterHostname")
	}

	c2.GitRemote = "a"
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("GitRemote")
	}

	c2.MaxBuildJobs = 8
	c1.MergeFrom(&c2)
	if c1 != c2 {
		t.Error("MaxBuildJobs")
	}

	// Platform should not be merged.
	c2.Platform = "a"
	c1.MergeFrom(&c2)
	if c1 == c2 {
		t.Error("Platform")
	}

}

func TestConfig_IsValid(t *testing.T) {
	validConfig := Config{
		Hostname:          "a",
		SourcePath:        "b",
		BuildPath:         "c",
		ServerPort:        8,
		RelativeBuildPath: "d",
		MbConfigName:      "e",
		Platform:          "f",
		MasterHostname:    "g",
		GitRemote:         "h",
		MaxBuildJobs:      8}

	// c as defined above, should be valid.
	if !validConfig.IsValid() {
		t.Fatal()
	}

	c := validConfig
	c.Hostname = ""
	if c.IsValid() {
		t.Fatal("Hostname")
	}

	c = validConfig
	c.ServerPort = 0
	if c.IsValid() {
		t.Fatal("ServerPort")
	}

	c = validConfig
	c.GitRemote = ""
	if c.IsValid() {
		t.Fatal("GitRemote")
	}

	c.MasterHostname = c.Hostname
	if !c.IsValid() {
		t.Fatal("GitRemote required on master")
	}
}

func TestConfig_ReadFrom(t *testing.T) {
	var c Config
	path := filepath.Join("testdata", "config-basic.json")
	err := c.ReadFrom(path, "linux")
	if err != nil {
		t.Fatal(err)
	}

	if c.ConfigurationFile != path {
		t.Fatal("COnfigurationFile")
	}

	if c.Platform != "linux" {
		t.Fatal("Platform")
	}

	if !c.IsMaster() {
		t.Fatal("IsMaster")
	}

	if c.GitRemote != "" {
		t.Fatal("GitRemote")
	}

	if c.SourcePath != "/src" {
		t.Fatal("SourcePath")
	}
}
