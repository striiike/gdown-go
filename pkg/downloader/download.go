package downloader

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/schollz/progressbar/v3"
)

func getRedirectURL(contents string) (string, error) {
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
		form.Find("input[type=g'hidden']").Each(func(i int, s *goquery.Selection) {
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

func DownloadFile(client *http.Client, opts DownloadOptions) (string, error) {
	if opts.Id == "" {
		return "", fmt.Errorf("must specify an ID in DownloadOptions")
	}

	if opts.UserAgent == "" {
		opts.UserAgent = userAgent
	}

	currentURL := "https://drive.google.com/uc?id=" + opts.Id
	var resp *http.Response
	for {
		req, err := http.NewRequest("GET", currentURL, nil)
		if err != nil {
			return "", fmt.Errorf("failed to download file: %w", err)
		}

		req.Header.Set("User-Agent", opts.UserAgent)

		resp, err = client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to make request: %w", err)
		}
		defer resp.Body.Close()

		if currentURL == "https://drive.google.com/uc?id="+opts.Id &&
			resp.StatusCode == http.StatusInternalServerError {
			return "", fmt.Errorf("google docs sheet pptx map drawio etc not supported")
		}

		// if resp.Header has Content-Disposition, it means it is the file
		contentDisposition := resp.Header.Get("Content-Disposition")
		if contentDisposition != "" {
			break
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response body: %w", err)
		}

		contents := string(bodyBytes)

		currentURL, err = getRedirectURL(contents)
		if err != nil {
			return "", fmt.Errorf("failed to get redirect URL: %w", err)
		}
	}

	finalPath := opts.Output
	if finalPath == "" {
		finalPath = getFilenameFromResponse(resp)
	}
	tmpPath := finalPath + ".tmp"

	var startSize int64 = 0

	req, err := http.NewRequest("GET", currentURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", opts.UserAgent)
	if startSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startSize))
	}

	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}

	totalSize := resp.ContentLength + startSize
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
	bar.Add64(startSize)

	_, err = io.Copy(io.MultiWriter(file, bar), resp.Body)
	if err != nil {
		return "", err
	}

	if err = file.Close(); err != nil {
		return "", err
	}

	return finalPath, os.Rename(tmpPath, finalPath)
}

func getFilenameFromResponse(resp *http.Response) string {
	cd := resp.Header.Get("Content-Disposition")
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
