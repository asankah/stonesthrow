package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
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
	flag.Parse()

	if *parentProcess == -1 || *goPackage == "" || flag.NArg() == 0 {
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

	err = RunCommandAndWait("go", "get", "-u", *goPackage)
	if err != nil {
		fmt.Printf("Failed to run 'go get -u %s': %s", *goPackage, err.Error())
		os.Exit(1)
	}

	args := flag.Args()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		os.Exit(1)
	}
	return
}
