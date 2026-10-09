package main

import (
	"context"
	"internal/persistence"
	"internal/server"
	"internal/store"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load("../.env"); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[SERVER:MAIN] Error loading .env file: %v", err);
		}
		log.Printf("[SERVER:MAIN] Note: No .env file loaded (%v). Relying on system/Docker environment.", err);
	}

	port := getEnv("RAYDASH_PORT", "13203");
	expiryIntervalMs := getEnvInt("RAYDASH_EXPIRY_INTERVAL_MS", 100);
	snapshotInteralSec := getEnvInt("RAYDASH_SNAPSHOT_INTERVAL_SECONDS", 30);
	maxKeys := getEnvInt("RAYDASH_MAX_KEYS", 0); // 0: unlimited
	postgresDSN := os.Getenv("RAYDASH_POSTGRES_DSN");
	authToken := os.Getenv("RAYDASH_AUTH_TOKEN"); // "": auth disabled

	if(authToken != "") {
		log.Printf("[SERVER:MAIN] AUTH enabled - clients must send AUTH before any other command");
	} else {
		log.Printf("[SERVER:MAIN] AUTH disabled - RAYDASH_AUTH_TOKEN not set, running with no authentication");
	}

	if(maxKeys > 0) {
		log.Printf("[SERVER:MAIN] max-keys cap enabled at %d (oldest-inserted eviction)", maxKeys);
	}

	// Persistence is optional meaning if no DSN then it runs pure in memory only
	var persister *persistence.PostgresPersister;
	if(postgresDSN != "") {
		p, err := persistence.NewPostgresPersister(postgresDSN);
		if(err != nil) { log.Fatalf("[SERVER:MAIN] Failed to connect to postgres: %v", err); }
		
		persister = p;
		defer persister.Close();
	} else {
		log.Printf("[SERVER:MAIN] RAYDASH_POSTGRES_DSN not set, running with no persistence in-memory only!");
	}

	myStore := store.NewStore(time.Duration(expiryIntervalMs) * time.Millisecond, maxKeys);
	defer myStore.Close();

	if(persister != nil) {
		entries, err := persister.Load();
		if(err != nil) { 
			log.Printf("[SERVER:MAIN] Failed to load snapshot from postgres, starting empty: %v", err); 
		} else {
			myStore.LoadSnapshot(entries);
			log.Printf("[SERVER:MAIN] Loaded %d keys from postgres snapshot", len(entries));
		}
	}

	serv := server.NewServer(":" + port, myStore, authToken);

	/*
	snapshotStopCh is only ever actually used when persistence is on (see the persister != nil check
	right below). It exists to the periodic save goroutine can be told to stop cleanly during shutdown,
	same closed-channel-as-broadcast idea used for graceful shutdown elsewhere in this codebase somewhere...
	*/
	snapshotStopCh := make(chan struct{});
	if(persister != nil) {
		go runPeriodicSnapshots(myStore, persister, time.Duration(snapshotInteralSec) * time.Second, snapshotStopCh);
	}

	/*
	Start() is blocking so we'll run it as a goroutine and let main() watch for a fatal start error
	or an OS signal asking for shut down.
	*/
	errCh := make(chan error, 1);
	go func() {
		errCh <- serv.Start(); // If there is an error in server start it sends that to errCh channel
	}();

	/*
	Similarly this is a channel to catch OS system signals:
	- SIGINT := triggers on Ctrl+C on terminal
	- SIGTERM := triggered by Docker when you type docker compose down or stop a container
	*/
	sigCh := make(chan os.Signal, 1);
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM);

	select {
	// For server crashes
	case err := <- errCh:
		if(err != nil) {
			log.Fatalf("[SERVER:MAIN] Fatal server crash: %v", err);
		}
	
	// For OS
	case sig := <- sigCh:
		log.Printf("[SERVER:MAIN] Received %s, shutting down gracefully", sig);
		
		/*
		No cruel instant termination we give in flight connections (refers to our Java client) a window
		to finish before we give up and exit. Docker grants around 10 seconds between SIGTERM & SIGKILL 
		and our duration fits in between that.
		*/
		ctx, cancel := context.WithTimeout(context.Background(), 8 * time.Second);
		defer cancel();


		if err := serv.Shutdown(ctx); err != nil {
			log.Printf("[SERVER:MAIN] Shutdown did not complete cleanly: %v", err);
		}

		// final snapshot, only after connections have drained
		if persister != nil {
			close(snapshotStopCh);
			if err := persister.Save(myStore.SnapshotEntries()); err != nil {
				log.Printf("[SERVER:MAIN] Final snapshot save failed: %v", err);
			} else {
				log.Printf("[SERVER:MAIN] Final snapshot saved");
			}
		}
	}

	log.Printf("[SERVER:MAIN] Bue!");
}

/*
Simply runs forever (until `stop` is closed) as its own goroutine, saving a snapshot to Postgres
once every `interval`. Same ticker+select shape as store.go's own startActiveExpiration. A time.Ticker
fires on its own channel repeatedly at a fixed interval, and select{} here just waits for either that
tick or stop signal, whichever comes first each time through the loop. A failed save is logged and skipped
rather than treated as fatal. This goroutine just tries again on the next tick, since a single missed
snapshot isn't worth crashing the whole server over.
*/
func runPeriodicSnapshots(st *store.Store, persister *persistence.PostgresPersister, interval time.Duration, stop <- chan struct{}) {
	ticker := time.NewTicker(interval);
	defer ticker.Stop();

	for {
		select {
		case <- ticker.C:
			if err := persister.Save(st.SnapshotEntries()); err != nil {
				log.Printf("[SERVER:MAIN] Periodic snapshot save failed: %v", err);
			}
		case <- stop:
			return;
		}
	}
}

/*
Small env-var helpers so every RAYDASH_* setting above gets the same "use if it's set and valid, 
otherwise fall back to a sane default and keep running behaviour", instead of the server refusing
to start over a missing or malformed config value.
*/
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" { return v; }
	return fallback;
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil { return n; }

		log.Printf("[SERVER:MAIN] Invalid integer for %s=%q, using default %d", key, v, fallback);
	}

	return fallback;
}
