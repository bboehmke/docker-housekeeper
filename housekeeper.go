package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/go-errors/errors"
)

const socket = "/tmp/housekeeper.socket"

// DatabaseConnection that is used for backup the database
type DatabaseConnection interface {
	Init() error
	WaitForConnection(duration time.Duration) error
	Backup(writer io.Writer) error
}

type Housekeeper struct {
	config Config

	db     DatabaseConnection
	backup *BackupService

	running atomic.Bool
}

// ServeHTTP handles health check
func (h *Housekeeper) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if h.running.Load() {
		writer.WriteHeader(http.StatusOK)
	} else {
		writer.WriteHeader(http.StatusNoContent)
	}
}

func (h *Housekeeper) StartHealthcheckServer() {
	// start http server
	go func() {
		unixListener, err := net.Listen("unix", socket)
		if err != nil {
			log.Fatalf("failed to create socket: %v", err)
		}
		log.Fatal(http.Serve(unixListener, h))
	}()
}

func (h *Housekeeper) Healthcheck() error {
	client := http.Client{
		Timeout: time.Millisecond * 100,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socket)
			},
		},
	}

	response, err := client.Get("http://unix" + socket)
	if err != nil {
		return err
	}

	if response.StatusCode != http.StatusOK {
		return errors.New("housekeeper not ready")
	}

	return nil
}

// LoadConfig from environment
func (h *Housekeeper) LoadConfig() error {
	log.Print("Load config")

	err := loadStruct(reflect.ValueOf(&h.config).Elem())
	if err != nil {
		return err
	}

	err = h.config.validate()
	if err != nil {
		return err
	}

	h.db = NewPostgresConnection(h.config.Database)

	h.backup = &BackupService{
		Config:   h.config.Backup,
		Database: h.db,
	}
	return nil
}

// Prepare database and backup
func (h *Housekeeper) Prepare() error {
	// start health check server
	h.StartHealthcheckServer()

	if h.db != nil {
		// connect to database
		log.Print("Wait for database connection")
		err := h.db.WaitForConnection(time.Minute)
		if err != nil {
			return err
		}

		// initialize database
		err = h.db.Init()
		if err != nil {
			return err
		}
	}

	if err := h.backup.Prepare(); err != nil {
		return err
	}

	h.running.Store(true)

	return nil
}
