// Tests for the quiz-serving side of the web package: the serve-nonce
// protocol (stepping actions must present the nonce of the page they were
// rendered on), the answer flow's progress recording, and the set-card
// entry loop. Auth and mail-rate tests live in auth_test.go; both share the
// fixtures and helpers here.
package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// typedDeck is one type-to-answer card. The preview-new header is belt and
// braces: the client helper also sets the intros=off cookie, since the
// guest's preference overrides the deck header in both directions.
const typedDeck = "# preview-new: off\n\napple\n---\nfruit\n"

// setDeck is one enumeration card: name both items, entry by entry.
const setDeck = "# preview-new: off\n\nname two\n---\n+ alpha\n+ beta\n"

// sentMail is one captured Mailer call.
type sentMail struct {
	email, link string
}

// fakeMailer captures SendLogin calls instead of delivering anything.
type fakeMailer struct {
	mu    sync.Mutex
	sends []sentMail
}

func (m *fakeMailer) SendLogin(to, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, sentMail{to, link})
	return nil
}

func (m *fakeMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sends)
}

func (m *fakeMailer) last() sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sends) == 0 {
		return sentMail{}
	}
	return m.sends[len(m.sends)-1]
}

// testBaseURL is the base the server mints login links against; tests peel
// it off the captured link to recover the token.
const testBaseURL = "http://example.test"

// testServer wires a Server over one fixture deck behind httptest.
type testServer struct {
	t       *testing.T
	s       *Server
	ts      *httptest.Server
	mailer  *fakeMailer
	dataDir string
	quiz    string // /q/{group}/{deck} for the fixture deck
}

func newTestServer(t *testing.T, deckContent string) *testServer {
	t.Helper()
	dir := t.TempDir()
	deckPath := filepath.Join(dir, "study-fixture.deck")
	if err := os.WriteFile(deckPath, []byte(deckContent), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	mailer := &fakeMailer{}
	s, err := New([]string{deckPath}, dataDir, testBaseURL, mailer)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	g := s.groups[0]
	return &testServer{
		t:       t,
		s:       s,
		ts:      ts,
		mailer:  mailer,
		dataDir: dataDir,
		quiz:    "/q/" + g.Slug + "/" + g.Decks[0].Slug,
	}
}

// bareClient is one genuinely fresh visitor: a cookie jar (so the minted
// guest identity sticks) and no redirect following, so tests can assert the
// 303s themselves.
func (srv *testServer) bareClient() *http.Client {
	srv.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		srv.t.Fatal(err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// client is bareClient with the intros preference pre-set to off, so new
// cards arrive as questions, not answer-visible previews.
func (srv *testServer) client() *http.Client {
	srv.t.Helper()
	c := srv.bareClient()
	u, err := url.Parse(srv.ts.URL)
	if err != nil {
		srv.t.Fatal(err)
	}
	c.Jar.SetCookies(u, []*http.Cookie{{Name: "intros", Value: "off"}})
	return c
}

// getPage GETs a path and returns the body, requiring 200.
func (srv *testServer) getPage(c *http.Client, path string) string {
	srv.t.Helper()
	resp, err := c.Get(srv.ts.URL + path)
	if err != nil {
		srv.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		srv.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		srv.t.Fatalf("GET %s: status %d, want 200", path, resp.StatusCode)
	}
	return string(body)
}

// postForm POSTs a form and returns the (unfollowed) response and its body.
func (srv *testServer) postForm(c *http.Client, path string, form url.Values, hdr map[string]string) (*http.Response, string) {
	srv.t.Helper()
	req, err := http.NewRequest("POST", srv.ts.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		srv.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		srv.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		srv.t.Fatal(err)
	}
	return resp, string(body)
}

// cookieValue reads a cookie from the client's jar, "" if absent.
func (srv *testServer) cookieValue(c *http.Client, name string) string {
	srv.t.Helper()
	u, err := url.Parse(srv.ts.URL)
	if err != nil {
		srv.t.Fatal(err)
	}
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

var nonceRe = regexp.MustCompile(`name="serve" value="([^"]+)"`)

// extractNonce pulls the serve nonce out of a rendered quiz page.
func extractNonce(t *testing.T, page string) string {
	t.Helper()
	m := nonceRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no serve nonce in page:\n%s", page)
	}
	return m[1]
}

func check303(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
}

// progressTotals sums recorded answers across a visitor's progress files;
// (0, 0) when nothing was ever saved.
func progressTotals(t *testing.T, dataDir, visitor string) (correct, wrong int) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dataDir, "users", visitor, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		var dp struct {
			Cards map[string]struct {
				TimesCorrect int `json:"times_correct"`
				TimesWrong   int `json:"times_wrong"`
			} `json:"cards"`
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &dp); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, cp := range dp.Cards {
			correct += cp.TimesCorrect
			wrong += cp.TimesWrong
		}
	}
	return correct, wrong
}

// TestAnswerFlowRecordsCorrect walks the happy path: question page, nonce
// out of the HTML, correct typed answer, result screen, progress on disk.
func TestAnswerFlowRecordsCorrect(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	c := srv.client()

	page := srv.getPage(c, srv.quiz)
	if !strings.Contains(page, `name="answer"`) {
		t.Fatalf("first serve is not a question screen:\n%s", page)
	}
	nonce := extractNonce(t, page)

	resp, _ := srv.postForm(c, srv.quiz+"/answer", url.Values{"serve": {nonce}, "answer": {"fruit"}}, nil)
	check303(t, resp)

	page = srv.getPage(c, srv.quiz)
	if !strings.Contains(page, "✓ correct") {
		t.Fatalf("after correct answer, page is not the correct-result screen:\n%s", page)
	}

	guest := srv.cookieValue(c, "guest")
	if guest == "" {
		t.Fatal("no guest cookie minted")
	}
	correct, wrong := progressTotals(t, srv.dataDir, guest)
	if correct != 1 || wrong != 0 {
		t.Fatalf("progress totals = %d correct, %d wrong, want 1, 0", correct, wrong)
	}
}

// TestDuplicateSubmitDropped re-posts the same answer form: the nonce is
// stale after the first apply, so the second submit must change nothing.
func TestDuplicateSubmitDropped(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	c := srv.client()

	page := srv.getPage(c, srv.quiz)
	nonce := extractNonce(t, page)
	form := url.Values{"serve": {nonce}, "answer": {"fruit"}}

	resp, _ := srv.postForm(c, srv.quiz+"/answer", form, nil)
	check303(t, resp)

	// Same form again: back button or double click.
	resp, _ = srv.postForm(c, srv.quiz+"/answer", form, nil)
	check303(t, resp)

	page = srv.getPage(c, srv.quiz)
	if !strings.Contains(page, "✓ correct") {
		t.Fatalf("duplicate submit moved the session off the result screen:\n%s", page)
	}
	guest := srv.cookieValue(c, "guest")
	correct, wrong := progressTotals(t, srv.dataDir, guest)
	if correct != 1 || wrong != 0 {
		t.Fatalf("progress totals = %d correct, %d wrong, want 1, 0 (no double count)", correct, wrong)
	}
}

// TestStaleNonceDropped posts an answer under a made-up serve value: the
// action must be dropped without grading anything.
func TestStaleNonceDropped(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	c := srv.client()

	srv.getPage(c, srv.quiz) // compose the session, mint the guest

	resp, _ := srv.postForm(c, srv.quiz+"/answer", url.Values{"serve": {"deadbeef:7"}, "answer": {"fruit"}}, nil)
	check303(t, resp)

	page := srv.getPage(c, srv.quiz)
	if !strings.Contains(page, `name="answer"`) || strings.Contains(page, "✓ correct") {
		t.Fatalf("stale-nonce answer was applied; want the question screen back:\n%s", page)
	}
	guest := srv.cookieValue(c, "guest")
	if correct, wrong := progressTotals(t, srv.dataDir, guest); correct != 0 || wrong != 0 {
		t.Fatalf("progress totals = %d correct, %d wrong, want 0, 0", correct, wrong)
	}
}

// TestMissingNonceDropped posts an answer with no serve field at all.
func TestMissingNonceDropped(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	c := srv.client()

	srv.getPage(c, srv.quiz)

	resp, _ := srv.postForm(c, srv.quiz+"/answer", url.Values{"answer": {"fruit"}}, nil)
	check303(t, resp)

	page := srv.getPage(c, srv.quiz)
	if !strings.Contains(page, `name="answer"`) || strings.Contains(page, "✓ correct") {
		t.Fatalf("nonce-less answer was applied; want the question screen back:\n%s", page)
	}
	guest := srv.cookieValue(c, "guest")
	if correct, wrong := progressTotals(t, srv.dataDir, guest); correct != 0 || wrong != 0 {
		t.Fatalf("progress totals = %d correct, %d wrong, want 0, 0", correct, wrong)
	}
}

// TestRecomposeInvalidatesOldNonce restarts the session (start needs no
// nonce) and then submits the pre-restart page's form: the fresh session's
// first card must not be graded by the stale form.
func TestRecomposeInvalidatesOldNonce(t *testing.T) {
	srv := newTestServer(t, typedDeck)
	c := srv.client()

	page := srv.getPage(c, srv.quiz)
	oldNonce := extractNonce(t, page)

	resp, _ := srv.postForm(c, srv.quiz+"/start", url.Values{}, nil)
	check303(t, resp)

	resp, _ = srv.postForm(c, srv.quiz+"/answer", url.Values{"serve": {oldNonce}, "answer": {"fruit"}}, nil)
	check303(t, resp)

	page = srv.getPage(c, srv.quiz)
	if !strings.Contains(page, `name="answer"`) || strings.Contains(page, "✓ correct") {
		t.Fatalf("old nonce graded the recomposed session's card:\n%s", page)
	}
	guest := srv.cookieValue(c, "guest")
	if correct, wrong := progressTotals(t, srv.dataDir, guest); correct != 0 || wrong != 0 {
		t.Fatalf("progress totals = %d correct, %d wrong, want 0, 0", correct, wrong)
	}
}

// TestSetCardFlow drives the enumeration loop: each entry needs the current
// nonce and each render mints a new one, a duplicate entry flashes ?f=dup,
// and giving up ends the card with the full reveal.
func TestSetCardFlow(t *testing.T) {
	srv := newTestServer(t, setDeck)
	c := srv.client()

	page := srv.getPage(c, srv.quiz)
	if !strings.Contains(page, "named 0 of 2") {
		t.Fatalf("not the set question screen:\n%s", page)
	}
	n1 := extractNonce(t, page)

	// First entry names alpha.
	resp, _ := srv.postForm(c, srv.quiz+"/entry", url.Values{"serve": {n1}, "entry": {"alpha"}}, nil)
	check303(t, resp)
	if loc := resp.Header.Get("Location"); loc != srv.quiz {
		t.Fatalf("hit entry redirected to %q, want %q", loc, srv.quiz)
	}
	page = srv.getPage(c, srv.quiz)
	if !strings.Contains(page, "named 1 of 2") || !strings.Contains(page, "alpha ✓") {
		t.Fatalf("alpha not counted in the transcript:\n%s", page)
	}
	n2 := extractNonce(t, page)
	if n2 == n1 {
		t.Fatalf("nonce did not advance after an applied entry: %q", n2)
	}

	// Naming alpha again is a duplicate: costless, flashed via ?f=dup.
	resp, _ = srv.postForm(c, srv.quiz+"/entry", url.Values{"serve": {n2}, "entry": {"alpha"}}, nil)
	check303(t, resp)
	if loc := resp.Header.Get("Location"); loc != srv.quiz+"?f=dup" {
		t.Fatalf("duplicate entry redirected to %q, want %q", loc, srv.quiz+"?f=dup")
	}
	page = srv.getPage(c, srv.quiz+"?f=dup")
	if !strings.Contains(page, "already named") {
		t.Fatalf("duplicate flash missing:\n%s", page)
	}
	if !strings.Contains(page, "named 1 of 2") {
		t.Fatalf("duplicate entry changed the count:\n%s", page)
	}
	n3 := extractNonce(t, page)
	if n3 == n2 {
		t.Fatalf("nonce did not advance after a duplicate entry: %q", n3)
	}

	// Give up with the current nonce: the card ends and the reveal lists
	// every item, named and not.
	resp, _ = srv.postForm(c, srv.quiz+"/giveup", url.Values{"serve": {n3}}, nil)
	check303(t, resp)
	page = srv.getPage(c, srv.quiz)
	if !strings.Contains(page, `<span class="named">alpha</span>`) ||
		!strings.Contains(page, `<span class="unnamed">beta</span>`) {
		t.Fatalf("give-up reveal does not list the items:\n%s", page)
	}
	if !strings.Contains(page, "named 1 of 2") {
		t.Fatalf("give-up result lost the count:\n%s", page)
	}
}

// TestIntrosSeededFromHeader: a guest who has never touched the
// Introductions toggle starts from the deck's # preview-new: header — an
// explicit off opens cold, silence teaches first — and flipping the toggle
// overrides the header from then on. The pre-per-group site-wide cookie
// still counts as a choice.
func TestIntrosSeededFromHeader(t *testing.T) {
	// Header off: a fresh guest gets the question, not the reveal.
	srv := newTestServer(t, typedDeck)
	c := srv.bareClient()
	page := srv.getPage(c, srv.quiz)
	if !strings.Contains(page, `name="answer"`) || strings.Contains(page, "quiz me") {
		t.Fatal("header preview-new: off did not seed intros off for a fresh guest")
	}

	// The toggle beats the header once used.
	srv.postForm(c, srv.quiz+"/intros", url.Values{}, nil)
	page = srv.getPage(c, srv.quiz)
	if !strings.Contains(page, "quiz me") {
		t.Fatal("flipping the toggle did not override the header")
	}

	// No header: a fresh guest is taught first.
	srv2 := newTestServer(t, "apple\n---\nfruit\n")
	c2 := srv2.bareClient()
	if page := srv2.getPage(c2, srv2.quiz); !strings.Contains(page, "quiz me") {
		t.Fatal("headerless deck did not default a fresh guest to intros on")
	}

	// The legacy site-wide cookie still counts as a stated choice.
	if page := srv2.getPage(srv2.client(), srv2.quiz); !strings.Contains(page, `name="answer"`) {
		t.Fatal("legacy intros=off cookie no longer honored")
	}
}
