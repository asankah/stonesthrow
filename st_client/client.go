package main

import (
	"context"
	"fmt"
	"github.com/asankah/stonesthrow"
)

func main() {
	err := stonesthrow.InvokeCommandline(context.Background(), func(config stonesthrow.Config) stonesthrow.OutputSink {
		return &ConsoleFormatter{config: &config}
	})

	if err != nil {
		fmt.Printf("%s", err.Error())
	}
}
