package pg

import (
	"log"
	"math"
	"math/rand"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	timeSliceLen     = 8
	trackerLen       = 8
	protocolICMP     = 1
	protocolIPv6ICMP = 58
)

var (
	ipv4Proto = map[string]string{"ip": "ip4:icmp", "udp": "udp4"}
	ipv6Proto = map[string]string{"ip": "ip6:ipv6-icmp", "udp": "udp6"}
)

// Pinger represents ICMP packet sender/receiver.
type Pinger struct {
	Count          int
	Debug          bool
	Interval       time.Duration
	Timeout        time.Duration
	PacketsSent    int
	PacketsRecieve int
	OnRecieve      func(*Packet)
	OnFinish       func(*Stats)
	Size           int
	Tracker        int64
	Source         string
	done           chan bool

	rtts     []time.Duration
	ipaddr   *net.IPAddr
	addr     string
	ipv4     bool
	size     int
	id       int
	sequence int
	network  string
}

// Packet represents a received and processed ICMP packet.
type Packet struct {
	Rtt      time.Duration
	IPAddr   *net.IPAddr
	Addr     string
	Nbytes   int
	Sequence int
	TTL      int
}

// Stats represents the statistics of running or finished pinger.
type Stats struct {
	PacketsRecieve int
	PacketsSent    int
	PacketsLoss    float64
	IPAddr         *net.IPAddr
	Addr           string
	Rtts           []time.Duration
	MinRtt         time.Duration
	MaxRtt         time.Duration
	AvgRtt         time.Duration
	StdDevRtt      time.Duration
}

// NewPinger returns a new Pinger.
func NewPinger(addr string) (*Pinger, error) {
	ipaddr, err := net.ResolveIPAddr("ip", addr)
	if err != nil {
		return nil, err
	}
	ipv4 := false
	if isIPv4(ipaddr.IP) {
		ipv4 = true
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return &Pinger{
		ipaddr:   ipaddr,
		addr:     addr,
		Interval: time.Second,
		Timeout:  time.Second * 100,
		Count:    -1,
		id:       r.Intn(math.MaxInt16),
		network:  "udp",
		ipv4:     ipv4,
		size:     timeSliceLen,
		Tracker:  r.Int63n(math.MaxInt64),
		done:     make(chan bool),
	}, nil
}

func isIPv4(ip net.IP) bool {
	return net.IPv4len == len(ip.To4())
}

func (p *Pinger) Run() {
	var conn *icmp.PacketConn
	if p.ipv4 {
		conn = p.listen(ipv4Proto[p.network])
		if conn == nil {
			return
		}
		conn.IPv4PacketConn().SetControlMessage(ipv4.FlagTTL, true)
	} else {
		conn = p.listen(ipv6Proto[p.network])
		if conn == nil {
			return
		}
		conn.IPv6PacketConn().SetControlMessage(ipv6.FlagHopLimit, true)
	}
	defer conn.Close()
	defer p.finish()
}

// GenerateStats returns the statistics of the pinger. This can be run while
// Pinger is runnig or after it is finished.
// OnFinish calls this func to get its finished stats.
func (p *Pinger) GenerateStats() *Stats {
	loss := float64(p.PacketsSent-p.PacketsRecieve) / float64(p.PacketsSent) * 100
	var min, max, total time.Duration

	if len(p.rtts) > 0 {
		min = p.rtts[0]
		max = p.rtts[0]
	}
	for _, rtt := range p.rtts {
		if rtt < min {
			min = rtt
		}
		if rtt > max {
			max = rtt
		}
		total += rtt
	}
	s := Stats{
		PacketsSent:    p.PacketsSent,
		PacketsRecieve: p.PacketsRecieve,
		PacketsLoss:    loss,
		Rtts:           p.rtts,
		Addr:           p.addr,
		IPAddr:         p.ipaddr,
		MaxRtt:         max,
		MinRtt:         min,
	}
	if len(p.rtts) > 0 {
		s.AvgRtt = total / time.Duration(len(p.rtts))
		var sumSquares time.Duration
		for _, rtt := range p.rtts {
			sumSquares += (rtt - s.AvgRtt) * (rtt - s.AvgRtt)
		}
		s.StdDevRtt = time.Duration(math.Sqrt(float64(sumSquares / time.Duration(len(p.rtts)))))
	}
	return &s
}

// finish method is called after the pinger stops.
func (p *Pinger) finish() {
	handler := p.OnFinish
	if handler != nil {
		s := p.GenerateStats()
		handler(s)
	}
}

func (p *Pinger) listen(netProto string) *icmp.PacketConn {
	conn, err := icmp.ListenPacket(netProto, p.Source)
	if err != nil {
		log.Println("Error listening for ICMP Packets: %s\n", err.Error())
		close(p.done)
		return nil
	}
	return conn
}
