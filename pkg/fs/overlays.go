package fs

import (
	"fmt"
	"os"
	"syscall"
)

func MountOverlay(lower, upper, work, target string) error {
	os.RemoveAll(target)
	if err := os.MkdirAll(upper, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(work, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lower, upper, work)
	if err := syscall.Mount("overlay", target, "overlay", 0, opts); err != nil {
		return fmt.Errorf("mount overlay: %w", err)
	}
	return nil
}
