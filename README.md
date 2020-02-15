# Sight API Go Client and Command-Line Tool

This repository contains

- a command-line tool to recognize text in documents
- the official Go client for the Sight API

## Command-line Quickstart

### Installation

Download the latest executable from [the releases page](https://github.com/siftrics/sight/releases).

### Usage

```
./sight receipt_1.pdf receipt_2.pdf --output recognized_text.json --prompt-api-key
```

You must specify an output file with `-o` or `--output`.

You must specify your API key with `--prompt-api-key` or `--api-key-file <filename>`. The latter flag expects a text file containing your API key on a single line.

### Getting an API Key

Go to [https://siftrics.com/](https://siftrics.com/), sign up for an account, then go to the [Sight dashboard](https://siftrics.com/sight.html) and create an API key.

## Go Client Quickstart

Import this repository:

```
import "github.com/siftrics/sight"
```

Create a client (it is up to you to set up the variable `apiKey`):

```
c := sight.NewClient(apiKey)
```

Recognize text in files:

```
pagesChan, err := client.Recognize("file1.png", "file2.jpeg", "file3.pdf")
if err != nil {
    ...
}
for {
    page, isOpen := <- pagesChan
    if !isOpen {
        break
    }
    if page.Error != "" {
        ...
    }
    ...
}
```

The `Recognize` function accepts a variable number of strings as input:

```
func (c *Client) Recognize(filePaths ...string) (<-chan RecognizedPage, error)
```

The pages from `pagesChan` are this type:

```
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
```

## Cost and Capabilities

The cost of the service is $0.50 per 1,000 pages, which is one third the price of Google Cloud Vision and Amazon Textract.

The accuracy and capability of the Sight API is comparable to Google Cloud Vision. It can handle human handwriting.

## Building from Source

```
go get -u github.com/siftrics/sight
```

This will place the executable command-line tool `sight` in your `$GOBIN` directory.

If that fails (due to environment variables, go tooling, etc.), you can try

```
$ git clone https://github.com/siftrics/sight
$ cd sight
$ go build
```

Now the `sight` executable should be in your current working directory.
