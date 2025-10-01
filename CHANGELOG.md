# Changelog

## [Unreleased] - 2025-09-30

### Added

#### 1. Windows TUN/TAP Support
- **Implementation**: `internal/dataplane/tun_bridge_windows.go`
- Full Windows TUN support using Wintun driver
- Automatic TUN device creation with configurable MTU
- Batch API support for efficient packet I/O
- Proper error handling and device cleanup
- Background goroutine for packet reading

**Configuration**:
```json
{
  "tunnel": {
    "type": "tun",
    "name": "stp0",
    "mtu": 1420
  }
}
```

**Requirements**:
- Wintun driver must be installed (bundled with wireguard-go)
- Administrator privileges for TUN device creation

#### 2. Enhanced Hot-Reload System
- **New Module**: `internal/state/reload.go`
- Tracks configuration reload history (last 10 events)
- Records successful and failed reload attempts
- Captures detailed change information

**Supported Hot-Reload Parameters**:
- Management API ACL (`management.acl`)
- Logging level (`logging.level`)
- Connection limits (`maxConnections`, `connectionRate`, `connectionBurst`)

**Reload Status in /state Endpoint**:
```json
{
  "device": { ... },
  "reloads": [
    {
      "timestamp": "2025-09-30T12:00:00Z",
      "success": true,
      "changes": ["management_acl", "log_level", "connection_limits"]
    }
  ]
}
```

#### 3. End-to-End Testing Suite
- **File**: `test/e2e_test.go`
- Three comprehensive test cases:
  1. `TestE2EUDPBridge`: Full end-to-end communication test
  2. `TestHandshakeOnly`: Handshake verification test
  3. `TestConfigReload`: Configuration hot-reload test

**Test Features**:
- Real client-server handshake
- UDP bridge dataplane testing
- Session ID verification
- Configuration reload validation
- Proper resource cleanup

### Fixed

#### UDP Session Management
- **File**: `transport/transport.go`
- Fixed deadlock in UDP listener close operation
- Separated `Close()` and `closeInternal()` to avoid circular cleanup
- Improved session cleanup ordering
- Better error handling in demux goroutine

**Changes**:
- Close sessions before holding mutex
- Check `closed` flag in demux loop
- Use `closeInternal()` when removing sessions to avoid cleanup recursion

#### Wireguard-go Batch API
- Updated TUN bridge implementations for wireguard-go's batch API
- Both Windows and Unix versions now use `Read(bufs, sizes, offset)`
- Write operations use `Write(bufs, offset)` with single-element slice

### Improved

#### Configuration Validation
- **File**: `config/config.go`
- Added validation for TUN MTU (576-65535 range)
- Default TUN interface name: `stp0`
- Default MTU: 1420 bytes

#### State Management
- Unified state snapshot format across client and server
- Reload history included in management API responses
- Better separation of concerns (device state vs reload state)

### Technical Details

#### TUN Device Implementation (Windows)
```go
// Creates TUN device with Wintun
dev, err := tun.CreateTUN(name, mtu)

// Batch read API
bufs := make([][]byte, 1)
sizes := make([]int, 1)
n, err := device.Read(bufs, sizes, 0)

// Batch write API
bufs := [][]byte{payload}
_, err := device.Write(bufs, 0)
```

#### Reload Tracking
```go
// Track successful reload
reloadTracker.RecordSuccess([]string{"management_acl", "log_level"})

// Track failed reload
reloadTracker.RecordFailure(err)

// Get history
history := reloadTracker.GetHistory()
```

### Testing

Run all tests:
```bash
go test ./test -v
```

Run specific test:
```bash
go test ./test -v -run TestHandshakeOnly
go test ./test -v -run TestConfigReload
```

Run with timeout:
```bash
go test ./test -v -timeout 30s
```

### Example Configuration

**TUN Mode (Windows)**:
```json
{
  "mode": "server",
  "listen": "0.0.0.0:51820",
  "psk": "your-secure-random-32-byte-psk",
  "tunnel": {
    "type": "tun",
    "name": "stp0",
    "mtu": 1420
  },
  "peers": [
    {
      "name": "client1",
      "allowedIPs": ["10.0.0.0/24"]
    }
  ]
}
```

**UDP Bridge Mode (Testing)**:
```json
{
  "mode": "client",
  "endpoint": "server.example.com:51820",
  "psk": "your-secure-random-32-byte-psk",
  "tunnel": {
    "type": "udp-bridge",
    "listen": "127.0.0.1:7002"
  },
  "peers": [
    {
      "name": "server",
      "endpoint": "127.0.0.1:7001",
      "allowedIPs": ["10.0.0.0/16"]
    }
  ]
}
```

### Known Limitations

1. **Hot-Reload Scope**:
   - Cannot reload peer configuration (requires restart)
   - Cannot change tunnel type dynamically
   - Cannot reload PSK (security requirement)

2. **TUN Mode**:
   - Requires administrator/root privileges
   - Wintun driver must be installed on Windows
   - Interface configuration (IP address, routes) must be done externally

3. **Testing**:
   - E2E tests require available UDP ports (7001, 7002, 15820)
   - Tests may fail if ports are in use
   - Some tests may timeout on slow systems

### Performance Notes

- TUN mode provides native OS routing performance
- UDP bridge mode has additional overhead (useful for testing)
- Loopback mode is fastest for testing/development

### Security Considerations

- TUN interface creation requires elevated privileges
- Wintun driver binaries should be verified
- Configuration file may contain sensitive PSK (use environment variables)
- Management API should be properly ACL-protected

### Migration Guide

**From Loopback to TUN**:
1. Install Wintun driver (if on Windows)
2. Update config: `"tunnel": {"type": "tun"}`
3. Run with administrator privileges
4. Configure IP address on TUN interface:
   ```powershell
   # Windows
   netsh interface ip set address "stp0" static 10.0.0.1 255.255.255.0
   ```

**Enable Hot-Reload Monitoring**:
1. Access management API: `http://localhost:7777/state`
2. Check `reloads` array for reload history
3. Monitor `changes` field for applied updates

### Contributors

- Implemented Windows TUN support with Wintun driver
- Enhanced hot-reload system with state tracking
- Added comprehensive E2E testing suite
- Fixed UDP session management deadlock
- Updated documentation and examples