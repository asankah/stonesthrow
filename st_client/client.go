package main

import (
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

	err = serverConfig.SelectServerConfig(&configFile, *serverPlatform, *repository)
	if err != nil {
		log.Fatal(err.Error())
	}

	if *passthrough {
		err = stonesthrow.RunPassthroughClient(serverConfig)
		if err != nil {
			log.Fatalf("Client failed : %#v", err)
		}
		return
	}

	err = clientConfig.SelectClientConfig(&configFile, *serverPlatform, *repository)
	if err != nil {
		log.Fatal(err.Error())
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

	executor := stonesthrow.ConsoleExecutor{HideStdout: false}
	var req stonesthrow.RequestMessage
	req.Command = arguments[0]
	req.Arguments = arguments[1:]
	req.Repository = *repository
	req.Revision, err = clientConfig.Repository.GitCreateBuilderHead(executor)
	if err != nil {
		log.Fatal(err.Error())
	}

	formatter := ConsoleFormatter{config: &serverConfig, porcelain: *porcelain}
	done := make(chan int)
	output := make(chan interface{})
	go func() {
		for message := range output {
			formatter.Format(message)
		}
		done <- 0
	}()

	err = stonesthrow.RunClient(executor, clientConfig, serverConfig, req, output)
	if err != nil {
		log.Fatalf("Client failed: %#v", err)
	}
	<- done
}
