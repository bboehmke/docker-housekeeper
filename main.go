package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"time"
)

func main() {
	// handle special actions
	var action string
	if len(os.Args) > 1 {
		action = strings.ToLower(os.Args[1])
	}

	// handle health check early
	var housekeeper Housekeeper
	if action == "healthcheck" {
		err := housekeeper.Healthcheck()
		if err != nil {
			log.Fatalf("check failed: %v", err)
		}
		os.Exit(0)
	}

	// load config
	err := housekeeper.LoadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// prepare housekeeper
	err = housekeeper.Prepare()
	if err != nil {
		log.Fatal(err)
	}

	switch action {
	case "": // no action -> default cron mode
		break

	case "backup": // manual backup
		err = housekeeper.backup.Backup()
		if err != nil {
			log.Fatal(err)
		}
		return
	default:
		log.Fatal("unknown action")
		return
	}

	// start backup schedule
	err = housekeeper.backup.StartSchedule()
	if err != nil {
		log.Fatal(err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	housekeeper.backup.StopSchedule(time.Minute * 5)
}
