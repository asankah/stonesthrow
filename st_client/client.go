package main

import (
	"context"
	"github.com/asankah/stonesthrow"
)

func main() {
	stonesthrow.InvokeCommandline(context.Background(), func(config stonesthrow.Config) stonesthrow.OutputSink {
		return &ConsoleFormatter{config: &config}
	})
}
