/*
This is an example application to demonstrate parsing an ID Token.
*/
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"

	// "github.com/gin-contrib/sessions"
	// "github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

)

const (
DefaultAuthenticatedURL = "/tralala"
)


func randString(nByte int) (string, error) {
	b := make([]byte, nByte)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
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


type AuthState struct {
	context		*context.Context
	provider	*oidc.Provider
	verifier	*oidc.IDTokenVerifier
	oauth2Conf  *oauth2.Config
}
func NewAuthState(ctx context.Context, clientID string, clientSecret string, issuerUrl string, redirectURL string) AuthState{
	provider, err := oidc.NewProvider(ctx, issuerUrl)
	if err != nil {
		log.Fatal(err)
	}
	oidcConfig := &oidc.Config{
		ClientID: clientID,
	}
	verifier := provider.Verifier(oidcConfig)
	config := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURL,
		// Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		Scopes:       []string{oidc.ScopeOpenID, "sub", "email"},
	}
	return AuthState{provider: provider, verifier: verifier, oauth2Conf: &config, context: &ctx}
}
func (state *AuthState) login(ctx *gin.Context){
	previousURL := ctx.Request.RequestURI // Current URL
	if previousURL == "" {
		previousURL = DefaultAuthenticatedURL
	}
	setCallbackCookieCtx(ctx, "redir", base64.RawURLEncoding.EncodeToString([]byte(previousURL)))


	w := ctx.Writer
	fmt.Println("Set nonce/state cookie")
	lstate, err := randString(16)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	nonce, err := randString(16)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	setCallbackCookieCtx(ctx, "state", lstate)
	setCallbackCookieCtx(ctx, "nonce", nonce)

	// http.Redirect(w, r, state.oauth2Conf.AuthCodeURL(lstate, oidc.Nonce(nonce)), http.StatusFound)
	ctx.Redirect(http.StatusFound, state.oauth2Conf.AuthCodeURL(lstate, oidc.Nonce(nonce)))
}
func (state *AuthState) logout() gin.HandlerFunc{
	// r := ctx.Request
	// _, err := ctx.Cookie("session")
	// if err != nil {
	// 	c := &http.Cookie{
	// 		Name:     "session",
	// 		Value:    "",
	// 		MaxAge:   -1,
	// 		Secure:   r.TLS != nil,
	// 		HttpOnly: true,
	// 	}
	// 	setLocalCookie(ctx, c)
	// }
	return func (ctx *gin.Context) {
		unsetLocalCookie(ctx,"session")
	}
}
func (state *AuthState) ensure_loggedin() gin.HandlerFunc{
	return func(ctx *gin.Context) {
		fmt.Println("Erst ensure_loggedin")
		w := ctx.Writer
		auth_cookie, err := ctx.Cookie("session")
		// if err != nil || auth_cookie.Value != "Y" {
		if err != nil {
			fmt.Println("Unauthorized")
			state.login(ctx)
			return
		} else {
			cookie := FromJSON(auth_cookie)
			if cookie.Authorized == false {
				fmt.Println("Unauthorized")
				state.login(ctx)
				return
			} else {
				fmt.Println("Authorized!")
				w.Write([]byte("Authorized!"))
			}

		}
	}

}
func (state *AuthState) callback_handler() func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		// w http.ResponseWriter, r *http.Request
		r := ctx.Request
		w := ctx.Writer
		state_cookie, err := r.Cookie("state")
		if err != nil {
			http.Error(w, "state not found", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != state_cookie.Value {
			http.Error(w, "state did not match", http.StatusBadRequest)
			return
		}

		oauth2Token, err := state.oauth2Conf.Exchange(*state.context, r.URL.Query().Get("code"))
		if err != nil {
			http.Error(w, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "No id_token field in oauth2 token.", http.StatusInternalServerError)
			return
		}
		idToken, err := state.verifier.Verify(*state.context, rawIDToken)
		if err != nil {
			http.Error(w, "Failed to verify ID Token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		nonce_cookie, err := r.Cookie("nonce")
		if err != nil {
			http.Error(w, "nonce not found", http.StatusBadRequest)
			return
		}
		if idToken.Nonce != nonce_cookie.Value {
			http.Error(w, "nonce did not match", http.StatusBadRequest)
			return
		}

		redir_cookie, err := r.Cookie("redir")
		redirection_url := ""
		if err != nil {
			redirection_url = DefaultAuthenticatedURL
		} else {
			decoded, err :=  base64.RawURLEncoding.DecodeString(redir_cookie.Value)
			redirection_url = string(decoded)
			if err != nil {
				panic(err)
			}
		}
		fmt.Println("Redirect to: ", redirection_url)
		// TODO: move these 3 cookies to one
		unsetLocalCookie(ctx, "redir")
		unsetLocalCookie(ctx, "nonce")
		unsetLocalCookie(ctx, "state")

		// oauth2Token.AccessToken = "*REDACTED*"

		resp := struct {
			OAuth2Token   *oauth2.Token
			IDTokenClaims *json.RawMessage // ID Token payload is just JSON.
		}{oauth2Token, new(json.RawMessage)}

		if err := idToken.Claims(&resp.IDTokenClaims); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data, err := json.MarshalIndent(resp, "", "    ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		new_cookie := cookie_content{}.New("Sub = TODO")
		new_cookie.Authorized = true
		new_cookie.ToCookie(ctx)


		// w.Write(append(data, []byte("\nYada yada yada")...))
		fmt.Println(string(data))
		ctx.Redirect(http.StatusFound, redirection_url)
	}
}


type cookie_content struct {
	Sub string			`json:"sub"`
	IssuedAt int		`json:"issued"`
	Authorized bool		`json:"auth"`
	// ExpiresAtMillis int		`json:"exp"`
}
func (cookie_content) New(sub string) cookie_content {
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
		MaxAge:   int(time.Minute.Seconds()*2),
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
	ctx := context.Background()
	clientID     := os.Getenv("DOCTRAY_CLIENTID")
	clientSecret := os.Getenv("DOCTRAY_CLIENTSECRET")
	issuerUrl    := "https://auth.lukasschumann.de/application/o/test-app/"
	redirectURL  := "http://127.0.0.1:5555/callback"
	auth_state := NewAuthState(ctx, clientID, clientSecret, issuerUrl, redirectURL)


	r := gin.Default()
	// store := cookie.NewStore([]byte("secret~"), nil)
	// store = cookie.NewStore(securecookie.GenerateRandomKey(32), securecookie.GenerateRandomKey(32))
	// r.Use(sessions.Sessions("cookiestore", store))



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
	r.GET("/callback", auth_state.callback_handler())
	r.GET("/logout", auth_state.logout())

	auth_r := r.Group("/auth")
	auth_r.Use(auth_state.ensure_loggedin())
	{
		auth_r.GET("/test", func(ctx *gin.Context) {
			fmt.Println("Dann inner function")

			auth_cookie, err := ctx.Cookie("session")
			if err != nil {
				fmt.Println("No Cookie, not authorized!")
				ctx.JSON(http.StatusUnauthorized, gin.H{"message":fmt.Sprint("unauthorized")})
				ctx.Abort()
				return
			}
			// s := sessions.Default(ctx)
			label := "nil"
			iss := -1
			cookie_values := FromJSON(auth_cookie)
			iss = cookie_values.IssuedAt
			label = cookie_values.Sub
			ctx.JSON(http.StatusOK, gin.H{"message":fmt.Sprint("authorized: ", label, ", issued: ", iss)})
		})
	}


	log.Printf("listening on http://%s/", "127.0.0.1:5555")
	r.Run(":5555")
	// log.Fatal(http.ListenAndServe("127.0.0.1:5555", nil))
}
