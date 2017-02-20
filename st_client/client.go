package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"github.com/asankah/stonesthrow"
)

func main() {
	defaultServerPlatform := path.Base(os.Args[0])
	defaultConfig := stonesthrow.GetDefaultConfigFile()

	serverPlatform := flag.String("server", defaultServerPlatform, "Server platform.")
	repository := flag.String("repository", "", "Repository")
	porcelain := flag.Bool("porcelain", false, "Porcelain.")
	configFile := flag.String("config", defaultConfig, "Configuration file")
	showConfig := flag.Bool("show_config", false, "Display configuration and exit")
	showColors := flag.Bool("show_colors", false, "Displaye a test color pattern")
	flag.Parse()

	if *showColors {
		WriteTestString()
		return
	}

	if *serverPlatform == "" {
		log.Fatal("Need to specify a platform")
		os.Exit(1)
	}

	var clientConfig, serverConfig stonesthrow.Config
	err := serverConfig.ReadServerConfig(*configFile, *serverPlatform, *repository)
	if err != nil {
		log.Fatal(err.Error())
	}

	err = clientConfig.ReadClientConfig(*configFile, *serverPlatform, *repository)
	if err != nil {
		log.Fatal(err.Error())
	}

	if *showConfig {
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

	var executor stonesthrow.ConsoleExecutor
	var req stonesthrow.RequestMessage
	req.Command = arguments[0]
	req.Arguments = arguments[1:]
	req.Repository = *repository
	req.Revision, err = clientConfig.Repository.GitCreateBuilderHead(executor)
	if err != nil {
		log.Fatal(err.Error())
	}

	formatter := ConsoleFormatter{config: &serverConfig, porcelain: *porcelain}
	output := make(chan interface{})
	go func() {
		for message := range output {
			formatter.Format(message)
		}
	}()

	err = stonesthrow.RunClient(serverConfig, req, output)
	if err != nil {
		log.Fatalf("Client failed: %#v", err)
	}
}
