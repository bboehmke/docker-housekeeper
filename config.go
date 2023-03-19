package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"filippo.io/age"
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

	AgeRecipients []*age.X25519Recipient `conf:"BACKUP_AGE_RECIPIENTS"`
	AgePassword   *age.ScryptRecipient   `conf:"BACKUP_AGE_PASSWORD"`

	RClonePath   string `conf:"BACKUP_RCLONE_PATH"`
	RCloneConfig string `conf:"BACKUP_RCLONE_CONFIG"`
}

func (c *BackupConfig) ageRecipients() []age.Recipient {
	var recipients []age.Recipient
	for _, recipient := range c.AgeRecipients {
		recipients = append(recipients, recipient)
	}
	if c.AgePassword != nil {
		recipients = append(recipients, c.AgePassword)
	}
	return recipients
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

	if c.Backup.AgeRecipients != nil && c.Backup.AgePassword != nil {
		return errors.New("only age recipients OR a password is supported")
	}

	return nil
}

// LoadConfig from environment
func LoadConfig() (Config, error) {
	var conf Config
	err := loadStruct(reflect.ValueOf(&conf).Elem())
	if err != nil {
		return conf, err
	}
	return conf, conf.validate()
}

func loadStruct(st reflect.Value) error {
	for i := 0; i < st.NumField(); i++ {
		field := st.Field(i)
		fieldType := st.Type().Field(i)

		// load sub structures
		if fieldType.Type.Kind() == reflect.Struct {
			err := loadStruct(field)
			if err != nil {
				return err
			}
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

		case reflect.Slice:
			if !valueGiven {
				continue
			}

			if fieldType.Type.Elem() == reflect.TypeOf(new(age.X25519Recipient)) {
				var recipients []*age.X25519Recipient
				for _, key := range strings.Split(value, ",") {
					recipient, err := age.ParseX25519Recipient(strings.TrimSpace(key))
					if err != nil {
						return fmt.Errorf("invalid recipient given %s: %w", key, err)
					}
					recipients = append(recipients, recipient)
				}
				field.Set(reflect.ValueOf(recipients))
			} else {
				panic("unsupported slice type")
			}

		case reflect.Ptr:
			if !valueGiven {
				continue
			}

			if fieldType.Type.Elem() == reflect.TypeOf(age.ScryptRecipient{}) {
				recipient, err := age.NewScryptRecipient(value)
				if err != nil {
					return fmt.Errorf("invalid password given: %w", err)
				}

				field.Set(reflect.ValueOf(recipient))
			} else {
				panic("unsupported pointer type")
			}

		default:
			panic("unsupported struct field type")
		}
	}
	return nil
}
