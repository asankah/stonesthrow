package main

import (
	"flag"
	"fmt"
	"github.com/asankah/stonesthrow"
	"log"
	"os"
	"os/exec"
)

func main() {
	platform := flag.String("platform", "", "Platform to use. See code for valid platform values.")
	repository := flag.String("repository", "", "Repository to use.")
	configFileName := flag.String("config", stonesthrow.GetDefaultConfigFile(), "Configuration file to use.")
	flag.Parse()

	if *platform == "" {
		log.Fatal("Need to specify the platform")
	}
	if *configFileName == "" {
		log.Fatal("Need a configuration file")
	}

	var configFile stonesthrow.ConfigurationFile
	err := configFile.ReadFrom(*configFileName)
	if err != nil {
		log.Fatal(err.Error())
	}

	var config stonesthrow.Config
	err = config.SelectServerConfig(&configFile, *platform, *repository)
	if err != nil {
		log.Fatal(err.Error())
	}

	arguments := flag.Args()
	if len(arguments) != 0 {
		switch arguments[0] {
		case "show_config":
			config.Dump(os.Stdout)
			os.Exit(1)
			return

		default:
			log.Fatalf("Unknown argument %s.", arguments[0])
			return
		}
	}

	log.Printf("Starting server for %s at %s. This is PID %d", config.PlatformName, config.Platform.Address, os.Getpid())
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
		log.Print("Launching st_reload to reload and update.")
		cmd := exec.Command("st_reload",
			"--pid", fmt.Sprintf("%d", os.Getpid()),
			"--package", "github.com/asankah/stonesthrow",
			"st_host", "--platform", config.PlatformName,
			"--config", config.ConfigurationFile.FileName,
			"--repository", config.RepositoryName)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Start()
		if err != nil {
			os.Exit(1)
		}
	}
}
