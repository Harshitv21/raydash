package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

/*
A tiny interactive REPL for poking at a running raydash server by hand typing a command, getting the
response and repeat. This is for a human testing things manually you know like a deployed server
somewhere and you want to access it see if it is working or not. The loadtest and java client sure
they can perform the same things too but this is more interactive than programmatic.
*/
func main() {
	serverAddr := flag.String("server", "", "Server address");
	authToken := flag.String("auth", "", "Authentication token");
	flag.Parse();

	/*
	Never echo the real auth token to the terminal. This is MASKING, not encryption since encryption
	specifically means a reversible transformation, and there's no getting the original token back
	from this string of '*'.
	*/
	fillChar := "*";
	lengthAuthToken := utf8.RuneCountInString(*authToken);
	encryptedAuthToken := strings.Repeat(fillChar, lengthAuthToken);

	fmt.Printf("[INFO] Connecting to Raydash Server: %s\n", *serverAddr);

	if(*authToken != "") { fmt.Printf("[INFO] Using Auth token: %s\n", encryptedAuthToken); }

	// in our server we used listen now in our cli we will use dial
	conn, err := net.Dial("tcp", *serverAddr);

	if(err != nil) { log.Fatalf("Failed to connect to server: %v\nMake sure your server is running!", err); }
	defer conn.Close();

	/*
	2 separate readers here:
	consoleReader which is the standard input will listen to what we type
	networkReader listens to what the server replies think of conn as the network cable
	*/
	consoleReader := bufio.NewReader(os.Stdin);
	networkReader := bufio.NewReader(conn);

	// Auth work first!
	/*
	Only attempted when a token was actually given. A server with no RAYDASH_AUTH_TOKEN set doesn't
	need this step, and sending AUTH at it anyway will just give us back "-ERR AUTH not required" for
	no apparent reason.
	*/
	if(*authToken != "") {
		if err := doAuth(conn, networkReader, *authToken); err != nil {
			log.Printf("AUTH failed for connection: %s\nError: %v", *serverAddr, err);
			return;
		}
	}

	fmt.Printf("\nConnected! to %s!\nType commands (SET, GET, HATE, INSANE) or type 'quit' to exit\n", *serverAddr);

	/*
	Infinite interactive loop
	*/
	for {
		fmt.Print("raydash> ");
		// wait for input (enter/newline press)
		input, err := consoleReader.ReadString('\n');
	
		if(err != nil) {
			log.Printf("Console read error: %v\n", err);
			break;
		}

		input = strings.TrimSpace(input);
		if(input == "") { continue; }

		if(strings.ToLower(input) == "quit") {
			fmt.Println("Ok bye!");
			break;
		}

		parts := strings.Fields(input);
		if(len(parts) == 0) { continue; }

		cmd := strings.ToUpper(parts[0]);

		/*
		All commands except "SET" cannot be simply forwarded as it is since we have to follow our own
		binary safe format:
		'
		 SET <key> <byte-length>\r\n
		 <exactly byte-length raw bytes>\r\n
		'
		Every other command except for this is just a single plain line, so only SET needs special 
		treatment :)
		*/
		if(cmd == "SET") {
			if(len(parts) < 3) {
				fmt.Println("usage: SET <key> <value...>");
				continue;
			}

			key := parts[1];
			/*
			A command like SET will have its own limitations in a setting like this. Everything
			we type after the key will become a value joined with single spaces. Multiple spaces
			you typed between words in the value get collapsed to one. This is the best we can 
			do for such environment proper complex structures like a JSON is for java client not
			here.
			*/
			value := strings.Join(parts[2:], " "); // join all values together
			payload := []byte(value);

			if _, err := fmt.Fprintf(conn, "SET %s %d\r\n", key, len(payload)); err != nil {
				fmt.Printf("Connection lost: %v\n", err);
				break;
			}

			// sending our payload
			if _, err := conn.Write(payload); err != nil {
				fmt.Printf("Connection lost: %v\n", err);
				break;
			}

			// all the things that go in the end
			if _, err := conn.Write([]byte("\r\n")); err != nil {
				fmt.Printf("Connection lost: %v\n", err);
				break;
			}		
		} else {
			// take our input and write or shoot via tcp connection straight to server
			if _, err := conn.Write([]byte(input + "\r\n")); err != nil {
				fmt.Printf("Connection lost: %v\n", err);
				break;
			}
		}

		/*
		Reading a response back is identical no matter which command we just send. io.ReadFull is
		what we should use tho instead of our old ReadString('\n'). line based read here again would
		miss silently actual content and might end prematurely for any value containing '\r' or '\n'
		inside it. Now realistically we wouldn't encounter such problem on cli tool but still why
		wait for something to happen you know?
		*/
		response, err := networkReader.ReadString('\n');
		if(err != nil) {
			fmt.Println("Connection dropped by server");
			break;
		}
		response = strings.TrimSpace(response);

		if(strings.HasPrefix(response, "$") && response != "$-1") {
			length, lengthErr := strconv.Atoi(response[1:]);
			if(lengthErr != nil) {
				fmt.Printf("Bad length in response: %q\n", response);
				continue;
			}

			payload := make([]byte, length);
			if _, err := io.ReadFull(networkReader, payload); err != nil {
				fmt.Println("Connection dropped while reading value.");
				break;
			}

			// consume trailing '\r\n'
			networkReader.ReadString('\n');

			fmt.Println(string(payload));
		} else {
			fmt.Println(response);
		}
	}
}

func doAuth(conn net.Conn, reader *bufio.Reader, token string) error {
	if _, err := fmt.Fprintf(conn, "AUTH %s\r\n", token); err != nil { return err; }

	resp, err := reader.ReadString('\n');

	if(err != nil) { return err; }
	if !strings.HasPrefix(resp, "+OK") { return fmt.Errorf("unexpected AUTH response: %q", resp); }

	return nil;
}