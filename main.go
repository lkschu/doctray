package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net/http"
	"html"
	"html/template"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"path"
	// "path/filepath"

	"encoding/json"

	oidcauth "github.com/TJM/gin-gonic-oidcauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)


// TODO: the (html/)template.HTML type (which is an alias for a string) can be used to pass html tags into the template
//  otherwise they are escaped
// TODO: this can be used in the future to add very limited markdown rendering
// TODO: store text html escaped -> add custom html tags for [lists, bold, italic]




var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
func rand_seq(n int) string {
	b:= make([]rune,n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}





func ExampleConfigAuthentik() (c *oidcauth.Config) {
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

type docentry_file struct {
	Url string			`json:"url"`
	Name string         `json:"name"`
	OrgName string      `json:"orgname"`
}

// INFO: this is (so far) not really needed
const (
	doctype_file string = "file"
	doctype_mesage string = "msg"
	doctype_image string = "image"
)

type test_data struct {
	DocID int           `json:"id"`
	Title template.HTML `json:"title"`
	Desc string         `json:"desc"`
	UrlLL string          `json:"url"`
	Type string         `json:"type"`
	Date string         `json:"date"`
	Starred bool        `json:"starred"`
	Files []docentry_file        `json:"files"`
}
func (t test_data) String() string {
	b,err := json.Marshal(t)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	return string(b)
}

type meta_info struct {
	selected bool
}

type doc struct {
	Date_added string   `json:"date"`
	Title string        `json:"title"`
	Description string  `json:"desc"`
	Url string          `json:"url"`    // Either site or dl for file
	Image string        `json:"image"`
	Tags []string       `json:"tags"`
	Meta meta_info      `json:"-"`
	// TODO: type, i.e. URL, file, multile files, text, image, ...
	// TODO: org_filename vs uuid_filename
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


func get_data_new_id(data *[]test_data) int {
	new_id := -1
	for _,f := range *data {
		if f.DocID > new_id {
			new_id = f.DocID
		}
	}
	return new_id + 1
}

func get_data(sub string) []test_data {
	file, err := os.Open(fmt.Sprintf("./data/%s.json", sub))
	defer file.Close()
	if errors.Is(err, fs.ErrNotExist) {
		return make([]test_data, 0)
	}
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

	// Validate data, set defaults
	// TODO: remove duplicate ids
	for i,d := range data {
		if d.Type != doctype_file && d.Type != doctype_mesage && d.Type != doctype_image {
			data[i].Type = doctype_mesage
		}
	}
	return data
}
func set_data(d []test_data, sub string) {
	b,err := json.Marshal(d)
	// fmt.Println(string(b))
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(fmt.Sprintf("./data/%s.json", sub), b, 0644)
	if err != nil{
		panic(err)
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

		c.HTML(http.StatusOK, "posts/hello.tmpl", name)
	})


	router_media := router.Group("/media", auth.AuthRequired())
	{
		router_media.GET("/:item", func(c *gin.Context){
			item := c.Param("item")
			sub := get_uuid(c)
			fmt.Println("Item: ", item)
			fullName := "uploads/" + sub + "/" + item
			fmt.Println("Fullpath: ", fullName)
			c.File(fullName)
		})
	}


	router_tray := router.Group("/tray", auth.AuthRequired())
	{
		router_tray.GET("/", func(c *gin.Context) {
			sub := get_uuid(c)
			test_data_array := get_data(sub)
			set_data(test_data_array, sub)
			c.HTML(http.StatusOK, "posts/tray.tmpl", test_data_array)
		})

		router_tray.POST("/doc-create", func(c *gin.Context){
			ret := func (c*gin.Context) bool { // Ugly hack to let the defer update the data before we use it in the tmpl
				form, err := c.MultipartForm()
				if err != nil {
					c.String(http.StatusBadRequest, "get form err: %s", err.Error())
					return false
				}
				sub := get_uuid(c)
				fmt.Println("Forms:\n", form.File)
				files := form.File["files"]
				titles := form.Value["title"]
				var title string
				if len(titles) > 0 {
					title = titles[0]
					title = html.EscapeString(title)
				} else {
					title = ""
				}
				fmt.Println("title Value: ", titles)

				date := time.Now().UTC().Format(http.TimeFormat)
				if len(files) == 0 {
					data := get_data(sub)
					data = append(data, test_data{DocID:get_data_new_id(&data),Title:template.HTML(title),Type:doctype_mesage, Date: date})
					set_data(data, sub)
				} else {
					data := get_data(sub)
					doc_id := get_data_new_id(&data)
					defer func() {set_data(data, sub)} ()
					docentry_new_files := make([]docentry_file, 0)
					new_data := test_data{DocID:doc_id,Title:template.HTML(title),Type:doctype_file,Date: date, Files: docentry_new_files}
					for _, file := range files {
						basename := fmt.Sprintf("%d__%s", time.Now().UnixMilli(), rand_seq(8)) + path.Ext(file.Filename)
						filename := "uploads/"+ sub +"/" + basename
						// TODO: error handling if first file is uploaded but later are failing
						if err := c.SaveUploadedFile(file, filename); err != nil {
							c.String(http.StatusBadRequest, "upload file err: %s", err.Error())
							return false
						}
						docentry_new_files = append(docentry_new_files, docentry_file{Url: "/media/"+basename, OrgName: path.Base(file.Filename), Name: basename})
						new_data.Files = docentry_new_files
					}
					// fmt.Println(data[len(data)-1].String())
					data = append(data, new_data)
				}
				return true
			} (c)
			if ret {
				sub := get_uuid(c)
				// c.Header("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
				c.HTML(http.StatusOK, "base/doc-list.tmpl", get_data(sub))
			}
		})

		router_tray.POST("/doc-delete", func(c *gin.Context) {
			id_str := c.PostForm("id")
			if id_str == "" {
				c.String(http.StatusBadRequest, fmt.Sprintln("ERROR! Missing ID!"))
			}
			fmt.Printf("Got ID: %s\n", id_str)
			id, err := strconv.Atoi(id_str)
			if err != nil {
				c.String(http.StatusBadRequest, fmt.Sprintf("ERROR! Can't parse ID:%s!\n", id_str))
			}

			sub := get_uuid(c)
			test_data_array := get_data(sub)
			to_drop := -1
			for i, e := range test_data_array {
				if e.DocID == id {
					to_drop = i
					break
				}
			}
			if to_drop == -1 {
				c.String(http.StatusBadRequest, fmt.Sprintf("ERROR! No such ID:%d!", id))
			} else {
				for _,f := range test_data_array[to_drop].Files {
					if strings.HasPrefix(f.Url, "/media/") {
						basename := strings.TrimPrefix(f.Url, "/media/")
						os.Remove("uploads/"+sub+"/"+basename)
					}
				}
				test_data_array = slices.Delete(test_data_array,to_drop,to_drop+1)
				set_data(test_data_array, sub)
				// c.Header("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
				// c.HTML(http.StatusOK, "base/doc-list.tmpl", test_data_array)

				c.Header("Content-Type", "text/html")
				answer := "<li class=\"doc-entry doc-type-removed\"> <i>Removed</i> </li>"
				c.String(http.StatusOK, answer)
			}
		})

		router_tray.POST("/doc-star", func(c *gin.Context) {
			id_str := c.PostForm("id")
			if id_str == "" {
				c.String(http.StatusBadRequest, fmt.Sprintln("ERROR! Missing ID!"))
			}
			fmt.Printf("Got ID: %s\n", id_str)
			id, err := strconv.Atoi(id_str)
			if err != nil {
				c.String(http.StatusBadRequest, fmt.Sprintf("ERROR! Can't parse ID:%s!\n", id_str))
			}

			sub := get_uuid(c)
			test_data_array := get_data(sub)
			toggle_star := -1
			for i, e := range test_data_array {
				if e.DocID == id {
					toggle_star = i
					break
				}
			}
			if toggle_star == -1 {
				c.String(http.StatusBadRequest, fmt.Sprintf("ERROR! No such ID:%d!", id))
			} else {
				test_data_array[toggle_star].Starred = ! test_data_array[toggle_star].Starred
				set_data(test_data_array, sub)
				c.Header("Content-Type", "text/html")
				answer := ""
				if test_data_array[toggle_star].Starred {
					answer = "<div class=\"doc-entry-button-fav starred\"> <button hx-post=\"/doc-star\" hx-vals='{\"id\":" + id_str + "}'hx-target=\"closest .doc-entry-button-fav\" hx-swap=\"outerHTML\">🌟</button> </div>"
				} else {
					answer = "<div class=\"doc-entry-button-fav\"> <button hx-post=\"/doc-star\" hx-vals='{\"id\":" + id_str +"}'hx-target=\"closest .doc-entry-button-fav\" hx-swap=\"outerHTML\">🌟</button> </div>"
				}
				c.String(http.StatusOK, answer)
			}
		})
	}

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
