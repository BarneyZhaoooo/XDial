//go:build linux && !android

package linuxcapture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"time"

	mnl "github.com/mdlayher/netlink"
	"github.com/sagernet/nftables"
	"github.com/sagernet/nftables/binaryutil"
	"github.com/sagernet/nftables/expr"
	rtnl "github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const routeProtocol = unix.RTPROT_STATIC // ownership also requires the journal, exact scope, and random realm

type kernel struct {
	rt     *rtnl.Handle
	nft    *nftables.Conn
	socket *mnl.Conn
}

func openKernel() (*kernel, error) {
	rt, e := rtnl.NewHandle(unix.NETLINK_ROUTE)
	if e != nil {
		return nil, e
	}
	if e = rt.SetSocketTimeout(3 * time.Second); e != nil {
		rt.Close()
		return nil, e
	}
	k := &kernel{rt: rt}
	k.nft, e = nftables.New(nftables.AsLasting(), nftables.WithSockOptions(func(c *mnl.Conn) error { k.socket = c; return nil }))
	if e != nil {
		rt.Close()
		return nil, e
	}
	return k, nil
}

func (k *kernel) deadline() error { return k.socket.SetDeadline(time.Now().Add(3 * time.Second)) }
func (k *kernel) close() {
	if k.nft != nil {
		k.nft.CloseLasting()
	}
	k.rt.Close()
}
func table(j *journal) *nftables.Table {
	return &nftables.Table{Family: nftables.TableFamilyIPv4, Name: "xdial_" + j.Token, Flags: 2 /* NFT_TABLE_F_OWNER, Linux UAPI nf_tables.h */}
}
func route(j *journal) *rtnl.Route {
	return &rtnl.Route{LinkIndex: j.Loopback, Family: unix.AF_INET, Table: j.Plan.Table, Type: unix.RTN_LOCAL, Scope: rtnl.SCOPE_HOST, Protocol: routeProtocol, Realm: int(j.Realm), Dst: &net.IPNet{IP: net.IP{0, 0, 0, 0}, Mask: net.CIDRMask(0, 32)}}
}
func rule(j *journal) *rtnl.Rule {
	r := rtnl.NewRule()
	r.Family = unix.AF_INET
	r.Table = j.Plan.Table
	r.Priority = j.Plan.Priority
	r.Mark = j.Plan.CaptureMark
	m := j.Plan.CaptureMark
	r.Mask = &m
	r.Protocol = routeProtocol
	r.Type = unix.RTN_UNICAST
	return r
}

func (k *kernel) tables() ([]*nftables.Table, error) {
	if e := k.deadline(); e != nil {
		return nil, e
	}
	return k.nft.ListTables()
}
func (k *kernel) tablePresent(j *journal) (bool, error) {
	ts, e := k.tables()
	if e != nil {
		return false, e
	}
	for _, t := range ts {
		if t.Family == table(j).Family && t.Name == table(j).Name {
			return true, nil
		}
	}
	return false, nil
}

func (k *kernel) preflight(j *journal) error {
	// Legacy packet mark programs cannot be inspected through nft netlink.
	for _, path := range []string{"/proc/net/ip_tables_names", "/proc/net/ip6_tables_names"} {
		b, e := os.ReadFile(path)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if strings.TrimSpace(string(b)) != "" {
			return errors.New("legacy iptables requires an explicit coexistence adapter")
		}
	}
	for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
		rs, e := k.rt.RouteListFiltered(family, &rtnl.Route{Table: j.Plan.Table}, rtnl.RT_FILTER_TABLE)
		if e != nil {
			return e
		}
		if len(rs) != 0 {
			return errors.New("reserved route table is occupied")
		}
		rules, e := k.rt.RuleList(family)
		if e != nil {
			return e
		}
		for _, r := range rules {
			if family == unix.AF_INET && r.Priority < j.Plan.Priority && (r.Priority != 0 || r.Table != unix.RT_TABLE_LOCAL) {
				return errors.New("capture priority would be shadowed by existing policy")
			}
			mask := uint32(0)
			if r.Mask != nil {
				mask = *r.Mask
			} else if r.Mark != 0 {
				mask = ^uint32(0)
			}
			if r.Table == j.Plan.Table || r.Priority == j.Plan.Priority || mask&(j.Plan.CaptureMark|j.Plan.BypassMark) != 0 {
				return errors.New("route rule reservation conflicts with existing policy")
			}
		}
	}
	ts, e := k.tables()
	if e != nil {
		return e
	}
	for _, t := range ts {
		if t.Name == table(j).Name {
			return errors.New("capture table already exists")
		}
	}
	if e = k.deadline(); e != nil {
		return e
	}
	chains, e := k.nft.ListChains()
	if e != nil {
		return e
	}
	for _, c := range chains {
		if e = k.deadline(); e != nil {
			return e
		}
		rules, e := k.nft.GetRules(c.Table, c)
		if e != nil {
			return e
		}
		for _, r := range rules {
			for _, x := range r.Exprs {
				switch v := x.(type) {
				case *expr.Meta:
					if v.Key == expr.MetaKeyMARK {
						return errors.New("existing nft mark policy requires a coexistence reservation")
					}
				case *expr.Ct:
					if v.Key == expr.CtKeyMARK {
						return errors.New("existing conntrack mark policy requires a coexistence reservation")
					}
				}
			}
		}
	}
	return nil
}

// Each install batch is atomic and owned by this lasting socket. Kernel socket
// teardown (including SIGKILL) removes capture, even before journal recovery.
func (k *kernel) installCapture(j *journal) error {
	if e := k.deadline(); e != nil {
		return e
	}
	t := k.nft.CreateTable(table(j))
	output := k.nft.AddChain(&nftables.Chain{Name: "output", Table: t, Type: nftables.ChainTypeRoute, Hooknum: nftables.ChainHookOutput, Priority: nftables.ChainPriorityMangle})
	input := k.nft.AddChain(&nftables.Chain{Name: "input", Table: t, Type: nftables.ChainTypeFilter, Hooknum: nftables.ChainHookPrerouting, Priority: nftables.ChainPriorityMangle})
	add := func(c *nftables.Chain, x ...expr.Any) { k.nft.AddRule(&nftables.Rule{Table: t, Chain: c, Exprs: x}) }
	for _, c := range []*nftables.Chain{output, input} {
		add(c, &expr.Meta{Key: expr.MetaKeyMARK, Register: 1}, &expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: binaryutil.NativeEndian.PutUint32(j.Plan.BypassMark), Xor: make([]byte, 4)}, &expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: make([]byte, 4)}, &expr.Verdict{Kind: expr.VerdictReturn})
	}
	// Only host-originated flows are captured. Forwarded packets, link-local,
	// multicast and broadcast remain with the opaque Underlay. Private unicast
	// is deliberately not excluded and is still decided by sing-box.
	for _, prefix := range []string{"169.254.0.0/16", "224.0.0.0/4", "255.255.255.255/32"} {
		p := netip.MustParsePrefix(prefix)
		a := p.Addr().As4()
		mask := net.CIDRMask(p.Bits(), 32)
		add(output, &expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4}, &expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: mask, Xor: make([]byte, 4)}, &expr.Cmp{Register: 1, Data: a[:]}, &expr.Verdict{Kind: expr.VerdictReturn})
	}
	a := netip.MustParseAddrPort(j.Plan.Listener)
	ip := a.Addr().As4()
	for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
		add(output, &expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1}, &expr.Cmp{Register: 1, Data: []byte{proto}}, &expr.Meta{Key: expr.MetaKeyMARK, Register: 1}, &expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: binaryutil.NativeEndian.PutUint32(^j.Plan.CaptureMark), Xor: binaryutil.NativeEndian.PutUint32(j.Plan.CaptureMark)}, &expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1})
		add(input, &expr.Meta{Key: expr.MetaKeyIIF, Register: 1}, &expr.Cmp{Register: 1, Data: binaryutil.NativeEndian.PutUint32(uint32(j.Loopback))}, &expr.Meta{Key: expr.MetaKeyMARK, Register: 1}, &expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: binaryutil.NativeEndian.PutUint32(j.Plan.CaptureMark), Xor: make([]byte, 4)}, &expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: make([]byte, 4)}, &expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1}, &expr.Cmp{Register: 1, Data: []byte{proto}}, &expr.Immediate{Register: 1, Data: ip[:]}, &expr.Immediate{Register: 2, Data: binaryutil.BigEndian.PutUint16(a.Port())}, &expr.TProxy{Family: unix.NFPROTO_IPV4, RegAddr: 1, RegPort: 2}, &expr.Verdict{Kind: expr.VerdictAccept})
	}
	return k.nft.Flush()
}

func (k *kernel) removeCapture(j *journal) error {
	present, e := k.tablePresent(j)
	if e != nil || !present {
		return e
	}
	if e = k.deadline(); e != nil {
		return e
	}
	k.nft.DelTable(table(j))
	return k.nft.Flush()
}

// Never flush a routing table or delete by priority alone. Validate the entire
// reserved scope before deleting anything; changed/extra objects are a conflict.
func (k *kernel) cleanupRoutes(j *journal) error {
	if e := verifyRuleWire(j); e != nil {
		return e
	}
	rules, e := k.rt.RuleList(unix.AF_INET)
	if e != nil {
		return e
	}
	var ownedRules []rtnl.Rule
	for _, r := range rules {
		if r.Priority == j.Plan.Priority || r.Table == j.Plan.Table {
			want := rule(j)
			want.Type = 0 // this dependency omits the action; verifyRuleWire checks it.
			if j.Intent < 2 || !reflect.DeepEqual(r, *want) {
				return fmt.Errorf("capture rule ownership changed")
			}
			ownedRules = append(ownedRules, r)
		}
	}
	if len(ownedRules) > 1 {
		return errors.New("ambiguous duplicate capture rule")
	}
	routes, e := k.rt.RouteListFiltered(unix.AF_INET, &rtnl.Route{Table: j.Plan.Table}, rtnl.RT_FILTER_TABLE)
	if e != nil {
		return e
	}
	for _, r := range routes {
		want := route(j)
		// rtnetlink represents a default destination as nil on some kernels.
		if r.Dst == nil {
			r.Dst = want.Dst
		}
		// The pinned decoder synthesizes default IPv4 as a 16-byte mapped
		// address, while explicit destinations use 4 bytes. Compare canonical
		// address representation without dropping any route attributes.
		if ip := r.Dst.IP.To4(); ip != nil {
			r.Dst = &net.IPNet{IP: ip, Mask: r.Dst.Mask}
		}
		if j.Intent < 1 || !reflect.DeepEqual(r, *want) {
			return errors.New("capture route ownership changed")
		}
	}
	if len(routes) > 1 {
		return errors.New("ambiguous capture routes")
	}
	for _, r := range ownedRules {
		if e = k.rt.RuleDel(&r); e != nil {
			return e
		}
	}
	for _, r := range routes {
		if e = k.rt.RouteDel(&r); e != nil {
			return e
		}
	}
	return nil
}

// The pinned high-level RuleList drops the action and unknown attributes.
// Recovery also validates the complete raw fib_rule_hdr/attributes, so a
// replacement blackhole/unreachable/extra-selector rule is never removed.
func verifyRuleWire(j *journal) error {
	c, e := mnl.Dial(unix.NETLINK_ROUTE, nil)
	if e != nil {
		return e
	}
	defer c.Close()
	if e = c.SetDeadline(time.Now().Add(3 * time.Second)); e != nil {
		return e
	}
	h := make([]byte, 12)
	h[0] = unix.AF_INET
	ms, e := c.Execute(mnl.Message{Header: mnl.Header{Type: unix.RTM_GETRULE, Flags: mnl.Request | mnl.Dump}, Data: h})
	if e != nil {
		return e
	}
	for _, m := range ms {
		if len(m.Data) < 12 {
			return errors.New("short kernel rule")
		}
		attrs, e := mnl.UnmarshalAttributes(m.Data[12:])
		if e != nil {
			return e
		}
		var priority, tableID uint32
		tableID = uint32(m.Data[4])
		for _, a := range attrs {
			if len(a.Data) == 4 {
				switch a.Type {
				case unix.FRA_PRIORITY:
					priority = binary.NativeEndian.Uint32(a.Data)
				case unix.FRA_TABLE:
					tableID = binary.NativeEndian.Uint32(a.Data)
				}
			}
		}
		if priority != uint32(j.Plan.Priority) && tableID != uint32(j.Plan.Table) {
			continue
		}
		header := make([]byte, 12)
		header[0] = unix.AF_INET
		// fib_nl_fill_rule uses this compatibility byte for tables >= 256;
		// the exact 32-bit identity remains mandatory in FRA_TABLE below.
		header[4] = unix.RT_TABLE_COMPAT
		header[7] = unix.RTN_UNICAST
		if j.Intent < 2 || !bytes.Equal(m.Data[:12], header) {
			return errors.New("capture rule header ownership changed")
		}
		expected := map[uint16][]byte{unix.FRA_TABLE: binaryutil.NativeEndian.PutUint32(uint32(j.Plan.Table)), unix.FRA_PRIORITY: binaryutil.NativeEndian.PutUint32(uint32(j.Plan.Priority)), unix.FRA_FWMARK: binaryutil.NativeEndian.PutUint32(j.Plan.CaptureMark), unix.FRA_FWMASK: binaryutil.NativeEndian.PutUint32(j.Plan.CaptureMark), unix.FRA_PROTOCOL: {routeProtocol}}
		for _, a := range attrs {
			if (a.Type == unix.FRA_SUPPRESS_PREFIXLEN || a.Type == unix.FRA_SUPPRESS_IFGROUP) && bytes.Equal(a.Data, []byte{255, 255, 255, 255}) {
				continue
			}
			want, ok := expected[a.Type]
			if !ok || !bytes.Equal(want, a.Data) {
				return errors.New("capture rule attributes changed")
			}
			delete(expected, a.Type)
		}
		if len(expected) != 0 {
			return errors.New("capture rule identity incomplete")
		}
	}
	return nil
}
