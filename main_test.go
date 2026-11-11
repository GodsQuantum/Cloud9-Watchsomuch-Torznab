package main

import (
    "encoding/xml"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
)

func TestHealthEvenWhenIndexerIsProtected(t *testing.T) {
    original := secret
    secret = "synthetic-key"
    t.Cleanup(func() { secret = original })
    w := httptest.NewRecorder()
    serve(w, httptest.NewRequest(http.MethodGet, "/health", nil))
    if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "ok" {
        t.Fatalf("health regression: %d", w.Code)
    }
}

func TestCapabilitiesAuthenticateAndReturnStandardXml(t *testing.T) {
    original := secret
    secret = "synthetic-key"
    t.Cleanup(func() { secret = original })
    w := httptest.NewRecorder()
    serve(w, httptest.NewRequest(http.MethodGet, "/api?t=caps&apikey=wrong", nil))
    if w.Code != 401 { t.Fatalf("missing Torznab API key authorization: %d", w.Code) }
    w = httptest.NewRecorder()
    serve(w, httptest.NewRequest(http.MethodGet, "/api?t=caps&apikey=synthetic-key", nil))
    if w.Code != 200 { t.Fatalf("caps not working: %d", w.Code) }
    var root struct { XMLName xml.Name }
    if err := xml.Unmarshal(w.Body.Bytes(), &root); err != nil { t.Fatal(err) }
    if root.XMLName.Local != "caps" { t.Fatalf("not a caps XML document: %s", root.XMLName.Local) }
    for _, token := range []string{"tv-search", "movie-search", "category id=\"2000\"", "category id=\"5000\""} {
        if !strings.Contains(w.Body.String(), token) { t.Fatalf("capability removed: %s", token) }
    }
}

func TestSearchEmptyResultIsValidRssWithoutUpstreamNetwork(t *testing.T) {
    originalSecret := secret
    secret = "synthetic-key"
    t.Cleanup(func() { secret = originalSecret })
    mu.Lock()
    oldMovies, oldFetched, oldFetching := movies, fetched, fetching
    movies = []Movie{{ID: 42, Name: "Synthetic Public Domain", Year: 2026, Source: "HQ-WebRip"}}
    fetched = time.Now()
    fetching = false
    mu.Unlock()
    t.Cleanup(func() { mu.Lock();movies, fetched, fetching = oldMovies, oldFetched, oldFetching;mu.Unlock() })
    w := httptest.NewRecorder()
    serve(w, httptest.NewRequest(http.MethodGet, "/api?t=search&q=nonexisting-synthetic-title&apikey=synthetic-key", nil))
    if w.Code != 200 { t.Fatalf("Torznab empty search failed: %d",w.Code) }
    var root struct { XMLName xml.Name; Channel struct { Title string `xml:"title"` } `xml:"channel"` }
    if err:=xml.Unmarshal(w.Body.Bytes(),&root);err!=nil { t.Fatal(err) }
    if root.XMLName.Local!="rss" || root.Channel.Title!="WatchSoMuch Torznab" { t.Fatalf("invalid RSS wrapper") }
    if strings.Contains(w.Body.String(), "<item>") { t.Fatalf("empty search emitted unrelated release") }
}

func TestUnsupportedCommandDenied(t *testing.T) {
    original := secret
    secret = ""
    t.Cleanup(func() { secret = original })
    w := httptest.NewRecorder()
    serve(w, httptest.NewRequest(http.MethodGet, "/api?t=withdraw", nil))
    if w.Code!=400 { t.Fatalf("unsupported action not rejected: %d",w.Code) }
}
