package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"stonesthrow/stonesthrow"
)

func main() {
	platform := flag.String("platform", "", "Platform to use. See code for valid platform values.")
	configFile := flag.String("config", stonesthrow.GetDefaultConfigFile(), "Configuration file to use.")
	flag.Parse()

	if *platform == "" {
		log.Fatal("Need to specify the platform")
	}
	if *configFile == "" {
		log.Fatal("Need a configuration file")
	}

	var config stonesthrow.Config
	err := config.ReadFrom(*configFile, *platform)
	if err != nil {
		log.Fatal(err.Error())
	}

	arguments := flag.Args()
	if len(arguments) != 0 {
		switch arguments[0] {
		case "show_port":
			fmt.Println(config.GetPort())
			os.Exit(1)
			return

		default:
			log.Fatalf("Unknown option %s.", arguments[0])
			return
		}
	}

	log.Printf("Starting server for %s on port %d. This is PID %d", config.Platform, config.ServerPort, os.Getpid())
	server := stonesthrow.Server{}
	reload := false

	stonesthrow.AddHandler("reload", "Reload and rebuild the server library.",
		func(s *stonesthrow.Session, req stonesthrow.RequestMessage) error {
			server.Quit()
			reload = true
			return nil
		})

	server.Run(config)

	if reload {
		packagePath, err := stonesthrow.GetPackageRootPath()
		if err != nil {
			fmt.Printf("Can't determine package root path")
			os.Exit(1)
		}
		log.Print("Launching st_reload to reload and update.")
		cmd := exec.Command("st_reload", "--pid", fmt.Sprintf("%d", os.Getpid()),
			"--platform", *platform,
			"--packagepath", packagePath,
			"--config", *configFile,
			fmt.Sprintf("--update=%t", !config.IsMaster))
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Start()
		if err != nil {
			os.Exit(1)
		}
	}
}
