package main

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
	_ "github.com/rclone/rclone/backend/all"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"
	"github.com/rclone/rclone/fs/object"
	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// BackupService handles database and directory backups
type BackupService struct {
	Config   BackupConfig
	Database DatabaseConnection

	Cron      *cron.Cron
	CronEntry cron.EntryID
	RClone    fs.Fs
}

// Prepare for backup (creating directories, checking credentials, ...)
func (s *BackupService) Prepare() error {
	err := os.MkdirAll(s.Config.Storage, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create backup dir %s: %w", s.Config.Storage, err)
	}

	if s.Config.RCloneConfig != "" {
		err = config.SetConfigPath(s.Config.RCloneConfig)
		if err != nil {
			return fmt.Errorf("failed to load rclone config %s: %w", s.Config.RCloneConfig, err)
		}
		configfile.Install()
	}

	if s.Config.RClonePath != "" {
		s.RClone, err = fs.NewFs(context.Background(), s.Config.RClonePath)
		if err != nil {
			return fmt.Errorf("failed create rclone FS %s: %w", s.Config.RClonePath, err)
		}
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
	file, fileClose, err := s.createFile(filename)
	if err != nil {
		return err
	}
	defer fileClose()

	encryptedFile, encryptClose, err := s.encryptFile(file)
	if err != nil {
		return err
	}
	defer encryptClose()

	// create zip writer (without compression)
	zipWriter := zip.NewWriter(encryptedFile)
	defer zipWriter.Close()

	meta := &BackupMeta{
		Version: 1,
		Date:    time.Now(),
	}

	if err = s.backupDatabase(zipWriter, meta); err != nil {
		return err
	}

	if err = s.backupDirectories(zipWriter, meta); err != nil {
		return err
	}

	// write meta file
	writer, err := zipWriter.CreateHeader(&zip.FileHeader{
		Name:     "backup.yml",
		Modified: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("failed to create backup.yml: %w", err)
	}

	if err = yaml.NewEncoder(writer).Encode(&meta); err != nil {
		return fmt.Errorf("failed to write backup.yml: %w", err)
	}

	log.Printf("backup finished")

	return nil
}

// createFile on local file system or remote via rclone
func (s *BackupService) createFile(filename string) (io.Writer, func(), error) {
	if s.RClone == nil {
		file, err := os.Create(filepath.Join(s.Config.Storage, filename))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create backup file %s: %w", filename, err)
		}
		return file, func() {
			file.Close()
		}, nil
	}

	reader, writer := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		_, err := s.RClone.Put(context.Background(), reader,
			object.NewStaticObjectInfo(
				filename, time.Now(), -1, false, nil, nil))
		if err != nil {
			_ = reader.CloseWithError(err)
		} else {
			reader.Close()
		}
		wg.Done()
	}()

	return writer, func() {
		writer.Close()
		wg.Wait()
	}, nil
}

// encryptFile if configured
func (s *BackupService) encryptFile(file io.Writer) (io.Writer, func(), error) {
	recipients := s.Config.ageRecipients()
	if len(recipients) > 0 {
		encryptedFile, err := age.Encrypt(file, recipients...)
		if err != nil {
			return nil, nil, fmt.Errorf("failed age encryption: %w", err)
		}
		return encryptedFile, func() {
			encryptedFile.Close()
		}, nil
	}
	return file, func() {}, nil
}

func (s *BackupService) backupDatabase(zipWriter *zip.Writer, meta *BackupMeta) error {
	if !s.Config.Database || s.Database == nil {
		return nil
	}

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

	return nil
}

func (s *BackupService) backupDirectories(zipWriter *zip.Writer, meta *BackupMeta) error {
	if s.Config.DataDirectories == "" {
		return nil
	}

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
	return nil
}
