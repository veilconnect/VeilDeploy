//go:build windows

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

	// Add routes
	for _, route := range routes {
		if err := addRoute(ifname, route); err != nil {
			return fmt.Errorf("failed to add route %s: %w", route, err)
		}
	}

	return nil
}

// setIPAddress sets the IP address on a Windows interface
func setIPAddress(ifname string, addr netip.Prefix) error {
	ip := addr.Addr().String()
	bits := addr.Bits()

	// Calculate netmask from prefix length
	netmask := prefixToNetmask(bits)

	// netsh interface ip set address "interface" static IP NETMASK
	cmd := exec.Command("netsh", "interface", "ip", "set", "address",
		ifname, "static", ip, netmask)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh command failed: %w, output: %s", err, string(output))
	}

	return nil
}

// addRoute adds a route to the Windows routing table
func addRoute(ifname string, prefix netip.Prefix) error {
	network := prefix.Masked().Addr().String()
	bits := prefix.Bits()
	netmask := prefixToNetmask(bits)

	// netsh interface ip add route PREFIX/MASK "interface"
	cmd := exec.Command("netsh", "interface", "ip", "add", "route",
		network, netmask, ifname)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore "already exists" errors
		if strings.Contains(string(output), "already exists") ||
			strings.Contains(string(output), "object already exists") {
			return nil
		}
		return fmt.Errorf("netsh route command failed: %w, output: %s", err, string(output))
	}

	return nil
}

// DeleteRoute removes a route from the Windows routing table
func DeleteRoute(ifname string, prefix netip.Prefix) error {
	network := prefix.Masked().Addr().String()
	bits := prefix.Bits()
	netmask := prefixToNetmask(bits)

	cmd := exec.Command("netsh", "interface", "ip", "delete", "route",
		network, netmask, ifname)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore "not found" errors
		if strings.Contains(string(output), "not found") ||
			strings.Contains(string(output), "Element not found") {
			return nil
		}
		return fmt.Errorf("netsh delete route failed: %w, output: %s", err, string(output))
	}

	return nil
}

// GetInterfaceIP gets the current IP address of an interface
func GetInterfaceIP(ifname string) (netip.Prefix, error) {
	cmd := exec.Command("netsh", "interface", "ip", "show", "addresses", ifname)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to get interface IP: %w", err)
	}

	// Parse output to extract IP address
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "IP Address:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				addr, err := netip.ParseAddr(parts[2])
				if err == nil {
					// Default to /24 if we can't determine the actual prefix
					return netip.PrefixFrom(addr, 24), nil
				}
			}
		}
	}

	return netip.Prefix{}, fmt.Errorf("no IP address found for interface %s", ifname)
}

// prefixToNetmask converts a CIDR prefix length to a dotted-decimal netmask
func prefixToNetmask(bits int) string {
	if bits < 0 || bits > 32 {
		return "255.255.255.0" // default
	}

	mask := ^uint32(0) << (32 - bits)

	return fmt.Sprintf("%d.%d.%d.%d",
		byte(mask>>24),
		byte(mask>>16),
		byte(mask>>8),
		byte(mask))
}

// SetMTU sets the MTU of an interface
func SetMTU(ifname string, mtu int) error {
	// netsh interface ipv4 set subinterface "interface" mtu=VALUE
	cmd := exec.Command("netsh", "interface", "ipv4", "set", "subinterface",
		ifname, fmt.Sprintf("mtu=%d", mtu))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set MTU: %w, output: %s", err, string(output))
	}

	return nil
}

// BringUp brings an interface up (enable)
func BringUp(ifname string) error {
	cmd := exec.Command("netsh", "interface", "set", "interface", ifname, "enable")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to bring interface up: %w, output: %s", err, string(output))
	}
	return nil
}

// BringDown brings an interface down (disable)
func BringDown(ifname string) error {
	cmd := exec.Command("netsh", "interface", "set", "interface", ifname, "disable")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to bring interface down: %w, output: %s", err, string(output))
	}
	return nil
}