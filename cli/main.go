package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

func main() {
	serverAddr := "localhost:13203";

	fmt.Printf("[INFO] Connecting to Raydash Server: %s\n", serverAddr);

	// in our server we used listen now in our cli we will use dial
	conn, err := net.Dial("tcp", serverAddr);

	if(err != nil) { log.Fatalf("Failed to connect to server: %v\nMake sure your server is running!", err); }
	defer conn.Close();

	fmt.Println("Connected! Type commands (SET, GET, HATE, INSANE) or type 'quit' to exit");

	/*
	2 separate readers here:
	consoleReader which is the standard input will listen to what we type
	networkReader listens to what the server replies think of conn as the network cable
	*/
	consoleReader := bufio.NewReader(os.Stdin);
	networkReader := bufio.NewReader(conn);

	// infinite interactive loop
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

		if(strings.ToLower(input) == "exit") {
			fmt.Println("Ok bye!");
			break;
		}

		// take our input and write or shoot via tcp connection straight to server
		_, err = conn.Write([]byte(input + "\r\n"));
		
		if(err != nil) {
			fmt.Printf("Connection lost: %v\n", err);
			break;
		}

		// wait for response from server
		response, err := networkReader.ReadString('\n');
		if(err != nil) {
			fmt.Println("Connection dropped by server");
			break;
		}

		response = strings.TrimSpace(response);

		/*
		this is our smart parser for GET since it returns 2 lines first the length of response and then the 
		response on the second line so here we read from the next line and discard the size tracking header
		*/
		if(strings.HasPrefix(response, "$") && response != "$-1") {
			valueLine, err := networkReader.ReadString('\n');
			if(err != nil) {
				fmt.Println("Connection dropped while reading string value.");
				break;
			}
			fmt.Println(strings.TrimSpace(valueLine));
		} else {
			fmt.Println(response);
		}
	}
}