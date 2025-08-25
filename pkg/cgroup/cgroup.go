package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CreateCG creates a cgroup v2 under /sys/fs/cgroup/myruntime-<name>-<ts>
func CreateCG(name, cpu, memory string) (string, error) {
	base := "/sys/fs/cgroup"
	if _, err := os.Stat(base); err != nil {
		return "", fmt.Errorf("cgroup v2 not available: %v", err)
	}
	cgName := fmt.Sprintf("myruntime-%s-%d", name, time.Now().Unix())
	path := filepath.Join(base, cgName)
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", err
	}
	if cpu != "" {
		if err := os.WriteFile(filepath.Join(path, "cpu.max"), []byte(cpu), 0644); err != nil {
			return "", err
		}
	}
	if memory != "" {
		if err := os.WriteFile(filepath.Join(path, "memory.max"), []byte(memory), 0644); err != nil {
			return "", err
		}
	}
	return path, nil
}
