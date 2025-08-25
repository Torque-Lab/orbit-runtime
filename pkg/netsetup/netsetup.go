package netsetup

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type PortMap struct {
	HostPort      int
	ContainerPort int
}

func ParsePortMap(s string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid format")
	}
	h, err := strconvAtoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	c, err := strconvAtoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return h, c, nil
}

func strconvAtoi(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

// EnsureBridge creates the bridge if not exists and sets the CIDR (host side IP)
func EnsureBridge(brName, cidr string) error {

	if _, err := netlink.LinkByName(brName); err == nil {
		return nil
	}
	br := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: brName}}
	if err := netlink.LinkAdd(br); err != nil {
		return fmt.Errorf("bridge add: %w", err)
	}
	addr, err := netlink.ParseAddr(cidrToIPNet(cidr))
	if err != nil {
		return err
	}
	if err := netlink.AddrAdd(br, addr); err != nil {
		return fmt.Errorf("addr add: %w", err)
	}
	if err := netlink.LinkSetUp(br); err != nil {
		return fmt.Errorf("link set up: %w", err)
	}
	return nil
}

func cidrToIPNet(cidr string) string {
	// pick first IP as gateway: replace host bits with .1
	ip, ipnet, _ := net.ParseCIDR(cidr)
	if ip == nil || ipnet == nil {
		return cidr
	}
	// compute gateway as first usable
	ones, bits := ipnet.Mask.Size()
	if ones >= bits-1 {
		return cidr
	}
	ip = ipnet.IP.To4()
	ip[3] = 1
	return fmt.Sprintf("%s/%d", ip.String(), ones+(bits-ones)/(bits/ones))
}

// SetupVethAndPortBinding: create veth pair, attach host side to bridge, move cont to netns of pid, assign IP, add route, and setup iptables DNAT
func SetupVethAndPortBinding(pid int, brName string, brCIDR string, pub PortMap) (contIP string, err error) {
	// create unique names
	hostIf := fmt.Sprintf("vethh%d", pid)
	contIf := fmt.Sprintf("vethc%d", pid)

	veth := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: hostIf}, PeerName: contIf}
	if err := netlink.LinkAdd(veth); err != nil {
		return "", fmt.Errorf("link add veth: %w", err)
	}

	hostLink, _ := netlink.LinkByName(hostIf)
	br, _ := netlink.LinkByName(brName)
	if err := netlink.LinkSetMaster(hostLink, br.(*netlink.Bridge)); err != nil {
		return "", fmt.Errorf("set master: %w", err)
	}
	netlink.LinkSetUp(hostLink)

	// move container side to container netns
	contLink, _ := netlink.LinkByName(contIf)
	ns, err := netns.GetFromPid(pid)
	if err != nil {
		return "", fmt.Errorf("get netns: %w", err)
	}
	if err := netlink.LinkSetNsFd(contLink, int(ns)); err != nil {
		return "", fmt.Errorf("link set ns: %w", err)
	}

	// choose container IP: pick .2 for pid or hash
	// simple scheme: take bridge CIDR base and give .%d where %d = pid % 250 + 2
	ip, ipnet, err := net.ParseCIDR(brCIDR)
	if err != nil {
		return "", fmt.Errorf("parse cidr: %w", err)
	}
	contAddr := net.IPv4(ip[0], ip[1], byte((pid%250)+2), 0)
	contAddr[3] = byte((pid % 250) + 2)
	addrStr := fmt.Sprintf("%s/%d", contAddr.String(), maskSize(ipnet.Mask))

	// configure interface inside the netns
	err = netsetupInNS(pid, contIf, addrStr, ip.String(), pub.ContainerPort)
	if err != nil {
		return "", fmt.Errorf("setup inside ns: %w", err)
	}

	// add iptables DNAT rule on host
	dnat := fmt.Sprintf("-p tcp --dport %d -j DNAT --to-destination %s:%d", pub.HostPort, contAddr.String(), pub.ContainerPort)
	cmd := exec.Command("iptables", "-t", "nat", "-A", "PREROUTING")
	cmd.Args = append(cmd.Args, strings.Split(dnat, " ")...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("iptables add failed: %v %s", err, out)
	}
	// add MASQUERADE for outgoing
	if out, err := exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-j", "MASQUERADE").CombinedOutput(); err != nil {
		return "", fmt.Errorf("iptables masquerade failed: %v %s", err, out)
	}

	return contAddr.String(), nil
}

func maskSize(m net.IPMask) int {
	ons, _ := m.Size()
	return ons
}

func netsetupInNS(pid int, ifName, addr, gw string, containerPort int) error {
	// use ip command inside the container netns by invoking nsenter -t <pid> -n <cmd>
	// bring interface up
	cmds := [][]string{
		{"ip", "link", "set", ifName, "up"},
		{"ip", "addr", "add", addr, "dev", ifName},
		{"ip", "route", "add", "default", "via", gw},
	}
	for _, c := range cmds {
		full := append([]string{"-t", fmt.Sprintf("%d", pid), "-n", "--"}, c...)
		out, err := exec.Command("nsenter", full...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("nsenter (%v) failed: %v %s", c, err, out)
		}
	}
	return nil
}
