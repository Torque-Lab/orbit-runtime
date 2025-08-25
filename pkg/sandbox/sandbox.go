package sandbox

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"myruntime/pkg/netsetup"

	"github.com/syndtr/gocapability/capability"
)

// Config describes a container/sandbox
type Config struct {
	Name         string
	Rootfs       string
	Cmd          *string
	CgroupPath   string
	CapAdd       *string
	CapDrop      *string
	Publish      []netsetup.PortMap
	BridgeName   *string
	BridgeCIDR   *string
	WorkDir      string
	OverlayLower string
	OverlayUpper string
	OverlayWork  string
}

func Run(cfg Config) error {
	// re-exec self into new namespaces
	self, err := os.Executable()
	if err != nil {
		return err
	}
	// prepare env
	env := os.Environ()
	env = append(env, "MYRUNTIME_IS_CHILD=1")
	env = append(env, "MYRUNTIME_ROOTFS="+cfg.Rootfs)
	env = append(env, "MYRUNTIME_CMD="+*cfg.Cmd)
	if cfg.CgroupPath != "" {
		env = append(env, "MYRUNTIME_CGROUP="+cfg.CgroupPath)
	}
	if cfg.CapAdd != nil && *cfg.CapAdd != "" {
		env = append(env, "MYRUNTIME_CAP_ADD="+*cfg.CapAdd)
	}
	if cfg.CapDrop != nil && *cfg.CapDrop != "" {
		env = append(env, "MYRUNTIME_CAP_DROP="+*cfg.CapDrop)
	}
	if cfg.BridgeName != nil {
		env = append(env, "MYRUNTIME_BRIDGE="+*cfg.BridgeName)
	}
	if cfg.BridgeCIDR != nil {
		env = append(env, "MYRUNTIME_BRIDGE_CIDR="+*cfg.BridgeCIDR)
	}

	cmd := exec.Command(self)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET | syscall.CLONE_NEWIPC,
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// If cgroup is present, add child to cgroup
	if cfg.CgroupPath != "" {
		pid := cmd.Process.Pid
		if err := os.WriteFile(filepath.Join(cfg.CgroupPath, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0644); err != nil {
			log.Printf("warn: adding to cgroup: %v", err)
		}
	}

	// If networking publish mappings exist, create veths and iptables rules
	for _, p := range cfg.Publish {
		contIP, err := netsetup.SetupVethAndPortBinding(cmd.Process.Pid, *cfg.BridgeName, *cfg.BridgeCIDR, p)
		if err != nil {
			log.Printf("network setup failed for publish %v: %v", p, err)
		} else {
			log.Printf("port %d forwarded to container %s:%d", p.HostPort, contIP, p.ContainerPort)
		}
	}

	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}

// childMain is executed when process is re-exec'd with MYRUNTIME_IS_CHILD=1
func init() {
	if os.Getenv("MYRUNTIME_IS_CHILD") != "1" {
		return
	}
	// child execution
	rootfs := os.Getenv("MYRUNTIME_ROOTFS")
	cmdline := os.Getenv("MYRUNTIME_CMD")
	cg := os.Getenv("MYRUNTIME_CGROUP")
	capAdd := os.Getenv("MYRUNTIME_CAP_ADD")
	capDrop := os.Getenv("MYRUNTIME_CAP_DROP")
	bridge := os.Getenv("MYRUNTIME_BRIDGE")
	bridgeCIDR := os.Getenv("MYRUNTIME_BRIDGE_CIDR")

	// Mount proc
	if err := syscall.Mount("proc", filepath.Join(rootfs, "proc"), "proc", 0, ""); err != nil {

		fmt.Fprintf(os.Stderr, "warn mount proc: %v\n", err)
	}

	// chroot
	if err := syscall.Chroot(rootfs); err != nil {
		fmt.Fprintf(os.Stderr, "chroot failed: %v\n", err)
		os.Exit(1)
	}
	if err := os.Chdir("/"); err != nil {
		fmt.Fprintf(os.Stderr, "chdir failed: %v\n", err)
		os.Exit(1)
	}

	// join cgroup if any
	if cg != "" {
		if err := os.WriteFile(filepath.Join(cg, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warn join cgroup: %v\n", err)
		}
	}

	// Setup network interface inside container namespace if env tells parent to create veths (parent will run nsenter commands)
	if bridge != "" && bridgeCIDR != "" {
		// nothing to do here beyond expecting the host to move a veth and set IP/route via nsenter
	}

	// Apply capabilities safely
	if capAdd != "" || capDrop != "" {
		capsAdd := parseCapsOrEmpty(capAdd)
		capsDrop := parseCapsOrEmpty(capDrop)

		capset, err := capability.NewPid2(0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load capabilities: %v\n", err)
			os.Exit(1)
		}

		// Clear all sets
		capset.Clear(capability.EFFECTIVE)
		capset.Clear(capability.PERMITTED)
		capset.Clear(capability.INHERITABLE)

		// Add requested caps
		for _, c := range capsAdd {
			capset.Set(capability.EFFECTIVE, c)
			capset.Set(capability.PERMITTED, c)
			capset.Set(capability.INHERITABLE, c)
		}

		// Drop requested caps (just in case they were in defaults)
		for _, c := range capsDrop {
			capset.Unset(capability.EFFECTIVE, c)
			capset.Unset(capability.PERMITTED, c)
			capset.Unset(capability.INHERITABLE, c)
		}

		// Restrict bounding set to final caps (important!)
		capset.Apply(capability.BOUNDS)

		// Finally apply all sets
		if err := capset.Apply(capability.CAPS); err != nil {
			fmt.Fprintf(os.Stderr, "failed to apply capabilities: %v\n", err)
			os.Exit(1)
		}
	}

	args := strings.Fields(cmdline)
	if len(args) == 0 {
		os.Exit(0)
	}
	if err := syscall.Exec(args[0], args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "exec failed: %v\n", err)
		os.Exit(1)
	}

}

var capNames = map[string]capability.Cap{
	"CAP_CHOWN":              capability.CAP_CHOWN,
	"CAP_DAC_OVERRIDE":       capability.CAP_DAC_OVERRIDE,
	"CAP_DAC_READ_SEARCH":    capability.CAP_DAC_READ_SEARCH,
	"CAP_FOWNER":             capability.CAP_FOWNER,
	"CAP_FSETID":             capability.CAP_FSETID,
	"CAP_KILL":               capability.CAP_KILL,
	"CAP_SETGID":             capability.CAP_SETGID,
	"CAP_SETUID":             capability.CAP_SETUID,
	"CAP_NET_BIND_SERVICE":   capability.CAP_NET_BIND_SERVICE,
	"CAP_NET_ADMIN":          capability.CAP_NET_ADMIN,
	"CAP_SYS_PTRACE":         capability.CAP_SYS_PTRACE,
	"CAP_SYS_ADMIN":          capability.CAP_SYS_ADMIN,
	"CAP_SYS_MODULE":         capability.CAP_SYS_MODULE,
	"CAP_SYS_RAWIO":          capability.CAP_SYS_RAWIO,
	"CAP_SYS_CHROOT":         capability.CAP_SYS_CHROOT,
	"CAP_SYS_NICE":           capability.CAP_SYS_NICE,
	"CAP_SYS_PACCT":          capability.CAP_SYS_PACCT,
	"CAP_SYS_BOOT":           capability.CAP_SYS_BOOT,
	"CAP_SYS_TIME":           capability.CAP_SYS_TIME,
	"CAP_SYS_TTY_CONFIG":     capability.CAP_SYS_TTY_CONFIG,
	"CAP_SETPCAP":            capability.CAP_SETPCAP,
	"CAP_SYSLOG":             capability.CAP_SYSLOG,
	"CAP_AUDIT_WRITE":        capability.CAP_AUDIT_WRITE,
	"CAP_AUDIT_CONTROL":      capability.CAP_AUDIT_CONTROL,
	"CAP_SETFCAP":            capability.CAP_SETFCAP,
	"CAP_MAC_OVERRIDE":       capability.CAP_MAC_OVERRIDE,
	"CAP_MAC_ADMIN":          capability.CAP_MAC_ADMIN,
	"CAP_PERFMON":            capability.CAP_PERFMON,
	"CAP_BPF":                capability.CAP_BPF,
	"CAP_CHECKPOINT_RESTORE": capability.CAP_CHECKPOINT_RESTORE,
}

func parseCapsOrEmpty(s string) []capability.Cap {
	if strings.TrimSpace(s) == "" {
		return nil
	}

	seen := map[capability.Cap]bool{}
	var out []capability.Cap

	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(strings.ToUpper(p))
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "CAP_") {
			p = "CAP_" + p
		}
		if c, ok := capNames[p]; ok {
			if !seen[c] {
				out = append(out, c)
				seen[c] = true
			}
		} else {
			fmt.Fprintf(os.Stderr, "warn: unknown capability %s\n", p)
		}
	}
	return out
}
