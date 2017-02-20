package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"github.com/asankah/stonesthrow"
	"strings"
)

func getModifiedFiles(c *stonesthrow.Config) ([]string, error) {
	gitStatus, err := c.Repository.RunHere("git", "status", "--porcelain=2",
		"--untracked-files=no", "--ignore-submodules")
	if err != nil {
		return nil, err
	}

	modifiedFiles := []string{}
	scanner := bufio.NewScanner(strings.NewReader(gitStatus))
	for scanner.Scan() {
		text := scanner.Text()
		if strings.HasPrefix(text, "#") {
			continue
		}
		if strings.HasPrefix(text, "u ") {
			return nil, stonesthrow.UnmergedChangesExistError
		}
		// Normal changed entry.
		if strings.HasPrefix(text, "1 ") {
			fields := strings.Split(text, " ")
			if len(fields) < 9 || len(fields[1]) != 2 || fields[1][1] == '.' {
				continue
			}
			modifiedFiles = append(modifiedFiles, fields[8])
		}

		if strings.HasPrefix(text, "2 ") {
			fields := strings.Split(text, " ")
			if len(fields) < 10 || len(fields[1]) != 2 || fields[1][1] == '.' {
				continue
			}
			paths := strings.Split(fields[9], "\t")
			if len(paths) != 2 {
				continue
			}

			modifiedFiles = append(modifiedFiles, paths[0])
		}
	}

	return modifiedFiles, nil
}

func prepareBuilderHead(c *stonesthrow.Config) (string, error) {
	modifiedFiles, err := getModifiedFiles(c)
	if err != nil {
		return "", err
	}

	var tree string
	if len(modifiedFiles) > 0 {
		command := []string{"git", "update-index", "--"}
		command = append(command, modifiedFiles...)
		_, err = c.Repository.RunHere(command...)
		if err != nil {
			return "", err
		}

		tree, err = c.Repository.RunHere("git", "write-tree")
		if err != nil {
			return "", err
		}
	} else {
		tree, err = c.Repository.GitRevision("HEAD^{tree}")
		if err != nil {
			return "", err
		}
	}

	builderTree, err := c.Repository.GitRevision("BUILDER_HEAD^{tree}")
	if err != nil || builderTree != tree {
		headCommit, err := c.Repository.GitRevision("HEAD")
		if err != nil {
			return "", err
		}
		revision, err := c.Repository.RunHere("git", "commit-tree", "-p", headCommit, "-m", "BUILDER_HEAD", tree)
		if err != nil {
			return "", err
		}
		_, err = c.Repository.RunHere("git", "update-ref", "refs/heads/BUILDER_HEAD", revision)
		if err != nil {
			return "", err
		}
		log.Printf("Created BUILDER_HEAD %s", revision)
		return revision, nil
	} else {
		return c.Repository.GitRevision("BUILDER_HEAD")
	}
}

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

	var req stonesthrow.RequestMessage
	req.Command = arguments[0]
	req.Arguments = arguments[1:]
	req.Repository = "chromium"
	req.Revision, err = prepareBuilderHead(&clientConfig)
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
