package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"time"
    "strconv"

    "path/filepath"

	"encoding/json"

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

type test_data struct {
    Num int             `json:"num"`
    Key string          `json:"key"`
}

type meta_info struct {
    selected bool
}

type doc struct {
    Date_added string   `json:"date"`
    Title string        `json:"title"`
    Description string  `json:"desc"`
    Url string          `json:"url"`
    Image string        `json:"image"`
    Tags []string       `json:"tags"`
    Meta meta_info      `json:"-"`
}
func (d doc) String() string {
    b,err := json.Marshal(d)
    if err != nil {
        fmt.Println(err)
        panic(err)
    }
    return string(b)
}
func (doc) init_data() []doc {
    doc1 := doc{Date_added:"2025-05-12T18:25", Title:"Coca-Cola braised carnitas | Homesick Texan",
    Description:"Coca-Cola and milk, along with warm flavors such as cinnamon, allspice, and chipotle give these carnitas a slight hint of bacon, which is never a bad thing at all. ",
    Url:"https://www.homesicktexan.com/coca-cola-braised-carnitas/",
    Image:"https://www.homesicktexan.com/wp-content/uploads/2017/02/Coca-Cola-braised-carnitas-DSC4126-1-1536x1020.jpg.webp",
    Tags: []string{"kochen","todo"}}
    doc2 := doc{Date_added:"2025-05-10T11:15", Title:"ich🚗iel : r/ich_iel",
    Description:"Lustige Memes und unterhaltsame Diskussionen. Tauche ein in die Welt der Memes und lach mit der Community",
    Url:"https://www.reddit.com/r/ich_iel/comments/1kklh3s/ichiel/",
    Image:"https://preview.redd.it/oir97h5vla0f1.jpeg?width=320&crop=smart&auto=webp&s=29bc2c71abd093799b896b36a9d94f3272311249",
    Tags: []string{"todo"}}
    return []doc{doc1, doc2}
}


func get_data() []test_data {
    file, err := os.Open("./data/test_data.json")
    defer file.Close()
    if err != nil {
        panic(err)
    }

    bytes_read := make([]byte, 1024*1024*1) // Up to 1MB of data
    n, err := file.Read(bytes_read)
    if err != nil && err != io.EOF {
        panic(err)
    }
    if n == 0 {
        panic("fail")
    }
    data := make([]test_data, 1)
    json.Unmarshal(bytes_read[0:n], &data)
    return data
}
func set_data(d []test_data) {
    b,err := json.Marshal(d)
    // fmt.Println(string(b))
    if err != nil {
        panic(err)
    }

    err = os.WriteFile("./data/test_data.json", b, 0644)
    if err != nil{
        panic(err)
    }
}

func main() {
	router := gin.Default()
    router.StaticFile("/favicon.ico", "./resources/favicon.ico")
    router.Static("/resources", "./resources/")
    router.LoadHTMLGlob("templates/**/*")

	// Session Config (Basic cookies)
	store := cookie.NewStore([]byte("secret"), nil)     // Do not use "secret", nil in production. This sets the keypairs for auth, encryption of the cookies.
	router.Use(sessions.Sessions("oidcauth-example", store)) // Sessions must be Use(d) before oidcauth, as oidcauth requires sessions

    router.MaxMultipartMemory = 10 << 20 // max 10 MiB

	// Authentication Config - Uses example dex config
	// - https://dexidp.io/docs/getting-started/
	auth, err := oidcauth.GetOidcAuth(ExampleConfigAuthentik())
	if err != nil {
		panic("auth setup failed")
	}
	router.GET("/login", auth.Login) // Unnecessary, as requesting a "AuthRequired" resource will initiate login, but potentially convenient
	router.GET("/callback", auth.AuthCallback)
	router.GET("/logout", auth.Logout)
	router.POST("/clicked", func(c *gin.Context) {
        c.String(http.StatusOK, time.Now().String())
    })
    router.POST("/upload", func(c *gin.Context){
        form, err := c.MultipartForm()
        if err != nil {
            c.String(http.StatusBadRequest, "get form err: %s", err.Error())
            return
        }
        fmt.Println("Forms:\n", form.File)
        files := form.File["files"]
        for _, file := range files {
            filename := filepath.Base(file.Filename)
            if err := c.SaveUploadedFile(file, filename); err != nil {
                c.String(http.StatusBadRequest, "upload file err: %s", err.Error())
                return
            }
        }
        c.String(http.StatusOK, "Uploaded successfully %d files", len(files))
    })
	router.POST("/doc-delete", func(c *gin.Context) {
        id_str := c.PostForm("id")
        if id_str == "" {
            c.String(http.StatusBadRequest, fmt.Sprintln("ERROR! Missing ID!"))
        }
        fmt.Printf("Got ID: %s\n", id_str)
        id, err := strconv.Atoi(id_str)
        if err != nil {
            c.String(http.StatusBadRequest, fmt.Sprintf("ERROR! Can't parse ID:%s!\n", id_str))
        }

        test_data_array := get_data()
        to_drop := -1
        for i, e := range test_data_array {
            if e.Num == id {
                to_drop = i
                break
            }
        }
        if to_drop == -1 {
            c.String(http.StatusBadRequest, fmt.Sprintf("ERROR! No such ID:%d!", id))
        } else {
            test_data_array = slices.Delete(test_data_array,to_drop,to_drop+1)
            set_data(test_data_array)
            c.HTML(http.StatusOK, "base/doc-list.tmpl", test_data_array)
        }
    })

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
        // c.HTML(http.StatusOK, "posts/hello.tmpl", []map[string]string{{"num":"1","key":"a"},{"num":"2","key":"b"}})

        // test_data_array := []test_data{
        //     {Num:1,Key:"a"},{Num:2,Key:"b"},{Num:3,Key:"c"},
        //     {Num:4,Key:"d"},{Num:5,Key:"e"},{Num:6,Key:"f"},
        //     {Num:7,Key:"g"},{Num:8,Key:"h"},{Num:9,Key:"i"} }
        test_data_array := get_data()
        set_data(test_data_array)
        c.HTML(http.StatusOK, "posts/hello.tmpl", test_data_array)
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
