package routing

import (
	"net"
	"os"
	"testing"
)

func TestGeoIPLookup(t *testing.T) {
	geoip := GenerateSampleGeoIP()

	tests := []struct {
		name     string
		ip       string
		expected string
	}{
		{"CN IP", "1.0.1.1", "CN"},
		{"CN IP 2", "114.114.114.114", "CN"},
		{"US IP", "8.8.8.8", "US"},
		{"US IP 2", "1.1.1.1", "US"},
		{"JP IP", "103.4.96.1", "JP"},
		{"Private IP", "192.168.1.1", "PRIVATE"},
		{"Unknown IP", "9.9.9.9", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			country := geoip.Lookup(ip)
			if country != tt.expected {
				t.Errorf("Expected country %s for IP %s, got %s", tt.expected, tt.ip, country)
			}
		})
	}
}

func TestGeoIPAddEntry(t *testing.T) {
	geoip := NewGeoIP()

	startIP := net.ParseIP("1.0.0.0")
	endIP := net.ParseIP("1.0.0.255")
	country := "TEST"

	err := geoip.AddEntry(startIP, endIP, country)
	if err != nil {
		t.Fatalf("Failed to add entry: %v", err)
	}

	// 测试查询
	testIP := net.ParseIP("1.0.0.100")
	result := geoip.Lookup(testIP)

	if result != country {
		t.Errorf("Expected country %s, got %s", country, result)
	}
}

func TestGeoIPSaveAndLoad(t *testing.T) {
	// 创建示例数据
	geoip1 := GenerateSampleGeoIP()

	// 保存到文件
	filename := t.TempDir() + "/geoip.csv"
	err := geoip1.SaveToFile(filename)
	if err != nil {
		t.Fatalf("Failed to save GeoIP: %v", err)
	}

	// 加载文件
	geoip2 := NewGeoIP()
	err = geoip2.LoadFromFile(filename)
	if err != nil {
		t.Fatalf("Failed to load GeoIP: %v", err)
	}

	// 验证数据一致性
	if geoip2.GetEntryCount() != geoip1.GetEntryCount() {
		t.Errorf("Entry count mismatch: expected %d, got %d",
			geoip1.GetEntryCount(), geoip2.GetEntryCount())
	}

	// 验证查询结果一致
	testIP := net.ParseIP("1.0.1.1")
	country1 := geoip1.Lookup(testIP)
	country2 := geoip2.Lookup(testIP)

	if country1 != country2 {
		t.Errorf("Lookup mismatch: expected %s, got %s", country1, country2)
	}
}

func TestGeoIPLoadNonExistentFile(t *testing.T) {
	geoip := NewGeoIP()

	err := geoip.LoadFromFile("/nonexistent/file.csv")
	if err == nil {
		t.Error("Expected error when loading non-existent file")
	}
}

func TestGeoIPInvalidIP(t *testing.T) {
	geoip := GenerateSampleGeoIP()

	// nil IP
	country := geoip.Lookup(nil)
	if country != "" {
		t.Errorf("Expected empty string for nil IP, got %s", country)
	}

	// 无效IP
	err := geoip.AddEntry(nil, net.ParseIP("1.0.0.1"), "TEST")
	if err == nil {
		t.Error("Expected error when adding invalid IP")
	}
}

func TestGeoIPLookupBatch(t *testing.T) {
	geoip := GenerateSampleGeoIP()

	ips := []net.IP{
		net.ParseIP("1.0.1.1"),   // CN
		net.ParseIP("8.8.8.8"),   // US
		net.ParseIP("103.4.96.1"), // JP
	}

	results := geoip.LookupBatch(ips)

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	if results["1.0.1.1"] != "CN" {
		t.Errorf("Expected CN for 1.0.1.1, got %s", results["1.0.1.1"])
	}

	if results["8.8.8.8"] != "US" {
		t.Errorf("Expected US for 8.8.8.8, got %s", results["8.8.8.8"])
	}

	if results["103.4.96.1"] != "JP" {
		t.Errorf("Expected JP for 103.4.96.1, got %s", results["103.4.96.1"])
	}
}

func TestGeoIPStats(t *testing.T) {
	geoip := GenerateSampleGeoIP()

	stats := geoip.GetStats()

	if stats.TotalEntries == 0 {
		t.Error("Expected non-zero total entries")
	}

	if len(stats.CountryCount) == 0 {
		t.Error("Expected non-zero country count")
	}

	// 验证中国条目数
	if stats.CountryCount["CN"] != 3 {
		t.Errorf("Expected 3 CN entries, got %d", stats.CountryCount["CN"])
	}

	// 验证美国条目数
	if stats.CountryCount["US"] != 2 {
		t.Errorf("Expected 2 US entries, got %d", stats.CountryCount["US"])
	}
}

func TestGeoIPIPv4Range(t *testing.T) {
	geoip := NewGeoIP()

	// 添加IPv4范围
	startIP := net.ParseIP("10.0.0.0")
	endIP := net.ParseIP("10.0.0.255")
	geoip.AddEntry(startIP, endIP, "TEST")

	// 测试范围内的IP
	tests := []struct {
		ip       string
		expected string
	}{
		{"10.0.0.0", "TEST"},
		{"10.0.0.1", "TEST"},
		{"10.0.0.128", "TEST"},
		{"10.0.0.255", "TEST"},
		{"10.0.1.0", ""}, // 超出范围
		{"9.255.255.255", ""}, // 超出范围
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		country := geoip.Lookup(ip)
		if country != tt.expected {
			t.Errorf("For IP %s: expected %s, got %s", tt.ip, tt.expected, country)
		}
	}
}

func TestCompareIP(t *testing.T) {
	tests := []struct {
		name     string
		ip1      string
		ip2      string
		expected int
	}{
		{"Equal IPs", "1.1.1.1", "1.1.1.1", 0},
		{"IP1 < IP2", "1.1.1.1", "1.1.1.2", -1},
		{"IP1 > IP2", "1.1.1.2", "1.1.1.1", 1},
		{"Different octets", "1.0.0.0", "2.0.0.0", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip1 := net.ParseIP(tt.ip1)
			ip2 := net.ParseIP(tt.ip2)
			result := compareIP(ip1, ip2)

			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestInRange(t *testing.T) {
	start := net.ParseIP("10.0.0.0")
	end := net.ParseIP("10.0.0.255")

	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"Start IP", "10.0.0.0", true},
		{"End IP", "10.0.0.255", true},
		{"Middle IP", "10.0.0.128", true},
		{"Before range", "9.255.255.255", false},
		{"After range", "10.0.1.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			result := inRange(ip, start, end)

			if result != tt.expected {
				t.Errorf("For IP %s: expected %v, got %v", tt.ip, tt.expected, result)
			}
		})
	}
}

func TestGeoIPFileFormat(t *testing.T) {
	geoip := NewGeoIP()

	// 创建测试文件
	filename := t.TempDir() + "/test.csv"
	content := `# GeoIP Test File
# Comment line
1.0.0.0,1.0.0.255,TEST1
2.0.0.0,2.0.0.255,TEST2

# Another comment
3.0.0.0,3.0.0.255,TEST3
`
	err := os.WriteFile(filename, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// 加载文件
	err = geoip.LoadFromFile(filename)
	if err != nil {
		t.Fatalf("Failed to load file: %v", err)
	}

	// 验证加载了3个条目
	if geoip.GetEntryCount() != 3 {
		t.Errorf("Expected 3 entries, got %d", geoip.GetEntryCount())
	}

	// 验证查询
	if country := geoip.Lookup(net.ParseIP("1.0.0.1")); country != "TEST1" {
		t.Errorf("Expected TEST1, got %s", country)
	}

	if country := geoip.Lookup(net.ParseIP("2.0.0.1")); country != "TEST2" {
		t.Errorf("Expected TEST2, got %s", country)
	}

	if country := geoip.Lookup(net.ParseIP("3.0.0.1")); country != "TEST3" {
		t.Errorf("Expected TEST3, got %s", country)
	}
}

func BenchmarkGeoIPLookup(b *testing.B) {
	geoip := GenerateSampleGeoIP()
	ip := net.ParseIP("1.0.1.1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		geoip.Lookup(ip)
	}
}

func BenchmarkGeoIPLookupBatch(b *testing.B) {
	geoip := GenerateSampleGeoIP()
	ips := []net.IP{
		net.ParseIP("1.0.1.1"),
		net.ParseIP("8.8.8.8"),
		net.ParseIP("103.4.96.1"),
		net.ParseIP("192.168.1.1"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		geoip.LookupBatch(ips)
	}
}
