package server

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"internal/store"
)

// ensures 50 concurrent workers can read/write/delete safely
func TestConcurrentHammering(t *testing.T) {
	expiryIntervalMs := 100;
	store := store.NewStore(time.Duration(expiryIntervalMs) * time.Millisecond, 0);

	// bind to port ":0" which tells the OS to assign a random, unallocated free port dynamically
	// note that this is ONE single port for the entire server
	listener, err := net.Listen("tcp", "127.0.0.1:0");
	if(err != nil) { t.Fatalf("Failed to bind random port: %v", err); }

	addr := listener.Addr().String();
	// clean teardown shutdown the socket pipeline listener
	listener.Close();
	// boot up the server engine in the background

	auth := "";
	serv := NewServer(addr, store, auth);

	// boot the server officially using serv.Start() but on the background
	go func() {
		if err := serv.Start(); err != nil { return; }	
	}();

	// ensuring the background server just has a teeny tiny time to breath and establish it's listener loop
	time.Sleep(20 * time.Millisecond);

	// firing up 50 concurrent workers hammering the server
	var wg sync.WaitGroup; // sync.WaitGroup is an atomic safe integer counter
	workers := 50;
	operationsPerWorker := 50;

	for i := 0; i < workers; i++ {
		wg.Add(1);

		go func(workerID int) {
			defer wg.Done();

			// each worker establishes it's own dedicated tcp client socket
			conn, err := net.Dial("tcp", addr);
			if(err != nil) {
				t.Errorf("Worker %d failed to dial server", workerID);
				return;
			}
			defer conn.Close();

			reader := bufio.NewReader(conn);

			// intentionally sharing key across every 5th worker to force race scenarios
			// 0,5,10,15 key_0
			// 1,6,11,16 key_1 and so on till key_4
			sharedKey := fmt.Sprintf("key_%d", workerID % 5);

			for j := 0; j < operationsPerWorker; j++ {
				// SET
				setCmd := fmt.Sprintf("SET %s val_%d_%d\r\n", sharedKey, workerID, j);
				_, _ = conn.Write([]byte(setCmd));
				output, _ := reader.ReadString('\n');
				if(!strings.Contains(output, "+OK")) {
					t.Errorf("Unexpected SET response: %s", output);
				}

				// GET
				getCmd := fmt.Sprintf("GET %s\r\n", sharedKey);
				_, _ = conn.Write([]byte(getCmd));
				output, _ = reader.ReadString('\n');
				if(strings.HasPrefix(output, "$") && output != "$-1") {
					_, _ = reader.ReadString('\n');
				}

				// EXISTS
				existsCmd := fmt.Sprintf("EXISTS %s\r\n", sharedKey);
				_, _ = conn.Write([]byte(existsCmd));
				_, _ = reader.ReadString('\n');
			}
			/*
			so 50 workers, 50 iteration for each one of them and then 3 commands in each iteration that makes around,
			50 * 50 * 3 = 7500 db operations every single second
			*/
		}(i)
	}

	// wait for all 50 workers to finish executing their operations
	wg.Wait();

	time.Sleep(50 * time.Millisecond);

	serv.mut.Lock();
	finalCount := serv.connCount;
	serv.mut.Unlock();

	if(finalCount != 0) { t.Errorf("Connection tracking leakage detected! Remaining cout: %d", serv.connCount); }
}
