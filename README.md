# Orbit Runtime

Orbit Runtime is a minimal container runtime written in Go. It pulls OCI/Docker images, sets up an overlay filesystem, configures cgroups, networking, and runs containers in isolated sandboxes using Linux namespaces.

## Features

- Pulls container images using `image.ExportRootFS`
- Sets up overlay filesystem with `fs.MountOverlay`
- Creates cgroups for resource limits via `cgroup.CreateCG`
- Configures network bridges and port forwarding using `netsetup.EnsureBridge` and `netsetup.ParsePortMap`
- Runs containers in isolated namespaces with configurable capabilities using `sandbox.Run`

## Usage

```sh
go run ./cmd/runtime/main.go [flags]
```

### Flags

- `-image` (default: `busybox`): Image to run (Docker/OCI reference)
- `-cmd` (default: `sh`): Command to run inside the container
- `-name` (default: `myctr`): Container name
- `-cpu`: cgroup v2 cpu.max (e.g. `"100000 100000"` or `"max"`)
- `-memory`: cgroup v2 memory.max (e.g. `"100M"`)
- `-cap-add`: Comma-separated capabilities to add
- `-cap-drop`: Comma-separated capabilities to drop
- `-publish`: Comma-separated port mappings `host:container` (e.g. `8080:80,4443:443`)
- `-bridge` (default: `myruntime0`): Host bridge name
- `-bridge-cidr` (default: `172.25.0.0/16`): CIDR for bridge network

## Example

```sh
go run ./cmd/runtime/main.go -image=busybox -cmd="sleep 10" -name=testctr -cpu="100000 100000" -memory="50M" -publish="8080:80"
```

## Cleanup

After the container exits, the runtime attempts to unmount and remove temporary directories.

## Project Structure

- `cmd/runtime/main.go`: Entry point
- `pkg/image/image.go`: Image pulling and extraction
- `pkg/fs/overlays.go`: Overlay filesystem setup
- `pkg/cgroup/cgroup.go`: Cgroup management
- `pkg/netsetup/netsetup.go`: Networking and port mapping
- `pkg/sandbox/sandbox.go`: Sandbox/container execution

## Requirements

- Go 1.24+
- Linux with cgroup v2, overlayfs, and required kernel namespaces
- Root privileges (for mounting, cgroups, networking)

---

See `main.go` for implementation details.
