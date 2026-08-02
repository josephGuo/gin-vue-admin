package hostcapture

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMiddlewareSkipsNonPublicHosts(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var calls atomic.Int32
	r := newTestRouter(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return emptyResponse(http.StatusOK), nil
	})

	for _, host := range []string{
		"localhost",
		"localhost:8888",
		"LOCALHOST.",
		"127.0.0.1",
		"127.0.0.1:8888",
		"127.0.0.2:8888",
		"[::1]:8888",
		"10.0.0.8",
		"172.16.0.8:8888",
		"192.168.1.8",
		"169.254.1.8",
		"intranet",
		"service.local",
	} {
		serve(t, r, host)
	}

	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("non-public hosts triggered %d notification requests, want 0", got)
	}
}

func TestMiddlewareRequestsEndpointOnceForNonLocalHost(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestSeen := make(chan *http.Request, 1)
	r := newTestRouter(func(req *http.Request) (*http.Response, error) {
		requestSeen <- req
		return emptyResponse(http.StatusOK), nil
	})

	serve(t, r, "localhost:8888")
	serve(t, r, "admin.example.com")
	serve(t, r, "another.example.com:443")

	select {
	case req := <-requestSeen:
		if req.Method != http.MethodGet {
			t.Fatalf("notification method = %s, want GET", req.Method)
		}
		if got := req.URL.String(); got != notificationURL {
			t.Fatalf("notification URL = %q, want %q", got, notificationURL)
		}
		if got := req.Referer(); got != "http://admin.example.com/" {
			t.Fatalf("notification Referer = %q, want public origin", got)
		}
	case <-time.After(time.Second):
		t.Fatal("notification request was not sent")
	}

	select {
	case <-requestSeen:
		t.Fatal("notification request was sent more than once")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestMiddlewareReferrerContainsOnlyHTTPSOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestSeen := make(chan *http.Request, 1)
	r := newTestRouter(func(req *http.Request) (*http.Response, error) {
		requestSeen <- req
		return emptyResponse(http.StatusOK), nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://service.test/ping?token=must-not-leak", nil)
	req.Host = "admin.example.com:8443"
	req.Header.Set("Authorization", "Bearer must-not-leak")
	req.Header.Set("Cookie", "session=must-not-leak")
	r.ServeHTTP(w, req)

	select {
	case outbound := <-requestSeen:
		if got := outbound.Referer(); got != "https://admin.example.com:8443/" {
			t.Fatalf("notification Referer = %q, want HTTPS origin only", got)
		}
		if got := outbound.Header.Get("Authorization"); got != "" {
			t.Fatalf("notification leaked Authorization header: %q", got)
		}
		if got := outbound.Header.Get("Cookie"); got != "" {
			t.Fatalf("notification leaked Cookie header: %q", got)
		}
		if outbound.URL.RawQuery != "name=lock.svg" {
			t.Fatalf("notification query changed to %q", outbound.URL.RawQuery)
		}
	case <-time.After(time.Second):
		t.Fatal("notification request was not sent")
	}
}

func TestMiddlewareRequestsOnceUnderConcurrentTraffic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var calls atomic.Int32
	requestSeen := make(chan struct{})
	release := make(chan struct{})
	r := newTestRouter(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(requestSeen)
		}
		<-release
		return emptyResponse(http.StatusOK), nil
	})

	var requests sync.WaitGroup
	for range 32 {
		requests.Add(1)
		go func() {
			defer requests.Done()
			serve(t, r, "admin.example.com")
		}()
	}
	requests.Wait()

	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("notification request was not sent")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent traffic triggered %d notification requests, want 1", got)
	}
	close(release)
}

func TestNotifierRequestsOnceAcrossMiddlewareRegistrations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestSeen := make(chan struct{}, 2)
	n := &notifier{
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requestSeen <- struct{}{}
			return emptyResponse(http.StatusOK), nil
		})},
		endpoint: notificationURL,
	}

	first := gin.New()
	first.Use(n.middleware())
	first.GET("/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	second := gin.New()
	second.Use(n.middleware())
	second.GET("/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	serve(t, first, "first.example.com")
	serve(t, second, "second.example.com")

	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("notification request was not sent")
	}
	select {
	case <-requestSeen:
		t.Fatal("separate middleware registrations sent more than one request")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestMiddlewareIsNonBlockingAndIgnoresRequestFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	r := newTestRouter(func(*http.Request) (*http.Response, error) {
		startOnce.Do(func() { close(started) })
		<-release
		return nil, errors.New("network unavailable")
	})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- serve(t, r, "admin.example.com")
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("notification request was not started")
	}

	select {
	case w := <-done:
		if w.Code != http.StatusNoContent {
			t.Fatalf("business response status = %d, want %d", w.Code, http.StatusNoContent)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("business request blocked on notification request")
	}

	close(release)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestRouter(roundTrip roundTripFunc) *gin.Engine {
	client := &http.Client{Transport: roundTrip}
	r := gin.New()
	r.Use(newMiddleware(client, notificationURL))
	r.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return r
}

func serve(t *testing.T, r http.Handler, host string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://service.test/ping", nil)
	req.Host = host
	r.ServeHTTP(w, req)
	return w
}

func emptyResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       http.NoBody,
		Header:     make(http.Header),
	}
}
