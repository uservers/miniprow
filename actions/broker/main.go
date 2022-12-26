package main

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/uservers/miniprow/pkg/miniprow"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-s" {
		os.Setenv("MINIPROW_EVENT", "COMMENT")
		os.Setenv("MINIPROW_TOKEN", os.Getenv("GITHUB_TOKEN"))
		//	os.Setenv("MINIPROW_COMMENT", "805368288")
		os.Setenv("MINIPROW_COMMENT", "806320613")
		os.Setenv("MINIPROW_REPO", "uServers/ulabs-infrastructure")
		// os.Setenv("MINIPROW_ISSUE", "839225055")
		os.Setenv("MINIPROW_PR", "64")
	}

	broker, err := miniprow.NewBroker()
	if err != nil {
		logrus.Error(fmt.Errorf("creating MiniProw broker: %w", err))
		os.Exit(1)
	}
	if err := broker.Run(); err != nil {
		logrus.Error(fmt.Errorf("miniprow broker run returned error: %w", err))
		os.Exit(1)
	}
}
