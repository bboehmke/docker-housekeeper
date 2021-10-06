package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"time"
)

func main() {
	log.Print("Load config")
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// prepare database connection
	var pg *PostgresConnection
	if config.Database.Host != "" {
		// connect to database
		log.Print("Wait for database connection")
		pg = NewPostgresConnection(config.Database)

		err = pg.WaitForConnection(time.Minute)
		if err != nil {
			log.Fatal(err)
		}

		// initialize database
		err = pg.Init()
		if err != nil {
			log.Fatal(err)
		}
	}

	backup := BackupService{
		Config:   config.Backup,
		Database: pg,
	}

	// prepare for backup
	err = backup.Prepare()
	if err != nil {
		log.Fatal(err)
	}

	// handle special actions
	var action string
	if len(os.Args) > 1 {
		action = strings.ToLower(os.Args[1])
	}

	switch action {
	case "": // no action -> default cron mode
		break

	case "backup": // manual backup
		err = backup.Backup()
		if err != nil {
			log.Fatal(err)
		}
		return
	default:
		log.Fatal("unknown action")
		return
	}

	// start backup schedule
	err = backup.StartSchedule()
	if err != nil {
		log.Fatal(err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	backup.StopSchedule(time.Minute * 5)
}
