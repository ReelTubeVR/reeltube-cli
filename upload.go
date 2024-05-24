package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cheggaaa/pb/v3"
	"github.com/spf13/cobra"
)

const (
	defaultConcurrency = 5 // Default number of concurrent uploads
)

func init() {
	rootCmd.AddCommand(uploadCmd)
}

var (
	concurrency int
)

var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload a media file to ReelTube",
	Run: func(cmd *cobra.Command, args []string) {
		// Allowed file extensions and MIME types for video and photo files
		var allowedExtensions = map[string]bool{
			".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
			".mp4": true, ".mov": true, ".avi": true, ".mkv": true,
		}
		var allowedMIMEs = map[string]bool{
			"image/jpeg": true, "image/png": true, "image/gif": true,
			"video/mp4": true, "video/quicktime": true, "video/x-msvideo": true, "video/x-matroska": true,
		}

		filePath, _ := cmd.Flags().GetString("file")
		uploadName, _ := cmd.Flags().GetString("name")

		if filePath == "" {
			fmt.Fprintln(os.Stderr, "Error: file path is required")
			os.Exit(1)
		}

		// Ensure the file path is absolute
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid file path: %v\n", err)
			os.Exit(1)
		}

		// Check if the file exists
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: file does not exist: %s\n", absPath)
			os.Exit(1)
		}

		// Check the file extension
		ext := strings.ToLower(filepath.Ext(absPath))
		if !allowedExtensions[ext] {
			fmt.Fprintf(os.Stderr, "Error: file type not allowed: %s\n", ext)
			os.Exit(1)
		}

		// Open the file to check the MIME type
		file, err := os.Open(absPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: unable to open file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()

		// Check the MIME type
		buffer := make([]byte, 512)
		_, err = file.Read(buffer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: unable to read file: %v\n", err)
			os.Exit(1)
		}
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			mimeType = http.DetectContentType(buffer)
		}
		if !allowedMIMEs[mimeType] {
			fmt.Fprintf(os.Stderr, "Error: MIME type not allowed: %s\n", mimeType)
			os.Exit(1)
		}

		fmt.Printf("File to upload: %s\n", absPath)
		if uploadName != "" {
			fmt.Printf("Upload name override: %s\n", uploadName)
		} else {
			uploadName = filepath.Base(absPath)
		}

		// Logic to handle file upload to ReelTube

		err = multipartUpload(absPath, uploadName)
		if err != nil {
			fmt.Println("Error uploading file:", err)
			os.Exit(1)
		}
		fmt.Println("File uploaded successfully")
	},
}

type Part struct {
	PartNumber int             `json:"part_number"`
	ETag       json.RawMessage `json:"etag"`
}

func systemConcurrency() int {
	numCores := runtime.NumCPU()
	if numCores > 1 {
		return numCores
	}
	return defaultConcurrency
}

func multipartUpload(filePath, fileName string) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	fileSize := fileInfo.Size()
	concurrency := systemConcurrency()

	// Step 1: Get presigned URLs from API
	createMediaUploadResp, err := createMediaUpload(fileName, int(fileSize))
	if err != nil {
		return fmt.Errorf("failed to get presigned URLs: %w", err)
	}

	mediaUpload := createMediaUploadResp.MediaUpload
	partSize := createMediaUploadResp.PartSize
	numParts := createMediaUploadResp.NumParts
	uploadID := createMediaUploadResp.UploadID
	presignedURLs := createMediaUploadResp.PresignedURLs

	// Step 2: Upload each part using the presigned URLs with a worker pool
	var wg sync.WaitGroup
	errChan := make(chan error, len(presignedURLs))
	parts := make([]Part, len(presignedURLs))

	jobs := make(chan int, numParts)

	// Initialize progress bar
	bar := pb.StartNew(numParts)
	startTime := time.Now()

	// Worker pool
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for partNum := range jobs {
				url := presignedURLs[partNum]
				partOffset := partNum * partSize
				buffer := make([]byte, partSize)

				// Open a new file descriptor for each goroutine
				file, err := os.Open(filePath)
				if err != nil {
					errChan <- fmt.Errorf("failed to open file: %w", err)
					return
				}

				_, err = file.Seek(int64(partOffset), io.SeekStart)
				if err != nil {
					file.Close()
					errChan <- fmt.Errorf("failed to seek file for part %d: %w", partNum, err)
					return
				}

				// Adjust read size for the last part
				readSize := partSize
				if partOffset+partSize > int(fileSize) {
					readSize = int(fileSize) - partOffset
				}

				n, err := file.Read(buffer[:readSize])
				if err != nil && err != io.EOF {
					file.Close()
					errChan <- fmt.Errorf("failed to read file for part %d: %w", partNum, err)
					return
				}
				file.Close()

				req, err := http.NewRequest("PUT", url, bytes.NewReader(buffer[:n]))
				if err != nil {
					errChan <- fmt.Errorf("failed to create PUT request for part %d: %w", partNum, err)
					return
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					errChan <- fmt.Errorf("failed to upload part %d: %w", partNum, err)
					return
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errChan <- fmt.Errorf("failed to upload part %d: received non-200 status code %d", partNum, resp.StatusCode)
					return
				}

				parts[partNum] = Part{
					PartNumber: partNum + 1,
					ETag:       json.RawMessage(resp.Header.Get("ETag")),
				}

				// Update progress bar
				bar.Increment()
				elapsed := time.Since(startTime)
				remaining := time.Duration((numParts-partNum-1)*int(elapsed.Seconds()/float64(partNum+1))) * time.Second
				bar.Set("remaining", fmt.Sprintf("ETA: %s", remaining))
			}
		}()
	}

	// Send jobs to the worker pool
	for i := 0; i < numParts; i++ {
		jobs <- i
	}
	close(jobs)

	wg.Wait()
	close(errChan)

	bar.Finish()

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	// Step 3: Complete multipart upload
	err = completeMultipartUpload(mediaUpload.ID, uploadID, parts)
	if err != nil {
		return fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	return nil
}

func createMediaUpload(fileName string, fileSize int) (*CreateMediaUploadResponse, error) {
	presignedURLResp, err := client.CreateMediaUpload(fileName, fileSize)
	if err != nil {
		return nil, fmt.Errorf("failed to get presigned URLs from API: %w", err)
	}

	return presignedURLResp, nil
}

func completeMultipartUpload(mediaUploadId, uploadID string, parts []Part) error {
	err := client.CompleteMultipartUpload(mediaUploadId, uploadID, parts)
	if err != nil {
		return fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	return nil
}
