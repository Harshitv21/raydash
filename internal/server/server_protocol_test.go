package server;

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
 
	"internal/store"
)

/*
server_test.go already covers raw connection-handling concurrency. This file covers protocol-level
correctness that had 0 test before. AUTH gating, INFO's reported numbers, FLUSHALL, and the highest
valued of them all, a value containing literal '\r' & '\n' bytes actually surviving a SET/GET round
trip intact. That last one is the exact thing the binary safe redesign exists for, and stupidly enough
it was not asserted up until now!
*/

/*
startTestServer spins up a real raydash server on a random free port and registers its teardown with
t.Cleanup, so every test below just gets a live addr to dial and never has to think about shutdown
itself.
*/
func startTestServer(t *testing.T, authToken string) (addr string, server *Server) {
	t.Helper();

	store := store.NewStore(100 * time.Millisecond, 0);

	listener, err := net.Listen("tcp", "127.0.0.1:0");
	if(err != nil) { t.Fatalf("failed to bind random port: %v", err); }

	addr = listener.Addr().String();
	listener.Close();

	server = NewServer(addr, store, authToken);

	go func() {
		if err := server.Start(); err != nil { return; }
	}();

	time.Sleep(20 * time.Millisecond); // give Accept()'s loop a moment to actually be listening

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second);
		defer cancel();
		server.Shutdown(ctx);
		store.Close();
	});

	return addr, server;
}

func TestAuthGating(t *testing.T) {
	addr, _ := startTestServer(t, "correct-token");

	conn, err := net.Dial("tcp", addr);
	if(err != nil) { t.Fatalf("failed to dial: %v", err); }
	defer conn.Close();

	reader := bufio.NewReader(conn);

	// before AUTH any command at all should be refused
	conn.Write([]byte("GET somekey\r\n"));
	resp, _ := reader.ReadString('\n');

	if(!strings.HasPrefix(resp, "-ERR NOAUTH")) {
		t.Errorf("expected NOAUTH before authenticating, got: %q", resp);
	}

	// a wrong token should fail, and must NOT authenticate the connection
	conn.Write([]byte("AUTH wrong-token\r\n"));
	resp, _ = reader.ReadString('\n');
	if(!strings.HasPrefix(resp, "-ERR invalid auth token")) {
		t.Errorf("expected invalid auth token error, got: %q", resp);
	}

	conn.Write([]byte("GET somekey\r\n"));
	resp, _ = reader.ReadString('\n');
	if(!strings.HasPrefix(resp, "-ERR NOAUTH")) {
		t.Errorf("expected still NOAUTH after a failed AUTH attempt, got: %q", resp);
	}

	// correct token should succeed however
	conn.Write([]byte("AUTH correct-token\r\n"));
	resp, _ = reader.ReadString('\n');
	if(!strings.HasPrefix(resp, "+OK")) {
		t.Errorf("expected +OK for the correct auth token, got: %q", resp);
	}

	// now commands should go through normally
	conn.Write([]byte("GET somekey\r\n"));
	resp, _ = reader.ReadString('\n');
	if(strings.HasPrefix(resp, "-ERR NOAUTH")) {
		t.Errorf("expected command to succeed after authenticating, got: %q", resp);
	}
}

/*
A server with no token configured should say so plainly when AUTH is attempting anyway, rather than
silently accepting or rejecting it.
*/
func TestAuthNotRequired(t *testing.T) {
	addr, _ := startTestServer(t, "");

	conn, err := net.Dial("tcp", addr);
	if(err != nil) { t.Fatalf("failed to dial: %v", err); }
	defer conn.Close();

	reader := bufio.NewReader(conn);

	conn.Write([]byte("GET somekey\r\n"));
	resp, _ := reader.ReadString('\n');
	if(strings.HasPrefix(resp, "-ERR NOAUTH")) {
		t.Error("a server with no auth token configured should never refuse commands with NOAUTH");
	}

	conn.Write([]byte("AUTH anything\r\n"));
	resp, _ = reader.ReadString('\n');
	if(!strings.HasPrefix(resp, "-ERR AUTH not required")) {
		t.Errorf("expected 'AUTH not required' when no token is configured, got: %q", resp);
	}
}

func TestInfoReportsAccurateCounts(t *testing.T) {
	addr, server := startTestServer(t, "");

	conn, err := net.Dial("tcp", addr);
	if(err != nil) { t.Fatalf("failed to dial: %v", err); }
	defer conn.Close();

	reader := bufio.NewReader(conn);

	conn.Write([]byte("SET a 1\r\nx\r\n"));
	reader.ReadString('\n');

	conn.Write([]byte("SET b 1\r\ny\r\n"));
	reader.ReadString('\n');

	conn.Write([]byte("INFO\r\n"));
	header, err := reader.ReadString('\n');
	if(err != nil) { t.Fatalf("failed to read INFO header: %v", err); }
	header = strings.TrimSpace(header);

	if(!strings.HasPrefix(header, "$")) {
		t.Fatalf("expected a bulk-string header for INFO, got: %q", header);
	}

	length, err := strconv.Atoi(header[1:]);
	if(err != nil) { t.Fatalf("bad length in INFO header %q: %v", header, err); }

	payload := make([]byte, length);
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatalf("failed to read INFO payload: %v", err);
	}
	info := string(payload);

	if(!strings.Contains(info, "key_count:2")) {
		t.Errorf("expected key_count:2 in INFO output after 2 SETs, got:\n%s", info);
	}
	if(!strings.Contains(info, "max_keys:unlimited")) {
		t.Errorf("expected max_keys:unlimited (server started with maxKeys=0), got:\n%s", info);
	}

	server.mut.Lock();
	activeConns := server.connCount;
	server.mut.Unlock();

	if(!strings.Contains(info, fmt.Sprintf("active_connections:%d", activeConns))) {
		t.Errorf("expected active_connections:%d in INFO output, got:\n%s", activeConns, info);
	}
}

func TestFlushAllClearsEverything(t *testing.T) {
	addr, _ := startTestServer(t, "");

	conn, err := net.Dial("tcp", addr);
	if(err != nil) { t.Fatalf("failed to dial: %v", err); }
	defer conn.Close();

	reader := bufio.NewReader(conn);

	conn.Write([]byte("SET a 1\r\nx\r\n"));
	reader.ReadString('\n');

	conn.Write([]byte("SET b 1\r\ny\r\n"));
	reader.ReadString('\n');

	conn.Write([]byte("EXISTS a\r\n"));
	resp, _ := reader.ReadString('\n');
	if(strings.TrimSpace(resp) != ":1") {
		t.Fatalf("setup failed! expected key 'a' to exist before FLUSHALL, got: %q", resp);
	}

	conn.Write([]byte("FLUSHALL\r\n"));
	resp, _ = reader.ReadString('\n');
	if(!strings.HasPrefix(resp, "+OK")) {
		t.Fatalf("expected +OK for FLUSHALL, got: %q", resp);
	}

	for _, key := range []string{"a", "b"} {
		conn.Write([]byte(fmt.Sprintf("EXISTS %s\r\n", key)));
		resp, _ = reader.ReadString('\n');

		if(strings.TrimSpace(resp) != ":0") {
			t.Errorf("expected key %q to be gone after FLUSHALL, got: %q", key, resp);
		}
	}
}

/*
Whole point of length prefixed framing redesign was that a value containing literal '\r' or '\n' bytes
must survive a SET/GET round trip byte-for-byte, instead of getting truncated at the first line break
found inside it. Nothing in this suite actually proved that until we do it right now. This test sends
one value containing an embedded CR, LF, CRLF, and a null byte together, and checks every byte comes
exactly.
*/
func TestBinarySafeValueRoundTrip(t *testing.T) {
	addr, _ := startTestServer(t, "");

	conn, err := net.Dial("tcp", addr);
	if(err != nil) { t.Fatalf("failed to dial: %v", err); }
	defer conn.Close();

	reader := bufio.NewReader(conn);

	value := "line one\r\nline two\nline three\r\x00binary-ish";
	payload := []byte(value);

	conn.Write([]byte(fmt.Sprintf("SET bin_key %d\r\n", len(payload))));
	conn.Write(payload);
	conn.Write([]byte("\r\n"));

	resp, err := reader.ReadString('\n');
	if(err != nil || !strings.HasPrefix(resp, "+OK")) {
		t.Fatalf("expected +OK for SET, got: %q (err: %v)", resp, err);
	}

	conn.Write([]byte("GET bin_key\r\n"));
	header, err := reader.ReadString('\n');
	if(err != nil) { t.Fatalf("failed to read GET header: %v", err); }
	header = strings.TrimSpace(header);

	length, err := strconv.Atoi(strings.TrimPrefix(header, "$"));
	if(err != nil) { t.Fatalf("bad length in GET response %q: %v", header, err); }

	if(length != len(payload)) {
		t.Fatalf("GET reported length %d, want %d. The value was already mangled before the payload was even read", length, len(payload));
	}

	got := make([]byte, length);
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatalf("failed to read GET payload: %v", err);
	}

	if(string(got) != value) {
		t.Errorf("value did not survice the round trip intact.\n sent: %q got: %q", value, string(got));
	}
}