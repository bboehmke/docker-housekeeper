package main

import (
	"errors"
	"os"
	"reflect"
	"strings"

	"github.com/spf13/cast"
)

type DatabaseConfig struct {
	Host string `conf:"DB_HOST"`
	Port int    `conf:"DB_PORT,5432"`

	RootUsername string `conf:"DB_ROOT_USER,postgres"`
	RootPassword string `conf:"DB_ROOT_PASSWORD"`

	Username string `conf:"DB_USER_NAME"`
	Password string `conf:"DB_USER_PASSWORD"`
	Database string `conf:"DB_DATABASE"`

	PgExtensions string `conf:"DB_PG_EXTENSIONS"`
}

type BackupConfig struct {
	Database               bool   `conf:"BACKUP_DATABASE,false"`
	DataDirectories        string `conf:"BACKUP_DATA_DIR"`
	DataDirectoriesExclude string `conf:"BACKUP_DATA_EXCLUDE"`

	Schedule string `conf:"BACKUP_SCHEDULE,@daily"`

	Storage string `conf:"BACKUP_STORAGE,/backup"`
}

type Config struct {
	Database DatabaseConfig
	Backup   BackupConfig
}

// validate configuration
func (c *Config) validate() error {
	db := c.Database
	if db.Host != "" {
		if db.Username == "" {
			return errors.New("database host given but username is missing")
		}
		if db.Password == "" {
			return errors.New("database host given but user password is missing")
		}
		if db.Database == "" {
			return errors.New("database host given but database name is missing")
		}
	}

	if c.Backup.Database && db.Host == "" {
		return errors.New("database config missing for backup")
	}

	return nil
}

// LoadConfig from environment
func LoadConfig() (Config, error) {
	var conf Config
	loadStruct(reflect.ValueOf(&conf).Elem())
	return conf, conf.validate()
}

func loadStruct(st reflect.Value) {
	for i := 0; i < st.NumField(); i++ {
		field := st.Field(i)
		fieldType := st.Type().Field(i)

		// load sub structures
		if fieldType.Type.Kind() == reflect.Struct {
			loadStruct(field)
			continue
		}

		// get conf tag and skip this field if tag does not exist
		tag, ok := fieldType.Tag.Lookup("conf")
		if !ok {
			continue
		}
		splitTag := strings.Split(tag, ",")

		// check if default value exists
		var defaultValue string
		if len(splitTag) > 1 {
			defaultValue = splitTag[1]
		}

		// get value from env
		value, valueGiven := os.LookupEnv(splitTag[0])

		// set value in struct
		switch fieldType.Type.Kind() {
		case reflect.String:
			if valueGiven {
				field.SetString(value)
			} else {
				field.SetString(defaultValue)
			}
		case reflect.Int:
			if valueGiven {
				field.SetInt(cast.ToInt64(value))
			} else {
				field.SetInt(cast.ToInt64(defaultValue))
			}
		case reflect.Bool:
			if valueGiven {
				field.SetBool(cast.ToBool(value))
			} else {
				field.SetBool(cast.ToBool(defaultValue))
			}

		default:
			panic("unsupported struct field type")
		}
	}
}
