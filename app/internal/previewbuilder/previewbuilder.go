package previewbuilder
// package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"unicode"
	"unicode/utf8"

	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"

	readability "github.com/go-shiori/go-readability"
)

// INFO:
// - Extract base preview from go-readability
// - If no image -> get all images and get biggest image (with fitting dimensions to ignore banners)
// - Overwrite title for specific websites (i.e. Reddit)



var (
	urls = []string{
		// this one is article, so it's parse-able
		"https://www.nytimes.com/2019/02/20/climate/climate-national-security-threat.html",
		// while this one is not an article, so readability will fail to parse.
		// "https://www.nytimes.com/",
		// // "https://www.reddit.com/r/Finanzen/comments/1nhhohn/ich_habe_wieder_arbeit_bin_etwas_planlos_was_ich/", // only text
		// "https://www.reddit.com/r/Finanzen/comments/1nh93j6/rossmann_otto_und_mediamarkt_wollen_wero_f%C3%BCr/", // link with image
		// "https://www.reddit.com/r/Finanzen/comments/1nexlq1/wer_von_euch_war_das/", // only image
		// "https://www.reddit.com/r/Finanzen/comments/1n97ax3/was_machen_die_menschen_da_%C3%BCberhaupt/", // image and text
		// "https://www.reddit.com/r/videos/comments/1nh93sg/the_streaming_war_is_over_piracy_won/", // yt video
		"https://acrobat.adobe.com/id/urn:aaid:sc:EU:dee0f93c-e275-4f41-b90e-e8cfcb99c750",
		"https://www.amazon.de/Anker-Powerbank-20-000mAh-integriertem-High-Speed/dp/B0CZ9LH53B?crid=Z4E4WTR6FGBY&dib=eyJ2IjoiMSJ9.vqZyeIbbL_r-FygNTNL3v3EFIzJ9mZ4EDlFkXGwh7RBDIjHzgxVNFwF3VaacojUHQglx4GWzBaAFPmVW_RQwVXiEOViep9yZi-_B65FB4wdkANg6utolYsWvqG8eYs9TF19rbT6IgY-VAzW_Jz0XEr26OMrc8y1QSL1wJfaUS5RELAFWRgvKBt6beTDfVmBe8TIBBUHGvHJb5xZ4w27IYeSO4yaVxjJltxcEI9H7bkA.WEDs5Akuig7IHxYxnWK22EEpNeWel0eFM8XK_OfEnTo&dib_tag=se&keywords=power%2Bbank&qid=1746199580&sprefix=power%2Caps%2C122&sr=8-15&th=1",
		"https://www.amazon.de/Anker-Powerbank-20-000mAh-integriertem-High-Speed/dp/B0CZ9LH53B",
		"https://imgur.com/chucks-bad-day-CDXTSxi",
		"https://arxiv.org/pdf/2107.06751",
	}
)

func StringCleanup(s string, maxlength int) string {
	bytes := []byte(s)
	out_len := len(bytes)
	if maxlength != -1 && maxlength < out_len {
		out_len = maxlength
	}
    out := make([]rune, 0, out_len)

    for len(bytes) > 0 && len(out) < out_len {
        r, size := utf8.DecodeRune(bytes)

        switch {
        case r == utf8.RuneError && size == 1:
            // Invalid byte → skip
		case !unicode.IsPrint(r):
			// skip non-printable runes
        case unicode.IsControl(r) && r != '\n' && r != '\t':
            // Skip control chars except useful ones
        default:
            out = append(out, r)
        }

        bytes = bytes[size:]
    }

    return string(out)
}

type URLPreview struct {
	URL string
	Title string
	Description string
	Favicon string
	Domain string
	Image string
}
func (URLPreview) New(input_url string) (URLPreview, error) {
	urlpreview := URLPreview{}

	// 1. Get bases
	resp, err := http.Get(input_url)
	if err != nil {
		return urlpreview, errors.New("Parse failure")
	}
	defer resp.Body.Close()

	url_parsed, err := url.Parse(input_url)
	if err != nil {
		return urlpreview, errors.New("Parse failure")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return urlpreview, errors.New("Parse failure")
	}
	if len(body) > 10000 {
		body = body[:10000]
	}
	raw_html := string(body)
	resp.Body = io.NopCloser(bytes.NewBuffer([]byte(raw_html)))

	article, err := readability.FromReader(resp.Body, url_parsed)
	if err != nil {
		return urlpreview, errors.New("Parse failure")
	}
	urlpreview.URL = input_url
	urlpreview.Title = StringCleanup(article.Title, 100)
	urlpreview.Description = StringCleanup(article.Excerpt, 360)
	urlpreview.Favicon = article.Favicon
	urlpreview.Domain = url_parsed.Hostname()

	urlpreview.Image = article.Image


	// 2. Find alternative images
	if urlpreview.Image == "" {
		extracted_image_urls := getImagesInRawHTML(raw_html)
		// fmt.Println(extracted_image_urls)
		extracted_image_sizes := make(map[string][]int)
		// fmt.Println(extracted_image_sizes)
		max_size := 1
		max_index := -1
		for i,u := range extracted_image_urls {
			x,y,err := getImageSize(u)
			if err != nil {
				xy := []int{0,0}
				extracted_image_sizes[u] = xy
			} else if xy_ratio := float64(x)/float64(y); xy_ratio > 8 || xy_ratio < 1.0/8.0 {
				xy := []int{0,0}
				extracted_image_sizes[u] = xy
			} else {
				extracted_image_sizes[u] = []int{x,y}
			}
			if pixels := extracted_image_sizes[u][0] * extracted_image_sizes[u][1]; pixels > max_size {
				max_size = pixels
				max_index = i
			}
		}

		if max_index >= 0 {
			urlpreview.Image = extracted_image_urls[max_index]
		}
	}
	if urlpreview.Image == "" && urlpreview.Favicon != "" || urlpreview.Image != "" && !checkIfOnline(urlpreview.Image) {
		urlpreview.Image = urlpreview.Favicon
	}


	// 3. Replace some titles
	switch urlpreview.Domain {
	case "www.reddit.com":
		reddit_title_regex := regexp.MustCompile(`<shreddit-title title="([^"]+)">`)
		returned_matches := reddit_title_regex.FindAllStringSubmatch(raw_html, -1)
		if len(returned_matches) >= 1 && len(returned_matches[0]) >=2 && returned_matches[0][1] != "" {
			urlpreview.Title = returned_matches[0][1]
		} else {
			f, err := os.Create(input_url)
			if err != nil {
			    panic(err)
			}
			defer f.Close()
			f.WriteString(raw_html)
			f.Sync()
		}
	}

	return urlpreview, nil
}

func (up URLPreview) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("URL         : %s\n", up.URL))
	sb.WriteString(fmt.Sprintf("Domain      : %s\n", up.Domain))
	sb.WriteString(fmt.Sprintf("Title       : %s\n", up.Title))
	sb.WriteString(fmt.Sprintf("Description : %s\n", up.Description))
	sb.WriteString(fmt.Sprintf("Favicon     : %s\n", up.Favicon))
	sb.WriteString(fmt.Sprintf("Image       : %s\n", up.Image))
	return sb.String()
}

func checkIfOnline(url string) bool {
	// fmt.Println(url)
	client := http.Client{
		Timeout: 500 * time.Millisecond,
	}
	resp, err := client.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func removeDuplicateStr(str_slice []string) []string {
	all_keys := make(map[string]bool)
	list := []string{}
	for _, item := range str_slice {
		if _, value := all_keys[item]; !value {
			all_keys[item] = true
			list = append(list, item)
		}
	}
	return list
}

func getImagesInRawHTML(html string) []string {
	// TODO: drop dublicates
	var image_regexp = regexp.MustCompile(`<img[^>]*src="([^"]+)"[^>]*>`)
	returns := make([]string, 0)

	matches := image_regexp.FindAllStringSubmatch(html, -1)
	for _,m := range matches {
		// the length indicates the number of capture group matches: #0=actual match #1...= capture groups
		if len(m) == 2 {
			returns = append(returns, m[1])
		}
	}

	return removeDuplicateStr(returns)
}


func getImageSize(url string) (int, int, error) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, 0, err
	}

	defer resp.Body.Close()

	// DecodeConfig only reads enough to determine format + dimensions
	cfg, _, err := image.DecodeConfig(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}







func main() {

	for _, url := range urls {
		preview, err := URLPreview{}.New(url)
		if err!= nil {
			panic(err)
		}
		fmt.Println(preview)
	}

}
