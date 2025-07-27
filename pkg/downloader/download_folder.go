package downloader

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func (f *File) printStructure(indent string) {
	if f.Type != TypeFolder {
		fmt.Printf("%s- %s (ID: %s)\n", indent, f.Name, f.ID)
		return
	}

	if f.Type == TypeFolder {
		fmt.Printf("%s- %s (ID: %s)\n", indent, f.Name, f.ID)
		for _, child := range f.Children {
			child.printStructure(indent + "  ")
		}
	}
}

func parseCurFolder(url, content string) (*File, []ChildInfo, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	encodedData := ""
	doc.Find("script").EachWithBreak(func(i int, s *goquery.Selection) bool {
		innerHTML := s.Text()
		if strings.Contains(innerHTML, "_DRIVE_ivd") {
			re := regexp.MustCompile(`'((?:[^'\\]|\\.)*)'`)
			matches := re.FindAllStringSubmatch(innerHTML, -1)
			if len(matches) > 1 {
				encodedData = matches[1][1]
				return false
			}
		}
		return true
	})

	if encodedData == "" {
		return nil, nil, fmt.Errorf("could not find folder's encoded JS string")
	}

	encodedData = strings.ReplaceAll(encodedData, `\/`, `/`)
	encodedData = strings.ReplaceAll(encodedData, `\\`, `\`)

	decoded, err := strconv.Unquote(`"` + encodedData + `"`)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unquote encoded data: %w", err)
	}

	var folderArr []interface{}
	if err := json.Unmarshal([]byte(decoded), &folderArr); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal folder JSON: %w", err)
	}

	title := doc.Find("title").First().Text()
	parts := strings.Split(title, " - ")
	if len(parts) < 2 {
		return nil, nil, fmt.Errorf("cannot extract folder name from title: %s", title)
	}
	folderName := strings.Join(parts[:len(parts)-1], " - ")

	urlParts := strings.Split(strings.Trim(url, "/"), "/")
	folderID := urlParts[len(urlParts)-1]

	rootFolder := &File{
		ID:       folderID,
		Name:     folderName,
		Type:     TypeFolder,
		Children: []*File{},
	}

	var children []ChildInfo
	if len(folderArr) > 0 && folderArr[0] != nil {
		folderContents, ok := folderArr[0].([]any)
		if !ok {
			return nil, nil, fmt.Errorf("invalid folder contents format")
		}

		for _, itemRaw := range folderContents {
			item, ok := itemRaw.([]any)
			if !ok || len(item) < 4 {
				continue
			}
			id, idOk := item[0].(string)
			name, nameOk := item[2].(string)
			typ, typOk := item[3].(string)
			if idOk && nameOk && typOk {
				children = append(children, ChildInfo{ID: id, Name: name, Type: typ})
			}
		}
	}

	return rootFolder, children, nil
}

func parseFolder(client *http.Client, url string) (*File, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgentFolder)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed for %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status for %s: %s", url, resp.Status)
	}

	contentBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	rootFolder, children, err := parseCurFolder(url, string(contentBytes))
	if err != nil {
		return nil, err
	}

	for _, childInfo := range children {
		if childInfo.Type != TypeFolder {
			rootFolder.Children = append(rootFolder.Children, &File{
				ID:   childInfo.ID,
				Name: childInfo.Name,
				Type: childInfo.Type,
			})
		} else {
			childURL := "https://drive.google.com/drive/folders/" + childInfo.ID
			childTree, err := parseFolder(client, childURL)
			if err != nil {
				log.Printf("Warning: failed to process sub-folder %s: %v", childInfo.Name, err)
				continue
			}
			rootFolder.Children = append(rootFolder.Children, childTree)

		}
	}

	return rootFolder, nil
}

func treeToArray(node *File, curPath string) []FileToDownload {
	var array []FileToDownload

	for _, child := range node.Children {
		sanitizedName := strings.ReplaceAll(child.Name, string(filepath.Separator), "_")
		newPath := filepath.Join(curPath, sanitizedName)

		if child.Type == TypeFolder {
			array = append(array, FileToDownload{ID: "", Path: newPath})

			recursiveResult := treeToArray(child, newPath)
			array = append(array, recursiveResult...)
		} else {
			array = append(array, FileToDownload{ID: child.ID, Path: newPath})
		}
	}
	return array
}

func DownloadFolder(client *http.Client, opts DownloadOptions) (string, error) {
	if opts.Id == "" {
		return "", fmt.Errorf("folder ID is required for downloading a folder")
	}

	folderURL := "https://drive.google.com/drive/folders/" + opts.Id
	rootNode, err := parseFolder(client, folderURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse folder: %w", err)
	}

	rootNode.printStructure("")

	downloadList := treeToArray(rootNode, rootNode.Name)
	if len(downloadList) == 0 {
		return "", fmt.Errorf("no files found in the folder")
	}

	rootPath := opts.Output
	for _, file := range downloadList {
		if file.ID == "" {
			// This is a folder, we just create the directory
			if err := os.MkdirAll(file.Path, 0755); err != nil {
				return "", fmt.Errorf("failed to create directory %s: %w", file.Path, err)
			}
			continue
		}
		_, err := DownloadFile(client, DownloadOptions{
			Id:        file.ID,
			Output:    filepath.Join(rootPath, file.Path),
			UserAgent: opts.UserAgent,
		})
		if err != nil {
			fmt.Printf("Error: failed to download file %s: %v\n", file.Path, err)
			continue
		} else {
			// fmt.Printf("File downloaded successfully to: %s\n", finalPath)
		}
	}

	return rootPath, nil
}
