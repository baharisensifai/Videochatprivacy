package turnserver

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/pion/turn/v2"
	"github.com/pion/webrtc/v3"
)

var username string
var password string
var Address string

var server struct {
	mu        sync.Mutex
	addresses []net.Addr
	server    *turn.Server
}

func publicAddresses() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	var as []net.IP

	for _, addr := range addrs {
		switch addr := addr.(type) {
		case *net.IPNet:
			a := addr.IP.To4()
			if a == nil {
				continue
			}
			if !a.IsGlobalUnicast() {
				continue
			}
			if a[0] == 10 ||
				a[0] == 172 && a[1] >= 16 && a[1] < 32 ||
				a[0] == 192 && a[1] == 168 {
				continue
			}
			as = append(as, a)
		}
	}
	return as, nil
}

func listener(a net.IP, port int, relay net.IP) (*turn.PacketConnConfig, *turn.ListenerConfig) {
	var pcc *turn.PacketConnConfig
	var lc *turn.ListenerConfig
	s := net.JoinHostPort(a.String(), strconv.Itoa(port))

	var g turn.RelayAddressGenerator
	if relay == nil || relay.IsUnspecified() {
		g = &turn.RelayAddressGeneratorNone{
			Address: a.String(),
		}
	} else {
		g = &turn.RelayAddressGeneratorStatic{
			RelayAddress: relay,
			Address:      a.String(),
		}
	}

	p, err := net.ListenPacket("udp4", s)
	if err == nil {
		pcc = &turn.PacketConnConfig{
			PacketConn:            p,
			RelayAddressGenerator: g,
		}
	} else {
		log.Printf("TURN: listenPacket(%v): %v", s, err)
	}

	l, err := net.Listen("tcp4", s)
	if err == nil {
		lc = &turn.ListenerConfig{
			Listener:              l,
			RelayAddressGenerator: g,
		}
	} else {
		log.Printf("TURN: listen(%v): %v", s, err)
	}

	return pcc, lc
}

func Start() error {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.server != nil {
		return nil
	}

	if Address == "" {
		return errors.New("built-in TURN server disabled")
	}

	ad := Address
	if Address == "auto" {
		ad = ":1194"
	}
	addr, err := net.ResolveUDPAddr("udp4", ad)
	if err != nil {
		return err
	}

	username = "galene"
	buf := make([]byte, 6)
	_, err = rand.Read(buf)
	if err != nil {
		return err
	}

	buf2 := make([]byte, 8)
	base64.RawStdEncoding.Encode(buf2, buf)
	password = string(buf2)

	var lcs []turn.ListenerConfig
	var pccs []turn.PacketConnConfig

	if addr.IP != nil && !addr.IP.IsUnspecified() {
		a := addr.IP.To4()
		if a == nil {
			return errors.New("couldn't parse address")
		}
		pcc, lc := listener(net.IP{0, 0, 0, 0}, addr.Port, a)
		if pcc != nil {
			pccs = append(pccs, *pcc)
			server.addresses = append(server.addresses, &net.UDPAddr{
				IP:   a,
				Port: addr.Port,
			})
		}
		if lc != nil {
			lcs = append(lcs, *lc)
			server.addresses = append(server.addresses, &net.TCPAddr{
				IP:   a,
				Port: addr.Port,
			})
		}
	} else {
		as, err := publicAddresses()
		if err != nil {
			return err
		}

		if len(as) == 0 {
			return errors.New("no public addresses")
		}

		for _, a := range as {
			pcc, lc := listener(a, addr.Port, nil)
			if pcc != nil {
				pccs = append(pccs, *pcc)
				server.addresses = append(server.addresses,
					&net.UDPAddr{
						IP:   a,
						Port: addr.Port,
					},
				)
			}
			if lc != nil {
				lcs = append(lcs, *lc)
				server.addresses = append(server.addresses,
					&net.TCPAddr{
						IP:   a,
						Port: addr.Port,
					},
				)
			}
		}
	}

	if len(pccs) == 0 && len(lcs) == 0 {
		return errors.New("couldn't establish any listeners")
	}

	log.Printf("Starting built-in TURN server on %v", addr.String())

	server.server, err = turn.NewServer(turn.ServerConfig{
		Realm: "galene.org",
		AuthHandler: func(u, r string, src net.Addr) ([]byte, bool) {
			if u != username || r != "galene.org" {
				return nil, false
			}
			return turn.GenerateAuthKey(u, r, password), true
		},
		ListenerConfigs:   lcs,
		PacketConnConfigs: pccs,
	})

	if err != nil {
		server.addresses = nil
		return err
	}

	return nil
}

func ICEServers() []webrtc.ICEServer {
	server.mu.Lock()
	defer server.mu.Unlock()

	if len(server.addresses) == 0 {
		return nil
	}

	var urls []string
	for _, a := range server.addresses {
		switch a := a.(type) {
		case *net.UDPAddr:
			urls = append(urls, "turn:"+a.String())
		case *net.TCPAddr:
			urls = append(urls, "turn:"+a.String()+"?transport=tcp")
		default:
			log.Printf("unexpected TURN address %T", a)
		}
	}

	return []webrtc.ICEServer{
		{
			URLs:       urls,
			Username:   username,
			Credential: password,
		},
	}

}

func Stop() error {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.addresses = nil
	if server.server == nil {
		return nil
	}
	log.Printf("Stopping built-in TURN server")
	err := server.server.Close()
	server.server = nil
	return err
}

func StartStop(start bool) error {
	if Address == "auto" {
		if start {
			return Start()
		}
		return Stop()
	} else if Address == "" {
		return Stop()
	}
	return Start()
}
