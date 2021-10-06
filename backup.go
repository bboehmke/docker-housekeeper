package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// DatabaseConnection that is used for backup the database
type DatabaseConnection interface {
	Backup(writer io.Writer) error
}

// BackupService handles database and directory backups
type BackupService struct {
	Config   BackupConfig
	Database DatabaseConnection

	Cron      *cron.Cron
	CronEntry cron.EntryID
}

// Prepare for backup (creating directories, checking credentials, ...)
func (s *BackupService) Prepare() error {
	err := os.MkdirAll(s.Config.Storage, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create backup dir %s: %w", s.Config.Storage, err)
	}
	return nil
}

// IsBackupEnabled returns true if any backup is enabled
func (s *BackupService) IsBackupEnabled() bool {
	return s.Config.Database || s.Config.DataDirectories != ""
}

// StartSchedule of backup cron
func (s *BackupService) StartSchedule() error {
	s.Cron = cron.New()
	s.Cron.Start()

	// only enable cron if any backup is enabled
	if s.IsBackupEnabled() && s.Config.Schedule != "" {
		var err error
		s.CronEntry, err = s.Cron.AddFunc(s.Config.Schedule, func() {
			err := s.Backup()
			if err != nil {
				log.Printf("backup failed: %v", err)
			}
			log.Printf("[Next Backup: %s]", s.Cron.Entry(s.CronEntry).Next)
		})
		if err != nil {
			return fmt.Errorf("failed to create backup schedule: %w", err)
		}
		log.Printf("[Next Backup: %s]", s.Cron.Entry(s.CronEntry).Next)
	}
	return nil
}

// StopSchedule cron of backup
func (s *BackupService) StopSchedule(timeout time.Duration) {
	if s.Cron != nil {
		ctx := s.Cron.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(timeout):
		}
	}
}

// Backup database and data directories
func (s *BackupService) Backup() error {
	if !s.IsBackupEnabled() {
		log.Print("Nothing to backup")
		return nil
	}

	recipients := s.Config.ageRecipients()
	var filename string
	if len(recipients) > 0 {
		filename = fmt.Sprintf("backup_%s.zip.age", time.Now().Format(time.RFC3339))
	} else {
		filename = fmt.Sprintf("backup_%s.zip", time.Now().Format(time.RFC3339))
	}
	log.Printf("create backup %s ...", filename)

	// open file
	file, err := os.Create(filepath.Join(s.Config.Storage, filename))
	if err != nil {
		return fmt.Errorf("failed to create backup file %s: %w", filename, err)
	}
	defer file.Close()

	// create encrypted file if configured
	var encryptedFile io.WriteCloser
	if len(recipients) > 0 {
		encryptedFile, err = age.Encrypt(file, recipients...)
		if err != nil {
			return fmt.Errorf("failed age encryption: %w", err)
		}
		defer encryptedFile.Close()
	} else {
		encryptedFile = file
	}

	// create zip writer (without compression)
	zipWriter := zip.NewWriter(encryptedFile)
	defer zipWriter.Close()

	meta := &BackupMeta{
		Version: 1,
		Date:    time.Now(),
	}

	// backup database
	if s.Config.Database && s.Database != nil {
		log.Printf("> dump database")
		writer, err := zipWriter.CreateHeader(&zip.FileHeader{
			Name:     "database.sql.gz",
			Modified: time.Now(),
		})
		if err != nil {
			return fmt.Errorf("failed to create database.sql.gz: %w", err)
		}

		// backup database
		gzipWriter := gzip.NewWriter(writer)
		err = s.Database.Backup(gzipWriter)
		gzipWriter.Close()
		if err != nil {
			return err
		}
		meta.DatabaseBackup = "database.sql.gz"
	}

	// backup directories
	if s.Config.DataDirectories != "" {
		log.Printf("> backup data directories")
		dirsSplit := strings.Split(s.Config.DataDirectories, ",")
		meta.Directories = make([]BackupMetaDirectory, len(dirsSplit))
		for idx, dir := range dirsSplit {
			log.Printf("-> %s", dir)
			dirBackupFilename := fmt.Sprintf("data_%d.tar.gz", idx)
			writer, err := zipWriter.CreateHeader(&zip.FileHeader{
				Name:     dirBackupFilename,
				Modified: time.Now(),
			})
			if err != nil {
				return fmt.Errorf("failed to create data_%d.tar.gz: %w", idx, err)
			}

			err = tarDir(writer, dir)
			if err != nil {
				return fmt.Errorf("failed to create data_%d.tar.gz: %w", idx, err)
			}

			meta.Directories[idx] = BackupMetaDirectory{
				DirectoryPath: dir,
				Filename:      dirBackupFilename,
			}
		}
	}

	// write meta file
	writer, err := zipWriter.CreateHeader(&zip.FileHeader{
		Name:     "backup.yml",
		Modified: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("failed to create backup.yml: %w", err)
	}
	err = yaml.NewEncoder(writer).Encode(&meta)
	if err != nil {
		return fmt.Errorf("failed to write backup.yml: %w", err)
	}

	log.Printf("backup finished")

	return nil
}

// tarDir creates a tar gz archive from a directory
func tarDir(writer io.Writer, dir string) error {
	// create gzip compressed tar writer
	gzipWriter := gzip.NewWriter(writer)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	return filepath.Walk(dir, func(file string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// handle symlinks
		var symLinkTarget string
		if info.Mode()&os.ModeSymlink != 0 {
			symLinkTarget, err = os.Readlink(file)
			if err != nil {
				return fmt.Errorf("failed to get symlink target of %s: %w", file, err)
			}
		}

		// generate tar header
		header, err := tar.FileInfoHeader(info, symLinkTarget)
		if err != nil {
			return err
		}

		// make file path relative
		fileRel, err := filepath.Rel(dir, file)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}
		header.Name = filepath.ToSlash(fileRel)

		// write tar file entry header
		err = tarWriter.WriteHeader(header)
		if err != nil {
			return err
		}

		// add content of files
		if info.Mode().IsRegular() {
			f, err := os.Open(file)
			if err != nil {
				return fmt.Errorf("failed to open %s: %w", file, err)
			}
			defer f.Close()

			_, err = io.Copy(tarWriter, f)
			if err != nil {
				return fmt.Errorf("failed add %s to  archive: %w", file, err)
			}
		}
		return nil
	})
}
