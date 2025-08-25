package image

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

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
			break // end of archive
		}
		if err != nil {
			return fmt.Errorf("reading tar archive: %w", err)
		}

		target := filepath.Join(dest, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			// create directory
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}

		case tar.TypeReg:
			// create regular file
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("creating file %s: %w", target, err)
			}
			if _, err := io.Copy(w, tarReader); err != nil {
				w.Close()
				return fmt.Errorf("writing file contents %s: %w", target, err)
			}
			w.Close()

		case tar.TypeSymlink:

			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("creating symlink %s -> %s: %w", target, header.Linkname, err)
			}

		case tar.TypeLink:
			// create hard link
			linkTarget := filepath.Join(dest, header.Linkname)
			if err := os.Link(linkTarget, target); err != nil {
				return fmt.Errorf("creating hardlink %s -> %s: %w", target, linkTarget, err)
			}

		case tar.TypeChar, tar.TypeBlock:
			// special device file (requires root)
			dev := unix.Mkdev(uint32(header.Devmajor), uint32(header.Devminor))
			mode := uint32(header.Mode)
			if header.Typeflag == tar.TypeBlock {
				mode |= unix.S_IFBLK
			} else {
				mode |= unix.S_IFCHR
			}
			if err := unix.Mknod(target, mode, int(dev)); err != nil {
				fmt.Fprintf(os.Stderr, "warn: could not create device %s: %v\n", target, err)
			}

		case tar.TypeFifo:
			// named pipe
			if err := syscall.Mkfifo(target, uint32(header.Mode)); err != nil {
				fmt.Fprintf(os.Stderr, "warn: could not create fifo %s: %v\n", target, err)
			}

		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			// extended attributes – ignore safely
			continue

		default:
			fmt.Fprintf(os.Stderr, "warn: skipping unsupported tar entry %s (type %c)\n", header.Name, header.Typeflag)
		}

		// Restore file timestamps & ownership (optional but Docker does this)
		os.Chtimes(target, header.AccessTime, header.ModTime)
		if err := os.Chown(target, header.Uid, header.Gid); err != nil {
			fmt.Fprintf(os.Stderr, "warn: could not chown %s: %v\n", target, err)
		}
	}

	return nil

}
