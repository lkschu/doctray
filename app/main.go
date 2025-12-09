package main

import (
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"io/fs"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"path"
	// "path/filepath"

	"encoding/json"

	// oidcauth "github.com/TJM/gin-gonic-oidcauth"

	"main/internal/openidauth"
	"main/internal/previewbuilder"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// TODO: the (html/)template.HTML type (which is an alias for a string) can be used to pass html tags into the template
//  otherwise they are escaped
// TODO: this can be used in the future to add very limited markdown rendering
// TODO: store text html escaped -> add custom html tags for [lists, bold, italic]
// TODO: inline monospace like this '`mono` mehr text' only shows the monospace text
// TODO: Define user information and think how to save it (Name,Email,Sub,etc.)
// TODO: Create a welcome page
// TODO: handle login constant redirect problem
// TODO: refactor this page and only then continue with the acutal doctray functionality
// TODO: doctray tags

var DATA_BASE_PATH = ""



var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
func rand_seq(n int) string {
	b:= make([]rune,n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

var known_file_suffixes = map[string]string{
	// Documents
	".pdf": "picture_as_pdf",
	".md": "markdown",
	".txt": "description",
	".doc": "description",
	".docx": "description",

	// Images
	".jpg": "imagesmode",
	".jpeg": "imagesmode",
	".png": "imagesmode",
	".webp": "imagesmode",

	// Audio
	".mp3":"music_note",
	".wav":"music_note",
	".mka":"music_note",
	".m4a":"music_note",
	".m4b":"music_note",

	// Movies
	".mkv": "movie",
	".webm": "movie",
	".mp4": "movie",
	".avi": "movie",
	".mov": "movie",

	// Program
	".sh": "terminal",
	".lua": "terminal",
	".ts": "terminal",
	".js": "terminal",
	".py": "terminal",
	".go": "terminal",
	".c": "terminal",
	".h": "terminal",

	// Default
	".default": "draft",
}


func assert(a bool) {
	if !a {
		panic(a)
	}
}


const (
	token_bold_open string = "<b>"
	token_bold_close string = "</b>"
	token_italic_open string = "<i>"
	token_italic_close string = "</i>"
	token_strike_open string = "<s>"
	token_strike_close string = "</s>"
	token_tt_open string = "<tt class=\"markdown-code\">"
	token_tt_close string = "</tt>"
)
type token_position struct {
	token string
	position int
}

func RandomColor() string {
	r := rand.Intn(256)
	g := rand.Intn(256)
	b := rand.Intn(256)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}
func RandomString() string {
	b1 := rand.Intn(256)
	b2 := rand.Intn(256)
	b3 := rand.Intn(256)
	b4 := rand.Intn(256)
	b5 := rand.Intn(256)
	b6 := rand.Intn(256)
	b7 := rand.Intn(256)
	b8 := rand.Intn(256)
	return fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x", b1, b2, b3, b4, b5, b6, b7, b8)
}


// TODO: refactor!!
func add_formatting_tags__add_text_decoration(line_bytes []byte) []byte {
	// tokenize string
	token_positions := make([]token_position, 0)
	i:= 0
	for i < len(line_bytes) {
		switch line_bytes[i]{
		case '*':
			double := i < len(line_bytes)-1 && line_bytes[i+1] == '*'
			if i == 0 {
				if double {
					token_positions = append(token_positions, token_position{token: token_bold_open, position: i})
				} else {
					token_positions = append(token_positions, token_position{token: token_italic_open, position: i})
				}
			} else if i == len(line_bytes) -1 || i== len(line_bytes)-2 && double {
				if double {
					token_positions = append(token_positions, token_position{token: token_bold_close, position: i})
				} else {
					token_positions = append(token_positions, token_position{token: token_italic_close, position: i})
				}
			} else {
				if double {
					if line_bytes[i-1] == ' ' {
						token_positions = append(token_positions, token_position{token: token_bold_open, position: i})
					} else if i < len(line_bytes)-2 && line_bytes[i+2] == ' '{
						token_positions = append(token_positions, token_position{token: token_bold_close, position: i})
					}
				} else {
					if line_bytes[i-1] == ' ' {
						token_positions = append(token_positions, token_position{token: token_italic_open, position: i})
					} else if i < len(line_bytes)-1 && line_bytes[i+1] == ' '{
						token_positions = append(token_positions, token_position{token: token_italic_close, position: i})
					}
				}
			}
			if double {
				i+=1
			}
		case '~':
			if i == 0 {
				token_positions = append(token_positions, token_position{token: token_strike_open, position: i})
			} else if i == len(line_bytes)-1 {
				token_positions = append(token_positions, token_position{token: token_strike_close, position: i})
			} else {
				if line_bytes[i-1] == ' ' {
					token_positions = append(token_positions, token_position{token: token_strike_open, position: i})
				} else if i < len(line_bytes)-1 && line_bytes[i+1] == ' '{
					token_positions = append(token_positions, token_position{token: token_strike_close, position: i})
				}
			}
		case '`':
			if i == 0 {
				token_positions = append(token_positions, token_position{token: token_tt_open, position: i})
			} else if i == len(line_bytes)-1 {
				token_positions = append(token_positions, token_position{token: token_tt_close, position: i})
			} else {
				if line_bytes[i-1] == ' ' {
					token_positions = append(token_positions, token_position{token: token_tt_open, position: i})
				} else if i < len(line_bytes)-1 && line_bytes[i+1] == ' '{
					token_positions = append(token_positions, token_position{token: token_tt_close, position: i})
				}
			}
		}
		i+=1
	}

	// trim to relevant tokens
	t_idx := 0
	var bold_tag token_position
	var italic_tag token_position
	var strike_tag token_position
	var tt_tag token_position
	matched_tags := make([]token_position,0)
	for t_idx < len(token_positions) {
		switch token_positions[t_idx].token {
		case token_bold_open:
			bold_tag = token_positions[t_idx] // old tag can be overwritten
		case token_bold_close:
			if bold_tag != (token_position{}) {
				matched_tags = append(matched_tags, bold_tag)
				matched_tags = append(matched_tags, token_positions[t_idx])
				bold_tag = token_position{}
			}
		case token_italic_open:
			italic_tag = token_positions[t_idx] // old tag can be overwritten
		case token_italic_close:
			if italic_tag != (token_position{}) {
				matched_tags = append(matched_tags, italic_tag)
				matched_tags = append(matched_tags, token_positions[t_idx])
				italic_tag = token_position{}
			}
		case token_strike_open:
			strike_tag = token_positions[t_idx] // old tag can be overwritten
		case token_strike_close:
			if strike_tag != (token_position{}) {
				matched_tags = append(matched_tags, strike_tag)
				matched_tags = append(matched_tags, token_positions[t_idx])
				strike_tag = token_position{}
			}
		case token_tt_open:
			tt_tag = token_positions[t_idx] // old tag can be overwritten
		case token_tt_close:
			if tt_tag != (token_position{}) {
				matched_tags = append(matched_tags, tt_tag)
				matched_tags = append(matched_tags, token_positions[t_idx])
				tt_tag = token_position{}
			}
		}
		t_idx += 1
	}
	sort.Slice(matched_tags, func(i,j int) bool { return matched_tags[i].position < matched_tags[j].position })

	// Replace tokens with html tags
	var line_rebuilt []byte
	last_idx := 0
	for _,tag := range matched_tags {
		assert(last_idx <= len(line_bytes))
		line_rebuilt = append(line_rebuilt,line_bytes[last_idx:tag.position]...)
		if tag.token == token_bold_open || tag.token == token_bold_close {
			last_idx = tag.position + 2
		} else {
			last_idx = tag.position + 1
		}

		line_rebuilt = append(line_rebuilt, []byte(tag.token)...)
	}
	line_rebuilt = append(line_rebuilt, line_bytes[last_idx:]...)
	return line_rebuilt
}

func add_formatting_tags__add_url(line_bytes []byte) []byte {
	loc := find_url_in_string(line_bytes)
	if  loc == nil {
		return line_bytes
	}

	ret_bytes := make([]byte, 0)
	last_indx := 0
	for i, _ := range loc {
		url := line_bytes[loc[i][0]:loc[i][1]]
		ret_bytes = append(ret_bytes, line_bytes[last_indx:loc[i][0]]...)
		ret_bytes = append(ret_bytes, []byte(fmt.Sprintf("<a href=\"%s\" target=\"_blank\">", url))...)
		if len(url) > 70 {
			url = url[:70]
			url = append(url, []byte("...")...)
		}
		ret_bytes = append(ret_bytes, url...)
		ret_bytes = append(ret_bytes, []byte(fmt.Sprintf("</a>"))...)
		last_indx = loc[i][1]
	}
	ret_bytes = append(ret_bytes, line_bytes[last_indx:]...)
	return ret_bytes
}

var url_regex *regexp.Regexp
func find_url_in_string(l []byte) [][]int{
	if url_regex == nil {
		url_regex, _ = regexp.Compile(`\b((https?:\/\/)?((localhost|(\d{1,3}\.){3}\d{1,3}|([a-zA-Z0-9-]+\.)+[a-zA-Z]{2,}))(:\d{2,5})?(\/[^\s]*)?)\b`)
	}
	// return url_regex.FindIndex(l)
	return url_regex.FindAllIndex(l, -1)
}


// TODO: codeblocks should be implemented early in this function
// TODO: missing: code blocks...
// Add bold/italic/strikethrough, inline code and codeblocks, itemize, url highlighting, etc..
func add_formatting_tags_to_string(s string) string{
	var return_string strings.Builder
	return_string.WriteString("")
	content_split_by_lines := strings.Split(s, "\n")
	skip_until_this_idx_because_code_block := -1
	skip_searching_for_new_code_blocks := false
	for line_idx,line := range content_split_by_lines {

		// Extract code blocks
		if line_idx < skip_until_this_idx_because_code_block { // content in matching codeblock tags
			return_string.WriteString(line)
			return_string.WriteString("<br>")
			skip_searching_for_new_code_blocks = true
			continue
		}
		if line_idx == skip_until_this_idx_because_code_block { // the line closing the codeblock
			return_string.WriteString("</tt>")
			if len(line) > 3 {
				return_string.WriteString("<br>")
				line = string([]byte(line)[3:])
			} else {
				skip_searching_for_new_code_blocks = false
				continue
			}
		}
		if !skip_searching_for_new_code_blocks && strings.HasPrefix(line, "```") {
			find_matching_pair := line_idx+1
			for find_matching_pair < len(content_split_by_lines) {
				if strings.HasPrefix(content_split_by_lines[find_matching_pair], "```") {
					break
				}
				find_matching_pair += 1
			}
			if find_matching_pair > line_idx+1 {
				skip_until_this_idx_because_code_block = find_matching_pair
				return_string.WriteString("<tt class=\"markdown-codeblock\">")
				if len(line) > 3 {
					return_string.WriteString(string([]byte(line)[3:]))
					return_string.WriteString("<br>")
				}
				continue
			}
		}
		skip_searching_for_new_code_blocks = false


		line_bytes := []byte(line)
		// fmt.Println(string(line_bytes))
		line_bytes = add_formatting_tags__add_text_decoration(line_bytes)
		line_bytes = add_formatting_tags__add_url(line_bytes)

		new_line := string(line_bytes)
		find_first_regular_character := func (bytes []byte) int {
			ret := 0
			before_dash := true
			for i,b := range bytes {
				if b != ' ' && b != '-' {
					return ret
				}
				if b == '-' && before_dash {
					before_dash = false
				}
				if b == ' ' && !before_dash {
					ret = i+1
				}
			}
			return ret
		}

		// fmt.Println(string(line_bytes))
		list_added := false
		if strings.HasPrefix(new_line, "- ") || strings.HasPrefix(new_line, " - ") {
			list_added = true
			drop_idx := find_first_regular_character(line_bytes)
			line_bytes = line_bytes[drop_idx:]
			line_bytes = append([]byte("<ul><div class=\"markdown-list-outer\"><li>"), line_bytes...)
			line_bytes = append(line_bytes,[]byte("</li></div></ul>")...)
		} else if strings.HasPrefix(new_line, "  - ") || strings.HasPrefix(new_line, "   - ") {
			list_added = true
			drop_idx := find_first_regular_character(line_bytes)
			line_bytes = line_bytes[drop_idx:]
			line_bytes = append([]byte("<ul><div class=\"markdown-list-inner\"><li>"), line_bytes...)
			line_bytes = append(line_bytes,[]byte("</li></div></ul>")...)
		}
		// fmt.Println("Wrapped:{" + string(line_bytes) + "}\n\n")
		return_string.WriteString(string(line_bytes))
		if !list_added {
			return_string.WriteString("<br>")
		}
	}
	return return_string.String()
}

func render_workspace_container_to_html(c *gin.Context)  {
		sub := get_uuid(c)
		c.HTML(http.StatusOK, "posts/workspace-container.tmpl", render_all(get_data(sub)))
}

func render_posts_to_html(c *gin.Context)  {
		sub := get_uuid(c)
		c.HTML(http.StatusOK, "base/doc-list.tmpl", render_all(get_data(sub)))
}

type docentry_file struct {
	Url string			`json:"url"`
	Name string         `json:"name"`
	OrgName string      `json:"orgname"`
	Icon string			`json:"icon"`
}
func (d docentry_file) String() string {
	b,err := json.Marshal(d)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	return string(b)
}

// INFO: this is (so far) not really needed
const (
	doctype_file string = "file"
	doctype_mesage string = "msg"
	doctype_image string = "image"
)

type tag struct {
	Nr string			`json:"-"`
	ID string			`json:"id"`
	Sym string			`json:"symbol"`
	Name string			`json:"name"`
	Color string		`json:"color"`
	Enabled bool		`json:"enabled"`
}
func (tag) New() tag {
	return tag{Nr: "0", ID: RandomString(), Sym: "?", Name: "tag name", Color: RandomColor()}
}
func (t tag) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Symbol: %s\n", t.Sym))
	sb.WriteString(fmt.Sprintf("Name: %s\n", t.Name))
	sb.WriteString(fmt.Sprintf("Color: %s\n", t.Color))
	return sb.String()
}
func (t tag) StringShort() string {
	b,err := json.Marshal(t)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	return string(b)
}


// Saves all data
type profile_data struct {
	Posts []post	`json:"posts"`
	Tags []tag			`json:"tags"`
	Tag_map map[string]*tag	`json:"-"` // id -> tag mapping
	Tag_edit bool		`json:"tag_edit"`
	Only_favorites bool	`json:"only_starred"`
}
func (p *profile_data) normalize_tag_nrs() {
	for i,_ := range p.Tags {
		p.Tags[i].Nr = fmt.Sprint(i)
	}
}
func (p *profile_data) find_post_by_id(id int) *post {
	idx := p.find_post_idx_by_id(id)
	if idx == -1 {
		return nil
	}
	return &p.Posts[idx]
}
func (p *profile_data) find_post_idx_by_id(id int) int {
	ret := -1

	for i, e := range p.Posts {
		if e.DocID == id {
			ret = i
			break
		}
	}
	return ret
}
func (p *profile_data) String() string {
	b,err := json.Marshal(p)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	return string(b)
}

type tag_enabled struct {
	Tag *tag
	Enabled bool
	BackRef *post
}

type post struct {
	DocID int				`json:"id"`
	Title template.HTML		`json:"title"`
	Desc string				`json:"desc"`
	UrlLL string			`json:"url"`
	Type string				`json:"type"`
	Date string				`json:"date"`
	Starred bool			`json:"starred"`
	Files []docentry_file	`json:"files"`
	Webpreview []previewbuilder.URLPreview	`json:"webpreview"`
	Tags []string			`json:"tags"`
	Tags_enabled []tag_enabled	`json:"-"`
}
func (t post) String() string {
	b,err := json.Marshal(t)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	return string(b)
}
// Toggles the tag (by string id) for the post
func (p *post) toggle_tag_by_id(tag_id string) (error) {
	disabled := false
	unknown_tag := true
	for i,t := range p.Tags {
		if t == tag_id {
			p.Tags = append(p.Tags[:i],p.Tags[i+1:]...)
			disabled = true
			unknown_tag = false
			break
		}
	}
	for i,t := range p.Tags_enabled {
		if t.Tag.ID == tag_id {
			unknown_tag = false
			p.Tags_enabled[i].Enabled = !p.Tags_enabled[i].Enabled
			break
		}
	}
	if unknown_tag {
		return errors.New("Unknown tag id")
	}

	if !disabled {
		p.Tags = append(p.Tags, tag_id)
	}
	return nil
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


func get_data_new_id(data *[]post) int {
	new_id := -1
	for _,f := range *data {
		if f.DocID > new_id {
			new_id = f.DocID
		}
	}
	return new_id + 1
}


func render_post(p post) post {
	// fmt.Println(data[i].String())
	p.Title = template.HTML(add_formatting_tags_to_string(string(p.Title)))
	// rendered_posts[i] = data[i]
	// fmt.Println(data[i].String())
	return p
}

func render_all(profile profile_data) profile_data {
	posts:=profile.Posts
	rendered_posts := make([]post, 0)
	for _,p := range posts {
		if profile.Only_favorites && !p.Starred {
			continue
		}

		// all disabled -> no filtering
		active_tag_filter := false
		for _,t := range profile.Tags {
			if t.Enabled {
				active_tag_filter = true
				break
			}
		}

		if active_tag_filter {
			posts_viewable_after_tag_filter := false
			for _,tag_id := range p.Tags {
				if tag_pointer,ok := profile.Tag_map[tag_id]; ok && tag_pointer.Enabled {
					posts_viewable_after_tag_filter = true
					break
				}
			}
			if !posts_viewable_after_tag_filter {
				continue
			}
		}
		// rendered_posts[i] = render_post(p)
		rendered_posts = append(rendered_posts, render_post(p))
	}
	profile.Posts = rendered_posts
	return profile
}


func get_data(sub string) profile_data {
	file, err := os.Open(fmt.Sprintf("%s/data/%s.json", DATA_BASE_PATH, sub))
	if errors.Is(err, fs.ErrNotExist) {
		return profile_data{}
	}
	if err != nil {
		panic(err)
	}
	defer file.Close()

	bytes_read := make([]byte, 1024*1024*1) // Up to 1MB of data
	n, err := file.Read(bytes_read)
	if err != nil && err != io.EOF {
		panic(err)
	}
	if n == 0 {
		panic("fail")
	}
	// data := make([]test_data, 1)
	profile_data := profile_data{}
	profile_data.Tag_map = make(map[string]*tag)
	json.Unmarshal(bytes_read[0:n], &profile_data)

	// Validate Tags
	profile_data.normalize_tag_nrs()
	drop_tags_by_idx := make([]int, 0)
	for i,t := range profile_data.Tags {
		if _, ok := profile_data.Tag_map[t.ID]; ok {
			// key is known, duplicate!
			drop_tags_by_idx = append(drop_tags_by_idx, i)
			fmt.Printf("Duplicate for UUID: '%s'; dropping!\n", t.ID)
		} else {
			profile_data.Tag_map[t.ID] = &profile_data.Tags[i]
		}
	}

	if x:= len(drop_tags_by_idx); x > 0 {
		fmt.Printf("Dropping <%d> tags!\n", x)
		for i:= len(drop_tags_by_idx)-1; i > 0; i-- {
			profile_data.Tags = append(profile_data.Tags[:i], profile_data.Tags[i+1:]...)
		}
	}


	// Validate posts, set defaults
	// TODO: remove duplicate ids
	posts := profile_data.Posts
	for i,d := range posts {
		if d.Type != doctype_file && d.Type != doctype_mesage && d.Type != doctype_image {
			posts[i].Type = doctype_mesage
		}
		if len(d.Files) > 0 {
			for j,f := range d.Files {
				if f.Icon == "" {
					icon, success := known_file_suffixes[filepath.Ext(f.OrgName)]
					if success {
						posts[i].Files[j].Icon = icon
					} else {
						posts[i].Files[j].Icon = known_file_suffixes[".default"]
					}
					// fmt.Println(f.String())
				}
			}
		}
	}

	for i,p := range posts {
		active_tags := make(map[string]bool)
		new_tags := make([]string, 0)
		// drop duplicate and old tags
		for _,t := range p.Tags {
			if _,ok := profile_data.Tag_map[t]; ok && t != "" {
				new_tags = append(new_tags, t)
			}
		}
		posts[i].Tags = new_tags

		for _,t := range p.Tags {
			active_tags[t] = true
		}
		posts[i].Tags_enabled = make([]tag_enabled, 0)
		for _,t := range profile_data.Tags {
			tag_enabled := tag_enabled{Enabled: active_tags[t.ID], Tag: &t, BackRef: &posts[i]}
			posts[i].Tags_enabled = append(posts[i].Tags_enabled, tag_enabled)
		}

	}
	profile_data.Posts = posts

	return profile_data
}
func set_data(profile profile_data, sub string) {
	profile.normalize_tag_nrs()
	b,err := json.Marshal(profile)
	// fmt.Println(string(b))
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(fmt.Sprintf("%s/data/%s.json", DATA_BASE_PATH, sub), b, 0644)
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
	clientID, success		:= os.LookupEnv("DOCTRAY_CLIENTID")
	if ! success {
		panic("DOCTRAY_CLIENTID not an environment variable!")
	}
	clientSecret,success	:= os.LookupEnv("DOCTRAY_CLIENTSECRET")
	if ! success {
		panic("DOCTRAY_CLIENTSECRET not an environment variable!")
	}
	issuerUrl,success	:= os.LookupEnv("DOCTRAY_ISSUERURL")
	if ! success {
		panic("DOCTRAY_ISSUERURL not an environment variable!")
	}
	redirectURL,success	:= os.LookupEnv("DOCTRAY_REDIRECTURL")
	if ! success {
		panic("DOCTRAY_REDIRECTURL not an environment variable!")
	}

	auth_handler := openidauth.NewAuthHandler(clientID, clientSecret, int64(time.Minute.Seconds()*2),issuerUrl, redirectURL)


	basepath,success	:= os.LookupEnv("DOCTRAY_TARGET_DIRECTORY")
	if ! success {
		panic("DOCTRAY_TARGET_DIRECTORY not an environment variable!")
	}
	if stat, err := os.Stat(basepath); err != nil || !stat.IsDir() {
		panic(fmt.Sprintf("DOCTRAY_TARGET_DIRECTORY specified path (%s) is not available!", basepath))
	}
	DATA_BASE_PATH = basepath

	router := gin.Default()
	router.StaticFile("/favicon.ico", "./resources/favicon.ico")
	router.Static("/resources", "./resources/")
	router.LoadHTMLGlob("templates/**/*")

	// Session Config (Basic cookies)
	// store = cookie.NewStore(securecookie.GenerateRandomKey(32), securecookie.GenerateRandomKey(32))
	// "github.com/gorilla/securecookie"
	store := cookie.NewStore([]byte("secret"), nil)     // TODO: <<<
	// router.Use(sessions.Sessions("oidcauth-example", store)) // Sessions must be Use(d) before oidcauth, as oidcauth requires sessions
	router.Use(sessions.Sessions("session", store)) // Sessions must be Use(d) before oidcauth, as oidcauth requires sessions

	router.MaxMultipartMemory = 10 << 20 // max 10 MiB

	router.GET("/login", auth_handler.Login()) // Unnecessary, as requesting a "AuthRequired" resource will initiate login, but potentially convenient
	router.GET("/callback", auth_handler.Callback_handler())
	router.GET("/logout", auth_handler.LogoutWithRedirect("/"))





	// Allow access to / for unauthenticated users, but authenticated users will be greated by name.
	router.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		name := "world"
		n := session.Get("sub")
		if n != nil {
			name = n.(string)
		}
		println(name)
		// session.Save() // if it has been changed, which it has not

		c.HTML(http.StatusOK, "posts/hello.tmpl", name)
	})


	router_media := router.Group("/media", auth_handler.Ensure_loggedin())
	{
		router_media.GET("/:item", func(c *gin.Context){
			item := c.Param("item")
			sub := get_uuid(c)
			fmt.Println("Item: ", item)
			fullName := DATA_BASE_PATH + "/uploads/" + sub + "/" + item
			fmt.Println("Fullpath: ", fullName)
			c.File(fullName)
		})
	}


	router_tray := router.Group("/tray", auth_handler.Ensure_loggedin())
	{
		router_tray.GET("/", func(c *gin.Context) {
			sub := get_uuid(c)
			profile_data := get_data(sub)
			set_data(profile_data, sub)
			c.HTML(http.StatusOK, "posts/tray.tmpl", render_all(profile_data))
		})

		router_tray.POST("/ping", func(ctx *gin.Context) {ctx.String(http.StatusOK, "All fine")})


		merge_form := func (string_slice []string) (string) {
			var ret string
			if len(string_slice) > 0 {
				ret = string_slice[0]
				ret = strings.TrimSpace(ret)
				ret = strings.ReplaceAll(ret,"\r","")
				ret = html.EscapeString(ret)
			} else {
				ret = "_"
			}
			return ret
		}
		extract_tags_from_multiform := func(form *multipart.Form) ([]tag) {
			ret_tags := make([]tag, 0)
			for i:=0; i<=32; i++ {
				if len(form.Value[fmt.Sprintf("tag[%d]name", i)]) > 0 {
					tagnr := fmt.Sprint(i)
					tagid := merge_form(form.Value[fmt.Sprintf("tag[%d]tag_id", i)])
					tagname := merge_form(form.Value[fmt.Sprintf("tag[%d]name", i)])
					tagsymbol := merge_form(form.Value[fmt.Sprintf("tag[%d]symbol", i)])
					tagcolor := merge_form(form.Value[fmt.Sprintf("tag[%d]color", i)])
					new_tag := tag{Nr: tagnr, ID: tagid, Name: tagname, Sym: tagsymbol, Color: tagcolor}
					ret_tags = append(ret_tags, new_tag)
				}
			}
			fmt.Printf("Extracted %d tags from multiform\n", len((ret_tags)))
			return ret_tags
		}

		router_tray.POST("/post-tag", func(c *gin.Context) {
			id_str := c.PostForm("id")
			if id_str == "" {
				c.String(http.StatusBadRequest, fmt.Sprintln("ERROR! Missing ID!"))
			}
			fmt.Printf("Got ID: %s\n", id_str)
			post_id, err := strconv.Atoi(id_str)
			if err != nil {
				c.String(http.StatusBadRequest, fmt.Sprintf("ERROR! Can't parse ID:%s!\n", id_str))
			}
			tag_uid := c.PostForm("tag")
			if tag_uid == "" {
				c.String(http.StatusBadRequest, fmt.Sprintln("ERROR! Missing tag!"))
			}

			sub := get_uuid(c)
			profile := get_data(sub)
			p_idx := profile.find_post_idx_by_id(post_id)
			err = profile.Posts[p_idx].toggle_tag_by_id(tag_uid)
			if err != nil {
				c.String(http.StatusBadRequest, "Unknown tag: %s", err.Error())
			}
			set_data(profile, sub)
			// c.HTML(http.StatusOK, "base/doc.tmpl", render_post(profile.Posts[p_idx]))

			var t_en *tag_enabled
			for i,t := range profile.Posts[p_idx].Tags_enabled {
				if t.Tag.ID == tag_uid {
					t_en = &profile.Posts[p_idx].Tags_enabled[i]
					break
				}
			}
			c.HTML(http.StatusOK, "base/doc-tagbar-segments.tmpl", t_en)
		})
		router_tray.POST("/star-filter", func(c *gin.Context) {
			sub := get_uuid(c)
			profile := get_data(sub)
			profile.Only_favorites = !profile.Only_favorites
			set_data(profile, sub)
			render_workspace_container_to_html(c)
		})
		router_tray.POST("/tag-toggle-filter", func(c *gin.Context) {
			sub := get_uuid(c)
			profile := get_data(sub)
			id_str := c.PostForm("id")
			if id_str == "" {
				c.String(http.StatusBadRequest, fmt.Sprintln("ERROR! Missing ID!"))
			}
			val,ok := profile.Tag_map[id_str]
			if !ok {
				c.String(http.StatusBadRequest, fmt.Sprintln("ERROR! Unknown ID \"", id_str, "\"!" ))
			}
			val.Enabled = !val.Enabled
			fmt.Printf("Toggled '%s' to %t\n", val.ID, val.Enabled)
			set_data(profile, sub)

			render_workspace_container_to_html(c)
		})
		router_tray.POST("/tag-edit", func(c *gin.Context){
			sub := get_uuid(c)
			profile_data := get_data(sub)
			profile_data.Tag_edit = true
			set_data(profile_data, sub)
			c.HTML(http.StatusOK, "base/tags.tmpl", render_all(profile_data))
		})
		router_tray.POST("/tag-apply", func(c *gin.Context){
			sub := get_uuid(c)
			form, err := c.MultipartForm()
			if err != nil {
				c.String(http.StatusBadRequest, "get form err: %s", err.Error())
				return
			}

			profile_data := get_data(sub)
			profile_data.Tags = extract_tags_from_multiform(form)
			profile_data.Tag_edit = false
			set_data(profile_data, sub)
			c.Header("HX-Refresh", "true")
			c.String(http.StatusOK, "")
		})
		router_tray.POST("/tag-create", func(c *gin.Context){
			sub := get_uuid(c)
			form, err := c.MultipartForm()
			if err != nil {
				c.String(http.StatusBadRequest, "get form err: %s", err.Error())
				return
			}

			profile_data := get_data(sub)
			profile_data.Tags = extract_tags_from_multiform(form)
			fmt.Println("Tags in memory before addition:")
			for _,t := range profile_data.Tags {
				fmt.Println(t.StringShort())
			}
			fmt.Println("")
			if len(profile_data.Tags) >= 32 {
				c.HTML(http.StatusOK, "base/tags.tmpl", render_all(profile_data))
				return
			}
			new_tag := tag{}.New()
			new_tag.Nr = fmt.Sprint(len(profile_data.Tags))
			profile_data.Tags = append(profile_data.Tags, new_tag)
			profile_data.normalize_tag_nrs()
			fmt.Println("Tags in memory:")
			for _,t := range profile_data.Tags {
				fmt.Println(t.StringShort())
			}
			fmt.Println("")
			c.HTML(http.StatusOK, "base/tags.tmpl", render_all(profile_data))
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
					title = strings.TrimSpace(title)
					title = strings.ReplaceAll(title,"\r","")
					title = html.EscapeString(title)
				} else {
					title = ""
				}
				fmt.Println("title Value: ", titles)

				//webpreviews
				preview_url_idxs := find_url_in_string([]byte(title))
				preview_urls := make([]string, len(preview_url_idxs))
				for i,idx_pair := range preview_url_idxs {
					preview_urls[i] = string([]byte(title)[idx_pair[0]:idx_pair[1]])
				}
				docentry_new_webpreviews := make([]previewbuilder.URLPreview, 0)
				for _,url_for_preview := range preview_urls {
					preview_build, err := previewbuilder.URLPreview{}.New(url_for_preview)
					if err == nil {
						docentry_new_webpreviews = append(docentry_new_webpreviews, preview_build)
					}
				}


				date_now_utc := time.Now().UTC()
				date_str := date_now_utc.Format(http.TimeFormat)
				if len(files) == 0 {
					if title == "" {
						return false
					}
					profile := get_data(sub)
					// defer func() {set_data(profile, sub)} ()
					data := profile.Posts
					data = append(data, post{DocID:get_data_new_id(&data),Title:template.HTML(title),Type:doctype_mesage, Date: date_str, Webpreview: docentry_new_webpreviews})
					profile.Posts = data
					set_data(profile, sub)
				} else {
					profile := get_data(sub)
					doc_id := get_data_new_id(&profile.Posts)
					defer func() {set_data(profile, sub)} ()
					docentry_new_files := make([]docentry_file, 0)
					new_data := post{DocID:doc_id,Title:template.HTML(title),Type:doctype_file,Date: date_str, Files: docentry_new_files, Webpreview: docentry_new_webpreviews}
					for _, file := range files {
						basename := fmt.Sprintf("%d__%d__%s", doc_id, date_now_utc.UnixMilli(), rand_seq(8)) + path.Ext(file.Filename)
						filename := DATA_BASE_PATH + "/uploads/"+ sub +"/" + basename
						fmt.Println(filename)
						fmt.Println(date_str)
						fmt.Println(time.Parse(http.TimeFormat, date_str))
						// TODO: error handling if first file is uploaded but later are failing
						if err := c.SaveUploadedFile(file, filename); err != nil {
							c.String(http.StatusBadRequest, "upload file err: %s", err.Error())
							return false
						}
						docentry_new_files = append(docentry_new_files, docentry_file{Url: "/media/"+basename, OrgName: path.Base(file.Filename), Name: basename})
						new_data.Files = docentry_new_files
					}
					profile.Posts = append(profile.Posts, new_data)
				}
				return true
			} (c)
			if ret {
				render_posts_to_html(c)
			} else {
				c.String(http.StatusBadRequest, "Empty message!\n")
				render_posts_to_html(c) // just resend the whole section, otherwise the upload progress bar must be changed
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
			profile := get_data(sub)
			to_drop := -1
			for i, e := range profile.Posts {
				if e.DocID == id {
					to_drop = i
					break
				}
			}
			if to_drop == -1 {
				c.String(http.StatusBadRequest, fmt.Sprintf("ERROR! No such ID:%d!", id))
			} else {
				for _,f := range profile.Posts[to_drop].Files {
					if strings.HasPrefix(f.Url, "/media/") {
						basename := strings.TrimPrefix(f.Url, "/media/")
						os.Remove(DATA_BASE_PATH +"/uploads/"+sub+"/"+basename)
					}
				}
				profile.Posts = slices.Delete(profile.Posts,to_drop,to_drop+1)
				set_data(profile, sub)

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
			profile := get_data(sub)
			toggle_star := profile.find_post_idx_by_id(id)
			if toggle_star == -1 {
				c.String(http.StatusBadRequest, fmt.Sprintf("ERROR! No such ID:%d!", id))
			} else {
				profile.Posts[toggle_star].Starred = ! profile.Posts[toggle_star].Starred
				set_data(profile, sub)
				c.Header("Content-Type", "text/html")
				answer := ""
				if profile.Posts[toggle_star].Starred {
					answer = "<div class=\"doc-entry-button-fav starred\"> <button hx-post=\"/tray/doc-star\" hx-vals='{\"id\":" + id_str + "}'hx-target=\"closest .doc-entry-button-fav\" hx-swap=\"outerHTML\">🌟</button> </div>"
				} else {
					answer = "<div class=\"doc-entry-button-fav\"> <button hx-post=\"/tray/doc-star\" hx-vals='{\"id\":" + id_str +"}'hx-target=\"closest .doc-entry-button-fav\" hx-swap=\"outerHTML\">🌟</button> </div>"
				}
				c.String(http.StatusOK, answer)
			}
		})
	}

	private := router.Group("/private", auth_handler.Ensure_loggedin())
	{
		private.GET("", func(c *gin.Context) {
			var name, email, sub, out string
			// login := c.GetString(oidcauth.AuthUserKey)
			login, err := auth_handler.GetUserID(c)
			if err != nil {
				login = "Error when getting UserID"
			}
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
