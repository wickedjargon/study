// Identity: who a visitor is beyond the guest cookie. A user is an email
// address; logging in is proving you can read that inbox (a magic link),
// never a password. SQLite holds only identity — users, single-use login
// tokens, sessions — while progress stays in the per-user file store, so the
// engine never knows accounts exist.
//
// Tokens and sessions are stored as SHA-256 hashes: a leaked database file
// can't be replayed as a login link or a cookie.
package web

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// loginTokenLife is how long an emailed link works. Short: the link is
// clicked within minutes or it was lost to an inbox.
const loginTokenLife = 15 * time.Minute

// sessionLife is how long a login lasts. Long: friends shouldn't re-login on
// a device they already proved.
const sessionLife = 365 * 24 * time.Hour

// errNoToken is redeeming a token that is expired, spent, or never existed —
// one indistinguishable answer, so the redeem page can't be used to probe.
var errNoToken = errors.New("invalid or expired login link")

type identity struct {
	db *sql.DB
}

func openIdentity(path string) (*identity, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("opening identity db: %w", err)
	}
	// One connection: the driver serializes everything, so writers never see
	// SQLITE_BUSY. Identity traffic is a rounding error next to quiz traffic.
	db.SetMaxOpenConns(1)

	const schema = `
	CREATE TABLE IF NOT EXISTS users (
		id      TEXT PRIMARY KEY,
		email   TEXT NOT NULL UNIQUE,
		created INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS tokens (
		hash    TEXT PRIMARY KEY,
		email   TEXT NOT NULL,
		guest   TEXT NOT NULL,
		expires INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS sessions (
		hash    TEXT PRIMARY KEY,
		user    TEXT NOT NULL REFERENCES users(id),
		expires INTEGER NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating identity schema: %w", err)
	}
	return &identity{db: db}, nil
}

// newSecret mints a random URL-safe secret and the hash it is stored under.
func newSecret() (secret, hash string) {
	buf := make([]byte, 32)
	rand.Read(buf)
	secret = hex.EncodeToString(buf)
	return secret, hashSecret(secret)
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// createToken mints a login token for an email, remembering which guest asked
// so a new account can adopt that guest's progress. Expired tokens are swept
// here — the only place the table grows.
func (ids *identity) createToken(email, guest string) (string, error) {
	secret, hash := newSecret()
	now := time.Now()
	if _, err := ids.db.Exec(`DELETE FROM tokens WHERE expires < ?`, now.Unix()); err != nil {
		return "", err
	}
	_, err := ids.db.Exec(`INSERT INTO tokens (hash, email, guest, expires) VALUES (?, ?, ?, ?)`,
		hash, email, guest, now.Add(loginTokenLife).Unix())
	if err != nil {
		return "", err
	}
	return secret, nil
}

// redeemToken spends a token: it stops working the moment it answers, so a
// link forwarded or replayed logs nobody else in.
func (ids *identity) redeemToken(secret string) (email, guest string, err error) {
	var expires int64
	err = ids.db.QueryRow(`DELETE FROM tokens WHERE hash = ? RETURNING email, guest, expires`,
		hashSecret(secret)).Scan(&email, &guest, &expires)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && time.Now().Unix() > expires) {
		return "", "", errNoToken
	}
	if err != nil {
		return "", "", err
	}
	return email, guest, nil
}

// findOrCreateUser resolves an email to a user, minting one on first login.
// User IDs share the guests' 32-hex shape, so the progress store treats both
// identically.
func (ids *identity) findOrCreateUser(email string) (id string, isNew bool, err error) {
	err = ids.db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	buf := make([]byte, 16)
	rand.Read(buf)
	id = hex.EncodeToString(buf)
	_, err = ids.db.Exec(`INSERT INTO users (id, email, created) VALUES (?, ?, ?)`,
		id, email, time.Now().Unix())
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// createSession logs a user in on one device, returning the cookie value.
func (ids *identity) createSession(userID string) (string, error) {
	secret, hash := newSecret()
	_, err := ids.db.Exec(`INSERT INTO sessions (hash, user, expires) VALUES (?, ?, ?)`,
		hash, userID, time.Now().Add(sessionLife).Unix())
	if err != nil {
		return "", err
	}
	return secret, nil
}

// sessionUser resolves a session cookie to its user, or "" for anything
// expired, deleted, or made up.
func (ids *identity) sessionUser(secret string) (userID, email string) {
	var expires int64
	err := ids.db.QueryRow(
		`SELECT users.id, users.email, sessions.expires
		 FROM sessions JOIN users ON users.id = sessions.user
		 WHERE sessions.hash = ?`, hashSecret(secret)).Scan(&userID, &email, &expires)
	if err != nil || time.Now().Unix() > expires {
		return "", ""
	}
	return userID, email
}

// deleteSession logs one device out.
func (ids *identity) deleteSession(secret string) error {
	_, err := ids.db.Exec(`DELETE FROM sessions WHERE hash = ?`, hashSecret(secret))
	return err
}
