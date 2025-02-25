package main

import "github.com/mcyouyou/unicore/pkg/daemon"

func main() {
	d, err := daemon.New()
	if err != nil {
		panic(err)
	}
	err = d.Start()
	if err != nil {
		panic(err)
	}
}
