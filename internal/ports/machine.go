package ports

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

type gopsutilMachine struct{}

func newMachine() gopsutilMachine {
	// The first non-blocking read is the average since boot; spend it here so
	// the first Stats covers only the time since launch.
	_, _ = cpu.Percent(0, false)
	return gopsutilMachine{}
}

func (gopsutilMachine) Stats() (Stats, error) {
	var s Stats
	p, err := cpu.Percent(0, false)
	if err != nil {
		return s, err
	}
	if len(p) > 0 {
		s.CPU = p[0] / 100
	}
	v, err := mem.VirtualMemory()
	if err != nil {
		return s, err
	}
	s.MemUsed, s.MemTotal = min(v.Used, v.Total), v.Total
	s.Volumes = volumes()
	s.Interfaces = interfaces()
	return s, nil
}

// volumes are the ones Finder lists, which it finds under /Volumes. The
// startup disk's entry there is a link to /, not a mount, so it's read at /;
// Recovery is mounted there too but Finder hides it.
func volumes() []Volume {
	mounted := map[string]bool{}
	if parts, err := disk.Partitions(false); err == nil {
		for _, p := range parts {
			mounted[p.Mountpoint] = true
		}
	}
	type at struct {
		Volume
		path string
	}
	all := []at{{Volume{Startup: true}, "/"}}
	entries, _ := os.ReadDir("/Volumes")
	for _, e := range entries {
		path := filepath.Join("/Volumes", e.Name())
		if e.Name() != "Recovery" && mounted[path] {
			all = append(all, at{Volume{Name: e.Name()}, path})
		}
	}
	var out []Volume
	for _, v := range all {
		u, err := disk.Usage(v.path)
		if err != nil || u.Total == 0 {
			continue
		}
		v.Total, v.Free = u.Total, min(u.Free, u.Total)
		out = append(out, v.Volume)
	}
	return out
}

// virtual are name prefixes of interfaces that carry traffic another interface
// sends again, on Linux and Windows: containers', VMs' and VPNs'.
var virtual = []string{"docker", "veth", "br-", "virbr", "vEthernet", "vmnet", "tap", "tun"}

// interfaces are those with a hardware address, which leaves out loopback and
// VPN tunnels. A Mac's bridges and AirDrop's have one too; only its en* carry
// traffic out.
func interfaces() []Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, i := range ifaces {
		if runtime.GOOS == "darwin" {
			out[i.Name] = strings.HasPrefix(i.Name, "en")
			continue
		}
		out[i.Name] = i.HardwareAddr != "" && !slices.ContainsFunc(virtual, func(p string) bool { return strings.HasPrefix(i.Name, p) })
	}
	counters, err := net.IOCounters(true)
	if err != nil {
		return nil
	}
	var found []Interface
	for _, c := range counters {
		if out[c.Name] {
			found = append(found, Interface{Name: c.Name, Recv: c.BytesRecv, Sent: c.BytesSent})
		}
	}
	return found
}
