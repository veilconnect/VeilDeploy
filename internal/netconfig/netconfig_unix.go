//go:build !windows

package netconfig

import (
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
)

// ConfigureTUN configures a TUN interface with IP address and routes
func ConfigureTUN(ifname string, localIP netip.Prefix, routes []netip.Prefix) error {
	// Set IP address
	if err := setIPAddress(ifname, localIP); err != nil {
		return fmt.Errorf("failed to set IP address: %w", err)
	}

	// Bring interface up
	if err := BringUp(ifname); err != nil {
		return fmt.Errorf("failed to bring interface up: %w", err)
	}

	// Add routes
	for _, route := range routes {
		if err := addRoute(ifname, route); err != nil {
			return fmt.Errorf("failed to add route %s: %w", route, err)
		}
	}

	return nil
}

// setIPAddress sets the IP address on a Unix interface
func setIPAddress(ifname string, addr netip.Prefix) error {
	addrStr := addr.String()

	// ip addr add ADDRESS dev INTERFACE
	cmd := exec.Command("ip", "addr", "add", addrStr, "dev", ifname)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore "exists" errors
		if strings.Contains(string(output), "exists") ||
			strings.Contains(string(output), "File exists") {
			return nil
		}
		return fmt.Errorf("ip addr command failed: %w, output: %s", err, string(output))
	}

	return nil
}

// addRoute adds a route to the Unix routing table
func addRoute(ifname string, prefix netip.Prefix) error {
	prefixStr := prefix.String()

	// ip route add PREFIX dev INTERFACE
	cmd := exec.Command("ip", "route", "add", prefixStr, "dev", ifname)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore "exists" errors
		if strings.Contains(string(output), "exists") ||
			strings.Contains(string(output), "File exists") {
			return nil
		}
		return fmt.Errorf("ip route command failed: %w, output: %s", err, string(output))
	}

	return nil
}

// DeleteRoute removes a route from the Unix routing table
func DeleteRoute(ifname string, prefix netip.Prefix) error {
	prefixStr := prefix.String()

	cmd := exec.Command("ip", "route", "del", prefixStr, "dev", ifname)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore "not found" errors
		if strings.Contains(string(output), "not found") ||
			strings.Contains(string(output), "No such process") {
			return nil
		}
		return fmt.Errorf("ip route del failed: %w, output: %s", err, string(output))
	}

	return nil
}

// GetInterfaceIP gets the current IP address of an interface
func GetInterfaceIP(ifname string) (netip.Prefix, error) {
	cmd := exec.Command("ip", "-o", "addr", "show", "dev", ifname)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to get interface IP: %w", err)
	}

	// Parse output: "2: eth0 inet 10.0.0.1/24 ..."
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == "inet" && i+1 < len(fields) {
				return netip.ParsePrefix(fields[i+1])
			}
		}
	}

	return netip.Prefix{}, fmt.Errorf("no IP address found for interface %s", ifname)
}

// SetMTU sets the MTU of an interface
func SetMTU(ifname string, mtu int) error {
	// ip link set dev INTERFACE mtu VALUE
	cmd := exec.Command("ip", "link", "set", "dev", ifname, "mtu", fmt.Sprintf("%d", mtu))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set MTU: %w, output: %s", err, string(output))
	}

	return nil
}

// BringUp brings an interface up
func BringUp(ifname string) error {
	cmd := exec.Command("ip", "link", "set", "dev", ifname, "up")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to bring interface up: %w, output: %s", err, string(output))
	}
	return nil
}

// BringDown brings an interface down
func BringDown(ifname string) error {
	cmd := exec.Command("ip", "link", "set", "dev", ifname, "down")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to bring interface down: %w, output: %s", err, string(output))
	}
	return nil
}