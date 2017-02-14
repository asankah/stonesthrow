package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func RunCommandAndWait(workDir string, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	programToRun := "st_host"

	parentProcess := flag.Int("pid", -1, "Process ID of parent.")
	platform := flag.String("platform", "", "Platform to use when restarting.")
	packagePath := flag.String("packagepath", "", "Path to package that will be updated")
	configFile := flag.String("config", "", "Config to use when restarting.")
	shouldUpdate := flag.Bool("update", true, "Update?")
	flag.Parse()

	if *parentProcess == -1 || *platform == "" || *configFile == "" || *packagePath == "" {
		fmt.Println("Invalid command line arguments")
		os.Exit(1)
	}

	proc, err := os.FindProcess(*parentProcess)
	if err != nil {
		fmt.Printf("Parent process %d not found. Proceeding anyway.")
	} else {
		proc.Wait()
		proc.Release()
	}

	*packagePath = filepath.Clean(*packagePath)

	if *shouldUpdate {
		err = RunCommandAndWait(*packagePath, "git", "pull", "origin", "master")
		if err != nil {
			os.Exit(1)
		}
	}

	packageToBuild := filepath.Base(*packagePath) + "/..."

	err = RunCommandAndWait(*packagePath, "go", "install", packageToBuild)
	if err != nil {
		os.Exit(1)
	}

	cmd := exec.Command(programToRun, "--platform", *platform, "--config", *configFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		os.Exit(1)
	}
	return
}
