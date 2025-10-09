package main

import (
	"flag"
	"fmt"
	scheduler "k8s.io/kubernetes/cmd/kube-scheduler/app"
	"os"
)

func main() {
	command := scheduler.NewSchedulerCommand(
		scheduler.WithPlugin("example-plugin1", ExamplePlugin1),
		scheduler.WithPlugin("example-plugin2", ExamplePlugin2))
	flag.CommandLine.Parse([]string{})
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
