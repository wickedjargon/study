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
	addr, err := mail.ParseAddress(strings.TrimSpace(r.FormValue("email")))
	if err != nil || len(addr.Address) > 254 {
		s.render(w, "login", loginView{Stage: "form", Error: "That doesn't look like an email address"})
		return
	}
	email := strings.ToLower(addr.Address)

	if !s.mayMail(email) {
		// Recently sent: point at the inbox again rather than re-emailing.
		s.render(w, "login", loginView{Stage: "sent", Email: email})
		return
	}
	token, err := s.ids.createToken(email, s.guestID(w, r))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.mailer.SendLogin(email, s.baseURL+"/auth/"+token); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "login", loginView{Stage: "sent", Email: email})
}

// mayMail enforces the per-address cooldown.
func (s *Server) mayMail(email string) bool {
	s.mailMu.Lock()
	defer s.mailMu.Unlock()
	now := time.Now()
	for e, t := range s.lastMail {
		if now.Sub(t) > mailCooldown {
			delete(s.lastMail, e)
		}
	}
	if _, recent := s.lastMail[email]; recent {
		return false
	}
	s.lastMail[email] = now
	return true
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
