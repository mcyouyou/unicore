package main

import (
	"schedule_plugin/pkg/plugin/gang"

	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"
	scheduler "k8s.io/kubernetes/cmd/kube-scheduler/app"

	"os"
)

func main() {
	klog.Info("starting unicore-scheduler..")
	command := scheduler.NewSchedulerCommand(
		scheduler.WithPlugin("unicore-gang-scheduler", gang.New))
	code := cli.Run(command)
	os.Exit(code)
}
