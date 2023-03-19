package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

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
