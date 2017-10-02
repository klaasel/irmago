package irmago

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

// HTTPTransport sends and receives JSON messages to a HTTP server.
type HTTPTransport struct {
	Server  string
	client  *http.Client
	headers map[string]string
}

const verbose = false

// NewHTTPTransport returns a new HTTPTransport.
func NewHTTPTransport(serverURL string) *HTTPTransport {
	url := serverURL
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	return &HTTPTransport{
		Server:  url,
		headers: map[string]string{},
		client: &http.Client{
			Timeout: time.Second * 5,
		},
	}
}

// SetHeader sets a header to be sent in requests.
func (transport *HTTPTransport) SetHeader(name, val string) {
	transport.headers[name] = val
}

func (transport *HTTPTransport) request(url string, method string, result interface{}, object interface{}) error {
	if method != http.MethodPost && method != http.MethodGet && method != http.MethodDelete {
		panic("Unsupported HTTP method " + method)
	}
	if method == http.MethodGet && object != nil {
		panic("Cannot GET and also post an object")
	}

	var isstr bool
	var reader io.Reader
	if object != nil {
		var objstr string
		if objstr, isstr = object.(string); isstr {
			reader = bytes.NewBuffer([]byte(objstr))
		} else {
			marshaled, err := json.Marshal(object)
			if err != nil {
				return &SessionError{ErrorType: ErrorSerialization, Err: err}
			}
			if verbose {
				fmt.Printf("%s %s: %s\n", method, url, string(marshaled))
			}
			reader = bytes.NewBuffer(marshaled)
		}
	} else {
		if verbose {
			fmt.Printf("%s %s\n", method, url)
		}
	}

	req, err := http.NewRequest(method, transport.Server+url, reader)
	if err != nil {
		return &SessionError{ErrorType: ErrorTransport, Err: err}
	}

	req.Header.Set("User-Agent", "irmago")
	if object != nil {
		if isstr {
			req.Header.Set("Content-Type", "text/plain; charset=UTF-8")
		} else {
			req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		}
	}
	for name, val := range transport.headers {
		req.Header.Set(name, val)
	}

	res, err := transport.client.Do(req)
	if err != nil {
		return &SessionError{ErrorType: ErrorTransport, Err: err}
	}

	if method == http.MethodDelete {
		return nil
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return &SessionError{ErrorType: ErrorServerResponse, Err: err, Status: res.StatusCode}
	}
	if res.StatusCode != 200 {
		apierr := &ApiError{}
		json.Unmarshal(body, apierr)
		if apierr.ErrorName == "" { // Not an ApiErrorMessage
			return &SessionError{ErrorType: ErrorServerResponse, Status: res.StatusCode}
		}
		if verbose {
			fmt.Printf("ERROR: %+v\n", apierr)
		}
		return &SessionError{ErrorType: ErrorApi, Status: res.StatusCode, ApiError: apierr}
	}

	if verbose {
		fmt.Printf("RESPONSE: %s\n", string(body))
	}
	if _, resultstr := result.(*string); resultstr {
		*result.(*string) = string(body)
	} else {
		err = json.Unmarshal(body, result)
		if err != nil {
			return &SessionError{ErrorType: ErrorServerResponse, Err: err, Status: res.StatusCode}
		}
	}

	return nil
}

// Post sends the object to the server and parses its response into result.
func (transport *HTTPTransport) Post(url string, result interface{}, object interface{}) error {
	return transport.request(url, http.MethodPost, result, object)
}

// Get performs a GET request and parses the server's response into result.
func (transport *HTTPTransport) Get(url string, result interface{}) error {
	return transport.request(url, http.MethodGet, result, nil)
}

// Delete performs a DELETE.
func (transport *HTTPTransport) Delete() {
	transport.request("", http.MethodDelete, nil, nil)
}
