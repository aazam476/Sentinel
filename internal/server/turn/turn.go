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
}

// Relay is a running TURN server.
type Relay struct {
	server  *pionturn.Server
	conn    net.PacketConn
	secret  []byte
	relayIP netip.Addr
	port    int
	clock   Clock
	logger  *slog.Logger
}

// Start listens on options.Listen and relays for holders of credentials
// from ICEServer.
func Start(ctx context.Context, options Options) (*Relay, error) {
	conn, err := (&net.ListenConfig{}).ListenPacket(ctx, "udp4", options.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen for TURN on %s: %w", options.Listen, err)
	}
	relay := &Relay{conn: conn, secret: make([]byte, 32), relayIP: options.RelayIP, clock: options.Clock, logger: options.Logger}
	_, _ = rand.Read(relay.secret) // crypto/rand.Read never fails
	if address, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		relay.port = address.Port
	}
	relay.server, err = pionturn.NewServer(pionturn.ServerConfig{
		Realm:       realm,
		AuthHandler: relay.authenticate,
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
	case address.IsLoopback():
		return r.relayIP.IsLoopback()
	case address.IsLinkLocalUnicast(), address.IsMulticast(), address.IsUnspecified(), address.IsInterfaceLocalMulticast():
		return false
	default:
		return true
	}
}
