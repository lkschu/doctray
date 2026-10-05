package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func TestPersistentSessionCookiePolicy(t *testing.T) {
	if auth_session_duration != 7*24*time.Hour {
		t.Fatal("local authentication must last one fixed week")
	}
	for _, secure := range []bool{true, false} {
		name := "HTTPS"
		if !secure {
			name = "HTTP development"
		}
		t.Run(name, func(t *testing.T) {
			store := newSessionStore([]byte("01234567890123456789012345678901"), []byte("01234567890123456789012345678901"), secure)
			router := gin.New()
			router.Use(sessions.Sessions("session", store))
			router.GET("/seed", func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("sub", "test-subject")
				if err := session.Save(); err != nil {
					t.Fatal(err)
				}
			})
			router.GET("/check", func(c *gin.Context) {
				if sessions.Default(c).Get("sub") != "test-subject" {
					t.Error("the persistent cookie must round-trip through the configured store")
				}
			})
			before := time.Now()
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/seed", nil))
			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("saving a session must issue one cookie")
			}
			cookie := cookies[0]
			if cookie.Name != "session" || cookie.MaxAge != 604800 || cookie.Secure != secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
				t.Fatal("the seven-day cookie must retain the existing security and host/path restrictions")
			}
			if cookie.Expires.Before(before.Add(auth_session_duration-time.Second)) || cookie.Expires.After(time.Now().Add(auth_session_duration)) {
				t.Error("the cookie must have a persistent expiry one week from saving")
			}
			request := httptest.NewRequest(http.MethodGet, "/check", nil)
			request.AddCookie(cookie)
			check := httptest.NewRecorder()
			router.ServeHTTP(check, request)
			if check.Code != http.StatusOK || check.Header().Get("Set-Cookie") != "" {
				t.Error("ordinary reads must not renew the fixed session lifetime")
			}
		})
	}
}
