//go:build linux && !android

package controlpipe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// UnixIdentity is attached by the kernel to each seqpacket message. Accept-time
// credentials alone are insufficient: the connected descriptor can be passed.
type UnixIdentity struct {
	UID, GID uint32
	PID      int32
}
type UnixHandler func(context.Context, UnixIdentity, Request) Response
type UnixServer struct {
	listener *net.UnixListener
	uid      uint32
	handler  UnixHandler
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	conns    map[*net.UnixConn]bool
	wg       sync.WaitGroup
}

func unixCredential(c *net.UnixConn) (*unix.Ucred, error) {
	raw, e := c.SyscallConn()
	if e != nil {
		return nil, e
	}
	var cred *unix.Ucred
	var inner error
	e = raw.Control(func(fd uintptr) { cred, inner = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	return cred, errors.Join(e, inner)
}
func unixPassCred(c *net.UnixConn) error {
	raw, e := c.SyscallConn()
	if e != nil {
		return e
	}
	var inner error
	e = raw.Control(func(fd uintptr) { inner = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1) })
	return errors.Join(e, inner)
}
func readUnixFrame(c *net.UnixConn, uid uint32, value any) (UnixIdentity, error) {
	b := make([]byte, MaxFrame+5)
	oob := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(4*16))
	n, on, flags, _, e := c.ReadMsgUnix(b, oob)
	if e != nil {
		return UnixIdentity{}, e
	}
	msgs, e := unix.ParseSocketControlMessage(oob[:on])
	if e != nil {
		return UnixIdentity{}, e
	}
	var cred *unix.Ucred
	invalid := flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0
	for _, m := range msgs {
		if m.Header.Level == unix.SOL_SOCKET && m.Header.Type == unix.SCM_RIGHTS {
			fds, _ := unix.ParseUnixRights(&m)
			for _, fd := range fds {
				unix.Close(fd)
			}
			invalid = true
			continue
		}
		if m.Header.Level != unix.SOL_SOCKET || m.Header.Type != unix.SCM_CREDENTIALS || cred != nil {
			invalid = true
			continue
		}
		cred, e = unix.ParseUnixCredentials(&m)
		if e != nil {
			invalid = true
		}
	}
	if invalid || cred == nil || cred.Uid != uid || cred.Pid <= 0 {
		return UnixIdentity{}, errors.New("control peer denied")
	}
	r := bytes.NewReader(b[:n])
	if e = readFrame(r, value); e != nil {
		return UnixIdentity{}, e
	}
	if r.Len() != 0 {
		return UnixIdentity{}, errors.New("trailing packet data")
	}
	return UnixIdentity{cred.Uid, cred.Gid, cred.Pid}, nil
}
func writeUnixFrame(c *net.UnixConn, value any) error {
	var b bytes.Buffer
	if e := writeFrame(&b, value); e != nil {
		return e
	}
	n, _, e := c.WriteMsgUnix(b.Bytes(), nil, nil)
	if e == nil && n != b.Len() {
		e = io.ErrShortWrite
	}
	return e
}

// ListenUnix never unlinks an existing endpoint. The privileged caller must
// hold its installation/state lock and validate the root-owned parent first.
func ListenUnix(path string, allowedUID uint32, handler UnixHandler) (*UnixServer, error) {
	if handler == nil {
		return nil, errors.New("missing control handler")
	}
	l, e := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
	if e != nil {
		return nil, e
	}
	l.SetUnlinkOnClose(true)
	raw, e := l.SyscallConn()
	if e == nil {
		var inner error
		e = raw.Control(func(fd uintptr) { inner = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1) })
		e = errors.Join(e, inner)
	}
	if e == nil {
		e = os.Chmod(path, 0666)
	}
	if e != nil {
		l.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &UnixServer{listener: l, uid: allowedUID, handler: handler, ctx: ctx, cancel: cancel, conns: map[*net.UnixConn]bool{}}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}
func (s *UnixServer) accept() {
	defer s.wg.Done()
	for {
		c, e := s.listener.AcceptUnix()
		if e != nil {
			return
		}
		cred, e := unixCredential(c)
		if e != nil || cred.Uid != s.uid {
			c.Close()
			continue
		}
		s.mu.Lock()
		if s.ctx.Err() != nil || len(s.conns) >= 32 {
			s.mu.Unlock()
			c.Close()
			continue
		}
		s.conns[c] = true
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serve(c)
	}
}
func (s *UnixServer) serve(c *net.UnixConn) {
	defer s.wg.Done()
	defer func() { c.Close(); s.mu.Lock(); delete(s.conns, c); s.mu.Unlock() }()
	var last uint64
	for {
		c.SetDeadline(time.Now().Add(5 * time.Second))
		var req Request
		id, e := readUnixFrame(c, s.uid, &req)
		if e != nil {
			return
		}
		if req.Version != Version || req.ID == 0 || req.ID <= last {
			return
		}
		last = req.ID
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		resp := s.handler(ctx, id, req)
		cancel()
		resp.Version = Version
		resp.ID = req.ID
		if e = writeUnixFrame(c, resp); e != nil {
			return
		}
	}
}
func (s *UnixServer) Close() error {
	s.cancel()
	e := s.listener.Close()
	s.mu.Lock()
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return e
}

// CallUnix opens one authenticated connection per request. No caller identity
// appears in the request. The service UID is pinned before sending any payload.
func CallUnix(ctx context.Context, path string, serverUID uint32, request Request) (Response, error) {
	var response Response
	d := net.Dialer{}
	conn, e := d.DialContext(ctx, "unixpacket", path)
	if e != nil {
		return response, e
	}
	c := conn.(*net.UnixConn)
	defer c.Close()
	cred, e := unixCredential(c)
	if e != nil || cred.Uid != serverUID {
		return response, errors.New("control server denied")
	}
	if e = unixPassCred(c); e != nil {
		return response, e
	}
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	request.Version = Version
	if request.ID == 0 {
		request.ID = 1
	}
	if e = writeUnixFrame(c, request); e != nil {
		return response, e
	}
	if _, e = readUnixFrame(c, serverUID, &response); e != nil {
		return response, e
	}
	if response.Version != Version || response.ID != request.ID {
		return Response{}, errors.New("invalid response identity")
	}
	return response, nil
}
