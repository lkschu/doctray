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
	"errors"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"cookietestbase/internal/openidauth"
)

const (
	DefaultSessionTimeout = 60 * 60 * 3		// 3 hours?
	session_label_userid = "sub"
	session_label_sessionid = "sessionid"
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



// Basically a singleton
type cookie_content struct {
	UserID string			`json:"sub"`
	SessionID string		`json:"sid"`
	Authorized bool			`json:"auth"`
	// ExpiresAtMillis int		`json:"exp"`
}
func (c cookie_content) Get(ctx *gin.Context) (cookie_content, error) {
	// now := int(time.Now().UnixMilli())
	session := sessions.Default(ctx)
	uid := session.Get(session_label_userid)
	if uid == nil {
		return c, errors.New("No UserID found!")
	}
	sid := session.Get(session_label_sessionid)
	if sid == nil {
		return c, errors.New("No SessionID found!")
	}
	return cookie_content{UserID: uid.(string), Authorized: true, SessionID: sid.(string)}, nil
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
func (c cookie_content) ToCookie(ctx *gin.Context) {
	// http_cookie := &http.Cookie{
	// 	Name:     "session",
	// 	Value:    c.ToJSON(),
	// 	MaxAge:   DefaultSessionTimeout,
	// 	Secure:   ctx.Request.TLS != nil,
	// 	HttpOnly: true,
	// }
	// setLocalCookie(ctx,http_cookie)
	session := sessions.Default(ctx)
	session.Set(session_label_userid, c.UserID)
	session.Set(session_label_sessionid, c.SessionID)
	err := session.Save()
	if err != nil {
	    panic(err)
	}
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
	auth_handler := openidauth.NewAuthHandler(clientID, clientSecret, int64(time.Minute.Seconds()*2),issuerUrl, redirectURL)


	r := gin.Default()
	// store := cookie.NewStore([]byte("secret~"), nil)
	// store = cookie.NewStore(securecookie.GenerateRandomKey(32), securecookie.GenerateRandomKey(32))
	store := cookie.NewStore(randBytes(32), randBytes(32))
	store.Options(sessions.Options{MaxAge: int(time.Minute.Seconds()) * 15, Path: "/"})
	r.Use(sessions.Sessions("session", store))



	fmt.Println("hello")
	r.GET("/test", func(ctx *gin.Context) {
		cookie,err := cookie_content{}.Get(ctx)
		if err != nil {
			ctx.JSON(http.StatusOK, gin.H{"message":"Testing success", "cookie":err.Error()})
		} else {
			ctx.JSON(http.StatusOK, gin.H{"message":"Testing success", "cookie":cookie.ToJSON()})
		}

	})
	r.GET("/callback", auth_handler.Callback_handler())
	r.GET("/logout", auth_handler.Logout())

	auth_r := r.Group("/auth")
	auth_r.Use(auth_handler.Ensure_loggedin())
	{
		auth_r.GET("/test", func(ctx *gin.Context) {
			iss := -1
			// TODO:
			// cookie_values := FromJSON(auth_cookie)
			// iss = cookie_values.IssuedAt
			label := sessions.Default(ctx).Get(auth_handler.UserIDLabel())
			if label == nil {
				fmt.Println("No Cookie, not authorized!")
				ctx.JSON(http.StatusUnauthorized, gin.H{"message":fmt.Sprint("unauthorized")})
				ctx.Abort()
				return
			}
			sessionid := ""
			if sessionid_req := sessions.Default(ctx).Get(auth_handler.SessionIDLabel()); sessionid_req != nil {
				sessionid = sessionid_req.(string)
			}
			ctx.JSON(http.StatusOK, gin.H{"message":fmt.Sprint("authorized: ", label.(string), ", issued: ", iss, ", sessionID: ",sessionid)})
		})
	}


	log.Printf("listening on http://%s/", "127.0.0.1:5555")
	r.Run(":5555")
	// log.Fatal(http.ListenAndServe("127.0.0.1:5555", nil))
}
