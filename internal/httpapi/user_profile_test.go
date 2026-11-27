package httpapi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"agentflow/internal/auth"
	"agentflow/internal/domain"
)

func testAvatarPNG(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func signedUserCookie(t *testing.T, client *auth.Client, user domain.User) *http.Cookie {
	t.Helper()
	token, err := client.SignUser(&auth.User{ID: user.ID, LoginName: user.LoginName, ChineseName: user.ChineseName})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: authCookieName, Value: token}
}

func profileRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, method, path, origin string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "http://localhost"+path, bytes.NewReader(body))
	request.Host = "localhost"
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestUserProfileUpdateIsSelfServiceOnly(t *testing.T) {
	t.Setenv("AGENTFLOW_AUTH_REQUIRED", "true")
	client := auth.NewClient(auth.Config{SessionSecret: "test-secret"})
	handler, store := newTestAuthServer(t, client)
	alice := domain.User{LoginName: "alice", ChineseName: "爱丽丝"}
	if err := store.PutUser(alice); err != nil {
		t.Fatal(err)
	}
	alice, _ = store.UserByLoginName("alice")
	cookie := signedUserCookie(t, client, alice)
	endpoint := "/api/users/" + profileID(alice.ID) + "/profile"

	recorder := profileRequest(t, handler, cookie, http.MethodPut, endpoint, "http://localhost", []byte(`{"displayName":"  Alice W  "}`), "application/json")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"displayName":"Alice W"`) {
		t.Fatalf("update display name: %d %s", recorder.Code, recorder.Body.String())
	}
	stored, _ := store.User(alice.ID)
	if stored.DisplayName != "Alice W" {
		t.Fatalf("stored display name = %q", stored.DisplayName)
	}

	// Anonymous, cross-origin and other-user writes are refused.
	if recorder := profileRequest(t, handler, nil, http.MethodPut, endpoint, "http://localhost", []byte(`{"displayName":"x"}`), "application/json"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", recorder.Code)
	}
	if recorder := profileRequest(t, handler, cookie, http.MethodPut, endpoint, "https://attacker.invalid", []byte(`{"displayName":"x"}`), "application/json"); recorder.Code != http.StatusForbidden {
		t.Fatalf("cross origin: %d", recorder.Code)
	}
	if recorder := profileRequest(t, handler, cookie, http.MethodPut, "/api/users/999999/profile", "http://localhost", []byte(`{"displayName":"x"}`), "application/json"); recorder.Code != http.StatusForbidden {
		t.Fatalf("other user: %d", recorder.Code)
	}

	longName := strings.Repeat("名", maxDisplayNameRunes+1)
	for _, body := range []string{
		`{"displayName":"` + longName + `"}`,
		`{"displayName":"line\nbreak"}`,
		`{"unexpected":true}`,
	} {
		if recorder := profileRequest(t, handler, cookie, http.MethodPut, endpoint, "http://localhost", []byte(body), "application/json"); recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid profile %q: %d %s", body, recorder.Code, recorder.Body.String())
		}
	}

	// Clearing the name restores the directory name and is allowed.
	if recorder := profileRequest(t, handler, cookie, http.MethodPut, endpoint, "http://localhost", []byte(`{"displayName":""}`), "application/json"); recorder.Code != http.StatusOK {
		t.Fatalf("clear display name: %d", recorder.Code)
	}
	stored, _ = store.User(alice.ID)
	if stored.DisplayName != "" || stored.ChineseName != "爱丽丝" {
		t.Fatalf("cleared profile = %+v", stored)
	}
}

func TestUserAvatarUploadServesAndRemoves(t *testing.T) {
	t.Setenv("AGENTFLOW_AUTH_REQUIRED", "true")
	client := auth.NewClient(auth.Config{SessionSecret: "test-secret"})
	handler, store := newTestAuthServer(t, client)
	if err := store.PutUser(domain.User{LoginName: "alice", ChineseName: "爱丽丝"}); err != nil {
		t.Fatal(err)
	}
	alice, _ := store.UserByLoginName("alice")
	cookie := signedUserCookie(t, client, alice)
	endpoint := "/api/users/" + profileID(alice.ID) + "/avatar"
	pngBytes := testAvatarPNG(t, 32)

	recorder := profileRequest(t, handler, cookie, http.MethodPut, endpoint, "http://localhost", pngBytes, "image/png")
	if recorder.Code != http.StatusOK {
		t.Fatalf("upload avatar: %d %s", recorder.Code, recorder.Body.String())
	}
	var uploaded struct {
		AvatarVersion    int64 `json:"avatarVersion"`
		AvatarConfigured bool  `json:"avatarConfigured"`
		User             struct {
			AvatarVersion int64  `json:"avatarVersion"`
			AvatarURL     string `json:"avatarUrl"`
		} `json:"user"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.AvatarVersion <= 0 || !uploaded.AvatarConfigured || uploaded.User.AvatarVersion != uploaded.AvatarVersion {
		t.Fatalf("upload response = %+v", uploaded)
	}
	if uploaded.User.AvatarURL != "" {
		t.Fatalf("domain users use the shared photo endpoint, not avatarUrl: %q", uploaded.User.AvatarURL)
	}

	versioned := "/api/auth/photo/100/alice.png?v=" + profileID(uploaded.AvatarVersion)
	recorder = profileRequest(t, handler, nil, http.MethodGet, versioned, "", nil, "")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("photo: %d %s", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	if !bytes.Equal(recorder.Body.Bytes(), pngBytes) {
		t.Fatal("photo body differs from upload")
	}
	if recorder.Header().Get("Cache-Control") != avatarCacheControl {
		t.Fatalf("versioned cache-control = %q", recorder.Header().Get("Cache-Control"))
	}
	etag := recorder.Header().Get("ETag")
	if etag == "" {
		t.Fatal("custom avatar needs an ETag")
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost"+versioned, nil)
	request.Header.Set("If-None-Match", etag)
	notModified := httptest.NewRecorder()
	handler.ServeHTTP(notModified, request)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("ETag revalidation: %d", notModified.Code)
	}

	// Without a version the URL is not cached forever, so a replacement is
	// visible even to callers that build the URL by hand.
	recorder = profileRequest(t, handler, nil, http.MethodGet, "/api/auth/photo/100/alice.png", "", nil, "")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("unversioned photo: %d %q", recorder.Code, recorder.Header().Get("Cache-Control"))
	}

	// Wrong type, tiny image and oversized body are refused.
	for name, test := range map[string]struct {
		body        []byte
		contentType string
		want        int
	}{
		"text":      {[]byte("not an image"), "text/plain", http.StatusUnsupportedMediaType},
		"tiny":      {testAvatarPNG(t, 1), "image/png", http.StatusUnsupportedMediaType},
		"oversized": {bytes.Repeat([]byte("x"), MaxAvatarBytes+1), "image/png", http.StatusRequestEntityTooLarge},
	} {
		if recorder := profileRequest(t, handler, cookie, http.MethodPut, endpoint, "http://localhost", test.body, test.contentType); recorder.Code != test.want {
			t.Fatalf("%s avatar: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}

	if recorder := profileRequest(t, handler, cookie, http.MethodDelete, endpoint, "http://localhost", nil, ""); recorder.Code != http.StatusOK {
		t.Fatalf("delete avatar: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = profileRequest(t, handler, nil, http.MethodGet, "/api/auth/photo/100/alice.png", "", nil, "")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(recorder.Body.String(), ">A</text>") {
		t.Fatalf("fallback avatar: %d %s %s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

func TestAuthCurrentCarriesProfileAndVersionedAvatar(t *testing.T) {
	t.Setenv("AGENTFLOW_AUTH_REQUIRED", "true")
	client := auth.NewClient(auth.Config{SessionSecret: "test-secret"})
	handler, store := newTestAuthServer(t, client)
	if _, err := store.CreateLocalAccount("alice", "password-123", false); err != nil {
		t.Fatal(err)
	}
	alice, _ := store.UserByLoginName("alice")
	if err := store.UpdateDisplayName(alice.ID, "Alice W"); err != nil {
		t.Fatal(err)
	}
	version, err := store.PutUserAvatar(alice.ID, "image/png", testAvatarPNG(t, 32))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/auth/current", nil)
	request.Host = "localhost"
	request.AddCookie(signedUserCookie(t, client, alice))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("current: %d %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		User auth.User `json:"user"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.User.DisplayName != "Alice W" || !body.User.PasswordLogin || !body.User.AvatarConfigured {
		t.Fatalf("profile fields = %+v", body.User)
	}
	want := "/api/auth/photo/100/alice.png?v=" + profileID(version)
	if body.User.AvatarURL != want {
		t.Fatalf("avatarUrl = %q, want %q", body.User.AvatarURL, want)
	}
}

func TestAuthChangePassword(t *testing.T) {
	t.Setenv("AGENTFLOW_AUTH_REQUIRED", "true")
	client := auth.NewClient(auth.Config{SessionSecret: "test-secret"})
	handler, store := newTestAuthServer(t, client)
	if _, err := store.CreateLocalAccount("bob", "old-password", false); err != nil {
		t.Fatal(err)
	}
	bob, _ := store.UserByLoginName("bob")
	cookie := signedUserCookie(t, client, bob)
	change := func(origin, body string) *httptest.ResponseRecorder {
		return profileRequest(t, handler, cookie, http.MethodPut, "/api/auth/password", origin, []byte(body), "application/json")
	}

	if recorder := change("http://localhost", `{"currentPassword":"wrong","newPassword":"new-password"}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("wrong current: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := change("https://attacker.invalid", `{"currentPassword":"old-password","newPassword":"new-password"}`); recorder.Code != http.StatusForbidden {
		t.Fatalf("cross origin: %d", recorder.Code)
	}
	if recorder := change("http://localhost", `{"currentPassword":"old-password","newPassword":"short"}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("short new password: %d", recorder.Code)
	}
	if recorder := change("http://localhost", `{"currentPassword":"old-password","newPassword":"new-password"}`); recorder.Code != http.StatusOK {
		t.Fatalf("change password: %d %s", recorder.Code, recorder.Body.String())
	}
	if store.CheckLocalPassword(bob.ID, "old-password") || !store.CheckLocalPassword(bob.ID, "new-password") {
		t.Fatal("password was not replaced")
	}
	if recorder := change("http://localhost", `{"currentPassword":"new-password","newPassword":"another-password"}`); recorder.Code != http.StatusOK {
		t.Fatalf("second change: %d", recorder.Code)
	}

	if err := store.PutUser(domain.User{LoginName: "sso-user"}); err != nil {
		t.Fatal(err)
	}
	sso, _ := store.UserByLoginName("sso-user")
	ssoCookie := signedUserCookie(t, client, sso)
	if recorder := profileRequest(t, handler, ssoCookie, http.MethodPut, "/api/auth/password", "http://localhost", []byte(`{"currentPassword":"x","newPassword":"new-password"}`), "application/json"); recorder.Code != http.StatusBadRequest {
		t.Fatalf("account without a password: %d %s", recorder.Code, recorder.Body.String())
	}
}

func profileID(v int64) string { return strconv.FormatInt(v, 10) }
