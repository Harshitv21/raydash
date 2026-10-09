/*
package persistence is to save and load raydash's in-memory store to and from postgres depends
on store.SnapshotEntry but the Store itself has no idea postgres or any persistence backend exists.
*/
package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// registers the pgx database/sql driver
	_ "github.com/jackc/pgx/v5/stdlib" 

	"internal/store"
)

/*
This whole file leans on Go's standard database/sql package, NOT on pgx's own native API directly.
pgx is only here to act as the low-level driver underneath it. That split is worth understanding up
front:
- database/sql (the "sql" package imported above) gives us the generic driver agnostic API like,
  sql.DB, transactions, Query/Exec, etc. This is the same API you'd use for Postgres, MySQL, SQLite
  or whatever.
- The underscore import `_ "github.com/jackc/pgx/v5/stdlib"` is a side-effect only import. We never
  call anything on it by name in this file. Importing is purely for its init() function, which 
  registers "pgx" as a driver name that database/sql's sql.Open() can find later on. That registration
  is the ONLY reason this import exists. The blank `_` is Go's way of saying "Yo I need this package's
  init() to run, but I'm not referencing anything from it directly".
*/

/*
schemaSQL is a simple one-timeish table definition that has safety of "IF NOT EXISTS" to prevent
duplication or error or something.
*/
const schemaSQL = `
CREATE TABLE IF NOT EXISTS raydash_snapshot (
	key         TEXT PRIMARY KEY,
	value       TEXT NOT NULL,
	expires_at  TIMESTAMPTZ,
	inserted_at TIMESTAMPTZ NOT NULL
)
`
/*
PostgresPersister wraps a *sql.DB, which despite the name isn't a single connection at all, it's a
whole connection POOL managed internally by database/sql. Every method below borrows a connection 
from that pool for the duration of one call and returns it automatically when done.
*/
type PostgresPersister struct {
	db *sql.DB
}

/* 
NewPostgresPersister opens a connection pool to dsn verifies it with a ping and make sures the snapshot 
table exists before returning.
*/
func NewPostgresPersister(dsn string) (*PostgresPersister, error) {
	/*
	sql.Open is deceptively named it does NOT actually open and connect to anything yet! It just 
	validates the dsn format and sets up the pool structure lazily. That's exactly why the PingContext
	call below exists without it a bad host/credentials/etc wouldn't surface until the first real
	query much later, possibly deep inside a snapshot save.
	*/
	db, err := sql.Open("pgx", dsn);
	if(err != nil) { return nil, fmt.Errorf("[SERVER:DB] Open postgres connection: %w", err); }

	/*
	pool tuning: cap on how many real Postgres connections we ever hold open at once (4 is plenty
	for our purpose of background snapshot job. This isn't a high traffic query path) and recycle
	connections periodically so we don't hold onto ones Postgres or a load balancer might want to
	cycle.
	*/
	db.SetMaxOpenConns(4);
	db.SetMaxIdleConns(4);
	db.SetConnMaxLifetime(30 * time.Minute);

	/*
	context.WithTimeout is Go's standard way of saying "give on this operation if it takes longer
	than X". It returns a context you pass into the call, plus a cancel function you MUST call (hence
	`defer cancel()` is called immediately after) to free the timer's resources once you are done,
	whether the operation succeeded, failed or timed out.
	*/
	ctx, cancel := context.WithTimeout(context.Background(), 5 * time.Second);
	defer cancel();

	if err := db.PingContext(ctx); err != nil {
		db.Close();
		return nil, fmt.Errorf("[SERVER:DB] Ping postgres: %w", err);
	}

	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		db.Close();
		return nil, fmt.Errorf("[SERVER:DB] Run schema migration: %w", err);
	}

	return &PostgresPersister{db: db}, nil;
}

/*
Save replaces the entire snapshot table with entries inside one transaction so nothing ever reads 
a half written snapshot it's either nil or complete it performs a full replace with TRUNCATE and 
re-insert which is the simplest way to clear deleted keys in our DB.
*/
func (p *PostgresPersister) Save(entries []store.SnapshotEntry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10 * time.Second);
	defer cancel();

	/*
	BeginTx starts a real Postgres transaction. Every ExecContext call on `tx` below happens inside
	it, invisible to any other connection until Commit() succeeds.
	*/
	tx, err := p.db.BeginTx(ctx, nil);
	if(err != nil) { return fmt.Errorf("[SERVER:DB] Begin snapshot transaction: %w", err) }

	/*
	Yo why are doing a rollback at the end tho? This is very non obvious at first but the Go+database/sql
	idiom: tx.Rollback() is deferred uncondtionally, even though we WANT this transaction to succeed.
	The trick is that calling Rollback() on a transaction that already Commit()'s successfully is
	documented to be a safe operation. So the real-world behaviour is: if we return early because of
	an error anywhere below, this deferred Rollback actually undoes the partial work and that is totally
	expected and fine but even if we make it all the way to tx.Commit() at the bottom and it succeeds,
	this deffered call fires too but does nothing, since there's nothing left to roll back! One line 
	that perfectly handles every exit path, success or failure.
	*/
	defer tx.Rollback();

	if _, err := tx.ExecContext(ctx, `TRUNCATE TABLE raydash_snapshot`); err != nil {
		return fmt.Errorf("[SERVER:DB] Truncate snapshot table: %w", err);
	}

	/*
	PrepareContext compiles this INSERT statement once, then we can execute it many times below (once
	per entry) just by supplying new argument values. Cheaper than having Postgres re-parse the same
	SQL text on every single row, and that what makes $1/$2/$3/$4 safe! These are placeholders the 
	driver fills separately from the SQL text itself, which is also what protects against SQL injection
	here.
	*/
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO raydash_snapshot (key, value, expires_at, inserted_at) VALUES ($1, $2, $3, $4)`)
	if(err != nil) { return fmt.Errorf("[SERVER:DB] Prepare snapshot insert: %w", err) }
	defer stmt.Close();

	for _, e := range entries {
		/*
		expires_at is a nullable column (no NOT NULL on it since a key with no TTL has no expiry at all)
		but e.ExpiresAt is a plain Go time.Time, which can never itself be "nil". The `var expiresAt interface{}`
		below starts out as true Go nil, and only gets a real time.Time value assigned to it when there 
		actually IS an expiry. database/sql specifically understands "pass a nil interface{} as a query
		argument" as "write a SQL NULL here". That's the mechanism that lets one Go variable represents
		both "no value" and "here's a timestamp" depending on what this particular entry needs.
		*/
		var expiresAt interface{};
		if !e.ExpiresAt.IsZero() { expiresAt = e.ExpiresAt; }

		/*
		inserted_at is NOT NULL in the schema, so unlike expires_at above we always need a real value
		here. Falling back to "now" covers the (normally never-hit) case of an entry that somehow arrives
		with a 0 InsertedAt.
		*/
		insertedAt := e.InsertedAt;
		if(insertedAt.IsZero()) { insertedAt = time.Now(); }

		if _, err := stmt.ExecContext(ctx, e.Key, e.Value, expiresAt, insertedAt); err != nil {
			return fmt.Errorf("[SERVER:DB] Insert snapshot row for key %q: %w", e.Key, err);
		}
	}

	/*
	This is the point where the whole transaction actually becomes visible/permanent. If the process
	crashed at any point before this line, Postgres would have rolled everything back on its own too,
	which is exactly the "either nil or complete" guarantee mentioned above.
	*/
	return tx.Commit();
}

/*
Load reads every row currently in the snapshot table back out Store.LoadSnapshot() rechecks expiry
on top of this so if a row happens to be expired in the gap between saving and loading it will be 
handled correctly.
*/
func (p *PostgresPersister) Load() ([]store.SnapshotEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10 * time.Second);
	defer cancel();

	rows, err := p.db.QueryContext(ctx, `SELECT key, value, expires_at, inserted_at FROM raydash_snapshot`);
	if(err != nil) { return nil, fmt.Errorf("[SERVER:DB] Query snapshot: %w", err); }

	// rows.Close() releases the underlying connection back to the pool
	defer rows.Close();

	var entries []store.SnapshotEntry;
	/*
	rows.Next() advances to the next row and reports whether one exists. This is the standard 
	database/sql loop shape: iterate while Next() returns true, Scan() the current row's columns
	into variables each time.
	*/
	for rows.Next() {
		var e store.SnapshotEntry;
		/*
		sql.NullTime is the read-side counterpart to the `interface{}` trick in Save() above.
		expires_at can be SQL NULL, and scanning a NULL column directly into a plain time.Time 
		would fail outright. sql.NullTime is a small struct with a Time field AND a Valid bool,
		so it can represent, "there was genuinely no value" without erroring. inserted_at doesn't
		need this treatment since its NOT NULL and can scan straight into e.InsertedAt (a plain
		time.Time) with no wrapper.
		*/
		var expiresAt sql.NullTime;

		if err := rows.Scan(&e.Key, &e.Value, &expiresAt, &e.InsertedAt); err != nil {
			return nil, fmt.Errorf("[SERVER:DB] Scan snapshot row: %w", err);
		}

		if(expiresAt.Valid) { e.ExpiresAt = expiresAt.Time; }
		entries = append(entries, e);
	}

	/*
	rows.Next() returning false means either,
	- We ran out of rows normally OR,
	- Something went wrong partway through
	rows.Err() is how you tell those 2 apart after the loop ends. It's nil in the normal case.
	*/
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("[SERVER:DB] Iterate snapshot rows: %w", err);
	}

	return entries, nil;
}

// closes up the whole connection pool. Called once on server shutdown (refer main.go)
func (p *PostgresPersister) Close() error { return p.db.Close(); }