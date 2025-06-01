/*
This is an example application to demonstrate parsing an ID Token.
*/
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"cookietestbase/internal/openidauth"
)

const (
	DefaultSessionTimeout = 60 * 60 * 3		// 3 hours?
)


func randBytes(nByte int) []byte {
	b := make([]byte, nByte)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	return b
}


func setLocalCookie(ctx *gin.Context, c *http.Cookie) {
	ctx.SetCookie(c.Name,c.Value,c.MaxAge,"/","127.0.0.1",c.Secure,c.HttpOnly)
}
func unsetLocalCookie(ctx *gin.Context, name string) {
	ctx.SetCookie(name,"",-1,"/","127.0.0.1", false, true)
}

func setCallbackCookieCtx(ctx *gin.Context, name, value string) {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   int(time.Minute.Seconds()),
		Secure:   ctx.Request.TLS != nil,
		HttpOnly: true,
	}
	setLocalCookie(ctx,c)
}



type cookie_values interface {
	FromJSON(string) cookie_values
	ToJSON() string
	FromCookie(*gin.Context) (cookie_content, error)
	ToCookie(*gin.Context)
}



type cookie_content struct {
	Sub string			`json:"sub"`
	IssuedAt int		`json:"issued"`
	Authorized bool		`json:"auth"`
	// ExpiresAtMillis int		`json:"exp"`
}
func (cookie_content) NewFromUserID(sub string) cookie_content {
	now := int(time.Now().UnixMilli())
	c := cookie_content{Sub: sub, IssuedAt: now}
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
func (c cookie_content) ToJSON() string {
	new_cookie_json, err := json.Marshal(c)
	if err != nil {
		panic("can't marshall cookie")
	}
	return string(new_cookie_json)
}
func FromCookie(ctx *gin.Context) (cookie_content, error) {
	cookie_json, err := ctx.Cookie("session")
	if err != nil {
	    return cookie_content{}, err
	}
	return FromJSON(cookie_json) , nil
}
func (c cookie_content) ToCookie(ctx *gin.Context) {
	http_cookie := &http.Cookie{
		Name:     "session",
		Value:    c.ToJSON(),
		MaxAge:   DefaultSessionTimeout,
		Secure:   ctx.Request.TLS != nil,
		HttpOnly: true,
	}
	setLocalCookie(ctx,http_cookie)
}




// TODO: next steps:
//  ✔️ commit
//  ✔️ port to gin gonic
//  - secure and cleanup cookies
//  -> middleware module
//  - then move to doctray

func main() {
	clientID, success		:= os.LookupEnv("DOCTRAY_CLIENTID")
	if ! success {
		panic("DOCTRAY_CLIENTID not an environment variable!")
	}
	clientSecret,success	:= os.LookupEnv("DOCTRAY_CLIENTSECRET")
	if ! success {
		panic("DOCTRAY_CLIENTSECRET not an environment variable!")
	}
	issuerUrl    := "https://auth.lukasschumann.de/application/o/test-app/"
	redirectURL  := "http://127.0.0.1:5555/callback"
	auth_state := openidauth.NewAuthState(clientID, clientSecret, issuerUrl, redirectURL)


	r := gin.Default()
	// store := cookie.NewStore([]byte("secret~"), nil)
	// store = cookie.NewStore(securecookie.GenerateRandomKey(32), securecookie.GenerateRandomKey(32))
	store := cookie.NewStore(randBytes(32), randBytes(32))
	r.Use(sessions.Sessions("session", store))



	fmt.Println("hello")
	r.GET("/test", func(ctx *gin.Context) {
		c := &http.Cookie{
			Name:     "TestCookie",
			Value:    "Yes",
			MaxAge:   int(time.Minute.Seconds()*5),
			Secure:   ctx.Request.TLS != nil,
			HttpOnly: true,
		}
		setLocalCookie(ctx, c)
		cookie,err := FromCookie(ctx)
		if err != nil {
			ctx.JSON(http.StatusOK, gin.H{"message":"Testing success", "cookie":err.Error()})
		} else {
			ctx.JSON(http.StatusOK, gin.H{"message":"Testing success", "cookie":cookie.ToJSON()})
		}

	})
	r.GET("/callback", auth_state.Callback_handler())
	r.GET("/logout", auth_state.Logout())

	auth_r := r.Group("/auth")
	auth_r.Use(auth_state.Ensure_loggedin())
	{
		auth_r.GET("/test", func(ctx *gin.Context) {
			fmt.Println("Dann inner function")

			// auth_cookie, err := ctx.Cookie("session")
			// s := sessions.Default(ctx)
			iss := -1
			// TODO:
			// cookie_values := FromJSON(auth_cookie)
			// iss = cookie_values.IssuedAt
			label := sessions.Default(ctx).Get(auth_state.UserIDLabel())
			if label == nil {
				fmt.Println("No Cookie, not authorized!")
				ctx.JSON(http.StatusUnauthorized, gin.H{"message":fmt.Sprint("unauthorized")})
				ctx.Abort()
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"message":fmt.Sprint("authorized: ", label.(string), ", issued: ", iss)})
		})
	}


	log.Printf("listening on http://%s/", "127.0.0.1:5555")
	r.Run(":5555")
	// log.Fatal(http.ListenAndServe("127.0.0.1:5555", nil))
}
