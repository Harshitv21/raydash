package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

/*
This is a throughput benchmark, not a unit test. Its whole job is to open a bunch of real, independent
TCP connections to a running Raydash server and hammer it with SET/GET as fast as it can for a fixed
duration, then report how many operations got through. There's no assertion here about correctness
(that's what actually running the server and eyeballing results is for). Simply stated:
"just how many ops/sec, and did anything error out or not".

Per-operation latency tracking: get a real number for p99. p99 latency is a percentile metric used to
measure the response time of a system or API, revealing how the slowest outlier requests perform.
In simple terms, p99 latency means 99% of all requests finish in this amount of time or less while the
slowest 1% take longer. Example, if p99 is 69ms, 99 out of all 100 requests complete under 69ms.

Each worker times its own SET and GET calls and keeps the results in a plain local slice with no locking
at all while the "hot" loop is running, since adding synchronization overhead to the exact thing we are
trying to measure would poison the measurement. Every worker merges its local slice into one shared slice
a SINGLE time, right when it finishes. That's so many lock acquisitions total for the whole run, not one
per operation so not distorting anything.
*/
func main() {
	/*
	The flag package is Go's standard command-line argument parser. flag.String/Int/Duration each
	register one named flag (like -workers 50) with a default value and a help description, and returns
	a "pointer" to where the parsed value will end up. That's why every use of these below is dereferenced
	with a leading * (like *workers or *duration). Nothing is actually populated until Flag.Parse() runs,
	which reads os.Args and fills in every registered flag's target variable.
	*/
	addr := flag.String("addr", "localhost:13203", "raydash address");
	workers := flag.Int("workers", 50, "number of concurrent client connections");
	duration := flag.Duration("duration", 10 * time.Second, "how long to run the loadtest");
	valueSize := flag.Int("value-size", 64, "size in bytes of the value written on each SET");
	authToken := flag.String("auth-token", "", "auth token to send, if the server has RAYDASH_AUTH_TOKEN set");
	flag.Parse();

	fmt.Printf("Load testing %s: %d workers for %s, %d-byte values\n\n", *addr, *workers, *duration, *valueSize);

	/*
	These 2 counters are written to by every worker goroutine at once, which is exactly why they're
	read/written through the atomic package below (atomic.AddInt64 etc.) rather than with a plain
	`totalOperations++`. Ordinary increments aren't safe when many goroutines can run one at the 
	literal same instant on different CPU cores. Atomic operations guarantee each increment is 
	indivisible, so none of them gets lost.
	*/
	var totalOperations int64;
	var totalErrors int64;

	/*
	sync.WaitGroup is the standard way to say "wait until every one of these N background goroutines
	has finished". wg.Add(1) increments a counter right before launching each worker, and every worker
	calls wg.Done() (decrementing it) when it exits. wg.Wait() further down blocks until that counter hits
	0. i.e., until every worker really is done.
	*/
	var wg sync.WaitGroup;

	/*
	latencyMut guards the 2 shared slices below. The ONLY thing it protects is the one-time append each
	worker does when it finishes, never anything inside the hot loop.
	*/
	var latencyMut sync.Mutex;
	var setLatencies []time.Duration;
	var getLatencies []time.Duration;

	/*
	stop is closed (not sent-to) when the test duration elapses. Closing a channel is a broadcast signal
	every goroutine selecting on it wakes up for simultaneously, which is exactly the "everyone stops now"
	shape we want, as opposed to a normal channel send which only one receiver would get.
	*/
	stop := make(chan struct{});
	value := strings.Repeat("x", *valueSize); // every worker writes this same fixed-size value on every SET

	start := time.Now();

	for i := 0; i < *workers; i++ {
		wg.Add(1);
		/*
		Each worker gets its own real goroutine and its own real TCP connection not a shared connection
		because the whole point is to exercise Raydash's one goroutine-per-connection model under genuine
		concurrent load, the same way many independent clients actually would hit it. 
		*/
		go func(workerID int) {
			defer wg.Done();

			conn, err := net.Dial("tcp", *addr);
			if(err != nil) {
				fmt.Printf("worker %d failed to connect: %v\n", workerID, err);

				log.Printf("worker %d: connect failed: %v", workerID, err);
				atomic.AddInt64(&totalErrors, 1);
				return;
			}
			defer conn.Close();

			/*
			A worker's own private latency bins written to freely with no locks since no other goroutine
			ever touches these 2 specific slices.
			*/
			localSetLatencies := make([]time.Duration, 0, 4096);
			localGetLatencies := make([]time.Duration, 0, 4096);

			/*
			merges this worker's timings into the shared totals exactly once, when the worker is about
			to exit (could be because of test duration elapsed or an error ending worker's loop early)
			defferred so it still runs on every exit path not just the clean one.
			*/
			defer func() {
				latencyMut.Lock();

				setLatencies = append(setLatencies, localSetLatencies...);
				getLatencies = append(getLatencies, localGetLatencies...);

				latencyMut.Unlock();
			}();

			reader := bufio.NewReader(conn);

			// auth token check
			if(*authToken != "") {
				if err := doAuth(conn, reader, *authToken); err != nil {
					log.Printf("worker %d: auth failed: %v", workerID, err);
					atomic.AddInt64(&totalErrors, 1);
					return;
				}
			}

			/*
			Each worker gets its own independently-seeded random source. UnixNano() alone could collide
			if 2 workers start in the same nanosecond, so mixing in workerID guarantees every worker's
			sequence of "random" keys is actually distinct.
			*/
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)));

			// Main hot loop! As fast as fucking possible, SET then GET, over and over until told to stop. 
			for {
				select {
				case <- stop:
					return;
				/*
				a `default` case makes this select non-blocking without it the loop would sit here
				waiting on `stop` and never get to the actual work below! This pattern (channel case + 
				default) is the standard Go idiom for "check a signal but don't wait around for it".
				*/
				default:
				}

				/*
				Each worker sticks to it's own key range so go routines aren't all contending on the same 
				handful of keys kinda like how independent clients would behave.
				*/
				key := fmt.Sprintf("loadtest:%d:%d", workerID, rng.Intn(1000));

				setStart := time.Now();
				if err := doSet(conn, reader, key, value); err != nil {
					// fmt.Printf("worker %d SET failed: %v\n", workerID, err);
					
					atomic.AddInt64(&totalErrors, 1);
					return; // even a single error ends this worker's loop rather than retrying. This sets the benchmarks standard high!
				}
				/*
				only successful operations get timed. A failed call doesn't represent real service latency,
				it represents a broken connection which would just pollute the percentiles.
				*/
				localSetLatencies = append(localSetLatencies, time.Since(setStart));
				atomic.AddInt64(&totalOperations, 1);

				getStart := time.Now();
				if err := doGet(conn, reader, key); err != nil {
					// fmt.Printf("worker %d GET failed: %v\n", workerID, err);
					
					atomic.AddInt64(&totalErrors, 1);
					return;
				}
				// same shit
				localGetLatencies = append(localGetLatencies, time.Since(getStart));
				atomic.AddInt64(&totalOperations, 1);
			}
		}(i)
	}

	/*
	schedules stop to be closed once `duration` has elapsed, without blocking this goroutine while it
	waits. AfterFunc runs its callback on its own goroutine in the background and returns immediately.
	*/
	time.AfterFunc(*duration, func() { close(stop) })

	wg.Wait(); // blocks here until every worker goroutine above has actually returned

	elapsed := time.Since(start);

	/*
	atomic.LoadInt64 reads the final value the same safe way they were written. Reading an int64 that
	other goroutines were concurrently writing to, without atomic, isn't guaranteed to give you a 
	coherent value on every platform.
	*/
	operations := atomic.LoadInt64(&totalOperations);
	errorsDuringOperations := atomic.LoadInt64(&totalErrors);

	fmt.Printf("=== results ===\n");
	fmt.Printf("Elapsed: %s\n", elapsed);
	fmt.Printf("Total operations (SET + GET combined): %d\n", operations);
	fmt.Printf("Errors combined in all operations: %d\n", errorsDuringOperations);
	fmt.Printf("Throughput: %.0f ops/sec\n", float64(operations) / elapsed.Seconds());


	fmt.Printf("\n=== latency (successful operations only) ===\n");
	printLatencyStats("SET", setLatencies);
	printLatencyStats("GET", getLatencies);

	/*
	A fresh combined slice deliberately not reusing any of the existing latencies array, since append()
	below could otherwise silently overwrite data still referenced elsewhere depending on capacity.
	*/
	combined := make([]time.Duration, 0, len(setLatencies) + len(getLatencies));
	combined = append(combined, setLatencies...);
	combined = append(combined, getLatencies...);

	printLatencyStats("COMBINED", combined);

	/*
	Smol pause before the process actually exits giving any still unwinding goroutines and buffered
	log output a moment to settle rather than the program potentially exiting mid-flush.
	*/
	time.Sleep(1 * time.Second);
}

/*
Sends "AUTH <token>\r\n" and requires a "+OK" reply, same handshake RaydashClient.java (Java client for Raydash
for reference) does on the Java side. Both have to agree on this exchange for the server to accept anything
further.
*/
func doAuth(conn net.Conn, reader *bufio.Reader, token string) error {
	if _, err := fmt.Fprintf(conn, "AUTH %s\r\n", token); err != nil { return err; }

	resp, err := reader.ReadString('\n');

	if(err != nil) { return err; }
	if !strings.HasPrefix(resp, "+OK") { return fmt.Errorf("unexpected AUTH response: %q", resp); }

	return nil;
}

/*
Writes one SET in Raydash's binary-safe wire format:
'
 SET <key> <byte-length>\r\n
 <exactly byte-length raw bytes>\r\n
'
This has to match server.go's SET case EXACTLY, byte by byte as if the 2 were designed together. The
command line goes out through Fprintf (safe as its plain ASCII text), the value itself goes through 
conn.Write() directly as raw bytes, specifically as this tool can exercise the same binary-safety 
guarantee real values will rely on, not just plain text.
*/
func doSet(conn net.Conn, reader *bufio.Reader, key, value string) error {
	payload := []byte(value);

	if _, err := fmt.Fprintf(conn, "SET %s %d\r\n", key, len(payload)); err != nil { return err; }
	if _, err := conn.Write(payload); err != nil { return err; }
	if _, err := conn.Write([]byte("\r\n")); err != nil { return err; }

	resp, err := reader.ReadString('\n');
	if(err != nil) { return err; }

	if !strings.HasPrefix(resp, "+OK") { return fmt.Errorf("unexpected SET response: %q", resp) }

	return nil;
}

/*
Reads GET's response the binary safe way: "$<length>" header line comes back through ReadString (safe
as its plain text), but the value itself is read with io.ReadFull for exactly that many bytes. Never a
line based read. That distinction matters here for the same reason it does in RaydashClient.java's (Java 
Client for Raydash) get(), a value could contain bytes that look like \r or \n very common with API
responses and only a length-prefixed exact read can tell where it actually ends without misreading it.
"$-1" is Raydash's way of saying "no such key" and nothing further to read.
*/
func doGet(conn net.Conn, reader *bufio.Reader, key string) error {
	if _, err := fmt.Fprintf(conn, "GET %s\r\n", key); err != nil { return err; }

	header, err := reader.ReadString('\n');
	if(err != nil) { return err; }

	header = strings.TrimSpace(header);

	if(header == "$-1") { return nil; } // miss nothing more to read
	if !strings.HasPrefix(header, "$") { return fmt.Errorf("unexpected GET response: %q", header) }

	length, err := strconv.Atoi(header[1:]);
	if(err != nil) { return fmt.Errorf("bad length in GET response %q: %w", header, err); }

	payload := make([]byte, length);
	
	/*
	io.ReadFull keeps reading from `reader` until it has exactly len(payload) bytes (or hits an
	error/EOF first). A single Read() call on a bufio.Reader is only ever guaranteed to return
	SOME bytes, not necessarily all of them in one shot, so this is the correct way to say "block
	until I have exactly this many".
	*/
	if _, err := io.ReadFull(reader, payload); err != nil { return err; }
	/*
	Consume the trailing '\r\n' that doSet() wrote after its payload, so the connection stays lined up
	correctly for whatever command comes next. 
	*/
	if _, err := reader.ReadString('\n'); err != nil { return err; }
	
	return nil;
}

/*
printLatencyStats() sorts a set of recorded durations and reports the values that actually matter for
writing for a claim worthy enough. p50 (typical case), p95 and p99 (the tail like what our slowest real
request look like), plus the outright min/max for sanity checking the run.
*/
func printLatencyStats(label string, latencies []time.Duration) {
	if(len(latencies) == 0) {
		fmt.Printf("%s latency: no successful samples :(\n", label);
		return;
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] });

	/*
	simple nearest-rank percentile: sort ascending, then the value at index (p * n) IS the pth percentile
	by definition, no interpolation between ranks this is exactly the kind of thing we need yo.
	*/
	percentile := func(p float64) time.Duration {
		idx := int(p * float64(len(latencies)));
		if(idx >= len(latencies)) { idx = len(latencies) - 1; }
		return latencies[idx];
	}

	fmt.Printf(
		"%-8s min=%-10s p50=%-10s p95=%-10s p99=%-10s max=%-10s (n=%d)\n",
		label,
		latencies[0],
		percentile(0.50),
		percentile(0.95),
		percentile(0.99),
		latencies[len(latencies) - 1],
		len(latencies),
	);
}