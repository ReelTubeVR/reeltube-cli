package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	debug      bool // Debug mode to log requests and responses
}

func NewClient(baseURL, apiKey string, debug ...bool) *Client {
	dbg := false
	if len(debug) > 0 {
		dbg = debug[0]
	}
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{},
		debug:      dbg,
	}
}

type APIResponse struct {
	Data   interface{}
	Error  string
	Status int // HTTP status code
}

//
// HTTP Methods
//

func (c *Client) DoRequest(method, path string, params interface{}, target interface{}) (*APIResponse, error) {
	fullURL, _ := url.JoinPath(c.baseURL, path)
	var body io.Reader

	if params != nil {
		jsonData, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		body = bytes.NewBuffer(jsonData)
		if c.debug {
			log.Printf("Request Body: %s\n", jsonData)
		}
	}

	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Content-Type", "application/json")

	if c.debug {
		log.Printf("Request Method: %s, URL: %s\n", req.Method, req.URL)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if c.debug {
		log.Printf("Response Status: %s\n", resp.Status)
	}

	apiResponse := &APIResponse{
		Status: resp.StatusCode,
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Parse error message assuming API errors are JSON
		if err := json.NewDecoder(resp.Body).Decode(&apiResponse.Error); err != nil {
			return nil, fmt.Errorf("API error: %s", err)
		}
		return apiResponse, nil
	}

	var respBodyBytes []byte
	respBodyBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if c.debug {
		var respData map[string]interface{}
		if err := json.Unmarshal(respBodyBytes, &respData); err != nil {
			return nil, err
		}
		jsonString, _ := json.Marshal(respData)
		log.Printf("Response Data: %+v\n", string(jsonString))
	}

	if err := json.Unmarshal(respBodyBytes, target); err != nil {
		return nil, err
	}

	apiResponse.Data = target
	return apiResponse, nil

}

func (c *Client) Get(path string, params, target interface{}) (*APIResponse, error) {
	return c.DoRequest("GET", path, params, target)
}

func (c *Client) Post(path string, params, target interface{}) (*APIResponse, error) {
	return c.DoRequest("POST", path, params, target)
}

func (c *Client) Put(path string, params, target interface{}) (*APIResponse, error) {
	return c.DoRequest("PUT", path, params, target)
}

func (c *Client) Delete(path string, params, target interface{}) (*APIResponse, error) {
	return c.DoRequest("DELETE", path, params, target)
}

//
// Domain Methods
//

type MeResponse struct {
	Profile struct {
		ID         string  `json:"id"`
		Handle     string  `json:"handle"`
		Bio        *string `json:"bio"`
		ImagePath  *string `json:"image_path"`
		IsBusiness bool    `json:"is_business"`
		IsVerified bool    `json:"is_verified"`
	} `json:"profile"`
}

func (c *Client) Me() (*MeResponse, error) {
	// var data map[string]interface{}
	var data MeResponse
	_, err := c.Get("/api/v0/me", nil, &data)
	if err != nil {
		return nil, err
	}
	return &data, nil
}

type MediaUpload struct {
	ID string `json:"id"`
}

type CreateMediaUploadResponse struct {
	UploadID      string      `json:"upload_id"`
	PartSize      int         `json:"part_size"`
	NumParts      int         `json:"num_parts"`
	PresignedURLs []string    `json:"presigned_urls"`
	MediaUpload   MediaUpload `json:"media_upload"`
}

func (c *Client) CreateMediaUpload(fileName string, fileSize int) (*CreateMediaUploadResponse, error) {
	var data CreateMediaUploadResponse
	body := map[string]interface{}{
		"filename": fileName,
		"size":     fileSize,
	}
	_, err := c.Post("/api/v0/media_uploads", body, &data)
	if err != nil {
		return nil, err
	}
	return &data, nil
}

type CompleteUploadRequest struct {
	MediaUploadID string `json:"id"`
	UploadID      string `json:"upload_id"`
	Parts         []Part `json:"parts"`
}

type CompleteUploadResponse struct {
	MediaUpload MediaUpload `json:"media_upload"`
}

func (c *Client) CompleteMultipartUpload(mediaUploadID, uploadID string, parts []Part) error {
	var data CreateMediaUploadResponse
	body := CompleteUploadRequest{
		MediaUploadID: mediaUploadID,
		UploadID:      uploadID,
		Parts:         parts,
	}
	_, err := c.Post(fmt.Sprintf("/api/v0/media_uploads/%s/complete", mediaUploadID), body, &data)
	if err != nil {
		return err
	}
	return nil
}
