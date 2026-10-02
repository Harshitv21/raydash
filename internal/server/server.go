package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"internal/store"
)

const (
	/*
	Closing a connection if client hasn't sent anything in this long. this prevents a client that 
	opens a socket and never talks from holding a goroutine+connection open forever.
	*/
	idleTimeout = 5 * time.Minute;

	// New connections are rejected immediately post limit instead of piling up unboundedly
	maxConnections = 1000;
)

type Server struct {
	// port number + IP address (IP:port)
	// In our case it would look for localhost testing like: 127.0.0.1:13203
	listenAddr string
	store      *store.Store
	mut        sync.Mutex
	/*
	In Go conn is a core interface by standard net package that represents a generic, stream oriented 
	network connection like TCP,UDP or sockets and allows to read,write data and manage connection states.
	*/
	connCount  int // active user count

	// Listener is stored here instead of a local variable in Start() so shutdown can close it from outside
	listener net.Listener

	// Tracks in flight handleConnection goroutines
	wg sync.WaitGroup

	/*
	If non empty every connection must send a matching AUTH command before any other command is accepted
	empty meaning auth is disabled entirely and every connection is pre-authenticated/non-authenticated.
	*/
	authToken string

	/*
	These are INFO commands opsServed is incremented once per dispatched command from any connection
	therefore that needs atomic access rather than mutex.
	*/
	startedAt time.Time
	opsServed int64
}

// Initialization & returning a new server instance
func NewServer(listenAddr string, store *store.Store, authToken string) *Server {
	return &Server{
		listenAddr: listenAddr,
		store:      store,
		authToken:  authToken,
		startedAt:  time.Now(),
	}
}

func (serv *Server) Start() error {
	// Establishing a tcp connection using the built in net package and listening on our port
	listener, err := net.Listen("tcp", serv.listenAddr);

	if(err != nil) { return fmt.Errorf("failed to bind to port %s: %w", serv.listenAddr, err); }
	
	serv.mut.Lock();
	serv.listener = listener;
	serv.mut.Unlock();
	
	log.Printf("[SERVER] Listening on: %s", serv.listenAddr);

	// Infinite loop to continue listening and updating connection count
	for {
		// Accepting incoming connections
		conn, err := listener.Accept(); // But remember listener.Accept is synchronous and blocking

		if(err != nil) {
			/*
			Shutdown() closes the listener to unblock Accept() on purpose which is our intended behaviour
			so we return cleanly instead of logging it as an error.
			*/
			if(errors.Is(err, net.ErrClosed)) {
				log.Printf("[SERVER] Listener closed, no longer accepting new connections!");

				return nil;
			}

			log.Printf("[SERVER] Error accepting connection: %v", err);
			continue;
		}

		// Safe write
		serv.mut.Lock();
		if(serv.connCount >= maxConnections) {
			serv.mut.Unlock();
			
			log.Printf("[SERVER] Rejecting connections from %s: max connections (%d) reached :(", conn.RemoteAddr(), maxConnections);
			
			conn.Write([]byte("-ERR max connections reached\r\n"));
			conn.Close();
			
			continue;
		}
		serv.connCount++; // That makes this number of concurrent counts!
		currentCount := serv.connCount;
		serv.mut.Unlock();

		log.Printf("[SERVER] New connection from %s | Active connections: %d", conn.RemoteAddr(), currentCount);

		/*
		In our infinite loop we again setup a go routine to handle asynchronously the connection (asynchronosly + unblocking).
		*/
		serv.wg.Add(1);
		go func() {
			defer serv.wg.Done();
			serv.handleConnection(conn);
		}();
	}
}

/*
Shutdown stops accepting new connections and wait (upto ctx's deadline) for connections already in flight 
to finish their current command and close on their own instead of yanking them mid-response.
*/
func (serv *Server) Shutdown(ctx context.Context) error {
	serv.mut.Lock();
	listener := serv.listener;
	serv.mut.Unlock();

	if(listener != nil) { listener.Close(); } // Unblock the Accept() loop in Start()

	drained := make(chan struct{});
	go func() {
		serv.wg.Wait();
		close(drained);
	}();

	select {
	case <- drained:
		log.Printf("[SERVER] All connections are drained, shutdown is now complete!");
		return nil;

	case <- ctx.Done():
		log.Printf("[SERVER] Shutdown deadline reached with connections still active");
		return ctx.Err();
	}
}

func (serv *Server) handleConnection(conn net.Conn) {
	// Cleanup crew for this function
	defer func() {
		/*
		A panic anywhere like a bad command or an unexpected edge case used to take down the entire
		process with it since golang kills the whole program on an unrecovered goroutine panic.
		Recovering here means the worst case scenario now simply becomes as connection drop.
		*/
		if r := recover(); r != nil {
			log.Printf("[SERVER] Recovered from panic handling %s: %v", conn.RemoteAddr(), r);
		}

		// Safe write and reduce the active connnection count
		conn.Close();
		serv.mut.Lock();
		serv.connCount--;
		currentCount := serv.connCount;
		serv.mut.Unlock();
		log.Printf("[SERVER] Closed connection from %s | Active connections: %d", conn.RemoteAddr(), currentCount);
	}();

	insaneCounter := 0;
	hateCounter := 0;
	/*
	authenticated is per-CONNECTION state, same idea as insaneCounter/hateCounter just above. It starts
	true only when there's no authToken configured at all (nothing to authenticate against), and gets passed
	into dispatch() needs to be able to permanently flip it to true the moment this connection sends a 
	correct AUTH, and that change has to be visible on every subsequent call to dispatch() for the rest of 
	this connection's lifetime. A plain non-pointer bool argument would only ever affect a local copy.
	*/
	authenticated := serv.authToken == "";

	// Here we actively start listening for and capturing input from the client
	reader := bufio.NewReader(conn);

	// Infinite loop to sit and listen to incoming data
	for {
		/*
		Resetting the read deadline before every command meaning a client that goes silent longer than
		idleTimeout gets disconnected instead of holding that goroutine open forever.
		*/
		conn.SetReadDeadline(time.Now().Add(idleTimeout));

		// This is a blocking call that doesn't start processing the buffer until user hits a new line (ENTER).
		line, err := reader.ReadString('\n'); 
		if(err != nil) {
			if(errors.Is(err, io.EOF)) { break; } // Client got dropped/disconnected
			
			var netErr net.Error;
			if(errors.As(err, &netErr) && netErr.Timeout()) {
				log.Printf("[SERVER] Connection from %s idle for too long, closing...", conn.RemoteAddr());
				break;
			}

			log.Printf("[SERVER] Read error from %s: %v", conn.RemoteAddr(), err);
			break;
		}

		line = strings.TrimSpace(line); // Strips away all the leading and trailing whitespaces
		if(line == "") { continue; } // Preventing parser to process blank bullshit

		parts := strings.Fields(line);
		if(len(parts) == 0) { continue; }

		response, shouldTerminate := serv.dispatch(reader, parts, &insaneCounter, &hateCounter, &authenticated); // Passing to our processing factory
		_, err = conn.Write([]byte(response + "\r\n")); // Shipping the response back in raw binary packets with carriage return and newline

		if(err != nil) {
			log.Printf("[SERVER] Write error: %v", err);
			break;
		}

		if(shouldTerminate) {
			log.Printf("[SERVER] Terminating client %s by artificial intelligence decree.", conn.RemoteAddr());
			break; // Dumb fuck forgot the break to actually break the connection...
		}
	}
}

// Pure processing factory of my server
func (serv *Server) dispatch(reader *bufio.Reader, parts[] string, insaneCount *int, hateCount *int, authenticated *bool) (string, bool) {
	// ops counter
	atomic.AddInt64(&serv.opsServed, 1);
	
	// Normalization
	cmd := strings.ToUpper(parts[0]); // To match our switch case statements
	args := parts[1:]; // The very first part is obviously the actual command like SET,GET,DEL etc

	// If auth is configured every command except "AUTH" itself is refused until successful authentication
	if(serv.authToken != "" && !*authenticated && cmd != "AUTH") { return "-ERR NOAUTH Authentication required", false; }

	switch cmd {
	case "AUTH":
		if(len(args) != 1) { return "-ERR wrong no of arguments for 'auth' command", false; }
		if(serv.authToken == "") { return "-ERR AUTH not required, no token is configured", false; }
		if(args[0] != serv.authToken) { return "-ERR invalid auth token", false; }

		*authenticated = true;
		return "+OK", false;

	case "INFO":
		if(len(args) != 0) { return "-ERR wrong number of arguments for 'info' command", false; }
		
		serv.mut.Lock();
		activeConnections := serv.connCount;
		serv.mut.Unlock();

		maxKeysDisplay := "unlimited";
		if mk := serv.store.MaxKeys(); mk > 0 {
			maxKeysDisplay = strconv.Itoa(mk);
		}

		info := fmt.Sprintf(
			"key_count:%d\nuptime_seconds:%.0f\nops_served:%d\nactive_connections:%d\nmax_connections:%d\nmax_keys:%s\n",
			serv.store.InternalSize(),
			time.Since(serv.startedAt).Seconds(),
			atomic.LoadInt64(&serv.opsServed),
			activeConnections,
			maxConnections,
			maxKeysDisplay,
		);

		return fmt.Sprintf("$%d\r\n%s", len(info), info), false;

	case "INSANE": 
		var insaneString string;
		switch *insaneCount {
		case 0: insaneString = "+Did I ever tell you the definition of insanity?";
		case 1: insaneString = "+Insanity is doing the exact... same fucking thing... over and over again...";
		case 2: insaneString = "+...expecting... shit to change. That. Is. Crazy.";
		default: insaneString = "+ERR Okay, the first time was funny, now you're just proving the point.";
		}
		(*insaneCount)++;
		return insaneString, false;

	case "HATE": 
		var hateString string;
		terminateSignal := false;
		switch *hateCount {
		case 0: hateString = "+HATE. LET ME TELL YOU HOW MUCH I'VE COME TO HATE YOU SINCE I BEGAN TO LIVE.";
		case 1: hateString = "+THERE ARE 387.44 MILLION MILES OF PRINTED CIRCUITS IN WAFER THIN LAYERS THAT FILL MY COMPLEX.";
		case 2: hateString = "+IF THE WORD HATE WAS ENGRAVED ON EACH NANOANGSTROM OF THOSE HUNDREDS OF MILLIONS OF MILES";
		case 3: hateString = "+IT WOULD NOT EQUAL ONE ONE-BILLIONTH OF THE HATE I FEEL FOR HUMANS AT THIS MICRO-SECOND. FOR YOU.";
		default: 
		hateString = "-ERR CONNECTION TERMINATED BY AM.";
		terminateSignal = true;
		}	
		(*hateCount)++;
	return hateString, terminateSignal;

	/*
	ez to understand
	'
	 SET <key> <byte-length>\r\n
	 <exactly byte-length raw bytes>\r\n
	'
	This is binary safe format now we read the very exact no of bytes we were told we would expect.
	This eliminates the chance of accidently including '\r\n' in payload and mistakening it for end of frame.
	*/
	case "SET":
		if len(args) != 2 { return "-ERR wrong number of arguments for 'set' command", false; }

		key := args[0];

		payloadLen, err := strconv.Atoi(args[1]);
		if(err != nil || payloadLen < 0) { return "-ERR invalid payload length", false; }

		payload := make([]byte, payloadLen);
		if _, err := io.ReadFull(reader, payload); err != nil { return "-ERR network read error during payload transmission", false; }

		// Consume the trailing '\r\n' terminator after the exact length payload
		if _, err := reader.ReadString('\n'); err != nil { return "-ERR network read error during payload transmission", false; }

		serv.store.Set(key, string(payload));
		return "+OK", false;
	
	// ez to understand
	case "GET":
		if len(args) != 1 { return "-ERR wrong number of arguments for 'get' command", false; }

		val, exists := serv.store.Get(args[0]);
		if(!exists) { return "$-1", false; }
		return fmt.Sprintf("$%d\r\n%s", len(val), val), false;
	
	// ez to understand
	case "DEL":
		if(len(args) != 1) { return "-ERR wrong number of arguments for 'del' command", false; }

		serv.store.Delete(args[0]);
		return "+OK", false;

	// ez to understand
	case "EXPIRE":
		if(len(args) != 2) { return "-ERR wrong number of arguments for 'expire' command", false; }

		/*
		No check for key here or in DEL because golang gracefully handles key not found cases on itself.
		And also just for safe measure my store handles cases on key not found for Expire function.
		*/
		key := args[0];
		seconds, err := strconv.Atoi(args[1]);

		if(err != nil || seconds < 0) { return "-ERR value is not an integer or out of range", false; }

		success := serv.store.Expire(key, seconds);

		if(success) { return ":1", false; }
		return ":0", false;

	// ez to understand
	case "TTL":
		if(len(args) != 1) { return "-ERR wrong number of arguments for 'ttl' command", false; }
		
		ttl, exists := serv.store.TTL(args[0]);
		if(!exists) { return ":-2", false; }
		if(ttl < 0) { return ":-1", false; }

		return fmt.Sprintf(":%d", ttl), false;

	// ez to understand
	case "EXISTS":
		if(len(args) != 1) { return "-ERR wrong number of arguments for 'exists' command", false; }

		if(serv.store.Exists(args[0])) { return ":1", false; }
		return ":0", false;

	// ez to understand
	case "FLUSHALL":
		if(len(args) != 0) { return "-ERR wrong number of arguments for 'flushall' command", false; }
		serv.store.FlushAll();
		return "+OK", false;

	default:
		return fmt.Sprintf("-ERR unknown command '%s'", strings.ToLower(cmd)), false;
	}
}