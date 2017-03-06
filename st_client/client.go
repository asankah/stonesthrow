package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/asankah/stonesthrow"
	"log"
	"os"
	"os/exec"
	"path"
)

func main() {
	stonesthrow.InitializeCommands()

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
			log.Fatalf("Passthrough client failed : %#v, \nWhile attempting to contact: %s from %s",
				err, serverConfig.Host.Name, clientConfig.Host.Name)
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

	// Need to locally handle reload.
	if len(arguments) == 1 && arguments[0] == "reload" && clientConfig.Host == serverConfig.Host {
		cmd := exec.Command("st_reload",
			"--pid", fmt.Sprintf("%d", os.Getpid()),
			"--package", "github.com/asankah/stonesthrow")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Start()
		if err != nil {
			os.Exit(1)
		}
		return
	}

	executor := stonesthrow.ConsoleExecutor{HideStdout: false}
	var req stonesthrow.RequestMessage
	req.Command = arguments[0]
	req.Arguments = arguments[1:]
	req.Repository = *repository
	req.SourceHostname = clientConfig.Host.Name
	if serverConfig.Host != clientConfig.Host {
		commandHandler, ok := stonesthrow.GetHandlerForCommand(req.Command)
		if !ok || commandHandler.NeedsRevision() {
			if serverConfig.Repository.GitConfig.RemoteHost != clientConfig.Host {
				log.Println("Creating BUILDER_HEAD branch, but the server may not be able to fetch it.")
			}
			req.Revision, err = clientConfig.Repository.GitCreateBuilderHead(context.Background(), executor)
			if err != nil {
				log.Fatal(err.Error())
			}
		}
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

	err = stonesthrow.SendRequestToRemoteServer(executor, clientConfig, serverConfig, req, output)
	if err != nil {
		log.Fatalf("Client failed: %#v", err)
	}
	<-done
}
