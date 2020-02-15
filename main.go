// Copyright © 2020 Siftrics
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh/terminal"
)

type SightRequest struct {
	Files []SightRequestFile
}

type SightRequestFile struct {
	MimeType   string
	Base64File string
}

type RecognizedPage struct {
	Error               string
	FileIndex           int
	PageNumber          int
	NumberOfPagesInFile int
	RecognizedText      []RecognizedText
}

type RecognizedText struct {
	Text                                                 string
	TopLeftX, TopLeftY, TopRightX, TopRightY             int
	BottomLeftX, BottomLeftY, BottomRightX, BottomRightY int
	Confidence                                           float64
}

type Client struct {
	apiKey string
}

func NewClient(apiKey string) *Client {
	return &Client{apiKey: apiKey}
}

// Recognize uses the Sight API to recognize all the text in the given files.
//
// If err != nil, then ioutil.ReadAll failed on a given file, a MIME type was
// failed to be inferred from the suffix (extension) of a given filename, or
// there was an error with the _initial_ HTTP request or response.
//
// This function blocks until receiving a response for the _initial_ HTTP request
// to the Sight API, so that non-200 responses for the initial request are conveyed
// via the returned error. All remaining work, including any additional network
// requests, is done in a separate goroutine. Accordingly, to avoid the blocking
// nature of the initial network request, this function must be run in a separate
// goroutine.
func (c *Client) Recognize(filePaths ...string) (<-chan RecognizedPage, error) {
	sr := SightRequest{
		Files: make([]SightRequestFile, len(filePaths), len(filePaths)),
	}
	for i, fp := range filePaths {
		if len(fp) < 4 {
			return nil, fmt.Errorf("failed to infer MIME type from file path: %v", fp)
		}
		switch fp[len(fp)-4 : len(fp)] {
		case ".bmp":
			sr.Files[i].MimeType = "image/bmp"
		case ".gif":
			sr.Files[i].MimeType = "image/gif"
		case ".pdf":
			sr.Files[i].MimeType = "application/pdf"
		case ".png":
			sr.Files[i].MimeType = "image/png"
		case ".jpg":
			sr.Files[i].MimeType = "image/jpg"
		default:
			if len(fp) >= 5 && fp[len(fp)-5:len(fp)] == ".jpeg" {
				sr.Files[i].MimeType = "image/jpeg"
			} else {
				return nil, fmt.Errorf("failed to infer MIME type from file path: %v", fp)
			}
		}
	}
	for i, fp := range filePaths {
		fileContents, err := ioutil.ReadFile(fp)
		if err != nil {
			return nil, err
		}
		sr.Files[i].Base64File = base64.StdEncoding.EncodeToString(fileContents)
	}
	buf, err := json.Marshal(&sr)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", "https://siftrics.com/api/sight/", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Basic %v", c.apiKey))
	var httpClient http.Client
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("Invalid API key; Received 401 Unauthorzied from initial HTTP request to the Sight API.\n")
	} else if resp.StatusCode != 200 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("Non-200 response from intial HTTP request to the Sight API. Status of inital HTTP response: %v. Furthermore, failed to read body of initial HTTP response.", resp.StatusCode)
		}
		return nil, fmt.Errorf("Non-200 response from intial HTTP request to the Sight API. Status of inital HTTP response: %v. Body of initial HTTP response:\n%v", resp.StatusCode, string(body))
	}
	var either struct {
		PollingURL     string
		RecognizedText []RecognizedText
	}
	if err := json.NewDecoder(resp.Body).Decode(&either); err != nil {
		return nil, fmt.Errorf("This should never happen and is not your fault: failed to decode body of initial HTTP request; error: %v", err)
	}

	pagesChan := make(chan RecognizedPage, 16)
	go func() {
		if either.PollingURL == "" {
			pagesChan <- RecognizedPage{
				Error:               "",
				FileIndex:           0,
				PageNumber:          1,
				NumberOfPagesInFile: 1,
				RecognizedText:      either.RecognizedText,
			}
			close(pagesChan)
			return
		}
		fileIndex2HaveSeenPage := make(map[int][]bool)
		errorCount := 0
		for {
			time.Sleep(time.Millisecond * 500)
			req, err := http.NewRequest("GET", either.PollingURL, nil)
			if err != nil {
				errorCount++
				if errorCount >= 5 {
					close(pagesChan)
					return
				}
				continue
			}
			req.Header.Set("Authorization", fmt.Sprintf("Basic %v", c.apiKey))
			var httpClient http.Client
			resp, err := httpClient.Do(req)
			if err != nil {
				errorCount++
				if errorCount >= 5 {
					close(pagesChan)
					return
				}
				continue
			}
			if resp.StatusCode == 401 {
				close(pagesChan)
				return
			} else if resp.StatusCode != 200 {
				if errorCount >= 5 {
					close(pagesChan)
					return
				}
				continue
			}
			var pages struct {
				Pages []RecognizedPage
			}
			if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
				errorCount++
				if errorCount >= 5 {
					close(pagesChan)
					return
				}
				continue
			}
			for _, p := range pages.Pages {
				haveSeenPage, ok := fileIndex2HaveSeenPage[p.FileIndex]
				if !ok || len(haveSeenPage) == 0 {
					fileIndex2HaveSeenPage[p.FileIndex] = make([]bool, p.NumberOfPagesInFile, p.NumberOfPagesInFile)
				}
				if p.PageNumber > 0 {
					fileIndex2HaveSeenPage[p.FileIndex][p.PageNumber-1] = true
				}
				pagesChan <- p
			}
			haveSeenEverything := true
			for fileIndex := 0; fileIndex < len(filePaths); fileIndex++ {
				haveSeenPage, ok := fileIndex2HaveSeenPage[fileIndex]
				if !ok {
					haveSeenEverything = false
					break
				}
				haveSeenAllPagesInThisFile := true
				for _, v := range haveSeenPage {
					if !v {
						haveSeenAllPagesInThisFile = false
						break
					}
				}
				if !haveSeenAllPagesInThisFile {
					haveSeenEverything = false
					break
				}
			}
			if haveSeenEverything {
				close(pagesChan)
				break
			}
		}
	}()
	return pagesChan, nil
}

func main() {
	containsHelp := false
	for _, s := range os.Args[1:] {
		if s == "-h" || s == "--help" {
			containsHelp = true
			break
		}
	}
	if len(os.Args) == 1 || containsHelp {
		fmt.Fprintf(os.Stderr, `usage: ./sight <--prompt-api-key|--api-key-file filename> <-o output filename> <image/document, ...>
examples:
example 1: ./sight -o recognized_text.json --prompt-api-key invoice.png receipt_1.pdf receipt_2.pdf
example 2: ./sight -o recognized_text.json --api-key-file my_api_key.txt invoice.pdf receipt.pdf
`)
		os.Exit(1)
	}

	promptApiKey := false
	apiKeyFile := ""
	outputFile := ""
	inputFiles := make([]string, 0)
	for i, s := range os.Args {
		if i == 0 {
			continue
		}
		switch s {
		case "--prompt-api-key":
			promptApiKey = true
		case "--api-key-file":
			if promptApiKey {
				fmt.Fprintf(os.Stderr, `error: Both --prompt-api-key and --api-key-file were specified.
This does not make sense, since each flag is used to pass in an API key but the program does not require two API keys.
Run ./sight -h for more help.
`)
				os.Exit(1)
			}
			if i+1 >= len(os.Args) {
				fmt.Fprintf(os.Stderr, `error: --api-key-file was specified but no filename came after it.
--api-key-file is supposed to be followed by the name of a file which contains an API key on a single line of text.
Run ./sight -h for more help.
`)
				os.Exit(1)
			}
			apiKeyFile = os.Args[i+1]
		case "-o":
			fallthrough
		case "--output":
			if i+1 >= len(os.Args) {
				fmt.Fprintf(os.Stderr, `error: -o (or --output) was specified but no filename came after it.
--output is supposed to be followed by the name of the file which will contain the recognized text from your images/documents.
Run ./sight -h for more help.
`)
				os.Exit(1)
			}
			if outputFile != "" {
				fmt.Fprintf(os.Stderr, `error: -o (or --output) was specified twice but it should only be specified once.
Run ./sight -h for more help.
`)
				os.Exit(1)
			}
			outputFile = os.Args[i+1]
		default:
			if !(os.Args[i-1] == "--api-key-file" || os.Args[i-1] == "-o" || os.Args[i-1] == "--output") {
				inputFiles = append(inputFiles, s)
			}
		}
	}
	if outputFile == "" {
		fmt.Fprintf(os.Stderr, `error: You must specify --output <filename> (you can use -o for shorthand).
Run ./sight -h for more help.
`)
		os.Exit(1)
	}
	if len(inputFiles) == 0 {
		fmt.Fprintf(os.Stderr, `error: You must specify documents or images in which to recognize text.
Run ./sight -h for more help.
`)
		os.Exit(1)
	}

	var client *Client
	var apiKeyBytes []byte
	var err error
	if promptApiKey {
		fmt.Print("enter your Sight API key: ")
		apiKeyBytes, err = terminal.ReadPassword(int(syscall.Stdin))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to read api key from stdin: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("")
	} else {
		if apiKeyFile == "" {
			fmt.Fprintf(os.Stderr, `error: You must specify either --prompt-api-key or --api-key-file <filename>.
Run ./sight -h for more help.
`)
			os.Exit(1)
		}
		apiKeyBytes, err = ioutil.ReadFile(apiKeyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
	apiKey := strings.TrimSpace(string(apiKeyBytes))
	if len(apiKey) != len("xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx") {
		fmt.Fprintf(os.Stderr, "error: the provided API key is not valid\nAPI keys should look like xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx\n")
		if apiKeyFile != "" {
			fmt.Fprintf(os.Stderr, "you specified to read the API key from the file %v\n", apiKeyFile)
		}
		fmt.Fprintf(os.Stderr, "run ./sight --help to see how to provide an API key\n")
		os.Exit(1)
	}
	client = NewClient(apiKey)
	of, err := os.Create(outputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Recognizing text...")

	pagesChan, err := client.Recognize(inputFiles...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(of, `{"Pages":[`)
	fileIndex2HaveSeenPage := make(map[int][]bool)
	numFilesComplete := 0
	isFirstPage := true
	for {
		page, isOpen := <-pagesChan
		if !isOpen {
			break
		}
		if !isFirstPage {
			fmt.Fprintf(of, ",")
		} else {
			isFirstPage = false
		}
		jsonBytes, err := json.Marshal(page)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nerror: failed to serialize JSON: %v\n", err)
			os.Exit(1)
		}
		of.Write(jsonBytes)

		_, ok := fileIndex2HaveSeenPage[page.FileIndex]
		if !ok {
			fileIndex2HaveSeenPage[page.FileIndex] = make([]bool, page.NumberOfPagesInFile, page.NumberOfPagesInFile)
		}
		if page.PageNumber > 0 {
			fileIndex2HaveSeenPage[page.FileIndex][page.PageNumber-1] = true
		}
		seenAllPages := true
		for _, b := range fileIndex2HaveSeenPage[page.FileIndex] {
			if !b {
				seenAllPages = false
				break
			}
		}
		if seenAllPages {
			numFilesComplete++
			fmt.Printf("%v out of %v input files are complete\n", numFilesComplete, len(inputFiles))
		}
	}
	fmt.Fprintf(of, "]}")
}
