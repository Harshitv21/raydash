package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"internal/store"
)

type Server struct {
	// port number + IP address (IP:port)
	// in our case it would look for localhost testing like: 127.0.0.1:13203
	listenAddr string
	store      *store.Store
	mut        sync.Mutex
	// in Go conn is a core interface by standard net package that represents a generic, stream oriented network connection
	// like TCP,UDP or sockets and allows to read,write data and manage connection states
	connCount  int // active user count
}

// initialization & returning a new server instance
func NewServer(listenAddr string, store *store.Store) *Server {
	return &Server{
		listenAddr: listenAddr,
		store:      store,
	}
}

func (serv *Server) Start() error {
	// establishing a tcp connection using the built in net package and listening on our port
	listener, err := net.Listen("tcp", serv.listenAddr);

	if(err != nil) { return fmt.Errorf("failed to bind to port %s: %w", serv.listenAddr, err); }
	defer listener.Close();

	log.Printf("[SERVER] Listening on: %s", serv.listenAddr);

	// infinite loop to continue listening and updating connection count
	for {
		// accepting incoming connections
		conn, err := listener.Accept(); // but remember listener.Accept is synchronous and blocking

		if(err != nil) {
			log.Printf("[SERVER] Error accepting connection: %v", err);
			continue;
		}

		// safe write
		serv.mut.Lock();
		serv.connCount++; // that makes this number of concurrent counts!
		currentCount := serv.connCount;
		serv.mut.Unlock();

		log.Printf("[SERVER] New connection from %s | Active connections: %d", conn.RemoteAddr(), currentCount);

		// in our infinite loop we again setup a go routine to handle asynchronously the connection (asynchronosly + unblocking)
		go serv.handleConnection(conn);
	}
}

func (serv *Server) handleConnection(conn net.Conn) {
	// cleanup crew for this function
	defer func() {
		// safe write and reduce the active connnection count
		conn.Close();
		serv.mut.Lock();
		serv.connCount--;
		currentCount := serv.connCount;
		serv.mut.Unlock();
		log.Printf("[SERVER] Closed connection from %s | Active connections: %d", conn.RemoteAddr(), currentCount);
	}();

	insaneCounter := 0;
	hateCounter := 0;

	// here we actively start listening for and capturing input from the client
	reader := bufio.NewReader(conn);

	// infinite loop to sit and listen to incoming data
	for {
		line, err := reader.ReadString('\n'); // this is a blocking call that doesn't start processing the buffer until user hits a new line (ENTER)
		if(err != nil) {
			if(errors.Is(err, io.EOF)) { break; } // client got dropped/disconnected
			
			log.Printf("[SERVER] Read error from %s: %v", conn.RemoteAddr(), err);
			break;
		}

		line = strings.TrimSpace(line); // strips away all the leading and trailing whitespaces
		if(line == "") { continue; } // preventing parser to process blank bullshit

		response, shouldTerminate := serv.dispatch(line, &insaneCounter, &hateCounter); // passing to our processing factory
		_, err = conn.Write([]byte(response + "\r\n")); // shipping the response back in raw binary packets with carriage return and newline

		if(err != nil) {
			log.Printf("[SERVER] Write error: %v", err);
			break;
		}

		if(shouldTerminate) {
			log.Printf("[SERVER] Terminating client %s by artificial intelligence decree.", conn.RemoteAddr());
		}
	}
}

// pure processing factory of my server
func (serv *Server) dispatch(line string, insaneCount *int, hateCount *int) (string, bool) {
	// tokenisation
	parts := strings.Fields(line); // Fields will split the string by the amount of whitespace characters
	if(len(parts) == 0) { return "-ERR empty command", false; }

	// normalization
	cmd := strings.ToUpper(parts[0]); // to match our switch case statements
	args := parts[1:]; // the very first part is obviously the actual command like SET,GET,DEL etc

	switch cmd {
	case "INSANE": 
		var insaneString string;
		switch *insaneCount {
		case 0: insaneString = "+Did I ever tell you the definition of insanity?";
		case 1: insaneString = "+Insanity is doing the exact... same fucking thing... over and over again...";
		case 2: insaneString = "+...expecting... shit to change. That. Is. Crazy.";
		default: insaneString = "+ERR Okay, the first time was funny, now you're just proving the point.";
		}
		*insaneCount++;
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
		*hateCount++;
	return hateString, terminateSignal;

	// ez to understand
	case "SET":
		if len(args) < 2 { return "-ERR wrong number of arguments for 'set' command", false; }

		key := args[0];
		val := args[1];

		serv.store.Set(key, val);
		return "+OK", false;
	
	// ez
	case "GET":
		if len(args) != 1 { return "-ERR wrong number of arguments for 'get' command", false; }

		val, exists := serv.store.Get(args[0]);
		if(!exists) { return "$-1", false; }
		return fmt.Sprintf("$%d\r\n%s", len(val), val), false;
	
	// ez
	case "DEL":
		if(len(args) != 1) { return "-ERR wrong number of arguments for 'del' command", false; }

		serv.store.Delete(args[0]);
		return "+OK", false;

	// ez i guess?
	case "EXPIRE":
		if(len(args) != 2) { return "-ERR wrong number of arguments for 'expire' command", false; }

		// no check for key here or in DEL because golang gracefully handles key not found cases on itself
		// and also just for safe measure my store handles cases on key not found for Expire function
		key := args[0];
		seconds, err := strconv.Atoi(args[1]);

		if(err != nil || seconds < 0) { return "-ERR value is not an integer or out of range", false; }

		duration := time.Duration(seconds) * time.Second;
		success := serv.store.Expire(key, int(duration));

		if(success) { return ":1", false; }
		return ":0", false;

	// ez
	case "TTL":
		if(len(args) != 1) { return "-ERR wrong number of arguments for 'ttl' command", false; }
		
		ttl, exists := serv.store.TTL(args[0]);
		if(!exists) { return ":-2", false; }
		if(ttl < 0) { return ":-1", false; }

		return fmt.Sprintf(":%d", ttl), false;

	default:
		return fmt.Sprintf("-ERR unknown command '%s'", strings.ToLower(cmd)), false;
	}
}