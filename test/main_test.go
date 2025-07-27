package test

import (
	"testing"

	"github.com/striiike/gdown-go/pkg/downloader"
)

func TestFolder(t *testing.T) {
	httpClient, err := downloader.CreateHTTPClient("")
	if err != nil {
		t.Fatalf("Error initializing HTTP client: %v", err)
	}

	// folder
	id := `1xbqdBIxY-pDCz58YnlMmIGsJ8M2hAFLk`
	opts := downloader.DownloadOptions{
		Id:        id,
		Output:    "",
		UserAgent: "",
	}

	_, err = downloader.Download(httpClient, opts)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

}

func TestFile(t *testing.T) {
	httpClient, err := downloader.CreateHTTPClient("")
	if err != nil {
		t.Fatalf("Error initializing HTTP client: %v", err)
	}

	// big file
	id := `1zJEE9YzgngFAbomJJHGBYvd3UMd9NPYV`
	opts := downloader.DownloadOptions{
		Id:        id,
		Output:    "",
		UserAgent: "",
	}
	_, err = downloader.Download(httpClient, opts)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

}
