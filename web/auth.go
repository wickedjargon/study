// Logging in: the "keep my progress" upgrade. A visitor types their email,
// gets a one-shot link, and the link turns their anonymous guest into (or
// back into) an account. Redeeming is a POST behind a confirm page — inbox
// link scanners prefetch GETs, and a prefetch must not spend the token.
//
// A brand-new account adopts the requesting guest's progress by renaming
// their directory; logging into an existing account just switches to it and
// leaves the guest data behind (the account wins).
package web

import (
	"log"
	"net"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// mailCooldown is the least time between two login emails to one address —
// a typo'd resubmit works a minute later, a loop can't drain the send quota.
const mailCooldown = time.Minute

// mailPerIPHourly and mailGlobalHourly cap outbound login email beyond the
// per-address cooldown: the endpoint is unauthenticated, so without these an
// address-spraying loop could drain the send quota (and the domain's
// reputation) while honoring every per-address cooldown.
const (
	mailPerIPHourly  = 5
	mailGlobalHourly = 30
)

// sameSitePOST reports whether a state-changing POST came from this site's
// own pages. The auth POSTs can't lean on cookies for it — redeeming a login
// link needs none — so without this a cross-site form could log a victim
// into the attacker's account. Requests that declare themselves cross-site
// are refused; absent headers (curl, old browsers) pass.
func (s *Server) sameSitePOST(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" && origin != s.baseURL {
		return false
	}
	return true
}

// loginView drives the login template through its stages: the form ("form"),
// the check-your-inbox page ("sent"), and the expired-link page ("gone").
// The emailed link itself renders the bare "redeem" template instead.
type loginView struct {
	Stage string
	Email string // logged-in email on "form", destination on "sent"
	Error string // validation complaint on "form"
}

// currentUser resolves the session cookie, or ("", "") for a guest.
func (s *Server) currentUser(r *http.Request) (id, email string) {
	c, err := r.Cookie("session")
	if err != nil {
		return "", ""
	}
	return s.ids.sessionUser(c.Value)
}

// visitorID is the identity progress is keyed by: the logged-in user when
// there is one, else the guest cookie (minted on first contact). Desktop
// mode is one machine, one user: a fixed identity, no cookies.
func (s *Server) visitorID(w http.ResponseWriter, r *http.Request) string {
	if s.Local {
		return "local"
	}
	if id, _ := s.currentUser(r); id != "" {
		return id
	}
	return s.guestID(w, r)
}

// localGuard bounces the auth pages home in desktop mode, where accounts
// don't exist. Reports whether it handled the request.
func (s *Server) localGuard(w http.ResponseWriter, r *http.Request) bool {
	if s.Local {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
	return s.Local
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.localGuard(w, r) {
		return
	}
	_, email := s.currentUser(r)
	s.render(w, "login", loginView{Stage: "form", Email: email})
}

func (s *Server) handleLoginSend(w http.ResponseWriter, r *http.Request) {
	if s.localGuard(w, r) {
		return
	}
	if !s.sameSitePOST(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(r.FormValue("email")))
	if err != nil || len(addr.Address) > 254 {
		s.render(w, "login", loginView{Stage: "form", Error: "That doesn't look like an email address"})
		return
	}
	email := strings.ToLower(addr.Address)

	if !s.mayMail(email, clientIP(r)) {
		// Recently sent (or over a cap): point at the inbox again rather
		// than re-emailing.
		s.render(w, "login", loginView{Stage: "sent", Email: email})
		return
	}
	token, err := s.ids.createToken(email, s.guestID(w, r))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.mailer.SendLogin(email, s.baseURL+"/auth/"+token); err != nil {
		// The email never left: clear the address cooldown so an immediate
		// retry isn't told to check an inbox holding nothing. The IP and
		// global counts stand — failures spend quota too.
		s.unrecordMail(email)
		s.fail(w, err)
		return
	}
	s.render(w, "login", loginView{Stage: "sent", Email: email})
}

// clientIP is the sending host for the per-IP mail cap: the proxy's
// forwarded address when present (nginx fronts the deploy), else the peer.
// Spoofable when reached directly — the global ceiling is the backstop.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// mayMail reserves the right to send one login email: the per-address
// cooldown plus per-IP and global hourly ceilings. Reserving (rather than
// recording after the send) keeps two racing submits from both mailing.
func (s *Server) mayMail(email, ip string) bool {
	s.mailMu.Lock()
	defer s.mailMu.Unlock()
	now := time.Now()
	for e, t := range s.lastMail {
		if now.Sub(t) > mailCooldown {
			delete(s.lastMail, e)
		}
	}
	prune := func(times []time.Time) []time.Time {
		kept := times[:0]
		for _, t := range times {
			if now.Sub(t) <= time.Hour {
				kept = append(kept, t)
			}
		}
		return kept
	}
	s.mailAll = prune(s.mailAll)
	s.mailByIP[ip] = prune(s.mailByIP[ip])
	if len(s.mailByIP[ip]) == 0 {
		delete(s.mailByIP, ip)
	}

	if _, recent := s.lastMail[email]; recent {
		return false
	}
	if len(s.mailByIP[ip]) >= mailPerIPHourly || len(s.mailAll) >= mailGlobalHourly {
		return false
	}
	s.lastMail[email] = now
	s.mailByIP[ip] = append(s.mailByIP[ip], now)
	s.mailAll = append(s.mailAll, now)
	return true
}

// unrecordMail releases a failed send's address cooldown.
func (s *Server) unrecordMail(email string) {
	s.mailMu.Lock()
	defer s.mailMu.Unlock()
	delete(s.lastMail, email)
}

func (s *Server) handleAuthPage(w http.ResponseWriter, r *http.Request) {
	if s.localGuard(w, r) {
		return
	}
	// The token is not checked here: a prefetching scanner learns nothing and
	// spends nothing. The page submits itself (a click, without JS) and the
	// POST decides.
	s.render(w, "redeem", r.PathValue("token"))
}

func (s *Server) handleAuthRedeem(w http.ResponseWriter, r *http.Request) {
	if s.localGuard(w, r) {
		return
	}
	if !s.sameSitePOST(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	email, guest, err := s.ids.redeemToken(r.PathValue("token"))
	if err == errNoToken {
		s.render(w, "login", loginView{Stage: "gone"})
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	userID, isNew, err := s.ids.findOrCreateUser(email)
	if err != nil {
		s.fail(w, err)
		return
	}
	if isNew && validGuestID(guest) {
		s.adoptGuest(guest, userID)
	}
	session, err := s.ids.createSession(userID)
	if err != nil {
		s.fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session,
		Path:     "/",
		MaxAge:   int(sessionLife / time.Second),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// adoptGuest hands a guest's progress directory to a freshly minted account.
// The guest may never have studied (no directory) — then there is nothing to
// adopt. Live quiz sessions under the guest key are dropped so no engine
// keeps writing to the old path.
func (s *Server) adoptGuest(guest, userID string) {
	s.mu.Lock()
	for k := range s.sessions {
		if strings.HasPrefix(k, guest+"|") {
			delete(s.sessions, k)
		}
	}
	s.mu.Unlock()

	from := filepath.Join(s.dataDir, "users", guest)
	to := filepath.Join(s.dataDir, "users", userID)
	if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
		log.Printf("adopting guest %s progress: %v", guest, err)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.localGuard(w, r) {
		return
	}
	if c, err := r.Cookie("session"); err == nil {
		if err := s.ids.deleteSession(c.Value); err != nil {
			log.Printf("deleting session: %v", err)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
