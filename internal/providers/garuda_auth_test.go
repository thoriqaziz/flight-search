package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"flight-search/internal/models"
	"flight-search/internal/retry"
)

// fakeGarudaAPI serves a token endpoint and a search endpoint that only
// accepts the most recently issued token.
type fakeGarudaAPI struct {
	tokenCalls  atomic.Int32
	searchCalls atomic.Int32
	current     atomic.Value // string
}

func (f *fakeGarudaAPI) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		n := f.tokenCalls.Add(1)
		id, secret, ok := r.BasicAuth()
		if !ok || id != "my-client" || secret != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"invalid_client"}`)
			return
		}
		if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "client_credentials" || r.PostForm.Get("scope") != "flights.search" {
			t.Errorf("bad token request form: %v", r.PostForm)
		}
		tok := "tok-" + string(rune('0'+n))
		f.current.Store(tok)
		io.WriteString(w, `{"access_token":"`+tok+`","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		f.searchCalls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("search: got %s with Content-Type %q, want POST application/json", r.Method, r.Header.Get("Content-Type"))
		}
		var body garudaSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("search: bad JSON body: %v", err)
		}
		if body != (garudaSearchRequest{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15", Passengers: 1}) {
			t.Errorf("search: unexpected body %+v", body)
		}
		if r.Header.Get("Authorization") != "Bearer "+f.current.Load().(string) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		io.WriteString(w, `{"status":"success","flights":[{"flight_id":"GA400","airline":"Garuda Indonesia","airline_code":"GA",
			"departure":{"airport":"CGK","city":"Jakarta","time":"2025-12-15T06:00:00+07:00"},
			"arrival":{"airport":"DPS","city":"Denpasar","time":"2025-12-15T08:50:00+08:00"},
			"stops":0,"price":{"amount":1250000,"currency":"IDR"},"available_seats":28,"fare_class":"economy",
			"baggage":{"carry_on":1,"checked":2}}]}`)
	})
	return mux
}

func newTestGaruda(srv *httptest.Server, secret string) *GarudaProvider {
	creds := ClientCredentials{TokenURL: srv.URL + "/oauth/token", ClientID: "my-client", ClientSecret: secret, Scope: "flights.search"}
	return NewGarudaProvider(srv.URL+"/search", creds, srv.Client(), nil, retry.Config{MaxAttempts: 3, BaseDelay: time.Millisecond})
}

var garudaTestParams = models.SearchParams{Origin: "CGK", Destination: "DPS", Date: "2025-12-15"}

func TestGarudaReusesCachedToken(t *testing.T) {
	api := &fakeGarudaAPI{}
	api.current.Store("")
	srv := httptest.NewServer(api.handler(t))
	defer srv.Close()
	p := newTestGaruda(srv, "s3cret")

	for i := 0; i < 3; i++ {
		flights, err := p.Search(context.Background(), garudaTestParams)
		if err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
		if len(flights) != 1 || flights[0].FlightNumber != "GA400" {
			t.Fatalf("search %d: unexpected flights %+v", i, flights)
		}
	}
	if n := api.tokenCalls.Load(); n != 1 {
		t.Errorf("token endpoint called %d times, want 1", n)
	}
}

func TestGarudaRefreshesTokenOn401(t *testing.T) {
	api := &fakeGarudaAPI{}
	api.current.Store("")
	srv := httptest.NewServer(api.handler(t))
	defer srv.Close()
	p := newTestGaruda(srv, "s3cret")

	if _, err := p.Search(context.Background(), garudaTestParams); err != nil {
		t.Fatal(err)
	}
	api.current.Store("revoked-server-side") // cached token no longer valid

	// The first attempt gets 401, so the client should fetch a new token and retry.
	if _, err := p.Search(context.Background(), garudaTestParams); err != nil {
		t.Fatalf("search after revocation: %v", err)
	}
	if n := api.tokenCalls.Load(); n != 2 {
		t.Errorf("token endpoint called %d times, want 2", n)
	}
}

func TestGarudaBadClientSecretFailsFast(t *testing.T) {
	api := &fakeGarudaAPI{}
	api.current.Store("")
	srv := httptest.NewServer(api.handler(t))
	defer srv.Close()
	p := newTestGaruda(srv, "wrong")

	if _, err := p.Search(context.Background(), garudaTestParams); err == nil {
		t.Fatal("expected error with bad client secret")
	}
	if n := api.tokenCalls.Load(); n != 1 {
		t.Errorf("token endpoint called %d times, want 1 (invalid_client must not be retried)", n)
	}
	if n := api.searchCalls.Load(); n != 0 {
		t.Errorf("search called %d times without a token", n)
	}
}

func TestGarudaWithoutCredentialsSendsNoAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			t.Errorf("unexpected Authorization header %q", h)
		}
		io.WriteString(w, `{"status":"success","flights":[]}`)
	}))
	defer srv.Close()

	p := NewGarudaProvider(srv.URL, ClientCredentials{}, srv.Client(), nil, retry.Config{MaxAttempts: 1})
	if _, err := p.Search(context.Background(), garudaTestParams); err != nil {
		t.Fatal(err)
	}
}
