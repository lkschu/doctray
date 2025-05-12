package main

import (
	"fmt"
	"net/http"

	oidcauth "github.com/TJM/gin-gonic-oidcauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)






func ExampleConfigAuthentik() (c *oidcauth.Config) {
	c = oidcauth.DefaultConfig()
	c.ClientID = "KK6B05dYMspF6uwIWMr69PFcCvzkluJaGiXCAUX4"
	c.ClientSecret = "hVSwjzSJOhk2gg4M3Os3Vcg3x7EQ7nMjVOIhpJTnAmjvd3m33mcrH6mrO9wJcIQdp9MSbmcA11Ek0NEblTaoCCal72b2hkjgSa6YpmNppKwxWu8iClE9OMXpzi4uC3N4"
	c.IssuerURL = "https://auth.lukasschumann.de/application/o/test-app/"
	c.RedirectURL = "http://127.0.0.1:5555/callback"
    c.LoginClaim = "sub"
	return
}


func main() {
	router := gin.Default()
    router.StaticFile("/favicon.ico", "./resources/favicon.ico")
    router.Static("/ressources", "./resources/")
    router.LoadHTMLGlob("templates/**/*")

	// Session Config (Basic cookies)
	store := cookie.NewStore([]byte("secret"), nil)     // Do not use "secret", nil in production. This sets the keypairs for auth, encryption of the cookies.
	router.Use(sessions.Sessions("oidcauth-example", store)) // Sessions must be Use(d) before oidcauth, as oidcauth requires sessions

	// Authentication Config - Uses example dex config
	// - https://dexidp.io/docs/getting-started/
	auth, err := oidcauth.GetOidcAuth(ExampleConfigAuthentik())
	if err != nil {
		panic("auth setup failed")
	}
	router.GET("/login", auth.Login) // Unnecessary, as requesting a "AuthRequired" resource will initiate login, but potentially convenient
	router.GET("/callback", auth.AuthCallback)
	router.GET("/logout", auth.Logout)

	// Allow access to / for unauthenticated users, but authenticated users will be greated by name.
	router.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		name := "world"
		n := session.Get("name")
		if n != nil {
			name = n.(string)
		}
        println(name)
		// session.Save() // if it has been changed, which it has not
		// c.String(http.StatusOK, fmt.Sprintf("<h1>Welcome</h1>\nHello, %s.", name))
        // c.HTML(http.StatusOK, "posts/hello.tmpl", gin.H{"name":name,})
        c.HTML(http.StatusOK, "posts/hello.tmpl", []map[string]string{{"num":"1","key":"a"},{"num":"2","key":"b"}})
	})

	private := router.Group("/private", auth.AuthRequired())
	{
		private.GET("", func(c *gin.Context) {
			var name, email, sub, out string
			login := c.GetString(oidcauth.AuthUserKey)
			session := sessions.Default(c)
			n := session.Get("name")
			if n == nil {
				name = "Someone without a name?"
			} else {
				name = n.(string)
			}
			e := session.Get("email")
			if e != nil {
				email = e.(string)
			}
			u := session.Get("sub")
			if u != nil {
				sub = u.(string)
			}
            out = fmt.Sprintf("Hello, %s <%s>.\nLogin: %s\n\nsub: %s", name, email, login, sub)
			// session.Save() // if it has been changed, which it has not
			c.String(http.StatusOK, out)
			return
		})
	}

	router.Run(":5555")
}
