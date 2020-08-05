package socks5

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/proxy"

	"github.com/thinkgos/go-socks5/statute"
)

func TestSOCKS5_Connect(t *testing.T) {
	// Create a local listener
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	go func() {
		conn, err := l.Accept()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		defer conn.Close()

		buf := make([]byte, 4)
		if _, err := io.ReadAtLeast(conn, buf, 4); err != nil {
			t.Fatalf("err: %v", err)
		}

		if !bytes.Equal(buf, []byte("ping")) {
			t.Fatalf("bad: %v", buf)
		}
		_, _ = conn.Write([]byte("pong"))
	}()
	lAddr := l.Addr().(*net.TCPAddr)

	// Create a socks server
	cator := UserPassAuthenticator{
		Credentials: StaticCredentials{"foo": "bar"},
	}
	serv := New(
		WithAuthMethods([]Authenticator{cator}),
		WithLogger(NewLogger(log.New(os.Stdout, "socks5: ", log.LstdFlags))),
	)

	// Start listening
	go func() {
		if err := serv.ListenAndServe("tcp", "127.0.0.1:12365"); err != nil {
			t.Fatalf("err: %v", err)
		}
	}()
	time.Sleep(10 * time.Millisecond)

	// Get a local conn
	conn, err := net.Dial("tcp", "127.0.0.1:12365")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Connect, auth and connec to local
	req := new(bytes.Buffer)
	req.Write([]byte{statute.VersionSocks5, 2, statute.MethodNoAuth, statute.MethodUserPassAuth})
	req.Write([]byte{statute.UserPassAuthVersion, 3, 'f', 'o', 'o', 3, 'b', 'a', 'r'})
	reqHead := statute.Header{
		Version:  statute.VersionSocks5,
		Command:  statute.CommandConnect,
		Reserved: 0,
		Address: statute.AddrSpec{
			"",
			net.ParseIP("127.0.0.1"),
			lAddr.Port,
		},
		AddrType: statute.ATYPIPv4,
	}
	req.Write(reqHead.Bytes())
	// Send a ping
	req.Write([]byte("ping"))

	// Send all the bytes
	conn.Write(req.Bytes())

	// Verify response
	expected := []byte{
		statute.VersionSocks5, statute.MethodUserPassAuth, // use user password auth
		statute.UserPassAuthVersion, statute.AuthSuccess, // response auth success
	}
	rspHead := statute.Header{
		Version:  statute.VersionSocks5,
		Command:  statute.RepSuccess,
		Reserved: 0,
		Address: statute.AddrSpec{
			"",
			net.ParseIP("127.0.0.1"),
			0, // Ignore the port
		},
		AddrType: statute.ATYPIPv4,
	}
	expected = append(expected, rspHead.Bytes()...)
	expected = append(expected, []byte("pong")...)

	out := make([]byte, len(expected))
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(conn, out); err != nil {
		t.Fatalf("err: %v", err)
	}

	t.Logf("proxy bind port: %d", statute.BuildPort(out[12], out[13]))

	// Ignore the port
	out[12] = 0
	out[13] = 0

	if !bytes.Equal(out, expected) {
		t.Fatalf("bad: %v", out)
	}
}

func TestSOCKS5_Associate(t *testing.T) {
	locIP := net.ParseIP("127.0.0.1")
	// Create a local listener
	lAddr := &net.UDPAddr{
		IP:   locIP,
		Port: 12398,
	}
	l, err := net.ListenUDP("udp", lAddr)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer l.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, remote, err := l.ReadFrom(buf)
			if err != nil {
				return
			}

			if !bytes.Equal(buf[:n], []byte("ping")) {
				t.Fatalf("bad: %v", buf)
			}
			_, _ = l.WriteTo([]byte("pong"), remote)
		}
	}()

	// Create a socks server
	cator := UserPassAuthenticator{Credentials: StaticCredentials{"foo": "bar"}}
	serv := New(
		WithAuthMethods([]Authenticator{cator}),
		WithLogger(NewLogger(log.New(os.Stdout, "socks5: ", log.LstdFlags))),
	)
	// Start listening
	go func() {
		if err := serv.ListenAndServe("tcp", "127.0.0.1:12355"); err != nil {
			t.Fatalf("err: %v", err)
		}
	}()
	time.Sleep(10 * time.Millisecond)

	// Get a local conn
	conn, err := net.Dial("tcp", "127.0.0.1:12355")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Connect, auth and connec to local
	req := new(bytes.Buffer)
	req.Write([]byte{statute.VersionSocks5, 2, statute.MethodNoAuth, statute.MethodUserPassAuth})
	req.Write([]byte{statute.UserPassAuthVersion, 3, 'f', 'o', 'o', 3, 'b', 'a', 'r'})
	reqHead := statute.Header{
		Version:  statute.VersionSocks5,
		Command:  statute.CommandAssociate,
		Reserved: 0,
		Address: statute.AddrSpec{
			"",
			locIP,
			lAddr.Port,
		},
		AddrType: statute.ATYPIPv4,
	}
	req.Write(reqHead.Bytes())
	// Send all the bytes
	conn.Write(req.Bytes())

	// Verify response
	expected := []byte{
		statute.VersionSocks5, statute.MethodUserPassAuth, // use user password auth
		statute.UserPassAuthVersion, statute.AuthSuccess, // response auth success
	}

	out := make([]byte, len(expected))
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(conn, out); err != nil {
		t.Fatalf("err: %v", err)
	}

	if !bytes.Equal(out, expected) {
		t.Fatalf("bad: %v", out)
	}

	rspHead, err := statute.ParseHeader(conn)
	if err != nil {
		t.Fatalf("bad response header: %v", err)
	}
	if rspHead.Version != statute.VersionSocks5 && rspHead.Command != statute.RepSuccess {
		t.Fatalf("parse success but bad header: %v", rspHead)
	}

	t.Logf("proxy bind listen port: %d", rspHead.Address.Port)

	udpConn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   locIP,
		Port: rspHead.Address.Port,
	})
	if err != nil {
		t.Fatalf("bad dial: %v", err)
	}
	// Send a ping
	_, _ = udpConn.Write(append([]byte{0, 0, 0, statute.ATYPIPv4, 0, 0, 0, 0, 0, 0}, []byte("ping")...))
	response := make([]byte, 1024)
	n, _, err := udpConn.ReadFrom(response)
	if err != nil || !bytes.Equal(response[n-4:n], []byte("pong")) {
		t.Fatalf("bad udp read: %v", string(response[:n]))
	}
	time.Sleep(time.Second * 1)
}

func Test_SocksWithProxy(t *testing.T) {
	// Create a local listener
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	go func() {
		conn, err := l.Accept()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		defer conn.Close()

		buf := make([]byte, 4)
		if _, err := io.ReadAtLeast(conn, buf, 4); err != nil {
			t.Fatalf("err: %v", err)
		}

		if !bytes.Equal(buf, []byte("ping")) {
			t.Fatalf("bad: %v", buf)
		}
		conn.Write([]byte("pong"))
	}()
	lAddr := l.Addr().(*net.TCPAddr)

	// Create a socks server
	cator := UserPassAuthenticator{Credentials: StaticCredentials{"foo": "bar"}}
	serv := New(
		WithAuthMethods([]Authenticator{cator}),
		WithLogger(NewLogger(log.New(os.Stdout, "socks5: ", log.LstdFlags))),
	)

	// Start listening
	go func() {
		if err := serv.ListenAndServe("tcp", "127.0.0.1:12395"); err != nil {
			t.Fatalf("err: %v", err)
		}
	}()
	time.Sleep(10 * time.Millisecond)

	dial, err := proxy.SOCKS5("tcp", "127.0.0.1:12395", &proxy.Auth{User: "foo", Password: "bar"}, proxy.Direct)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Connect, auth and connect to local
	conn, err := dial.Dial("tcp", lAddr.String())
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Send a ping
	_, _ = conn.Write([]byte("ping"))

	out := make([]byte, 4)
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(conn, out); err != nil {
		t.Fatalf("err: %v", err)
	}

	if !bytes.Equal(out, []byte("pong")) {
		t.Fatalf("bad: %v", out)
	}
}

/*****************************    auth         *******************************/

func TestNoAuth_Server(t *testing.T) {
	req := bytes.NewBuffer(nil)
	rsp := new(bytes.Buffer)
	s := New()

	ctx, err := s.authenticate(rsp, req, "", []byte{statute.MethodNoAuth})
	require.NoError(t, err)
	assert.Equal(t, statute.MethodNoAuth, ctx.Method)
	assert.Equal(t, []byte{statute.VersionSocks5, statute.MethodNoAuth}, rsp.Bytes())
}

func TestPasswordAuth_Valid_Server(t *testing.T) {
	req := bytes.NewBuffer([]byte{1, 3, 'f', 'o', 'o', 3, 'b', 'a', 'r'})
	rsp := new(bytes.Buffer)
	cator := UserPassAuthenticator{
		StaticCredentials{
			"foo": "bar",
		},
	}
	s := New(WithAuthMethods([]Authenticator{cator}))

	ctx, err := s.authenticate(rsp, req, "", []byte{statute.MethodUserPassAuth})
	require.NoError(t, err)
	assert.Equal(t, statute.MethodUserPassAuth, ctx.Method)

	val, ok := ctx.Payload["username"]
	require.True(t, ok)
	require.Equal(t, "foo", val)

	val, ok = ctx.Payload["password"]
	require.True(t, ok)
	require.Equal(t, "bar", val)

	assert.Equal(t, []byte{statute.VersionSocks5, statute.MethodUserPassAuth, 1, statute.AuthSuccess}, rsp.Bytes())
}

func TestPasswordAuth_Invalid_Server(t *testing.T) {
	req := bytes.NewBuffer([]byte{1, 3, 'f', 'o', 'o', 3, 'b', 'a', 'z'})
	rsp := new(bytes.Buffer)
	cator := UserPassAuthenticator{
		StaticCredentials{
			"foo": "bar",
		},
	}
	s := New(WithAuthMethods([]Authenticator{cator}))

	ctx, err := s.authenticate(rsp, req, "", []byte{statute.MethodNoAuth, statute.MethodUserPassAuth})
	require.True(t, errors.Is(err, statute.ErrUserAuthFailed))
	require.Nil(t, ctx)

	assert.Equal(t, []byte{statute.VersionSocks5, statute.MethodUserPassAuth, 1, statute.AuthFailure}, rsp.Bytes())
}

func TestNoSupportedAuth_Server(t *testing.T) {
	req := bytes.NewBuffer(nil)
	rsp := new(bytes.Buffer)
	cator := UserPassAuthenticator{
		StaticCredentials{
			"foo": "bar",
		},
	}

	s := New(WithAuthMethods([]Authenticator{cator}))

	ctx, err := s.authenticate(rsp, req, "", []byte{statute.MethodNoAuth})
	require.True(t, errors.Is(err, statute.ErrNoSupportedAuth))
	require.Nil(t, ctx)

	assert.Equal(t, []byte{statute.VersionSocks5, statute.MethodNoAcceptable}, rsp.Bytes())
}