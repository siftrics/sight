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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/siftrics/sight"
)

func main() {
	containsHelp := false
	for _, s := range os.Args[1:] {
		if s == "-h" || s == "--help" {
			containsHelp = true
			break
		}
	}
	if len(os.Args) == 1 || containsHelp {
		fmt.Fprintf(os.Stderr, `usage: ./sight <--prompt-api-key|--api-key-file filename> <-o output filename> [-w|--words] <image/document, ...>
examples:
example 1: ./sight -o recognized_text.json --prompt-api-key invoice.png receipt_1.pdf receipt_2.pdf
example 2: ./sight -o recognized_text.json --api-key-file my_api_key.txt invoice.pdf receipt.pdf
`)
		os.Exit(1)
	}

	makeSentences := true
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
		case "-w":
			fallthrough
		case "--words":
			makeSentences = false
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

	var client *sight.Client
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
	client = sight.NewClient(apiKey)
	of, err := os.Create(outputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Recognizing text...")

	var pagesChan <-chan sight.RecognizedPage
	if makeSentences {
		pagesChan, err = client.Recognize(inputFiles...)
	} else {
		pagesChan, err = client.RecognizeWords(inputFiles...)
	}
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
