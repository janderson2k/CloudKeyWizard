package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
)

// Active scouting: the two features the user specifically asked to be "more active... a function
// you can turn on and off" -- user-triggered on demand, never scheduled/background, unlike the
// passive Monitoring checks above. Scoped to the device's own local subnet by default (the IP range
// scanner doesn't accept an arbitrary user-typed range) -- this already runs as root with real
// network reach, so the same "name the real risk plainly, don't hide it" posture as WireGuard/
// purge/account-cleanup elsewhere in this app applies here: bound the blast radius rather than
// leave it wide open, without refusing to build the feature at all.

type ScanHost struct {
	IP       string `json:"ip"`
	MAC      string `json:"mac,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// LocalSubnetCIDR returns the CIDR of the device's own primary interface, e.g. "192.168.1.0/24" --
// the range scanner's only allowed target, not a free-typed value.
func LocalSubnetCIDR() (string, error) {
	iface, addresses, _ := currentNetworkState()
	if iface == "" || len(addresses) == 0 {
		return "", fmt.Errorf("couldn't determine the local network")
	}
	_, network, err := net.ParseCIDR(addresses[0])
	if err != nil {
		return "", err
	}
	return network.String(), nil
}

// ScanLocalSubnet pings every host in the device's own local subnet -- the default, no-typing-
// required path most scans will use.
func ScanLocalSubnet() ([]ScanHost, error) {
	cidr, err := LocalSubnetCIDR()
	if err != nil {
		return nil, err
	}
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	ones, bits := network.Mask.Size()
	if bits-ones > 10 {
		return nil, fmt.Errorf("subnet %s is too large to scan (limit: /22)", cidr)
	}

	var hosts []string
	for addr := network.IP.Mask(network.Mask); network.Contains(addr); addr = nextIP(addr) {
		hosts = append(hosts, addr.String())
	}
	return scanHostList(hosts), nil
}

// maxCustomRangeHosts bounds a user-typed start/end range -- generous enough for real use (a /20
// worth of addresses) while still ruling out an accidental "scan the whole internet" fat-finger.
const maxCustomRangeHosts = 4096

// ScanIPRange pings every address from startIP to endIP inclusive -- the user-specified alternative
// to ScanLocalSubnet, for scanning something other than this device's own immediate subnet (a
// Tailscale/WireGuard-reachable range, a different VLAN, etc.). Still bounded (maxCustomRangeHosts)
// and still only ever pings, never anything more invasive -- same posture as the local-subnet scan,
// just user-directed instead of auto-derived.
func ScanIPRange(startStr, endStr string) ([]ScanHost, error) {
	start := net.ParseIP(startStr).To4()
	end := net.ParseIP(endStr).To4()
	if start == nil {
		return nil, fmt.Errorf("invalid start address %q", startStr)
	}
	if end == nil {
		return nil, fmt.Errorf("invalid end address %q", endStr)
	}

	startN := binary.BigEndian.Uint32(start)
	endN := binary.BigEndian.Uint32(end)
	if endN < startN {
		startN, endN = endN, startN // be forgiving about which field is "start" vs "end"
	}
	count := endN - startN + 1
	if count > maxCustomRangeHosts {
		return nil, fmt.Errorf("range is too large to scan (%d addresses, limit: %d)", count, maxCustomRangeHosts)
	}

	hosts := make([]string, 0, count)
	for n := startN; n <= endN; n++ {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], n)
		hosts = append(hosts, net.IP(b[:]).String())
		if n == endN {
			break // avoids a uint32 wraparound infinite loop if endN is 0xFFFFFFFF
		}
	}
	return scanHostList(hosts), nil
}

// scanHostList pings every given host concurrently (bounded, polite to the network and to this
// box's own resources) and reports which answered, then reads /proc/net/arp (populated by the ping
// sweep itself) to attach a MAC address where available. Shared by both ScanLocalSubnet and
// ScanIPRange -- one implementation, not two copies that could drift.
func scanHostList(hosts []string) []ScanHost {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var alive []string
	sem := make(chan struct{}, 32)
	for _, h := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(host string) {
			defer wg.Done()
			defer func() { <-sem }()
			if up, _, _ := pingHost(host, 1); up {
				mu.Lock()
				alive = append(alive, host)
				mu.Unlock()
			}
		}(h)
	}
	wg.Wait()

	arpTable := readARPTable()
	result := make([]ScanHost, 0, len(alive))
	for _, ipStr := range alive {
		sh := ScanHost{IP: ipStr, MAC: arpTable[ipStr]}
		if names, err := net.LookupAddr(ipStr); err == nil && len(names) > 0 {
			sh.Hostname = strings.TrimSuffix(names[0], ".")
		}
		result = append(result, sh)
	}
	return result
}

func nextIP(ip net.IP) net.IP {
	ip = ip.To4()
	out := make(net.IP, len(ip))
	copy(out, ip)
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			break
		}
	}
	return out
}

// readARPTable parses /proc/net/arp (the same source the passive LAN list uses) into a map keyed
// by IP address.
func readARPTable() map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return out
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // header row
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mac := fields[3]
		if mac != "00:00:00:00:00:00" {
			out[fields[0]] = mac
		}
	}
	return out
}

// --- Open port scanner ------------------------------------------------------------------------

// commonPorts is a curated list, not all 65535 -- keeps a scan fast and its results readable. Covers
// the services someone actually checking "is this reachable" would care about.
var commonPorts = []int{
	21, 22, 23, 25, 53, 80, 110, 111, 123, 135, 137, 138, 139, 143, 161, 179, 389, 443, 445,
	465, 514, 515, 587, 631, 636, 993, 995, 1080, 1194, 1433, 1521, 1723, 2049, 2375, 2376,
	3000, 3306, 3389, 3690, 5000, 5060, 5222, 5432, 5900, 5901, 6379, 6443, 7000, 8000, 8006,
	8008, 8080, 8081, 8086, 8123, 8181, 8443, 8880, 8883, 8888, 8920, 9000, 9090, 9091, 9100,
	9200, 9443, 10000, 27017, 32400,
}

type PortScanResult struct {
	Port int    `json:"port"`
	Open bool   `json:"open"`
	Name string `json:"name,omitempty"`
}

var portNames = map[int]string{
	21: "FTP", 22: "SSH", 23: "Telnet", 25: "SMTP", 53: "DNS", 80: "HTTP", 110: "POP3",
	143: "IMAP", 443: "HTTPS", 445: "SMB", 587: "SMTP (submission)", 993: "IMAPS", 995: "POP3S",
	1433: "MSSQL", 1521: "Oracle", 2049: "NFS", 3000: "dev/HTTP", 3306: "MySQL", 3389: "RDP",
	5432: "PostgreSQL", 5900: "VNC", 6379: "Redis", 8006: "Proxmox", 8080: "HTTP-alt",
	8123: "Home Assistant", 8443: "HTTPS-alt", 9100: "printer/JetDirect", 27017: "MongoDB",
	32400: "Plex",
}

// ScanPorts checks commonPorts against host concurrently, short timeout each. User-triggered only,
// same as the range scanner -- not scheduled.
func ScanPorts(host string) []PortScanResult {
	var wg sync.WaitGroup
	results := make([]PortScanResult, len(commonPorts))
	sem := make(chan struct{}, 64)
	for i, port := range commonPorts {
		wg.Add(1)
		sem <- struct{}{}
		go func(i, port int) {
			defer wg.Done()
			defer func() { <-sem }()
			up, _, _ := tcpCheck(host, port, 2)
			results[i] = PortScanResult{Port: port, Open: up, Name: portNames[port]}
		}(i, port)
	}
	wg.Wait()
	return results
}

// --- Wake-on-LAN --------------------------------------------------------------------------------

// SendWakeOnLAN builds and broadcasts a standard magic packet (6 bytes of 0xFF followed by the
// target MAC repeated 16 times) via UDP -- the well-documented, simple WoL protocol, no
// dependencies needed.
func SendWakeOnLAN(mac string) error {
	hwAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("invalid MAC address: %w", err)
	}
	packet := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		packet = append(packet, 0xFF)
	}
	for i := 0; i < 16; i++ {
		packet = append(packet, hwAddr...)
	}

	conn, err := net.Dial("udp", "255.255.255.255:9")
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(packet)
	return err
}

// --- Passive LAN device list ---------------------------------------------------------------------

// LANDeviceList reads what the kernel already knows (the ARP cache) rather than actively probing --
// distinct from ScanLocalSubnet above, which sends real pings. Cheap, always-safe to call.
func LANDeviceList() []ScanHost {
	arpTable := readARPTable()
	out := make([]ScanHost, 0, len(arpTable))
	for ip, mac := range arpTable {
		sh := ScanHost{IP: ip, MAC: mac}
		if names, err := net.LookupAddr(ip); err == nil && len(names) > 0 {
			sh.Hostname = strings.TrimSuffix(names[0], ".")
		}
		out = append(out, sh)
	}
	return out
}
