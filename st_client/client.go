package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/asankah/stonesthrow"
	"log"
	"os"
	"path"
)

func main() {
	defaultServerPlatform := path.Base(os.Args[0])
	defaultConfig := stonesthrow.GetDefaultConfigFile()

	serverPlatform := flag.String("server", defaultServerPlatform, "Server platform.")
	repository := flag.String("repository", "", "Repository")
	porcelain := flag.Bool("porcelain", false, "Porcelain.")
	configFileName := flag.String("config", defaultConfig, "Configuration file")
	showConfig := flag.Bool("show_config", false, "Display configuration and exit")
	showColors := flag.Bool("show_colors", false, "Displaye a test color pattern")
	passthrough := flag.Bool("passthrough", false, "Passthrough mode")
	flag.Parse()

	if *showColors {
		WriteTestString()
		return
	}

	if *serverPlatform == "" {
		log.Fatal("Need to specify a platform")
		os.Exit(1)
	}

	var configFile stonesthrow.ConfigurationFile
	var clientConfig, serverConfig stonesthrow.Config
	err := configFile.ReadFrom(*configFileName)
	if err != nil {
		log.Fatal(err.Error())
	}

	err = serverConfig.SelectLocalServerConfig(&configFile, *serverPlatform, *repository)
	if err != nil {
		log.Fatal(err.Error())
	}

	err = clientConfig.SelectLocalClientConfig(&configFile, *serverPlatform, *repository)
	if err != nil {
		log.Fatal(err.Error())
	}

	if *passthrough {
		err = stonesthrow.RunPassthroughClient(clientConfig, serverConfig)
		if err != nil {
			log.Fatalf("Passthrough client failed : %#v, \nWhile attempting to contact: %s from %s\nError: %s",
				err, serverConfig.Host.Name, clientConfig.Host.Name, err.Error())
		}
		return
	}

	if *showConfig {
		if clientConfig.Host == serverConfig.Host {
			fmt.Println("Running locally")
		}
		fmt.Println("Client configuration:")
		clientConfig.Dump(os.Stdout)
		fmt.Println("Server configuration:")
		serverConfig.Dump(os.Stdout)
		return
	}

	arguments := flag.Args()
	if len(arguments) == 0 {
		log.Fatal("No arguments")
	}

	rpc_connection, err := stonesthrow.ConnectTo(context.Background(), clientConfig, serverConfig)
	if err != nil {
		log.Fatalf("Can't connect to remote. %#v (%s)", err, err.Error())
	}

	formatter := ConsoleFormatter{config: &serverConfig, porcelain: *porcelain}
	stonesthrow.InvokeCommandline(context.Background(), clientConfig, serverConfig, &formatter, rpc_connection, arguments...)
}
