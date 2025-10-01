package routing

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
)

// GeoIP GeoIP数据库
type GeoIP struct {
	mu      sync.RWMutex
	entries []*GeoIPEntry
}

// GeoIPEntry GeoIP条目
type GeoIPEntry struct {
	StartIP   net.IP
	EndIP     net.IP
	StartIPv4 uint32
	EndIPv4   uint32
	Country   string
}

// NewGeoIP 创建GeoIP数据库
func NewGeoIP() *GeoIP {
	return &GeoIP{
		entries: make([]*GeoIPEntry, 0),
	}
}

// LoadFromFile 从文件加载GeoIP数据
// 支持简化的CSV格式: start_ip,end_ip,country_code
func (g *GeoIP) LoadFromFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open geoip file: %w", err)
	}
	defer file.Close()

	g.mu.Lock()
	defer g.mu.Unlock()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 解析CSV: start_ip,end_ip,country_code
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}

		startIP := net.ParseIP(strings.TrimSpace(parts[0]))
		endIP := net.ParseIP(strings.TrimSpace(parts[1]))
		country := strings.TrimSpace(parts[2])

		if startIP == nil || endIP == nil {
			continue
		}

		entry := &GeoIPEntry{
			StartIP: startIP,
			EndIP:   endIP,
			Country: country,
		}

		// 如果是IPv4，转换为uint32以加快查询
		if startIPv4 := startIP.To4(); startIPv4 != nil {
			entry.StartIPv4 = binary.BigEndian.Uint32(startIPv4)
		}
		if endIPv4 := endIP.To4(); endIPv4 != nil {
			entry.EndIPv4 = binary.BigEndian.Uint32(endIPv4)
		}

		g.entries = append(g.entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading geoip file: %w", err)
	}

	return nil
}

// Lookup 查询IP所属国家
func (g *GeoIP) Lookup(ip net.IP) string {
	if ip == nil {
		return ""
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	// IPv4快速查询
	if ipv4 := ip.To4(); ipv4 != nil {
		ipNum := binary.BigEndian.Uint32(ipv4)

		for _, entry := range g.entries {
			if entry.StartIPv4 <= ipNum && ipNum <= entry.EndIPv4 {
				return entry.Country
			}
		}
		return ""
	}

	// IPv6查询
	for _, entry := range g.entries {
		if inRange(ip, entry.StartIP, entry.EndIP) {
			return entry.Country
		}
	}

	return ""
}

// inRange 检查IP是否在范围内
func inRange(ip, start, end net.IP) bool {
	return compareIP(ip, start) >= 0 && compareIP(ip, end) <= 0
}

// compareIP 比较两个IP地址
func compareIP(ip1, ip2 net.IP) int {
	// 确保长度相同
	if len(ip1) != len(ip2) {
		ip1 = ip1.To16()
		ip2 = ip2.To16()
	}

	for i := 0; i < len(ip1); i++ {
		if ip1[i] < ip2[i] {
			return -1
		}
		if ip1[i] > ip2[i] {
			return 1
		}
	}
	return 0
}

// AddEntry 添加GeoIP条目
func (g *GeoIP) AddEntry(startIP, endIP net.IP, country string) error {
	if startIP == nil || endIP == nil {
		return fmt.Errorf("invalid IP address")
	}

	entry := &GeoIPEntry{
		StartIP: startIP,
		EndIP:   endIP,
		Country: country,
	}

	// IPv4转换
	if startIPv4 := startIP.To4(); startIPv4 != nil {
		entry.StartIPv4 = binary.BigEndian.Uint32(startIPv4)
	}
	if endIPv4 := endIP.To4(); endIPv4 != nil {
		entry.EndIPv4 = binary.BigEndian.Uint32(endIPv4)
	}

	g.mu.Lock()
	g.entries = append(g.entries, entry)
	g.mu.Unlock()

	return nil
}

// GetEntryCount 获取条目数量
func (g *GeoIP) GetEntryCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.entries)
}

// GenerateSampleGeoIP 生成示例GeoIP数据（用于测试）
func GenerateSampleGeoIP() *GeoIP {
	geoip := NewGeoIP()

	// 中国IP段（示例）
	geoip.AddEntry(net.ParseIP("1.0.1.0"), net.ParseIP("1.0.3.255"), "CN")
	geoip.AddEntry(net.ParseIP("1.0.8.0"), net.ParseIP("1.0.15.255"), "CN")
	geoip.AddEntry(net.ParseIP("114.114.114.0"), net.ParseIP("114.114.114.255"), "CN")

	// 美国IP段（示例）
	geoip.AddEntry(net.ParseIP("8.8.8.0"), net.ParseIP("8.8.8.255"), "US")
	geoip.AddEntry(net.ParseIP("1.1.1.0"), net.ParseIP("1.1.1.255"), "US")

	// 日本IP段（示例）
	geoip.AddEntry(net.ParseIP("103.4.96.0"), net.ParseIP("103.4.96.255"), "JP")

	// 私有IP段
	geoip.AddEntry(net.ParseIP("10.0.0.0"), net.ParseIP("10.255.255.255"), "PRIVATE")
	geoip.AddEntry(net.ParseIP("172.16.0.0"), net.ParseIP("172.31.255.255"), "PRIVATE")
	geoip.AddEntry(net.ParseIP("192.168.0.0"), net.ParseIP("192.168.255.255"), "PRIVATE")

	return geoip
}

// SaveToFile 保存GeoIP数据到文件
func (g *GeoIP) SaveToFile(filename string) error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	// 写入头部注释
	writer.WriteString("# GeoIP Database\n")
	writer.WriteString("# Format: start_ip,end_ip,country_code\n")
	writer.WriteString("#\n")

	// 写入数据
	for _, entry := range g.entries {
		line := fmt.Sprintf("%s,%s,%s\n",
			entry.StartIP.String(),
			entry.EndIP.String(),
			entry.Country,
		)
		writer.WriteString(line)
	}

	return nil
}

// LookupBatch 批量查询
func (g *GeoIP) LookupBatch(ips []net.IP) map[string]string {
	result := make(map[string]string)

	for _, ip := range ips {
		country := g.Lookup(ip)
		result[ip.String()] = country
	}

	return result
}

// Stats GeoIP统计
type GeoIPStats struct {
	TotalEntries   int            `json:"total_entries"`
	CountryCount   map[string]int `json:"country_count"`
}

// GetStats 获取统计信息
func (g *GeoIP) GetStats() *GeoIPStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	stats := &GeoIPStats{
		TotalEntries: len(g.entries),
		CountryCount: make(map[string]int),
	}

	for _, entry := range g.entries {
		stats.CountryCount[entry.Country]++
	}

	return stats
}
