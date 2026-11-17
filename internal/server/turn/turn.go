// SPDX-License-Identifier: Apache-2.0

package turn

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	pionturn "github.com/pion/turn/v5"

	"github.com/aazam476/sentinel/internal/protocol"
)

// realm names this relay in its challenges.
const realm = "sentinel"

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Options configures the relay.
type Options struct {
	// Listen is the UDP address to listen on, such as ":3478".
	Listen string
	// RelayIP is the address browsers and agents reach the relay at.
	RelayIP netip.Addr
	// FirstPort and LastPort bound the UDP ports relayed connections use;
	// zero lets the system choose.
	FirstPort, LastPort uint16
	Clock               Clock
	Logger              *slog.Logger
	// OwnAddresses lists this machine's addresses, which the relay won't
	// send to; nil reads them from the network interfaces.
	OwnAddresses func() []netip.Addr
}

// Quotas on allocations: a remote desktop session's credentials are used
// by its browser and its agent, which each need one or two.
const (
	maxAllocationsPerUser = 4
	maxAllocations        = 200
)

// Relay is a running TURN server.
type Relay struct {
	server  *pionturn.Server
	conn    net.PacketConn
	secret  []byte
	relayIP netip.Addr
	port    int
	clock   Clock
	logger  *slog.Logger
	// own are this machine's addresses: relaying to them would reach the
	// server's own services from outside.
	own   map[netip.Addr]bool
	quota quota
}

// Start listens on options.Listen and relays for holders of credentials
// from ICEServer.
func Start(ctx context.Context, options Options) (*Relay, error) {
	conn, err := (&net.ListenConfig{}).ListenPacket(ctx, "udp4", options.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen for TURN on %s: %w", options.Listen, err)
	}
	relay := &Relay{conn: conn, secret: make([]byte, 32), relayIP: options.RelayIP, clock: options.Clock, logger: options.Logger,
		own: ownAddresses(options), quota: quota{perUser: map[string]int{}}}
	_, _ = rand.Read(relay.secret) // crypto/rand.Read never fails
	if address, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		relay.port = address.Port
	}
	relay.server, err = pionturn.NewServer(pionturn.ServerConfig{
		Realm:        realm,
		AuthHandler:  relay.authenticate,
		QuotaHandler: relay.quota.allow,
		EventHandler: pionturn.EventHandler{
			OnAllocationCreated: func(_, _ net.Addr, _, username, _ string, _ net.Addr, _ int) { relay.quota.add(username, 1) },
			OnAllocationDeleted: func(_, _ net.Addr, _, username, _ string) { relay.quota.add(username, -1) },
		},
		PacketConnConfigs: []pionturn.PacketConnConfig{{
			PacketConn:            conn,
			RelayAddressGenerator: relayAddresses(options),
			PermissionHandler:     relay.permitted,
		}},
	})
	if err != nil {
		_ = conn.Close() // the relay never started
		return nil, fmt.Errorf("start the TURN relay: %w", err)
	}
	return relay, nil
}

// relayAddresses makes relayed connections' sockets: in the configured
// port range, so a firewall or Docker can let them through.
func relayAddresses(options Options) pionturn.RelayAddressGenerator {
	address := net.IP(options.RelayIP.AsSlice())
	if options.FirstPort == 0 {
		return &pionturn.RelayAddressGeneratorStatic{RelayAddress: address, Address: "0.0.0.0"}
	}
	return &pionturn.RelayAddressGeneratorPortRange{
		RelayAddress: address, Address: "0.0.0.0", MinPort: options.FirstPort, MaxPort: options.LastPort,
	}
}

// Close stops relaying.
func (r *Relay) Close() error {
	if err := r.server.Close(); err != nil {
		return fmt.Errorf("stop the TURN relay: %w", err)
	}
	return nil
}

// Port is the UDP port the relay listens on.
func (r *Relay) Port() int { return r.port }

// ICEServer returns this relay with credentials that work for ttl.
func (r *Relay) ICEServer(ttl time.Duration) protocol.ICEServer {
	name := make([]byte, 8)
	_, _ = rand.Read(name) // crypto/rand.Read never fails
	username := strconv.FormatInt(r.clock.Now().Add(ttl).Unix(), 10) + ":" + hex.EncodeToString(name)
	address := netip.AddrPortFrom(r.relayIP, uint16(r.port)) //nolint:gosec // G115: a UDP port fits
	return protocol.ICEServer{
		URLs:       []string{"turn:" + address.String() + "?transport=udp"},
		Username:   username,
		Credential: r.password(username),
	}
}

// password is the credential for username: an HMAC only this relay can make.
func (r *Relay) password(username string) string {
	mac := hmac.New(sha256.New, r.secret)
	mac.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// errExpired means a username's expiry time has passed.
var errExpired = errors.New("the TURN credentials have expired")

// authenticate gives Pion the key for a username this relay made, unless
// it has expired.
func (r *Relay) authenticate(attributes *pionturn.RequestAttributes) (string, []byte, bool) {
	if err := r.checkUsername(attributes.Username); err != nil {
		r.logger.Debug("refused a TURN request", "from", attributes.SrcAddr.String(), "error", err)
		return "", nil, false
	}
	return attributes.Username, pionturn.GenerateAuthKey(attributes.Username, realm, r.password(attributes.Username)), true
}

func (r *Relay) checkUsername(username string) error {
	expiry, _, ok := strings.Cut(username, ":")
	if !ok {
		return fmt.Errorf("username %q has no expiry time", username)
	}
	seconds, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		return fmt.Errorf("username %q has no expiry time", username)
	}
	if !r.clock.Now().Before(time.Unix(seconds, 0)) {
		return errExpired
	}
	return nil
}

// permitted says whether the relay may send to a peer. It refuses the
// addresses that would let a signed-in user reach the server's own
// services through it: loopback (unless the relay itself is on loopback,
// as in tests), link-local, multicast and unspecified addresses.
func (r *Relay) permitted(_ net.Addr, peer net.IP) bool {
	address, ok := netip.AddrFromSlice(peer)
	if !ok {
		return false
	}
	address = address.Unmap()
	switch {
	case r.own[address], address == r.relayIP && !address.IsLoopback():
		return false
	case address.IsLoopback():
		return r.relayIP.IsLoopback()
	case address.IsLinkLocalUnicast(), address.IsMulticast(), address.IsUnspecified(), address.IsInterfaceLocalMulticast():
		return false
	default:
		return true
	}
}

// ownAddresses is the set of addresses the relay won't send to.
func ownAddresses(options Options) map[netip.Addr]bool {
	list := options.OwnAddresses
	if list == nil {
		list = interfaceAddresses
	}
	own := map[netip.Addr]bool{}
	for _, address := range list() {
		if !address.IsLoopback() {
			own[address.Unmap()] = true
		}
	}
	return own
}

// interfaceAddresses are the addresses of this machine's network
// interfaces.
func interfaceAddresses() []netip.Addr {
	networks, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var addresses []netip.Addr
	for _, network := range networks {
		if prefix, err := netip.ParsePrefix(network.String()); err == nil {
			addresses = append(addresses, prefix.Addr())
		}
	}
	return addresses
}

// quota counts allocations, per user and in all.
type quota struct {
	mutex   sync.Mutex
	perUser map[string]int
	total   int
}

// allow says whether a user may have another allocation.
func (q *quota) allow(username, _ string, _ net.Addr) bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return q.perUser[username] < maxAllocationsPerUser && q.total < maxAllocations
}

// add counts an allocation made (1) or ended (-1).
func (q *quota) add(username string, change int) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.perUser[username] += change
	q.total += change
	if q.perUser[username] <= 0 {
		delete(q.perUser, username)
	}
}
