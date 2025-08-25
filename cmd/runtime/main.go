package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"myruntime/pkg/cgroup"
	"myruntime/pkg/fs"
	"myruntime/pkg/image"
	"myruntime/pkg/netsetup"
	"myruntime/pkg/sandbox"
)

func main() {
	imageName := flag.String("image", "busybox", "image to run (docker/oci)")
	cmd := flag.String("cmd", "sh", "command to run inside container (quoted string)")
	name := flag.String("name", "myctr", "container name")
	cpu := flag.String("cpu", "", "cgroup v2 cpu.max (e.g. \"100000 100000\" or \"max\")")
	memory := flag.String("memory", "", "cgroup v2 memory.max (e.g. \"100M\")")
	capAdd := flag.String("cap-add", "", "comma-separated caps to add")
	capDrop := flag.String("cap-drop", "", "comma-separated caps to drop")
	publish := flag.String("publish", "", "comma-separated port mappings host:container (eg 8080:80,4443:443)")
	bridge := flag.String("bridge", "myruntime0", "host bridge name to attach containers to")
	networkCidr := flag.String("bridge-cidr", "172.25.0.0/16", "CIDR for bridge network")
	flag.Parse()

	workRoot := filepath.Join(os.TempDir(), "myruntime", *name)
	lower := filepath.Join(workRoot, "lower")
	mount := filepath.Join(workRoot, "rootfs")
	upper := filepath.Join(workRoot, "upper")
	workDir := filepath.Join(workRoot, "work")

	log.Printf("pulling image %s\n", *imageName)
	if err := image.ExportRootFS(*imageName, lower); err != nil {
		log.Fatalf("image export failed: %v", err)
	}

	log.Printf("mounting overlayfs\n")
	if err := fs.MountOverlay(lower, upper, workDir, mount); err != nil {
		log.Fatalf("overlay mount failed: %v", err)
	}

	cgPath := ""
	if *cpu != "" || *memory != "" {
		var err error
		cgPath, err = cgroup.CreateCG(*name, *cpu, *memory)
		if err != nil {
			log.Fatalf("cgroup create failed: %v", err)
		}
		log.Printf("created cgroup: %s\n", cgPath)
	}

	// Parse publish rules
	pubs := []netsetup.PortMap{}
	if *publish != "" {
		pairs := strings.Split(*publish, ",")
		for _, p := range pairs {
			m := strings.TrimSpace(p)
			if m == "" {
				continue
			}
			hostPort, contPort, err := netsetup.ParsePortMap(m)
			if err != nil {
				log.Fatalf("invalid publish mapping %s: %v", m, err)
			}
			pubs = append(pubs, netsetup.PortMap{HostPort: hostPort, ContainerPort: contPort})
		}
	}

	// Prepare sandbox configuration
	cfg := sandbox.Config{
		Name:         *name,
		Rootfs:       mount,
		Cmd:          cmd,
		CgroupPath:   cgPath,
		CapAdd:       capAdd,
		CapDrop:      capDrop,
		Publish:      pubs,
		BridgeName:   bridge,
		BridgeCIDR:   networkCidr,
		WorkDir:      workRoot,
		OverlayLower: lower,
		OverlayUpper: upper,
		OverlayWork:  workDir,
	}

	// Create bridge if needed
	if err := netsetup.EnsureBridge(*cfg.BridgeName, *cfg.BridgeCIDR); err != nil {
		log.Fatalf("bridge setup failed: %v", err)
	}
	log.Printf("running sandbox\n")
	if err := sandbox.Run(cfg); err != nil {
		log.Fatalf("run failed: %v", err)
	}

	fmt.Println("container exited")

	// attempt cleanup
	exec.Command("/bin/sh", "-c", fmt.Sprintf("umount %s || true && rm -rf %s || true", mount, workRoot)).Run()
}
