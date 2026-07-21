// Tests for the login flow: single-use magic links, guest-progress adoption
// on first login, cross-site refusal, and the outbound-mail rate caps.
// Fixtures and helpers live in server_test.go.
package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// study answers the fixture deck's one card correctly, so the visitor has a
// progress file on disk.
func (srv *testServer) study(c *http.Client) {
	srv.t.Helper()
	page := srv.getPage(c, srv.quiz)
	nonce := extractNonce(srv.t, page)
	resp, _ := srv.postForm(c, srv.quiz+"/answer", url.Values{"serve": {nonce}, "answer": {"fruit"}}, nil)
	check303(srv.t, resp)
}

// requestLink posts the login form and recovers the token from the link the
// fake mailer captured.
func (srv *testServer) requestLink(c *http.Client, email string) string {
	srv.t.Helper()
	resp, body := srv.postForm(c, "/login", url.Values{"email": {email}}, nil)
	if resp.StatusCode != http.StatusOK {
		srv.t.Fatalf("POST /login: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Check your email") {
		srv.t.Fatalf("login response is not the sent page:\n%s", body)
	}
	link := srv.mailer.last().link
	token := strings.TrimPrefix(link, testBaseURL+"/auth/")
	if token == link || token == "" {
		srv.t.Fatalf("mailed link %q does not carry a token under %s/auth/", link, testBaseURL)
	}
	return token
}

// sessionCookie returns the session cookie a response set, or nil.
func sessionCookie(resp *http.Response) *http.Cookie {
	for _, ck := range resp.Cookies() {
		if ck.Name == "session" && ck.Value != "" {
			return ck
		}
	}
	return nil
}

// userDirs lists the visitor directories under dataDir/users.
func userDirs(t *testing.T, dataDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dataDir, "users"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestAuthTokenSingleUse redeems a login link twice: the first redeem logs
// in, the second renders the gone page and sets no session.
func TestAuthTokenSingleUse(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	c := srv.client()

	token := srv.requestLink(c, "friend@example.com")
	if got := srv.mailer.count(); got != 1 {
		t.Fatalf("mailer called %d times, want 1", got)
	}

	resp, _ := srv.postForm(c, "/auth/"+token, url.Values{}, nil)
	check303(t, resp)
	if sessionCookie(resp) == nil {
		t.Fatal("first redeem set no session cookie")
	}

	// The link was spent: a replay renders the gone page, logs nobody in.
	resp, body := srv.postForm(c, "/auth/"+token, url.Values{}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second redeem: status %d, want 200 (gone page)", resp.StatusCode)
	}
	if !strings.Contains(body, "That link has expired") {
		t.Fatalf("second redeem is not the gone page:\n%s", body)
	}
	if sessionCookie(resp) != nil {
		t.Fatal("second redeem set a session cookie")
	}
}

// TestGuestAdoptionOnFirstLogin checks both halves of adoption: a brand-new
// account takes over the requesting guest's progress directory, while a
// login into that now-existing account leaves the second guest's directory
// alone.
func TestGuestAdoptionOnFirstLogin(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	const email = "adopt@example.com"

	// Guest A studies, so there is progress to adopt.
	cA := srv.client()
	srv.study(cA)
	guestA := srv.cookieValue(cA, "guest")
	if correct, _ := progressTotals(t, srv.dataDir, guestA); correct != 1 {
		t.Fatalf("guest A progress = %d correct, want 1", correct)
	}

	// First login with a fresh email: the new account adopts guest A's
	// directory by rename.
	token := srv.requestLink(cA, email)
	resp, _ := srv.postForm(cA, "/auth/"+token, url.Values{}, nil)
	check303(t, resp)

	if _, err := os.Stat(filepath.Join(srv.dataDir, "users", guestA)); !os.IsNotExist(err) {
		t.Fatalf("guest A directory still present after adoption (err=%v)", err)
	}
	dirs := userDirs(t, srv.dataDir)
	if len(dirs) != 1 {
		t.Fatalf("users/ = %v, want exactly the adopted account", dirs)
	}
	userID := dirs[0]
	if userID == guestA {
		t.Fatalf("users/ still keyed by guest ID %s", guestA)
	}
	if correct, _ := progressTotals(t, srv.dataDir, userID); correct != 1 {
		t.Fatalf("adopted progress = %d correct, want 1", correct)
	}

	// Guest B studies, then logs into the same (now existing) account: the
	// account wins, guest B's directory stays where it is.
	cB := srv.client()
	srv.study(cB)
	guestB := srv.cookieValue(cB, "guest")
	if correct, _ := progressTotals(t, srv.dataDir, guestB); correct != 1 {
		t.Fatalf("guest B progress = %d correct, want 1", correct)
	}

	// The per-address cooldown would swallow the second mail inside a
	// minute; release it rather than sleeping.
	srv.s.unrecordMail(email)
	token = srv.requestLink(cB, email)
	resp, _ = srv.postForm(cB, "/auth/"+token, url.Values{}, nil)
	check303(t, resp)

	if correct, _ := progressTotals(t, srv.dataDir, guestB); correct != 1 {
		t.Fatalf("guest B progress gone after logging into an existing account: %d correct, want 1", correct)
	}
	if correct, _ := progressTotals(t, srv.dataDir, userID); correct != 1 {
		t.Fatalf("account progress = %d correct, want 1", correct)
	}
}

// TestCrossSiteLoginRefused refuses POSTs that declare themselves
// cross-site, on /login and on the redeem endpoint, without mailing.
func TestCrossSiteLoginRefused(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	c := srv.client()
	crossSite := map[string]string{"Sec-Fetch-Site": "cross-site"}

	resp, _ := srv.postForm(c, "/login", url.Values{"email": {"victim@example.com"}}, crossSite)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site /login: status %d, want 403", resp.StatusCode)
	}
	if got := srv.mailer.count(); got != 0 {
		t.Fatalf("mailer called %d times on a refused request, want 0", got)
	}

	resp, _ = srv.postForm(c, "/auth/sometoken", url.Values{}, crossSite)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site /auth: status %d, want 403", resp.StatusCode)
	}
}

// TestMailAddressCooldown submits the same address twice back to back: both
// render the sent page, only one mail leaves.
func TestMailAddressCooldown(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	c := srv.client()

	srv.requestLink(c, "again@example.com")
	resp, body := srv.postForm(c, "/login", url.Values{"email": {"again@example.com"}}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second /login: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Check your email") {
		t.Fatalf("second /login is not the sent page:\n%s", body)
	}
	if got := srv.mailer.count(); got != 1 {
		t.Fatalf("mailer called %d times, want 1 (address cooldown)", got)
	}
}

// TestMailPerIPCap sprays six distinct addresses from one client IP: the
// per-IP hourly ceiling stops the sixth.
func TestMailPerIPCap(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	c := srv.client()

	addrs := []string{"a@example.com", "b@example.com", "c@example.com",
		"d@example.com", "e@example.com", "f@example.com"}
	for _, a := range addrs {
		resp, body := srv.postForm(c, "/login", url.Values{"email": {a}}, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("/login %s: status %d, want 200", a, resp.StatusCode)
		}
		if !strings.Contains(body, "Check your email") {
			t.Fatalf("/login %s is not the sent page:\n%s", a, body)
		}
	}
	if got := srv.mailer.count(); got != mailPerIPHourly {
		t.Fatalf("mailer called %d times for %d addresses, want %d (per-IP cap)", got, len(addrs), mailPerIPHourly)
	}
}
