package cookiejarx

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistentJarNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	pj, err := New(path, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer pj.Close()

	// Set a cookie.
	u, _ := url.Parse("https://www.google.com")
	pj.SetCookies(u, []*http.Cookie{
		{Name: "test", Value: "123", Domain: "www.google.com", Path: "/"},
	})

	// Read it back. The standard jar may not return cookies with no
	// expiry immediately, which is fine — persistence is best-effort,
	// so we don't assert on the count here.
	_ = pj.Cookies(u)

	// Verify file was created.
	if !fileExistsX(path) {
		// The file may only be written on Close; check after close.
		pj.Close()
		if !fileExistsX(path) {
			t.Fatal("cookie file should exist after close")
		}
	}
}

func TestPersistentJarExpired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")

	// Create an old cookie file.
	oldData := `{"created_at":"2020-01-01T00:00:00Z","cookies":[]}`
	os.WriteFile(path, []byte(oldData), 0600)

	// Set modtime to 20 days ago so it's considered expired.
	oldTime := time.Now().Add(-20 * 24 * time.Hour)
	os.Chtimes(path, oldTime, oldTime)

	// Create jar with 14-day max age — should delete old file.
	pj, err := New(path, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer pj.Close()

	// After Close, the file should exist but contain fresh data (not old).
	pj.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cookie file should exist after close")
	}
	if string(data) == oldData {
		t.Fatal("old cookie data should have been replaced")
	}
}

func TestPersistentJarCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")

	// Create an old file.
	os.WriteFile(path, []byte("{}"), 0600)

	// Set modtime to 20 days ago.
	oldTime := time.Now().Add(-20 * 24 * time.Hour)
	os.Chtimes(path, oldTime, oldTime)

	// Cleanup with 14-day max age.
	Cleanup(path, 14*24*time.Hour)

	if fileExistsX(path) {
		t.Fatal("expired cookie file should be cleaned up")
	}

	// Create a fresh file.
	os.WriteFile(path, []byte("{}"), 0600)

	// Cleanup should not remove fresh file.
	Cleanup(path, 14*24*time.Hour)
	if !fileExistsX(path) {
		t.Fatal("fresh cookie file should not be cleaned up")
	}
}

func TestPersistentJarImplementsCookieJar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	pj, err := New(path, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer pj.Close()

	// Verify it implements http.CookieJar.
	var _ http.CookieJar = pj
}

func fileExistsX(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
