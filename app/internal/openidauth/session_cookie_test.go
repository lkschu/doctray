package openidauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestLogoutSessionCookieSecurity(t *testing.T) {
	tests := []struct {
		name      string
		configure bool
		secure    bool
	}{
		{name: "secure by default", secure: true},
		{name: "explicitly secure", configure: true, secure: true},
		{name: "HTTP development", configure: true, secure: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := cookie.NewStore([]byte("01234567890123456789012345678901"))
			store.Options(sessions.Options{
				Path:     "/",
				MaxAge:   7 * 24 * 60 * 60,
				HttpOnly: true,
				Secure:   test.secure,
				SameSite: http.SameSiteLaxMode,
			})
			handler := AuthHandler{}
			if test.configure {
				handler.SetSessionCookieSecure(test.secure)
			}
			router := gin.New()
			router.Use(sessions.Sessions("session", store))
			router.GET("/logout", handler.Logout())

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://example.test/logout", nil))
			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("logout returned status %d", response.StatusCode)
			}
			cookies := response.Cookies()
			if len(cookies) != 1 || cookies[0].Name != "session" {
				t.Fatal("logout must clear the session cookie")
			}
			got := cookies[0]
			if got.Secure != test.secure {
				t.Errorf("logout cookie Secure = %t, want %t", got.Secure, test.secure)
			}
			if !got.HttpOnly || got.SameSite != http.SameSiteLaxMode || got.Path != "/" {
				t.Error("loosening Secure must not change the remaining cookie protections")
			}
			if got.MaxAge >= 0 {
				t.Error("logout must expire the session cookie")
			}
		})
	}
}

func TestAuthenticatedSessionFixedLifetime(t *testing.T) {
	const lifetime = 7 * 24 * time.Hour
	store := cookie.NewStore([]byte("01234567890123456789012345678901"))
	store.Options(sessions.Options{Path: "/", MaxAge: int(lifetime.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	handler := AuthHandler{expirationTimer: int64(lifetime.Seconds()), session_label_userid: "sub", session_label_expired: "expiration"}
	router := gin.New()
	router.Use(sessions.Sessions("session", store))
	router.GET("/verified-login", func(c *gin.Context) {
		if err := handler.saveAuthenticatedSession(c, "test-subject"); err != nil {
			t.Fatal(err)
		}
	})
	var savedExpiry int64
	router.GET("/check", func(c *gin.Context) {
		uid, err := handler.GetUserID(c)
		if err != nil || uid != "test-subject" || !handler.IsLoggedIn(c) {
			t.Error("a completed login must establish a usable local session")
		}
		savedExpiry, _ = sessions.Default(c).Get("expiration").(int64)
	})
	before := time.Now().Unix()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/verified-login", nil))
	after := time.Now().Unix()
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != int(lifetime.Seconds()) {
		t.Fatal("completed login must save a persistent session cookie")
	}
	request := httptest.NewRequest(http.MethodGet, "/check", nil)
	request.AddCookie(cookies[0])
	check := httptest.NewRecorder()
	router.ServeHTTP(check, request)
	if savedExpiry < before+int64(lifetime.Seconds()) || savedExpiry > after+int64(lifetime.Seconds()) {
		t.Error("the local deadline must be one fixed week from completed login")
	}
	if check.Header().Get("Set-Cookie") != "" {
		t.Error("authentication checks must not slide or refresh the cookie lifetime")
	}
}

func TestGetUserIDSessionValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		uid    any
		expiry any
		want   error
	}{
		{name: "still valid after six days", uid: "test-subject", expiry: time.Now().Add(24 * time.Hour).Unix()},
		{name: "expired", uid: "test-subject", expiry: time.Now().Add(-time.Minute).Unix(), want: ErrSessionExpired},
		{name: "expiry boundary", uid: "test-subject", expiry: time.Now().Unix(), want: ErrSessionExpired},
		{name: "missing identity", expiry: time.Now().Add(time.Hour).Unix(), want: ErrInvalidSession},
		{name: "empty identity", uid: "", expiry: time.Now().Add(time.Hour).Unix(), want: ErrInvalidSession},
		{name: "wrong identity type", uid: 123, expiry: time.Now().Add(time.Hour).Unix(), want: ErrInvalidSession},
		{name: "missing expiry", uid: "test-subject", want: ErrInvalidSession},
		{name: "wrong expiry type", uid: "test-subject", expiry: "private invalid expiry", want: ErrInvalidSession},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := cookie.NewStore([]byte("01234567890123456789012345678901"))
			handler := AuthHandler{session_label_userid: "sub", session_label_expired: "expiration"}
			router := gin.New()
			router.Use(sessions.Sessions("session", store))
			router.GET("/seed", func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("auth_login_binding", "test-binding")
				if test.uid != nil {
					session.Set("sub", test.uid)
				}
				if test.expiry != nil {
					session.Set("expiration", test.expiry)
				}
				if err := session.Save(); err != nil {
					t.Fatal(err)
				}
			})
			router.GET("/check", func(c *gin.Context) {
				uid, err := handler.GetUserID(c)
				if !errors.Is(err, test.want) || handler.IsLoggedIn(c) != (test.want == nil) {
					t.Errorf("authentication error = %v, want %v", err, test.want)
				}
				if (test.want == nil && uid != "test-subject") || (test.want != nil && uid != "") {
					t.Error("only a valid, unexpired session may return a user identity")
				}
				if sessions.Default(c).Get("expiration") != test.expiry {
					t.Error("validation must leave the original local deadline unchanged")
				}
			})
			seed := httptest.NewRecorder()
			router.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/seed", nil))
			cookies := seed.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("session fixture must produce one cookie")
			}
			request := httptest.NewRequest(http.MethodGet, "/check", nil)
			request.AddCookie(cookies[0])
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Header().Get("Set-Cookie") != "" {
				t.Error("neither valid nor rejected checks may renew the session")
			}
		})
	}
}
