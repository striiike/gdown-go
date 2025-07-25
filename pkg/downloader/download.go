package downloader

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/schollz/progressbar/v3"
)

const (
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.95 Safari/537.36"
)

type GDownloader struct {
	client *http.Client
}

type DownloadOptions struct {
	url       string
	id        string
	output    string
	userAgent string
	onlyLink  bool
}

func NewDownloadOptions(url, id, output, userAgent string, onlyLink bool) DownloadOptions {
	return DownloadOptions{
		url:       url,
		id:        id,
		output:    output,
		userAgent: userAgent,
		onlyLink:  onlyLink,
	}
}

func GetRedirectURL(contents string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(contents))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// try to find the download form first
	form := doc.Find("form#download-form")
	if form.Length() > 0 {
		action, exists := form.Attr("action")
		if !exists {
			return "", fmt.Errorf("form action not found in the document")
		}

		baseURL, err := url.Parse(action)
		if err != nil {
			return "", fmt.Errorf("failed to parse form action URL: %w", err)
		}

		values := baseURL.Query()
		form.Find("input[type='hidden']").Each(func(i int, s *goquery.Selection) {
			name, _ := s.Attr("name")
			value, _ := s.Attr("value")
			if name != "" {
				values.Set(name, value)
			}
		})
		baseURL.RawQuery = values.Encode()

		return baseURL.String(), nil
	}

	reHref := regexp.MustCompile(`href="(\/uc\?export=download[^"]+)"`)
	if match := reHref.FindStringSubmatch(contents); len(match) > 1 {
		foundURL := "https://docs.google.com" + strings.ReplaceAll(match[1], "&amp;", "&")
		return foundURL, nil
	}

	reDownloadURL := regexp.MustCompile(`"downloadUrl":"([^"]+)"`)
	if match := reDownloadURL.FindStringSubmatch(contents); len(match) > 1 {
		var foundURL string
		rawURL := `"` + match[1] + `"` // Add quotes to make it a valid JSON string
		if err := json.Unmarshal([]byte(rawURL), &foundURL); err == nil {
			return foundURL, nil
		}
		return strings.NewReplacer(`\u003d`, `=`, `\u0026`, `&`).Replace(match[1]), nil
	}

	errorCaption := doc.Find("p.uc-error-subcaption").Text()
	if errorCaption != "" {
		return "", errors.New(errorCaption)
	}

	// no redirect link found
	return "", fmt.Errorf(
		"try again later or use a different link with different access",
	)
}

func (g *GDownloader) DownloadFile(opts DownloadOptions) (string, error) {
	if (opts.id == "") == (opts.url == "") {
		return "", fmt.Errorf("must specify either URL or ID")
	}

	// var isDirectLink bool
	if opts.id != "" {
		opts.url = "https://drive.google.com/uc?id=" + opts.id
		// isDirectLink = true
	}

	if opts.url != "" {
		id, _, err := ParseURL(opts.url, true)
		if err != nil {
			return "", fmt.Errorf("failed to parse URL: %w", err)
		}
		opts.id = id
	}

	if opts.userAgent == "" {
		opts.userAgent = userAgent
	}

	currentURL := opts.url
	var res *http.Response
	for {
		req, err := http.NewRequest("GET", currentURL, nil)
		if err != nil {
			return "", fmt.Errorf("failed to download file: %w", err)
		}

		req.Header.Set("User-Agent", opts.userAgent)

		res, err = g.client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to make request: %w", err)
		}
		defer res.Body.Close()

		// fmt.Println(res.Header)

		contentType := res.Header.Get("Content-Type")
		if strings.Contains(contentType, "text/html") {
			// doc, err := goquery.NewDocumentFromReader(res.Body)
			// if err != nil {
			// 	return "", err
			// }

			// form := doc.Find("#download-form")
			// if action, exists := form.Attr("action"); exists {
			// 	currentURL = "https://docs.google.com" + action
			// 	continue
			// }

			// return "", fmt.Errorf("can't retrieve download link from confirmation page")
		}

		// if res.Header has Content-Disposition, it means it is the file
		contentDisposition := res.Header.Get("Content-Disposition")
		if contentDisposition != "" && strings.Contains(contentDisposition, "attachment") {
			break
		}

		bodyBytes, err := io.ReadAll(res.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response body: %w", err)
		}

		contents := string(bodyBytes)

		// Needs to redirect to a download link
		currentURL, err = GetRedirectURL(contents)
		if err != nil {
			return "", fmt.Errorf("failed to get redirect URL: %w", err)
		}
	}

	// if it is onlyLink request, return the current URL
	if opts.onlyLink {
		return currentURL, nil
	}

	finalPath := opts.output
	if finalPath == "" {
		finalPath = getFilenameFromResponse(res)
	}
	tmpPath := finalPath + ".tmp"

	var startSize int64
	startSize = 0

	req, err := http.NewRequest("GET", currentURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", opts.userAgent)
	if startSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startSize))
	}

	res, err = g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}

	// visualization
	totalSize := res.ContentLength + startSize
	bar := progressbar.NewOptions64(
		totalSize,
		progressbar.OptionSetDescription(filepath.Base(finalPath)),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(15),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() { fmt.Fprint(os.Stderr, "\n") }),
	)
	bar.Add64(startSize) // For resuming

	_, err = io.Copy(io.MultiWriter(file, bar), res.Body)
	if err != nil {
		return "", err
	}

	if err = file.Close(); err != nil {
		return "", err
	}

	fmt.Println("File downloaded successfully")

	return finalPath, os.Rename(tmpPath, finalPath)
}

func NewGDownloader(proxyURL string) (*GDownloader, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{}
	if proxyURL != "" {
		p, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid url for proxy: %w", err)
		}
		transport.Proxy = http.ProxyURL(p)
	}

	return &GDownloader{
		client: &http.Client{
			Jar:       jar,
			Transport: transport,
		},
	}, nil

}

func isGoogleDriveURL(u *url.URL) bool {
	return u.Host == "drive.google.com" || u.Host == "docs.google.com"
}

// returns the file ID and whether the URL is a direct download link.
func ParseURL(urlStr string, warning bool) (string, bool, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", false, fmt.Errorf("failed to parse URL: %w", err)
	}

	if !isGoogleDriveURL(parsedURL) {
		return "", false, nil
	}

	isDownloadLink := strings.HasSuffix(parsedURL.Path, "/uc")

	// Try to get file ID from the 'id' query parameter.
	fileID := parsedURL.Query().Get("id")

	// If not found in the query, try to extract it from the path using regex.
	if fileID == "" {
		patterns := []*regexp.Regexp{
			regexp.MustCompile(`^/file/d/(.*?)/(?:edit|view)$`),
			regexp.MustCompile(`^/file/u/\d+/d/(.*?)/(?:edit|view)$`),
			regexp.MustCompile(`^/document/d/(.*?)/(?:edit|htmlview|view)$`),
			regexp.MustCompile(`^/document/u/\d+/d/(.*?)/(?:edit|htmlview|view)$`),
			regexp.MustCompile(`^/presentation/d/(.*?)/(?:edit|htmlview|view)$`),
			regexp.MustCompile(`^/presentation/u/\d+/d/(.*?)/(?:edit|htmlview|view)$`),
			regexp.MustCompile(`^/spreadsheets/d/(.*?)/(?:edit|htmlview|view)$`),
			regexp.MustCompile(`^/spreadsheets/u/\d+/d/(.*?)/(?:edit|htmlview|view)$`),
		}

		for _, pattern := range patterns {
			match := pattern.FindStringSubmatch(parsedURL.Path)
			if len(match) > 1 {
				fileID = match[1]
				break
			}
		}
	}

	// If a warning is requested, a file ID was found, and it's not a direct download link, log a message.
	if warning && fileID != "" && !isDownloadLink {
		downloadURL := fmt.Sprintf("https://drive.google.com/uc?id=%s", fileID)
		log.Printf(
			"You specified a Google Drive link that is not the correct link to download a file. You might want to try the following url: %s",
			downloadURL,
		)
	}

	return fileID, isDownloadLink, nil
}

func getFilenameFromResponse(res *http.Response) string {
	cd := res.Header.Get("Content-Disposition")
	if cd == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return ""
	}

	if filename, ok := params["filename*"]; ok {
		if strings.HasPrefix(filename, "UTF-8''") {
			decoded, err := url.QueryUnescape(filename[7:])
			if err == nil {
				return decoded
			}
		}
	}
	return params["filename"]
}
