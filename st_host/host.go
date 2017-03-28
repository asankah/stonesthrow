package main

import (
	"flag"
	"github.com/asankah/stonesthrow"
	"log"
	"os"
)

func main() {
	platform := flag.String("platform", "", "Platform to use. See code for valid platform values.")
	repository := flag.String("repository", "", "Repository to use.")
	configFileName := flag.String("config", stonesthrow.GetDefaultConfigFile(), "Configuration file to use.")
	flag.Parse()

	if *platform == "" || *configFileName == "" {
		flag.Usage()
		return
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

	err = stonesthrow.RunServer(config)
	if err != nil {
		log.Printf("Failed to start server: %s", err.Error())
		os.Exit(1)
	}
}
