package previewbuilder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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
	"net/netip"
	"net/url"

	readability "github.com/go-shiori/go-readability"
	htmlparser "golang.org/x/net/html"
)

const (
	previewRequestTimeout       = 5 * time.Second
	maxPreviewBodyBytes         = 500000
	maxImageConfigBytes         = 1024 * 1024
	maxFallbackImageCandidates  = 32
	fallbackImageTimeout        = 4 * time.Second
	maxFallbackImageAspectRatio = 4.0
)

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

var previewHTTPClient = &http.Client{
	Timeout: previewRequestTimeout,
	Transport: &http.Transport{
		DialContext: dialPublicContext,
	},
	CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return validatePublicURL(request.URL)
	},
}

func isPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func validatePublicURL(parsedURL *url.URL) error {
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return errors.New("URL must use HTTP or HTTPS")
	}
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return errors.New("URL has no host")
	}
	if ip := net.ParseIP(hostname); ip != nil && !isPublicIP(ip) {
		return errors.New("URL host is not public")
	}
	return nil
}

func dialPublicContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{}
	var lastErr error
	for _, resolvedAddress := range resolved {
		if !isPublicIP(resolvedAddress.IP) {
			continue
		}
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolvedAddress.IP.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("URL host %q has no public address", host)
}

func fetchPublicURL(ctx context.Context, method, rawURL string) (*http.Response, *url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	if err := validatePublicURL(parsedURL); err != nil {
		return nil, nil, err
	}

	request, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	response, err := previewHTTPClient.Do(request)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		response.Body.Close()
		return nil, nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	if response.Request == nil || response.Request.URL == nil {
		response.Body.Close()
		return nil, nil, errors.New("response has no final URL")
	}

	return response, response.Request.URL, nil
}

func resolvePreviewURL(baseURL *url.URL, rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	resolvedURL := baseURL.ResolveReference(parsedURL)
	if err := validatePublicURL(resolvedURL); err != nil {
		return ""
	}
	return resolvedURL.String()
}

func fallbackImageURLs(rawHTML string, baseURL *url.URL) []string {
	document, err := htmlparser.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return nil
	}

	imageURLs := make([]string, 0, maxFallbackImageCandidates)
	seenURLs := make(map[string]bool)
	var visit func(*htmlparser.Node)
	visit = func(node *htmlparser.Node) {
		if len(imageURLs) >= maxFallbackImageCandidates {
			return
		}
		if node.Type == htmlparser.ElementNode && strings.EqualFold(node.Data, "img") {
			for _, attribute := range node.Attr {
				if !strings.EqualFold(attribute.Key, "src") {
					continue
				}
				imageURL := resolvePreviewURL(baseURL, attribute.Val)
				if imageURL != "" && !seenURLs[imageURL] {
					seenURLs[imageURL] = true
					imageURLs = append(imageURLs, imageURL)
				}
				break
			}
		}
		for child := node.FirstChild; child != nil && len(imageURLs) < maxFallbackImageCandidates; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	return imageURLs
}

func isSuitableFallbackImage(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	aspectRatio := float64(width) / float64(height)
	return aspectRatio <= maxFallbackImageAspectRatio && aspectRatio >= 1/maxFallbackImageAspectRatio
}

func getImageSize(ctx context.Context, imageURL string) (int, int, error) {
	response, _, err := fetchPublicURL(ctx, http.MethodGet, imageURL)
	if err != nil {
		return 0, 0, err
	}
	defer response.Body.Close()

	config, _, err := image.DecodeConfig(io.LimitReader(response.Body, maxImageConfigBytes))
	if err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}

func findFallbackImage(rawHTML string, baseURL *url.URL) string {
	ctx, cancel := context.WithTimeout(context.Background(), fallbackImageTimeout)
	defer cancel()

	bestImageURL := ""
	bestPixels := int64(0)
	for _, imageURL := range fallbackImageURLs(rawHTML, baseURL) {
		width, height, err := getImageSize(ctx, imageURL)
		if err != nil || !isSuitableFallbackImage(width, height) {
			continue
		}
		if pixels := int64(width) * int64(height); pixels > bestPixels {
			bestImageURL = imageURL
			bestPixels = pixels
		}
	}
	return bestImageURL
}

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
	resp, url_parsed, err := fetchPublicURL(context.Background(), http.MethodGet, input_url)
	if err != nil {
		return urlpreview, errors.New("Parse failure")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPreviewBodyBytes+1))
	if err != nil {
		return urlpreview, errors.New("Parse failure")
	}
	if len(body) > maxPreviewBodyBytes {
		body = body[:maxPreviewBodyBytes]
	}
	raw_html := string(body)

	article, err := readability.FromReader(bytes.NewReader(body), url_parsed)
	if err != nil {
		return urlpreview, errors.New("Parse failure")
	}
	urlpreview.URL = url_parsed.String()
	urlpreview.Title = StringCleanup(article.Title, 200)
	urlpreview.Description = StringCleanup(article.Excerpt, 500)
	urlpreview.Favicon = resolvePreviewURL(url_parsed, article.Favicon)
	urlpreview.Domain = url_parsed.Hostname()
	urlpreview.Image = resolvePreviewURL(url_parsed, article.Image)
	if urlpreview.Image == "" {
		urlpreview.Image = findFallbackImage(raw_html, url_parsed)
	}
	if urlpreview.Image == "" {
		urlpreview.Image = urlpreview.Favicon
	}


	// 3. Replace some titles
	switch urlpreview.Domain {
	case "www.reddit.com":
		reddit_title_regex := regexp.MustCompile(`<shreddit-title title="([^"]+)">`)
		returned_matches := reddit_title_regex.FindAllStringSubmatch(raw_html, -1)
		if len(returned_matches) >= 1 && len(returned_matches[0]) >=2 && returned_matches[0][1] != "" {
			urlpreview.Title = returned_matches[0][1]
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
