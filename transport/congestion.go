package transport

import (
	"math"
	"sync"
	"time"
)

// CongestionControl implements adaptive congestion control
type CongestionControl struct {
	// Window parameters
	cwnd         float64       // Congestion window (in packets)
	ssthresh     float64       // Slow start threshold
	maxCwnd      float64       // Maximum window size
	minRTT       time.Duration // Minimum RTT observed
	srtt         time.Duration // Smoothed RTT
	rttVar       time.Duration // RTT variance

	// Packet tracking
	packetsSent     uint64
	packetsAcked    uint64
	packetsLost     uint64
	bytesInFlight   uint64

	// Timing
	lastAck         time.Time
	lastLoss        time.Time
	lastUpdate      time.Time

	// State
	state           congestionState
	mu              sync.RWMutex

	// Configuration
	initialCwnd     float64
	mss             uint32 // Maximum segment size
	minCwnd         float64
	betaMul         float64 // Multiplicative decrease factor
	alphaAdd        float64 // Additive increase factor
}

type congestionState uint8

const (
	stateSlowStart congestionState = iota
	stateCongestionAvoidance
	stateRecovery
)

// NewCongestionControl creates a new congestion control instance
func NewCongestionControl() *CongestionControl {
	now := time.Now()
	return &CongestionControl{
		cwnd:        10.0, // Initial window: 10 packets
		ssthresh:    math.MaxFloat64,
		maxCwnd:     1000.0,
		minCwnd:     2.0,
		initialCwnd: 10.0,
		mss:         1400, // Typical MSS
		betaMul:     0.7,  // Multiplicative decrease by 30%
		alphaAdd:    1.0,  // Additive increase by 1 MSS per RTT
		lastAck:     now,
		lastUpdate:  now,
		state:       stateSlowStart,
	}
}

// OnPacketSent is called when a packet is sent
func (cc *CongestionControl) OnPacketSent(size uint64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.packetsSent++
	cc.bytesInFlight += size
}

// OnPacketAcked is called when a packet is acknowledged
func (cc *CongestionControl) OnPacketAcked(size uint64, rtt time.Duration) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	now := time.Now()
	cc.packetsAcked++
	cc.lastAck = now

	if cc.bytesInFlight >= size {
		cc.bytesInFlight -= size
	} else {
		cc.bytesInFlight = 0
	}

	// Update RTT estimates
	cc.updateRTT(rtt)

	// Update congestion window
	switch cc.state {
	case stateSlowStart:
		cc.slowStart()
	case stateCongestionAvoidance:
		cc.congestionAvoidance()
	case stateRecovery:
		cc.recovery()
	}

	cc.lastUpdate = now
}

// OnPacketLost is called when a packet loss is detected
func (cc *CongestionControl) OnPacketLost(size uint64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	now := time.Now()
	cc.packetsLost++
	cc.lastLoss = now

	if cc.bytesInFlight >= size {
		cc.bytesInFlight -= size
	}

	// Enter congestion avoidance or recovery
	cc.ssthresh = math.Max(cc.cwnd*cc.betaMul, cc.minCwnd)
	cc.cwnd = cc.ssthresh

	if cc.state == stateSlowStart {
		cc.state = stateCongestionAvoidance
	} else {
		cc.state = stateRecovery
	}
}

// slowStart implements the slow start algorithm
func (cc *CongestionControl) slowStart() {
	// Exponential increase: double cwnd every RTT
	cc.cwnd += 1.0

	if cc.cwnd >= cc.ssthresh {
		cc.state = stateCongestionAvoidance
	}

	if cc.cwnd > cc.maxCwnd {
		cc.cwnd = cc.maxCwnd
	}
}

// congestionAvoidance implements AIMD congestion avoidance
func (cc *CongestionControl) congestionAvoidance() {
	// Additive increase: increase cwnd by 1 MSS per RTT
	// This is approximated by incrementing cwnd by 1/cwnd per ACK
	cc.cwnd += cc.alphaAdd / cc.cwnd

	if cc.cwnd > cc.maxCwnd {
		cc.cwnd = cc.maxCwnd
	}
}

// recovery implements fast recovery
func (cc *CongestionControl) recovery() {
	// Linear increase during recovery
	cc.cwnd += 0.5 / cc.cwnd

	// Exit recovery after receiving ACKs
	if time.Since(cc.lastLoss) > cc.srtt*2 {
		cc.state = stateCongestionAvoidance
	}
}

// updateRTT updates RTT estimates using exponential moving average
func (cc *CongestionControl) updateRTT(rtt time.Duration) {
	if rtt <= 0 {
		return
	}

	// Track minimum RTT
	if cc.minRTT == 0 || rtt < cc.minRTT {
		cc.minRTT = rtt
	}

	// Update smoothed RTT (SRTT)
	if cc.srtt == 0 {
		cc.srtt = rtt
		cc.rttVar = rtt / 2
	} else {
		// SRTT = (1 - alpha) * SRTT + alpha * RTT
		// RTTVar = (1 - beta) * RTTVar + beta * |SRTT - RTT|
		const alpha = 0.125
		const beta = 0.25

		diff := cc.srtt - rtt
		if diff < 0 {
			diff = -diff
		}

		cc.rttVar = time.Duration((1-beta)*float64(cc.rttVar) + beta*float64(diff))
		cc.srtt = time.Duration((1-alpha)*float64(cc.srtt) + alpha*float64(rtt))
	}
}

// GetSendWindow returns the current send window in bytes
func (cc *CongestionControl) GetSendWindow() uint64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	return uint64(cc.cwnd * float64(cc.mss))
}

// GetAvailableWindow returns available send window
func (cc *CongestionControl) GetAvailableWindow() uint64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	sendWindow := uint64(cc.cwnd * float64(cc.mss))
	if cc.bytesInFlight >= sendWindow {
		return 0
	}

	return sendWindow - cc.bytesInFlight
}

// GetRTO returns the retransmission timeout
func (cc *CongestionControl) GetRTO() time.Duration {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	if cc.srtt == 0 {
		return 1 * time.Second
	}

	// RTO = SRTT + 4 * RTTVar
	rto := cc.srtt + 4*cc.rttVar

	// Clamp RTO between 200ms and 60s
	const minRTO = 200 * time.Millisecond
	const maxRTO = 60 * time.Second

	if rto < minRTO {
		rto = minRTO
	} else if rto > maxRTO {
		rto = maxRTO
	}

	return rto
}

// GetRTT returns the smoothed RTT
func (cc *CongestionControl) GetRTT() time.Duration {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.srtt
}

// GetCongestionWindow returns the current congestion window
func (cc *CongestionControl) GetCongestionWindow() float64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.cwnd
}

// GetState returns the current congestion state
func (cc *CongestionControl) GetState() string {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	switch cc.state {
	case stateSlowStart:
		return "slow_start"
	case stateCongestionAvoidance:
		return "congestion_avoidance"
	case stateRecovery:
		return "recovery"
	default:
		return "unknown"
	}
}

// GetStatistics returns congestion control statistics
func (cc *CongestionControl) GetStatistics() map[string]interface{} {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	lossRate := 0.0
	if cc.packetsSent > 0 {
		lossRate = float64(cc.packetsLost) / float64(cc.packetsSent)
	}

	return map[string]interface{}{
		"cwnd":             cc.cwnd,
		"ssthresh":         cc.ssthresh,
		"bytes_in_flight":  cc.bytesInFlight,
		"packets_sent":     cc.packetsSent,
		"packets_acked":    cc.packetsAcked,
		"packets_lost":     cc.packetsLost,
		"loss_rate":        lossRate,
		"srtt_ms":          cc.srtt.Milliseconds(),
		"min_rtt_ms":       cc.minRTT.Milliseconds(),
		"rto_ms":           cc.GetRTO().Milliseconds(),
		"state":            cc.GetState(),
		"send_window":      cc.GetSendWindow(),
		"available_window": cc.GetAvailableWindow(),
	}
}

// Reset resets the congestion control state
func (cc *CongestionControl) Reset() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	now := time.Now()
	cc.cwnd = cc.initialCwnd
	cc.ssthresh = math.MaxFloat64
	cc.bytesInFlight = 0
	cc.lastAck = now
	cc.lastUpdate = now
	cc.state = stateSlowStart
}

// SetMaxWindow sets the maximum congestion window
func (cc *CongestionControl) SetMaxWindow(maxCwnd float64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.maxCwnd = maxCwnd
}

// SetMSS sets the maximum segment size
func (cc *CongestionControl) SetMSS(mss uint32) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.mss = mss
}

// CanSend returns whether more data can be sent
func (cc *CongestionControl) CanSend(size uint64) bool {
	return cc.GetAvailableWindow() >= size
}
