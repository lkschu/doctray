package openidauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
