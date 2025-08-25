package image

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/crane"
)

func ExportRootFS(ref, dest string) error {
	// remove any existing
	os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp("", "export-*.tar")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// First pull the image to get a v1.Image
	img, err := crane.Pull(ref)
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	// Then export the image
	if err := crane.Export(img, tmpFile); err != nil {
		return fmt.Errorf("crane export: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("syncing file: %w", err)
	}

	// Extract the tar file to the destination directory
	// Open the tar file for reading
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return fmt.Errorf("seeking to start of file: %w", err)
	}

	tarReader := tar.NewReader(tmpFile)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			// end of archive
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar archive: %w", err)
		}

		target := filepath.Join(dest, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			// create directory
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}
		case tar.TypeReg:
			// create file
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("creating file: %w", err)
			}
			if _, err := io.Copy(w, tarReader); err != nil {
				w.Close()
				return fmt.Errorf("writing file contents: %w", err)
			}
			w.Close()
		default:
			// unsupported type,
			return fmt.Errorf("unsupported type in tar: %c", header.Typeflag)
		}
	}
	return nil

}
