package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Shin-R2un/kpot/internal/store"
	"github.com/Shin-R2un/kpot/internal/vault"
)

const testPass = "p4ssword"

// makeVault creates a vault file with three sample notes covering
// note-name-only matches, body matches, secret fields, and url fields.
func makeVault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.kpot")
	v := store.New()
	if _, err := v.Put("ai/openai", "# OpenAI\n\nid: shin@example.com\nurl: https://platform.openai.com\napikey: sk-test-12345\n\n## memo\nProduction OpenAI account.\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Put("ai/anthropic", "# Anthropic\n\nurl: https://console.anthropic.com\napikey: sk-ant-test-67890\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Put("server/fw0", "# Firewall\n\nip: 10.0.0.1\nuser: admin\npass: hunter2\n"); err != nil {
		t.Fatal(err)
	}
	pt, err := v.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := vault.Create(path, []byte(testPass), pt); err != nil {
		t.Fatal(err)
	}
	return path
}

// startServer wires a Server to httptest with the given idle timeout.
// The cookie jar threads cookies across follow-up requests.
func startServer(t *testing.T, idle time.Duration) (*httptest.Server, *http.Client, *Server) {
	t.Helper()
	path := makeVault(t)
	s := newServerForTests(path, idle)
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	cli := &http.Client{
		Jar: jar,
		// Don't follow redirects so we can assert 302 responses.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return ts, cli, s
}

func login(t *testing.T, ts *httptest.Server, cli *http.Client, pass string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"passphrase": pass})
	req, _ := http.NewRequest("POST", ts.URL+"/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := cli.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// ----- 1. login happy path -----

func TestLoginSucceedsAndCanQuery(t *testing.T) {
	ts, cli, _ := startServer(t, 30*time.Minute)

	res := login(t, ts, cli, testPass)
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("login: status=%d body=%s", res.StatusCode, body)
	}
	if len(res.Cookies()) == 0 {
		t.Fatal("login set no cookie")
	}

	// Now hit /api/notes?q=openai with the cookie jar.
	res2, err := cli.Get(ts.URL + "/api/notes?q=openai")
	if err != nil {
		t.Fatal(err)
	}
	if res2.StatusCode != 200 {
		t.Fatalf("notes: status=%d", res2.StatusCode)
	}
	var data listResp
	json.NewDecoder(res2.Body).Decode(&data)
	if len(data.Matches) == 0 {
		t.Fatalf("expected matches for 'openai', got 0")
	}
	foundOpenai := false
	for _, m := range data.Matches {
		if m.Name == "ai/openai" {
			foundOpenai = true
			break
		}
	}
	if !foundOpenai {
		t.Errorf("ai/openai not in match list: %+v", data.Matches)
	}
}

// ----- 2. login wrong passphrase -----

func TestLoginRejectsBadPassphrase(t *testing.T) {
	ts, cli, _ := startServer(t, 30*time.Minute)
	res := login(t, ts, cli, "wrong")
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
	for _, c := range res.Cookies() {
		if c.Name == cookieName && c.Value != "" {
			t.Errorf("cookie was set on bad login: %v", c)
		}
	}
}

// ----- 3. rate limit on repeated bad attempts -----

func TestLoginRateLimit(t *testing.T) {
	ts, cli, _ := startServer(t, 30*time.Minute)
	// 3 failures bring us to threshold; 4th attempt should be 429.
	for i := 0; i < 3; i++ {
		res := login(t, ts, cli, "wrong")
		if res.StatusCode != 401 {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, res.StatusCode)
		}
	}
	res := login(t, ts, cli, "wrong")
	if res.StatusCode != 429 {
		t.Fatalf("4th attempt: expected 429, got %d", res.StatusCode)
	}
}

// ----- 4. search response shape: no body in list view -----

func TestSearchResponseDoesNotIncludeBody(t *testing.T) {
	ts, cli, _ := startServer(t, 30*time.Minute)
	login(t, ts, cli, testPass)
	res, err := cli.Get(ts.URL + "/api/notes?q=openai")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if strings.Contains(string(body), "sk-test-12345") {
		t.Errorf("list endpoint leaked apikey in response: %s", body)
	}
	if strings.Contains(string(body), "Production OpenAI account") {
		t.Errorf("list endpoint leaked memo: %s", body)
	}
}

// ----- 5. field secret retrieval -----

func TestFieldSecretRetrieval(t *testing.T) {
	ts, cli, _ := startServer(t, 30*time.Minute)
	login(t, ts, cli, testPass)
	// detail view: secret values redacted, fields list secret flag
	res, err := cli.Get(ts.URL + "/api/notes/" + "ai%2Fopenai")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("detail: status=%d body=%s", res.StatusCode, body)
	}
	var d detailResp
	json.NewDecoder(res.Body).Decode(&d)
	foundSecretFlag := false
	for _, f := range d.Fields {
		if f.Key == "apikey" {
			if !f.IsSecret {
				t.Errorf("apikey should have is_secret=true")
			}
			foundSecretFlag = true
		}
	}
	if !foundSecretFlag {
		t.Errorf("apikey field not in detail response")
	}
	if strings.Contains(d.BodyRedacted, "sk-test-12345") {
		t.Errorf("body_redacted should mask the apikey value: %s", d.BodyRedacted)
	}
	if !strings.Contains(d.BodyRedacted, secretMask) {
		t.Errorf("body_redacted should contain the mask: %s", d.BodyRedacted)
	}

	// granular field endpoint returns the real value
	res2, err := cli.Get(ts.URL + "/api/notes/ai%2Fopenai/field/apikey")
	if err != nil {
		t.Fatal(err)
	}
	var fv fieldValueResp
	json.NewDecoder(res2.Body).Decode(&fv)
	if fv.Value != "sk-test-12345" {
		t.Errorf("field value = %q, want sk-test-12345", fv.Value)
	}
	if !fv.IsSecret {
		t.Errorf("is_secret should be true for apikey")
	}
}

// ----- 6. idle lock fires -----

func TestIdleLockReturns401ReauthOnNextRequest(t *testing.T) {
	// 50ms idle so the test wraps quickly.
	ts, cli, _ := startServer(t, 50*time.Millisecond)
	login(t, ts, cli, testPass)
	// Wait past the idle period.
	time.Sleep(120 * time.Millisecond)

	res, err := cli.Get(ts.URL + "/api/notes?q=openai")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401 after idle lock, got %d", res.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(res.Body).Decode(&body)
	if body["action"] != actionReauth {
		t.Errorf("expected action=reauth, got %v", body["action"])
	}
}

// ----- 7. re-login from Locked reuses cookie, restores active state -----

func TestReloginFromLockedReusesCookie(t *testing.T) {
	ts, cli, _ := startServer(t, 50*time.Millisecond)

	// initial login
	res := login(t, ts, cli, testPass)
	if res.StatusCode != 200 {
		t.Fatalf("first login failed: %d", res.StatusCode)
	}
	// remember the cookie
	u, _ := url.Parse(ts.URL)
	cookies := cli.Jar.Cookies(u)
	var origSid string
	for _, c := range cookies {
		if c.Name == cookieName {
			origSid = c.Value
			break
		}
	}
	if origSid == "" {
		t.Fatal("no session cookie after login")
	}

	// idle expires
	time.Sleep(120 * time.Millisecond)

	// re-login with same cookie jar
	res2 := login(t, ts, cli, testPass)
	if res2.StatusCode != 200 {
		t.Fatalf("re-login failed: %d", res2.StatusCode)
	}
	cookies2 := cli.Jar.Cookies(u)
	var newSid string
	for _, c := range cookies2 {
		if c.Name == cookieName {
			newSid = c.Value
			break
		}
	}
	if newSid != origSid {
		t.Errorf("cookie should be reused on relogin: orig=%q new=%q", origSid, newSid)
	}

	// /api/notes works again
	res3, _ := cli.Get(ts.URL + "/api/notes")
	if res3.StatusCode != 200 {
		t.Errorf("post-relogin notes failed: %d", res3.StatusCode)
	}
}

// ----- 8. RW endpoints rejected -----

func TestReadWriteEndpointsAreRejected(t *testing.T) {
	ts, cli, _ := startServer(t, 30*time.Minute)
	login(t, ts, cli, testPass)
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		req, _ := http.NewRequest(m, ts.URL+"/api/notes/ai%2Fopenai", nil)
		res, err := cli.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 405 {
			t.Errorf("%s: expected 405, got %d", m, res.StatusCode)
		}
	}
}

// ----- 9. concurrent reads safe under lock race -----

func TestConcurrentReadsAndLockNoPanic(t *testing.T) {
	// 1s idle so manual Lock test fires before that. We use the
	// session's Lock directly to simulate idle-fire under heavy read
	// concurrency.
	ts, cli, srv := startServer(t, 1*time.Second)
	login(t, ts, cli, testPass)

	// pluck the active session
	u, _ := url.Parse(ts.URL)
	cookies := cli.Jar.Cookies(u)
	var sid string
	for _, c := range cookies {
		if c.Name == cookieName {
			sid = c.Value
			break
		}
	}
	if sid == "" {
		t.Fatal("no cookie after login")
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = cli.Get(ts.URL + "/api/notes?q=ai")
				}
			}
		}()
	}
	// let readers run, then yank the rug
	time.Sleep(50 * time.Millisecond)
	if sess := srv.sessions.Lookup(sid); sess != nil {
		sess.Lock()
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
	// If we get here without panic, the RWMutex is doing its job.
}

// ----- 10. status endpoint reports state correctly -----

func TestStatusReportsSessionState(t *testing.T) {
	ts, cli, _ := startServer(t, 30*time.Minute)

	// no session
	res, _ := cli.Get(ts.URL + "/api/status")
	var s1 statusResponse
	json.NewDecoder(res.Body).Decode(&s1)
	if s1.State != "none" {
		t.Errorf("state=%q want none", s1.State)
	}

	login(t, ts, cli, testPass)
	res2, _ := cli.Get(ts.URL + "/api/status")
	var s2 statusResponse
	json.NewDecoder(res2.Body).Decode(&s2)
	if s2.State != "active" {
		t.Errorf("state=%q want active", s2.State)
	}
	if s2.IdleRemainingS == 0 {
		t.Errorf("idle_remaining_s should be > 0 in active state")
	}
}

// ----- 11. URL field redirect endpoint -----

func TestURLRedirectEndpoint(t *testing.T) {
	ts, cli, _ := startServer(t, 30*time.Minute)
	login(t, ts, cli, testPass)
	res, err := cli.Get(ts.URL + "/api/notes/ai%2Fopenai/url")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 302 {
		t.Fatalf("expected 302 redirect, got %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if loc != "https://platform.openai.com" {
		t.Errorf("Location=%q want platform.openai.com", loc)
	}

	// note without a url field → 404
	res2, _ := cli.Get(ts.URL + "/api/notes/server%2Ffw0/url")
	if res2.StatusCode != 404 {
		t.Errorf("expected 404 for note without url, got %d", res2.StatusCode)
	}
}

// ----- 12. logout clears the session -----

func TestLogoutDestroysSession(t *testing.T) {
	ts, cli, _ := startServer(t, 30*time.Minute)
	login(t, ts, cli, testPass)
	req, _ := http.NewRequest("POST", ts.URL+"/api/logout", nil)
	res, _ := cli.Do(req)
	if res.StatusCode != 204 {
		t.Errorf("logout: expected 204, got %d", res.StatusCode)
	}
	res2, _ := cli.Get(ts.URL + "/api/notes")
	if res2.StatusCode != 401 {
		t.Errorf("post-logout notes: expected 401, got %d", res2.StatusCode)
	}
}

// ----- 13. bootstrap mode lets cookieless requests succeed -----

func TestBootstrapAutoMintsSessionForCookielessRequests(t *testing.T) {
	ts, cli, srv := startServer(t, 30*time.Minute)

	// simulate a successful keychain bootstrap by setting bootstrapDV
	// directly. Without this, cookieless requests get 401.
	v := store.New()
	v.Put("a", "body")
	srv.bootstrapDV = v

	res, err := cli.Get(ts.URL + "/api/notes")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200 with bootstrap, got %d", res.StatusCode)
	}
	// cookie should have been set
	u, _ := url.Parse(ts.URL)
	cookies := cli.Jar.Cookies(u)
	gotCookie := false
	for _, c := range cookies {
		if c.Name == cookieName && c.Value != "" {
			gotCookie = true
		}
	}
	if !gotCookie {
		t.Errorf("bootstrap should mint a cookie; got none")
	}
}

func TestBindAddressClassification(t *testing.T) {
	tests := []struct {
		host     string
		loopback bool
		wildcard bool
	}{
		{"", true, false},
		{"localhost", true, false},
		{"127.0.0.1", true, false},
		{"::1", true, false},
		{"10.0.0.1", false, false},
		{"100.64.0.1", false, false},
		{"0.0.0.0", false, true},
		{"::", false, true},
		{"vpn.example", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := isLoopback(tt.host); got != tt.loopback {
				t.Errorf("isLoopback(%q)=%v want %v", tt.host, got, tt.loopback)
			}
			if got := isWildcard(tt.host); got != tt.wildcard {
				t.Errorf("isWildcard(%q)=%v want %v", tt.host, got, tt.wildcard)
			}
		})
	}
}

func TestRunRejectsWildcardBind(t *testing.T) {
	err := Run(Options{
		VaultPath: "unused.kpot",
		BindAddr:  "0.0.0.0",
		NoCache:   true,
	})
	if err == nil {
		t.Fatal("expected wildcard bind to be rejected")
	}
	if !strings.Contains(err.Error(), "refusing wildcard bind") {
		t.Fatalf("unexpected error: %v", err)
	}
	if errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Run should reject before starting server: %v", err)
	}
}
