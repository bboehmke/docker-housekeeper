package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/cast"
)

// PostgresConnection used for initialization and backup
type PostgresConnection struct {
	Config DatabaseConfig

	// used for initial setup and connection check
	ConnectionString string
}

// NewPostgresConnection from the given configuration
func NewPostgresConnection(config DatabaseConfig) *PostgresConnection {
	conf := PostgresConnection{
		Config: config,
	}

	// if root password is missing create connection from user credentials
	if config.RootPassword != "" {
		conf.ConnectionString = fmt.Sprintf("postgres://%s:%s@%s:%d?sslmode=disable",
			config.RootUsername, config.RootPassword,
			config.Host, config.Port)
	} else {
		conf.ConnectionString = fmt.Sprintf("postgres://%s:%s@%s:%d?sslmode=disable",
			config.Username, config.Password,
			config.Host, config.Port)
	}
	return &conf
}

// WaitForConnection for a maximum of duration
func (c *PostgresConnection) WaitForConnection(duration time.Duration) error {
	db, err := sql.Open("postgres", c.ConnectionString)
	if err != nil {
		return fmt.Errorf("failed to create connection: %w", err)
	}
	defer db.Close()

	// ticker to check every second for a connection
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timeoutExceeded := time.After(duration)
	for {
		select {
		case <-timeoutExceeded:
			return errors.New("timeout while trying to connect to database")

		case <-ticker.C:
			err = db.Ping()
			if err == nil {
				return nil
			}
		}
	}
}

// Init database if root password is given
func (c *PostgresConnection) Init() error {
	if c.Config.RootPassword == "" {
		log.Print("no root password given -> skip user and database creation")
		return nil
	}
	log.Printf("initialize database ...")

	db, err := sql.Open("postgres", c.ConnectionString)
	if err != nil {
		return fmt.Errorf("failed to create connection: %w", err)
	}
	defer db.Close()

	var dummy string
	// create user if not exist
	err = db.QueryRow("SELECT usename FROM pg_catalog.pg_user WHERE usename = $1", c.Config.Username).Scan(&dummy)
	if err != nil {
		if err != sql.ErrNoRows {
			return fmt.Errorf("failed to check if user exists: %w", err)
		}
		_, err = db.Exec(fmt.Sprintf("CREATE ROLE %s with LOGIN CREATEDB PASSWORD '%s'", c.Config.Username, c.Config.Password))
		if err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
		log.Printf("> user %s created", c.Config.Username)
	} else {
		log.Printf("> user %s already exist", c.Config.Username)
	}

	// create database if not exist
	err = db.QueryRow("SELECT datname FROM pg_catalog.pg_database WHERE datname like $1", c.Config.Database).Scan(&dummy)
	if err != nil {
		if err != sql.ErrNoRows {
			return fmt.Errorf("failed to check if database exists: %w", err)
		}
		_, err = db.Exec(fmt.Sprintf("create database %s OWNER %s", c.Config.Database, c.Config.Username))
		if err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
		log.Printf("> database %s created", c.Config.Database)
	} else {
		log.Printf("> database %s already exist", c.Config.Database)
	}

	// ensure user has permissions in database
	_, err = db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s to %s", c.Config.Database, c.Config.Username))
	if err != nil {
		return fmt.Errorf("failed to grant database permissions: %w", err)
	}

	// add PG extensions
	if c.Config.PgExtensions != "" {
		_, err = db.Exec(fmt.Sprintf("CREATE EXTENSION %s", c.Config.PgExtensions))
		if err != nil {
			return fmt.Errorf("failed add extensions: %w", err)
		}
	}

	return nil
}

// Backup database to the given writer
func (c *PostgresConnection) Backup(writer io.Writer) error {
	cmd := exec.Command("pg_dump",
		"-h", c.Config.Host,
		"-p", cast.ToString(c.Config.Port),
		"-U", c.Config.Username,
		c.Config.Database)

	// set PGPASSWORD env variable
	env := os.Environ()
	env = append(env, "PGPASSWORD="+c.Config.Password)
	cmd.Env = env

	// redirect stdout to backup writer
	cmd.Stdout = writer
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
