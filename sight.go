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

package sight

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

// Config is used to consolidate the parameters to the function
// func (c *Client) RecognizeCfg. As the Sight API becomes more configurable,
// the number of parameters will grow unwieldy. This allows RecognizeCfg
// interface to remain readable (few parameters) and unchanged over time.
type Config struct {
	MakeSentences bool
	DoExifRotate  bool
	DoAutoRotate  bool
	DoAsync       bool
}

type SightRequest struct {
	Files         []SightRequestFile
	MakeSentences bool
	DoExifRotate  bool
	DoAutoRotate  bool
	DoAsync       bool
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
	Base64Image         string `json:",omitempty"`
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

// Recognize is shorthand for calling RecognizeCfg with all the default config values.
func (c *Client) Recognize(filePaths ...string) (<-chan RecognizedPage, error) {
	return c.RecognizeCfg(
		Config{
			MakeSentences: true,
			DoExifRotate:  false,
			DoAutoRotate:  false,
			DoAsync:       false,
		},
		filePaths...,
	)
}

// RecognizeWords is shorthand for calling RecognizeCfg with all default config values,
// except MakeSentences is disabled, so word-level bounding boxes are returned.
func (c *Client) RecognizeWords(filePaths ...string) (<-chan RecognizedPage, error) {
	return c.RecognizeCfg(
		Config{
			MakeSentences: false,
			DoExifRotate:  false,
			DoAutoRotate:  false,
			DoAsync:       false,
		},
		filePaths...,
	)
}

// RecognizeCfg uses the Sight API to recognize all the text in the given files.
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
func (c *Client) RecognizeCfg(cfg Config, filePaths ...string) (<-chan RecognizedPage, error) {
	sr := SightRequest{
		Files:         make([]SightRequestFile, len(filePaths), len(filePaths)),
		MakeSentences: cfg.MakeSentences,
		DoExifRotate:  cfg.DoExifRotate,
		DoAutoRotate:  cfg.DoAutoRotate,
		DoAsync:       cfg.DoAsync,
	}
	for i, fp := range filePaths {
		if len(fp) < 4 {
			return nil, fmt.Errorf("failed to infer MIME type from file path: %v", fp)
		}
		switch strings.ToLower(fp[len(fp)-4 : len(fp)]) {
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
			if len(fp) >= 5 && strings.ToLower(fp[len(fp)-5:len(fp)]) == ".jpeg" {
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
		Base64Image    string
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
				Base64Image:         either.Base64Image,
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
