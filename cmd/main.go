package main

import (
	"flag" // For command-line argument parsing
	"fmt"
	"log" // For logging fatal errors
	"os"  // For os.Args[0] in the init function

	// Import your downloader package.
	// Replace "example.com/gdown-go" with your actual module path from go.mod
	"gdown-go/pkg/downloader"
)

func main() {
	// --- 1. Define command-line flags ---
	urlFlag := flag.String("url", "", "Google Drive file URL (e.g., https://drive.google.com/file/d/FILE_ID/view)")
	idFlag := flag.String("id", "", "Google Drive File ID (e.g., FILE_ID)")
	outputFlag := flag.String("o", "", "Output path for the downloaded file (defaults to filename from Content-Disposition)")
	proxyURLFlag := flag.String("proxy", "", "Proxy URL (e.g., http://user:pass@host:port). Empty for no proxy.")
	onlyLinkFlag := flag.Bool("only-link", false, "Only print the final download link without downloading the file")
	userAgentFlag := flag.String("user-agent", "", "Custom User-Agent header for HTTP requests (defaults to common desktop UA)")
	helpFlag := flag.Bool("help", false, "Show help message and exit")

	// Parse the command-line arguments
	flag.Parse()

	// --- 2. Handle help flag ---
	if *helpFlag {
		flag.Usage() // Prints the usage message defined in init()
		return       // Exit the program
	}

	// --- 3. Validate required and mutually exclusive arguments ---
	if (*urlFlag == "" && *idFlag == "") || (*urlFlag != "" && *idFlag != "") {
		fmt.Println("Error: You must specify either -url or -id, but not both.")
		flag.Usage()
		log.Fatal("Invalid arguments: URL or ID must be provided exclusively.") // Log and exit
	}

	// --- 4. Initialize GDownloader ---
	// Create a new GDownloader instance with the provided proxy URL
	gDownloader, err := downloader.NewGDownloader(*proxyURLFlag)
	if err != nil {
		log.Fatalf("Error initializing downloader: %v", err) // Log error and exit
	}

	// --- 5. Create DownloadOptions ---
	// Populate the DownloadOptions struct using the values from the parsed flags
	opts := downloader.NewDownloadOptions(
		*urlFlag,
		*idFlag,
		*outputFlag,
		*userAgentFlag,
		*onlyLinkFlag,
	)

	// --- 6. Call the DownloadFile method ---
	fmt.Println("Starting Google Drive download process...")
	finalPath, err := gDownloader.DownloadFile(opts) // Call the core download logic

	// --- 7. Handle results and errors from the download ---
	if err != nil {
		log.Fatalf("Download failed: %v", err) // Log any error and exit
	}

	// --- 8. Print success message based on 'onlyLink' flag ---
	if *onlyLinkFlag {
		fmt.Printf("Successfully retrieved final download link: %s\n", finalPath)
	} else {
		fmt.Printf("File downloaded successfully to: %s\n", finalPath)
	}
}

// init function is called before main(). Used to customize the default usage message.
func init() {
	flag.Usage = func() {
		fmt.Printf("Usage: %s [flags]\n", os.Args[0]) // os.Args[0] is the program's executable name
		fmt.Println("  A powerful command-line tool to download files from Google Drive.")
		fmt.Println("\nExamples:")
		fmt.Println("  Download by File ID:      gdown -id <your_file_id> -o my_download.zip")
		fmt.Println("  Download by Drive URL:    gdown -url \"https://drive.google.com/file/d/FILE_ID/view\" -o my_file.zip")
		fmt.Println("  Get final download link:  gdown -id <your_file_id> -only-link")
		fmt.Println("  Use a proxy:              gdown -id <file_id> -o file.zip -proxy http://localhost:8080")
		fmt.Println("\nFlags:")
		flag.PrintDefaults() // Prints the default usage for all defined flags
	}
}
