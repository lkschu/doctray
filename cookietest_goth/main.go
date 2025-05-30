package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"


	// "github.com/joho/godotenv"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/openidConnect"
	// "github.com/markbates/goth/providers/google"


	// "github.com/gorilla/securecookie"
	oidcauth "github.com/TJM/gin-gonic-oidcauth"
)


func ExampleConfigAuthentik() (c *oidcauth.Config) {
	// Scopes:                  []string{oidc.ScopeOpenID, "profile", "email"},
	// LoginClaim:              "email",
	c = oidcauth.DefaultConfig()
	c.ClientID = os.Getenv("DOCTRAY_CLIENTID")
	if c.ClientID == "" {
		panic("Can't read: DOCTRAY_CLIENTID")
	}
	c.ClientSecret = os.Getenv("DOCTRAY_CLIENTSECRET")
	if c.ClientSecret == "" {
		panic("Can't read: DOCTRAY_CLIENTSECRET")
	}
	c.IssuerURL = os.Getenv("DOCTRAY_ISSUERURL")
	if c.IssuerURL == "" {
		panic("Can't read: DOCTRAY_ISSUERURL")
	}
	c.RedirectURL = os.Getenv("DOCTRAY_REDIRECTURL")
	if c.RedirectURL == "" {
		panic("Can't read: DOCTRAY_REDIRECTURL")
	}
	c.LoginClaim = "sub"
	return
}


func cookie_helper_oauth_expired(session sessions.Session) bool {
	now := float64(time.Now().UnixMilli()) / 1000
	fmt.Println("OAuth2: ", session.Get("exp").(float64), " - ", now, " = ", session.Get("exp").(float64)-now)
	return session.Get("exp").(float64) < now
}
func cookie_helper_local_session_expired(session sessions.Session) bool {
	userdata := session.Get("userdata")
	if userdata == nil {
		panic("No userdata")
	}
	cookie := FromJSON(userdata.(string))
	now := float64(time.Now().UnixMilli())
	fmt.Println("Local: ", float64(cookie.ExpiresAtMillis), " - ", now, " = ", float64(cookie.ExpiresAtMillis)-now)
	return float64(cookie.ExpiresAtMillis) < now
}


func Authentication() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userdata := session.Get("userdata")
		if userdata == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"message": fmt.Sprintf("unauthorized"),
			})
			c.Abort()
			return
		}
		cookie := FromJSON(userdata.(string))
		if cookie_helper_local_session_expired(session) || cookie_helper_oauth_expired(session) {
			exp_lab_local := ""
			exp_lab_oauth := ""
			if cookie_helper_local_session_expired(session){
				exp_lab_local = "local"
			}
			if cookie_helper_oauth_expired(session){
				exp_lab_oauth = "oauth"
			}
			c.JSON(http.StatusNotFound, gin.H{
				"message": fmt.Sprintf("expired: (%s,%s)",exp_lab_local, exp_lab_oauth),
			})
			fmt.Println("cookie_exp: ", float64(cookie.ExpiresAtMillis)/1000, "\nsession_exp: ", session.Get("exp").(float64))
			c.Abort()
			return
		}
	}
}

func get_uuid(c *gin.Context) string{
	session := sessions.Default(c)
	u := session.Get("sub")
	var sub string
	if u != nil {
		sub = u.(string)
	} else {
		sub = "public"
	}
	return sub

}

type cookie_content struct {
	Sub string		`json:"sub"`
	IssuedAt int		`json:"issued"`
	ExpiresAtMillis int		`json:"exp"`
}
func (cookie_content) New(sub string) cookie_content {
	now := int(time.Now().UnixMilli())
	c := cookie_content{Sub: sub, IssuedAt: now, ExpiresAtMillis: now+(3*60*1000)}
	return c
}
func FromJSON(cookie string) cookie_content {
	var c cookie_content
	err := json.Unmarshal([]byte(cookie),&c)
	if err != nil {
	    panic(err)
	}
	return c
}


func Login(c *gin.Context) {
	sub := get_uuid(c)
	// session := sessions.DefaultMany(c, "userconfig")
	// session := sessions.Default(c)
	session := sessions.Default(c)
	cookiej, err :=json.Marshal(cookie_content{}.New(sub))
	if err != nil {
	    panic(err)
	}
	fmt.Println(string(cookiej))
	session.Set("userdata", string(cookiej))
	session.Set("email", "test@gmail.com")
	session.Options(sessions.Options{MaxAge: 60 * 60 * 24 * 7})
	err = session.Save()
	if err != nil {
	    panic(err)
	}
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("User (%s) Sign In successfully", sub),
	})
}
func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	c.JSON(http.StatusOK, gin.H{
		"message": "User Sign out successfully",
	})
}

var store cookie.Store



func main() {

	// func ExampleConfigAuthentik() (c *oidcauth.Config) {
	// 	// Scopes:                  []string{oidc.ScopeOpenID, "profile", "email"},
	// 	// LoginClaim:              "email",
	// 	c = oidcauth.DefaultConfig()
	// 	c.ClientID = os.Getenv("DOCTRAY_CLIENTID")
	// 	if c.ClientID == "" {
	// 		panic("Can't read: DOCTRAY_CLIENTID")
	// 	}
	// 	c.ClientSecret = os.Getenv("DOCTRAY_CLIENTSECRET")
	// 	if c.ClientSecret == "" {
	// 		panic("Can't read: DOCTRAY_CLIENTSECRET")
	// 	}
	// 	c.IssuerURL = os.Getenv("DOCTRAY_ISSUERURL")
	// 	if c.IssuerURL == "" {
	// 		panic("Can't read: DOCTRAY_ISSUERURL")
	// 	}
	// 	c.RedirectURL = os.Getenv("DOCTRAY_REDIRECTURL")
	// 	if c.RedirectURL == "" {
	// 		panic("Can't read: DOCTRAY_REDIRECTURL")
	// 	}
	// 	c.LoginClaim = "sub"
	// 	return
	// }
	openidconcect, _ := openidConnect.New(os.Getenv("DOCTRAY_CLIENTID"), os.Getenv("DOCTRAY_CLIENTSECRET"), "http://localhost:5555/callback", "https://auth.lukasschumann.de/application/o/test-app/.well-known/openid-configuration")
	if openidconcect == nil {
		panic("openidconnect == nil")
	}
	goth.UseProviders(openidconcect)
	m := map[string]string {
		"openid-connect":  "OpenID Connect",
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}



	r := gin.Default()
	store := cookie.NewStore([]byte("secret~"), nil)
	// store = cookie.NewStore(securecookie.GenerateRandomKey(32), securecookie.GenerateRandomKey(32))
	r.Use(sessions.Sessions("cookiestore", store))



	r.GET("/test", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"message":"Testing success"})
	})
	r.GET("/login", Login)
	r.GET("/callback", auth.AuthCallback)
	r.GET("/logout", Logout)

	private := r.Group("/p" ,auth.AuthRequired())
	{
		private.GET("/login", Login)
	}

	auth_r := r.Group("/auth")
	auth_r.Use(Authentication())
	{
		auth_r.GET("/test", func(ctx *gin.Context) {
			s := sessions.Default(ctx)
			label := "nil"
			iss := -1
			if s.Get("userdata") != nil {
				label = s.Get("userdata").(string)
				iss = FromJSON(label).IssuedAt
				label = FromJSON(label).Sub
			}
			ctx.JSON(http.StatusOK, gin.H{"message":fmt.Sprint("authorized: ", label, ", issued: ", iss)})
		})
	}




	r.Run(":5555")
}







