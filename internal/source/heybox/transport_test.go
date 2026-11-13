package heybox

// transport_test.go — the transport against a fake heybox that does
// what the real one does: verify the signature server-style (recompute
// hkey from the request's own _time/nonce over the canonical path) and
// serve the verdict documents the smoke captured. The fixtures are
// the smoke's own sample shapes (ad/comma/429/版本 below), so the
// matrix here is the measured one.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// captured is one request the fake heybox saw.
type captured struct {
	path   string
	params url.Values
	header http.Header
}

// fakeHeybox verifies every request's signature server-style and
// answers one fixed document per leg. A bad signature fails the leg
// with the real host's own 非法请求 refusal.
type fakeHeybox struct {
	shape clientShape
	serve func(w http.ResponseWriter, c captured)
	got   []captured
}

func (f *fakeHeybox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := captured{path: r.URL.Path, params: r.URL.Query(), header: r.Header.Clone()}
	f.got = append(f.got, c)
	hkey := c.params.Get("hkey")
	ts, _ := strconv.ParseInt(c.params.Get("_time"), 10, 64)
	nonce := c.params.Get("nonce")
	if hkey == "" || nonce == "" || ts == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"msg":"缺少必要参数[_time]","result":{},"status":"failed"}`))
		return
	}
	if want := f.shape.sign(c.path, ts, nonce); hkey != want {
		// The crossed-pair verdict the smoke measured (P3/P4): a
		// signature that does not match the version's variant.
		_, _ = w.Write([]byte(`{"msg":"非法请求","result":{},"status":"failed"}`))
		return
	}
	f.serve(w, c)
}

// transportAt wires a transport to a fresh fake with the default v3
// shape and no politeness delay.
func transportAt(t *testing.T, serve func(w http.ResponseWriter, c captured)) (*Transport, *fakeHeybox) {
	t.Helper()
	fake := &fakeHeybox{shape: shapeWebV3, serve: serve}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return NewTransport(srv.URL, "0123456789abcdef0123456789abcdef", WithMinGap(0)), fake
}

func TestTransportRequestShape(t *testing.T) {
	tr, fake := transportAt(t, func(w http.ResponseWriter, _ captured) {
		_, _ = w.Write([]byte(`{"msg":"","result":{"links":[]},"status":"ok"}`))
	})
	res := tr.Get(t.Context(), "leg", "/bbs/app/feeds", url.Values{"offset": {"0"}, "pull": {"0"}, "dw": {"800"}})
	if res.Err != nil {
		t.Fatalf("Get: %v", res.Err)
	}
	if len(fake.got) != 1 {
		t.Fatalf("fake saw %d requests, want 1", len(fake.got))
	}
	c := fake.got[0]
	// The endpoint's own params ride through.
	for k, want := range map[string]string{"offset": "0", "pull": "0", "dw": "800"} {
		if got := c.params.Get(k); got != want {
			t.Errorf("param %s = %q, want %q", k, got, want)
		}
	}
	// The common web set — the smoke's 精简集 + heybox_id + device_id.
	for k, want := range map[string]string{
		"os_type":       "web",
		"version":       "999.0.3",
		"x_app":         "heybox_website",
		"x_client_type": "web",
		"x_os_type":     "Windows",
		"heybox_id":     "-1",
		"device_id":     "0123456789abcdef0123456789abcdef",
	} {
		if got := c.params.Get(k); got != want {
			t.Errorf("param %s = %q, want %q", k, got, want)
		}
	}
	// The signature triple is present and server-verified (the fake
	// recomputes hkey from these — reaching here means it matched).
	for _, k := range []string{"hkey", "_time", "nonce"} {
		if c.params.Get(k) == "" {
			t.Errorf("signature param %s missing", k)
		}
	}
	// The measured headers: desktop Chrome UA, the site Referer, JSON
	// Accept.
	if got := c.header.Get("User-Agent"); got != DesktopUA {
		t.Errorf("User-Agent = %q, want the desktop Chrome spelling", got)
	}
	if got := c.header.Get("Referer"); got != WebReferer {
		t.Errorf("Referer = %q, want %q", got, WebReferer)
	}
	if got := c.header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json", got)
	}
}

// TestTransportSignatureSignsCanonicalPathOnly: extra query params
// must not change the signature — the protocol signs the path alone,
// and the fake verifies with the request's own params aboard.
func TestTransportSignatureSignsCanonicalPathOnly(t *testing.T) {
	tr, _ := transportAt(t, func(w http.ResponseWriter, _ captured) {
		_, _ = w.Write([]byte(`{"msg":"","result":{},"status":"ok"}`))
	})
	if res := tr.Get(t.Context(), "leg", "/bbs/app/feeds", url.Values{"offset": {"7"}}); res.Err != nil {
		t.Fatalf("Get with extra params: %v", res.Err)
	}
}

// TestTransportWrongVariantRefused: a signature computed by the other
// variant fails server-side verification — pinning the version↔signer
// binding from the client side of the wire.
func TestTransportWrongVariantRefused(t *testing.T) {
	fake := &fakeHeybox{shape: shapeWebV4, serve: func(w http.ResponseWriter, _ captured) {
		t.Error("the fake must refuse before serving")
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	tr := NewTransport(srv.URL, "0123456789abcdef0123456789abcdef", WithMinGap(0)) // signs A, server verifies B
	res := tr.Get(t.Context(), "leg", "/bbs/app/feeds", nil)
	if res.Err == nil {
		t.Fatal("a crossed signature pair must fail")
	}
	if errors.Is(res.Err, ErrTryLater) {
		t.Error("非法请求 is a broken request, not the risk family — wrong error class")
	}
}

func TestTransportCaptchaIsTryLater(t *testing.T) {
	tr, _ := transportAt(t, func(w http.ResponseWriter, _ captured) {
		_, _ = w.Write([]byte(`{"msg":"","result":{},"status":"show_captcha"}`))
	})
	res := tr.Get(t.Context(), "leg", "/bbs/app/feeds", nil)
	if !errors.Is(res.Err, ErrTryLater) {
		t.Fatalf("captcha verdict: err = %v, want ErrTryLater", res.Err)
	}
}

func TestTransport429IsTryLater(t *testing.T) {
	tr, _ := transportAt(t, func(w http.ResponseWriter, _ captured) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"msg":"too fast","result":{},"status":"failed"}`))
	})
	res := tr.Get(t.Context(), "leg", "/bbs/app/feeds", nil)
	if !errors.Is(res.Err, ErrTryLater) {
		t.Fatalf("HTTP 429: err = %v, want ErrTryLater", res.Err)
	}
}

func TestTransportVersionGateIsTryLater(t *testing.T) {
	tr, _ := transportAt(t, func(w http.ResponseWriter, _ captured) {
		_, _ = w.Write([]byte(`{"msg":"版本过低，请升级客户端","result":{},"status":"failed"}`))
	})
	res := tr.Get(t.Context(), "leg", "/bbs/app/feeds", nil)
	if !errors.Is(res.Err, ErrTryLater) {
		t.Fatalf("version gate: err = %v, want ErrTryLater", res.Err)
	}
}

func TestTransportPlainRefusalIsNotTryLater(t *testing.T) {
	tr, _ := transportAt(t, func(w http.ResponseWriter, _ captured) {
		_, _ = w.Write([]byte(`{"msg":"非法请求","result":{},"status":"failed"}`))
	})
	res := tr.Get(t.Context(), "leg", "/bbs/app/feeds", nil)
	if res.Err == nil {
		t.Fatal("a failed envelope must be an error")
	}
	if errors.Is(res.Err, ErrTryLater) {
		t.Error("非法请求 is not the risk family — no try-later sugar-coating")
	}
}

func TestTransportNonJSONAndHTTPStatus(t *testing.T) {
	tr, _ := transportAt(t, func(w http.ResponseWriter, _ captured) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	})
	if res := tr.Get(t.Context(), "leg", "/bbs/app/feeds", nil); res.Err == nil {
		t.Error("a non-JSON 200 must be an error")
	}
	tr2, _ := transportAt(t, func(w http.ResponseWriter, _ captured) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if res := tr2.Get(t.Context(), "leg", "/bbs/app/feeds", nil); res.Err == nil || res.Status != 500 {
		t.Errorf("HTTP 500: err=%v status=%d, want a plain error carrying 500", res.Err, res.Status)
	}
}

// TestTransportPolitenessGap: the first request rides immediately, the
// second waits out the gap — the observable no-429 delay the smoke
// ran with, context-cancellable.
func TestTransportPolitenessGap(t *testing.T) {
	const gap = 60 * time.Millisecond
	fake := &fakeHeybox{shape: shapeWebV3, serve: func(w http.ResponseWriter, _ captured) {
		_, _ = w.Write([]byte(`{"msg":"","result":{},"status":"ok"}`))
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	tr := NewTransport(srv.URL, "0123456789abcdef0123456789abcdef", WithMinGap(gap))

	start := time.Now()
	if res := tr.Get(t.Context(), "one", "/bbs/app/feeds", nil); res.Err != nil {
		t.Fatalf("first Get: %v", res.Err)
	}
	if d := time.Since(start); d >= gap {
		t.Errorf("first request waited %v — the gap governs the second onwards", d)
	}
	if res := tr.Get(t.Context(), "two", "/bbs/app/feeds", nil); res.Err != nil {
		t.Fatalf("second Get: %v", res.Err)
	}
	if d := time.Since(start); d < gap {
		t.Errorf("second request waited only %v, want >= %v", d, gap)
	}
}

// TestTransportGapSpansTransports: the politeness ledger outlives its
// transport — the runtime builds a fresh source (hence a fresh
// transport) per F5/Ctrl+N, so a per-transport clock would never
// fire. Two transports against one base pace like one.
func TestTransportGapSpansTransports(t *testing.T) {
	const gap = 60 * time.Millisecond
	fake := &fakeHeybox{shape: shapeWebV3, serve: func(w http.ResponseWriter, _ captured) {
		_, _ = w.Write([]byte(`{"msg":"","result":{},"status":"ok"}`))
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	start := time.Now()
	if res := NewTransport(srv.URL, "0123456789abcdef0123456789abcdef", WithMinGap(gap)).
		Get(t.Context(), "f5", "/bbs/app/feeds", nil); res.Err != nil {
		t.Fatalf("first transport's Get: %v", res.Err)
	}
	if res := NewTransport(srv.URL, "0123456789abcdef0123456789abcdef", WithMinGap(gap)).
		Get(t.Context(), "ctrl-n", "/bbs/app/feeds", nil); res.Err != nil {
		t.Fatalf("second transport's Get: %v", res.Err)
	}
	if d := time.Since(start); d < gap {
		t.Errorf("two transports dialed %v apart, want >= %v — the ledger must span them", d, gap)
	}
}

// TestTransportGapCancelledByContext: a cancelled context breaks the
// politeness wait instead of blocking the caller for the full gap.
func TestTransportGapCancelledByContext(t *testing.T) {
	const gap = 5 * time.Second
	fake := &fakeHeybox{shape: shapeWebV3, serve: func(w http.ResponseWriter, _ captured) {
		_, _ = w.Write([]byte(`{"msg":"","result":{},"status":"ok"}`))
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	tr := NewTransport(srv.URL, "0123456789abcdef0123456789abcdef", WithMinGap(gap))
	if res := tr.Get(t.Context(), "one", "/bbs/app/feeds", nil); res.Err != nil {
		t.Fatalf("first Get: %v", res.Err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if res := tr.Get(ctx, "two", "/bbs/app/feeds", nil); res.Err == nil {
		t.Fatal("the cancelled wait must surface the context error")
	}
	if d := time.Since(start); d >= time.Second {
		t.Errorf("the wait blocked %v despite cancellation", d)
	}
}

// TestTransportV4ShapeSwitch: the stop-loss seam — WithShape(v4) sends
// version 999.0.4 and a signature the v4-verifying server accepts.
func TestTransportV4ShapeSwitch(t *testing.T) {
	fake := &fakeHeybox{shape: shapeWebV4, serve: func(w http.ResponseWriter, c captured) {
		if got := c.params.Get("version"); got != "999.0.4" {
			t.Errorf("version = %q, want the v4 spelling under the switch", got)
		}
		_, _ = w.Write([]byte(`{"msg":"","result":{},"status":"ok"}`))
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	tr := NewTransport(srv.URL, "0123456789abcdef0123456789abcdef", WithMinGap(0), WithShape(shapeWebV4))
	if res := tr.Get(t.Context(), "leg", "/bbs/app/feeds", nil); res.Err != nil {
		t.Fatalf("Get under the v4 shape: %v", res.Err)
	}
}
