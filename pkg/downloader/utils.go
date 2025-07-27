package downloader

import (
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

const (
	userAgent       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.95 Safari/537.36"
	userAgentFolder = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.7204.159 Safari/537.36"
)

type DownloadOptions struct {
	Id        string
	Output    string
	UserAgent string
}

const (
	TypeFolder = "application/vnd.google-apps.folder"
)

type File struct {
	ID       string
	Name     string
	Type     string
	Children []*File
}

type ChildInfo struct {
	ID   string
	Name string
	Type string
}

type FileToDownload struct {
	ID   string // ID is empty for directories
	Path string // The relative local path
}

func Download(client *http.Client, opts DownloadOptions) (string, error) {
	if opts.Id == "" {
		return "", fmt.Errorf("file ID is required")
	}
	url := "https://drive.google.com/drive/folders/" + opts.Id
	if opts.UserAgent == "" {
		opts.UserAgent = userAgentFolder
	}
	resp, err := client.Head(url)
	if err != nil {
		return "", fmt.Errorf("failed to make HEAD request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		opts.UserAgent = userAgent
		return DownloadFile(client, opts)
	} else {
		return DownloadFolder(client, opts)
	}

}

func CreateHTTPClient(proxyURL string) (*http.Client, error) {
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

	client := &http.Client{
		Jar:       jar,
		Transport: transport,
	}
	return client, nil
}

func IsGoogleDriveURL(u *url.URL) bool {
	return u.Host == "drive.google.com" || u.Host == "docs.google.com"
}

func ParseURL(urlStr string, warning bool) (string, bool, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", false, fmt.Errorf("failed to parse URL: %w", err)
	}

	if !IsGoogleDriveURL(parsedURL) {
		return "", false, nil
	}

	isDownloadLink := strings.HasSuffix(parsedURL.Path, "/uc")
	fileID := parsedURL.Query().Get("id")

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

	if warning && fileID != "" && !isDownloadLink {
		downloadURL := fmt.Sprintf("https://drive.google.com/uc?id=%s", fileID)
		log.Printf(
			"You specified a Google Drive link that is not the correct link to download a file. You might want to try the following url: %s",
			downloadURL,
		)
	}

	return fileID, isDownloadLink, nil
}
