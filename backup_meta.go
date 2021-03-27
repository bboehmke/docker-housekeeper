package main

import "time"

// BackupMeta for backup file
type BackupMeta struct {
	// Version of backup file format
	Version int `yaml:"version"`
	// Date of backup creation
	Date time.Time `yaml:"date"`

	// DatabaseBackup contains the name of the database dump file
	DatabaseBackup string `yaml:"database_backup,omitempty"`

	// Directories list all directory backups stored in the backup file
	Directories []BackupMetaDirectory `yaml:"directories,omitempty"`
}

type BackupMetaDirectory struct {
	// DirectoryPath where the data was located
	DirectoryPath string `yaml:"directory_path"`

	// Filename of directory backup
	Filename string `yaml:"filename"`
}
