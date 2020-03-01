This repository contains

- A command-line tool to recognize text in documents.
- The official Go client for the Sight API. [GoDoc here](https://godoc.org/github.com/siftrics/sight).

## [Command-line Quickstart](#command-line-quickstart)

Download the latest executable from [the releases page](https://github.com/siftrics/sight/releases).

### Usage

```
./sight receipt_1.jpg receipt_2.pdf -o recognized_text.json --prompt-api-key
```

You must specify an output file with `-o` or `--output`.

You must specify your API key with `--prompt-api-key` or `--api-key-file <filename>`. The latter flag expects a text file containing your API key on a single line.

Run `./sight` with no flags or arguments to display the full usage section and list all optional flags.

_Mac and Linux users may need to run `chmod u+x sight` on the downloaded executable before it can be executed._

### Getting an API Key

Go to [https://siftrics.com/](https://siftrics.com/), sign up for an account, then go to the [Sight dashboard](https://siftrics.com/sight.html) and create an API key.

## [Go Client Quickstart](#go-client-quickstart)

Here's the [GoDoc page](https://godoc.org/github.com/siftrics/sight).

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

### Word-Level Bounding Boxes

The function `(c *Client) RecognizeWords` has the same signature has `Recognize`, but it returns word-level bounding boxes instead of sentence-level bounding boxes.

### Auto-Rotate

The Sight API can rotate and return input images so the majority of the recognized text is upright. To enable this behavior, call the `RecognizeCfg` function with `DoAutoRotate` set to `true`:

```
pagesChan, err := c.RecognizeCfg(
    sight.Config{
        DoAutoRotate: true,
        MakeSentences: true,
    },
    "invoice1.jpg",
    "invoice2.jpg",
)
```

Now, the `Base64Image` field will be set in the `page` objects you receive from `pagesChan`.

A common desire is to decode the images and write them to disk. Here's a snippet of code that does that:

```
f, err := os.Create("auto-rotated.png")
if err != nil {
    ...
}
defer f.Close()
if _, err := io.Copy(f, base64.NewDecoder(base64.StdEncoding, strings.NewReader(page.Base64Image))); err != nil {
    ...
}
```

### Why are the bounding boxes are rotated 90 degrees?

Some images, particularly .jpeg images, use the [EXIF](https://en.wikipedia.org/wiki/Exif) data format. This data format contains a metadata field indicating the orientation of an image --- i.e., whether the image should be rotated 90 degrees, 180 degrees, flipped horizontally, etc., when viewing it in an image viewer.

This means that when you view such an image in Chrome, Firefox, Safari, or the stock Windows and Mac image viewer applications, it will appear upright, despite the fact that the underlying pixels of the image are encoded in a different orientation.

If you find your bounding boxes are rotated or flipped relative to your image, it is because the image decoder you are using to load images in your program obeys EXIF orientation, but the Sight API ignores it (or vice versa).

All the most popular imaging libraries in Go, such as "image" and "github.com/disintegration/imaging", ignore EXIF orientation. You should determine whether your image decoder obeys EXIF orientation and tell the Sight API to do the same thing. You can tell the Sight API to obey the EXIF orientation by calling the `RecognizeCfg` function with `DoExifRotate` set to `true`:

```
pagesChan, err := c.RecognizeCfg(
    sight.Config{
        DoExifRotate: true,
        MakeSentences: true,
    },
    "invoice1.jpg",
    "invoice2.jpg",
)
```

By default, the Sight API ignores EXIF orientation.

## Cost and Capabilities

The cost of the service is $0.50 per 1,000 pages, which is one third the price of Google Cloud Vision and Amazon Textract.

The accuracy and capability of the Sight API is comparable to Google Cloud Vision. It can handle human handwriting.

## Building from Source

```
go get -u github.com/siftrics/sight/...
```

This will place the executable command-line tool `sight` in your `$GOBIN` directory.

If that fails (due to environment variables, go tooling, etc.), you can try

```
$ git clone https://github.com/siftrics/sight
$ cd sight/cli
$ go build -o sight main.go
```

Now the `sight` executable should be in your current working directory.
