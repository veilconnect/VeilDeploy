package dataplane

// Frame represents a payload that should traverse the secure tunnel.
type Frame struct {
	Peer    string
	Payload []byte
}

// Interface describes the minimal contract required by the device layer to
// exchange traffic with the system dataplane (e.g. TUN/TAP, UDP bridge, tests).
type Interface interface {
	// Outbound returns a read-only channel that emits frames originating from
	// the local dataplane which must be transported over STP.
	Outbound() <-chan Frame

	// Deliver injects a payload that arrived over STP into the local dataplane.
	Deliver(peer string, payload []byte) error

	// Close releases any resources associated with the dataplane.
	Close() error
}
