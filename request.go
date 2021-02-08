package httpc

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/brotli/go/cbrotli"
)

type Request struct {
	httpc    *HttpClient
	request  *http.Request
	response *http.Response
	method   string
	url      string
	header   map[string]string
	cookies  *[]*http.Cookie
	data     url.Values
	jsonData string
	fileData map[bool]map[string]string
	verbose  bool
	err      error
}

func NewRequest(client *HttpClient) *Request {
	return &Request{
		httpc:    client,
		method:   "GET",
		header:   make(map[string]string),
		cookies:  new([]*http.Cookie),
		data:     url.Values{},
		fileData: make(map[bool]map[string]string),
	}
}

func (this *Request) SetMethod(name string) *Request {
	this.method = strings.ToUpper(name)
	return this
}

func (this *Request) SetUrl(url string) *Request {
	this.url = url
	return this
}

func (this *Request) SetHeader(name, value string) *Request {
	this.header[name] = value
	return this
}

func (this *Request) SetCookies(cookies *[]*http.Cookie) *Request {
	this.cookies = cookies
	return this
}

func (this *Request) SetVerbose(d bool) *Request {
	this.verbose = d
	return this
}

func (this *Request) SetData(name, value string) *Request {
	this.data.Set(name, value)
	return this
}

func (this *Request) SetJsonData(s string) *Request {
	this.jsonData = s
	return this
}

func (this *Request) SetFileData(name, value string, isFile bool) *Request {
	this.fileData[isFile] = map[string]string{name: value}
	return this
}

func (this *Request) Send(a ...interface{}) *Request {
	var err error

	if len(a) == 0 || a[0] == "url" {
		this.request, err = http.NewRequest(this.method, this.url, strings.NewReader(this.data.Encode()))
		defer this.log("url")
		if err != nil {
			this.err = err
			return this
		}

		if this.method == "POST" {
			if len(this.request.Header.Get("Content-Type")) == 0 {
				this.request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
			}
		}
	} else if a[0] == "json" {
		this.request, err = http.NewRequest(this.method, this.url, strings.NewReader(this.jsonData))
		defer this.log("json")
		if err != nil {
			this.err = err
			return this
		}
	} else {
		bodyBuf := &bytes.Buffer{}
		bodyWriter := multipart.NewWriter(bodyBuf)
		for h, m := range this.fileData {
			for k, v := range m {
				if h {
					fd, err := os.Open(v)
					if err != nil {
						this.err = err
						return this
					}
					fileWriter, _ := bodyWriter.CreateFormFile(k, filepath.Base(v))
					_, _ = io.Copy(fileWriter, fd)
					fd.Close()
				} else {
					_ = bodyWriter.WriteField(k, v)
				}
			}
		}

		contentType := bodyWriter.FormDataContentType()
		_ = bodyWriter.Close()
		this.request, err = http.NewRequest(this.method, this.url, ioutil.NopCloser(bodyBuf))
		defer this.log("file")
		if err != nil {
			this.err = err
			return this
		}

		this.request.Header.Set("Content-Type", contentType)
	}
	for k, v := range this.header {
		this.request.Header.Set(k, v)
	}

	for _, v := range *this.cookies {
		s := fmt.Sprintf("%s=%s", v.Name, v.Value)
		if c := this.request.Header.Get("Cookie"); c != "" {
			this.request.Header.Set("Cookie", c+"; "+s)
		} else {
			this.request.Header.Set("Cookie", s)
		}
	}

	this.response, err = this.httpc.client.Do(this.request)
	if err != nil {
		this.err = err
		return this
	}

	return this
}

func (this *Request) log(t string) {
	if this.verbose == true {
		fmt.Printf("-------------------------------------------------------------------\n")
		fmt.Printf("Request: %s %s\nHeader: %v\nCookies: %v\n", this.method, this.url, this.request.Header, this.request.Cookies())
		if t == "url" {
			fmt.Printf("Body: %v\n", this.data)
		} else if t == "json" {
			fmt.Printf("Body: %v\n", this.jsonData)
		} else {
			fmt.Printf("Body: %v\n", this.fileData)
		}
		fmt.Printf("-------------------------------------------------------------------\n")
	}
}

func (this *Request) End() (*http.Response, string, error) {
	rsp, buf, err := this.EndBytes()
	if err != nil {
		return rsp, "", err
	}
	return rsp, string(buf), nil
}

func (this *Request) EndBytes() (*http.Response, []byte, error) {
	var buf []byte
	var err error
	if this.err != nil {
		return nil, []byte(""), errors.New(this.err.Error())
	}

	if this.response.StatusCode != http.StatusOK {
		return this.response, nil, errors.New(this.response.Status)
	}
	defer this.response.Body.Close()
	switch this.response.Header.Get("Content-Encoding") {
	case "gzip":
		r, err := gzip.NewReader(this.response.Body)
		if err != nil {
			break
		}
		defer r.Close()
		buf, err = ioutil.ReadAll(r)
	case "br":
		r := cbrotli.NewReader(this.response.Body)
		defer r.Close()
		buf, err = ioutil.ReadAll(r)
	default:
		buf, err = ioutil.ReadAll(this.response.Body)
	}
	if err != nil {
		return this.response, nil, err
	}
	return this.response, buf, nil
}

func (this *Request) EndFile(savePath, saveFileName string) (*http.Response, error) {
	if this.err != nil {
		return nil, errors.New(this.err.Error())
	}

	if this.response.StatusCode != http.StatusOK {
		return nil, errors.New("Not written")
	}

	if saveFileName == "" {
		path := strings.Split(this.request.URL.String(), "/")
		if len(path) > 1 {
			saveFileName = path[len(path)-1]
		}
	}

	bodyByte, _ := ioutil.ReadAll(this.response.Body)
	_ = this.response.Body.Close()
	err := ioutil.WriteFile(savePath+saveFileName, bodyByte, 0777)
	if err != nil {
		return nil, errors.New(err.Error())
	}

	return this.response, nil
}
