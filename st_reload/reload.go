package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func RunCommandAndWait(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	parentProcess := flag.Int("pid", -1,
		"Process ID of parent. Waits for the process with this ID to exit before doing anything.")
	goPackage := flag.String("package", "",
		"Path to Go package that will be updated. Runs 'go get -u <package>'.")
	command := flag.String("command", "",
		"Command to run after updating the package at <package>. Remaining arguments are passed along to <command>.")
	flag.Parse()

	if *parentProcess == -1 || *command == "" || *goPackage == "" {
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

	*goPackage = filepath.Clean(*goPackage)
	packageToBuild := filepath.Base(*goPackage) + "/..."

	err = RunCommandAndWait("go", "get", "-u", packageToBuild)
	if err != nil {
		fmt.Printf(err)
		os.Exit(1)
	}

	cmd := exec.Command(*command, flag.Args()...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		os.Exit(1)
	}
	return
}
