// internal/catalog/providercache_test.go
package catalog_test

import "testing"

func TestGetCachedResponseMiss(t *testing.T) {
	cat := openTestCatalog(t)

	_, ok, err := cat.GetCachedResponse("musicbrainz", "some-key")
	if err != nil {
		t.Fatalf("GetCachedResponse: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for an uncached key")
	}
}

func TestSetThenGetCachedResponse(t *testing.T) {
	cat := openTestCatalog(t)

	if err := cat.SetCachedResponse("musicbrainz", "some-key", `{"recordings":[]}`, 100); err != nil {
		t.Fatalf("SetCachedResponse: %v", err)
	}

	body, ok, err := cat.GetCachedResponse("musicbrainz", "some-key")
	if err != nil {
		t.Fatalf("GetCachedResponse: %v", err)
	}
	if !ok || body != `{"recordings":[]}` {
		t.Fatalf("unexpected cached response: %q (ok=%v)", body, ok)
	}
}

func TestSetCachedResponseOverwritesExisting(t *testing.T) {
	cat := openTestCatalog(t)

	if err := cat.SetCachedResponse("musicbrainz", "some-key", `{"first":true}`, 100); err != nil {
		t.Fatalf("first SetCachedResponse: %v", err)
	}
	if err := cat.SetCachedResponse("musicbrainz", "some-key", `{"second":true}`, 200); err != nil {
		t.Fatalf("second SetCachedResponse: %v", err)
	}

	body, ok, err := cat.GetCachedResponse("musicbrainz", "some-key")
	if err != nil {
		t.Fatalf("GetCachedResponse: %v", err)
	}
	if !ok || body != `{"second":true}` {
		t.Fatalf("expected overwritten response, got %q (ok=%v)", body, ok)
	}
}

func TestCacheKeysAreScopedPerProvider(t *testing.T) {
	cat := openTestCatalog(t)

	if err := cat.SetCachedResponse("musicbrainz", "shared-key", `{"provider":"mb"}`, 100); err != nil {
		t.Fatalf("SetCachedResponse musicbrainz: %v", err)
	}

	_, ok, err := cat.GetCachedResponse("discogs", "shared-key")
	if err != nil {
		t.Fatalf("GetCachedResponse discogs: %v", err)
	}
	if ok {
		t.Fatal("expected a cache entry for one provider not to be visible under another provider's name")
	}
}
